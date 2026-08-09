package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infowall/infowall/internal/model"
)

const ingestionStateColumns = `enabled, timezone, active_start, active_end,
	interval_minutes, overlap_minutes, excluded_chat_ids, last_success_end,
	last_backfill_at, next_run_at, status, last_error, current_run_id,
	lease_until, requested, updated_at`

const ingestionRunColumns = `id, trigger, status, window_start, window_end,
	messages_seen, messages_candidate, created, updated, skipped, review_count,
	missing_context_count, input_tokens, cached_input_tokens, output_tokens,
	started_at, finished_at, error`

func (s *Store) GetFeishuIngestionState(ctx context.Context) (*model.FeishuIngestionState, error) {
	return scanIngestionState(s.db.QueryRowContext(ctx,
		`SELECT `+ingestionStateColumns+` FROM feishu_ingestion_state WHERE id = 1`).Scan)
}

func scanIngestionState(scan func(...any) error) (*model.FeishuIngestionState, error) {
	var state model.FeishuIngestionState
	var enabled, requested int
	var exclusions string
	var successRaw, backfillRaw, nextRaw, leaseRaw, updatedRaw any
	if err := scan(&enabled, &state.Timezone, &state.ActiveStart, &state.ActiveEnd,
		&state.IntervalMinutes, &state.OverlapMinutes, &exclusions, &successRaw,
		&backfillRaw, &nextRaw, &state.Status, &state.LastError, &state.CurrentRunID,
		&leaseRaw, &requested, &updatedRaw); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	state.Enabled = enabled != 0
	state.Requested = requested != 0
	if err := json.Unmarshal([]byte(exclusions), &state.ExcludedChatIDs); err != nil {
		return nil, fmt.Errorf("decode Feishu ingestion exclusions: %w", err)
	}
	if state.ExcludedChatIDs == nil {
		state.ExcludedChatIDs = []string{}
	}
	state.LastSuccessEnd = optionalSQLiteTime(successRaw)
	state.LastBackfillAt = optionalSQLiteTime(backfillRaw)
	state.NextRunAt = optionalSQLiteTime(nextRaw)
	state.LeaseUntil = optionalSQLiteTime(leaseRaw)
	state.UpdatedAt = parseSQLiteTime(updatedRaw)
	return &state, nil
}

func optionalSQLiteTime(raw any) *time.Time {
	if value := parseSQLiteTime(raw); !value.IsZero() {
		return &value
	}
	return nil
}

func (s *Store) ConfigureFeishuIngestion(ctx context.Context, state model.FeishuIngestionState, resumeFrom *time.Time) (*model.FeishuIngestionState, error) {
	if state.Timezone == "" {
		state.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(state.Timezone); err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", state.Timezone, err)
	}
	if !validClock(state.ActiveStart) || !validClock(state.ActiveEnd) {
		return nil, errors.New("active_start and active_end must use HH:MM")
	}
	if state.IntervalMinutes < 5 || state.IntervalMinutes > 1440 {
		return nil, errors.New("interval_minutes must be between 5 and 1440")
	}
	if state.OverlapMinutes < 0 || state.OverlapMinutes >= state.IntervalMinutes {
		return nil, errors.New("overlap_minutes must be non-negative and less than interval_minutes")
	}
	exclusions := normalizeStrings(state.ExcludedChatIDs)
	raw, err := json.Marshal(exclusions)
	if err != nil {
		return nil, err
	}
	status := "disabled"
	if state.Enabled {
		status = "idle"
	}
	now := time.Now().UTC()
	var resumeAt, backfillAt any
	if resumeFrom != nil {
		resumeAt = resumeFrom.UTC()
		backfillAt = now
		var existingRuns int
		var existingSuccess any
		if err := s.db.QueryRowContext(ctx, `SELECT
			(SELECT count(*) FROM feishu_ingestion_runs), last_success_end
			FROM feishu_ingestion_state WHERE id = 1`).Scan(&existingRuns, &existingSuccess); err != nil {
			return nil, err
		}
		if existingRuns != 0 || optionalSQLiteTime(existingSuccess) != nil {
			return nil, errors.New("resume_from is only allowed before the first ingestion run")
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE feishu_ingestion_state SET
		enabled = ?, timezone = ?, active_start = ?, active_end = ?,
		interval_minutes = ?, overlap_minutes = ?, excluded_chat_ids = ?,
		last_success_end = COALESCE(?, last_success_end),
		last_backfill_at = COALESCE(?, last_backfill_at),
		status = ?, last_error = '', requested = CASE WHEN ? = 1 THEN requested ELSE 0 END,
		next_run_at = CASE WHEN ? = 1 THEN next_run_at ELSE NULL END, updated_at = ?
		WHERE id = 1`, boolInt(state.Enabled), state.Timezone, state.ActiveStart,
		state.ActiveEnd, state.IntervalMinutes, state.OverlapMinutes, string(raw), resumeAt, backfillAt, status,
		boolInt(state.Enabled), boolInt(state.Enabled), now)
	if err != nil {
		return nil, fmt.Errorf("configure Feishu ingestion: %w", err)
	}
	return s.GetFeishuIngestionState(ctx)
}

func validClock(value string) bool {
	_, err := time.Parse("15:04", value)
	return err == nil
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *Store) RequestFeishuIngestion(ctx context.Context) (*model.FeishuIngestionState, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE feishu_ingestion_state
		SET requested = 1, status = CASE WHEN status = 'running' THEN status ELSE 'pending' END,
		last_error = '', next_run_at = ?, updated_at = ? WHERE id = 1 AND enabled = 1`, now, now)
	if err != nil {
		return nil, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return nil, errors.New("Feishu chat ingestion is disabled")
	}
	return s.GetFeishuIngestionState(ctx)
}

func (s *Store) SetFeishuIngestionNextRun(ctx context.Context, next time.Time) error {
	var value any
	if !next.IsZero() {
		value = next.UTC()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE feishu_ingestion_state
		SET next_run_at = ?, updated_at = ? WHERE id = 1`, value, time.Now().UTC())
	return err
}

func (s *Store) StartFeishuIngestionRun(ctx context.Context, trigger string, start, end time.Time, lease time.Duration) (*model.FeishuIngestionRun, error) {
	if !start.Before(end) {
		return nil, errors.New("ingestion window start must be before end")
	}
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	now := time.Now().UTC()
	run := &model.FeishuIngestionRun{ID: uuid.NewString(), Trigger: trigger, Status: "running", WindowStart: start.UTC(), WindowEnd: end.UTC(), StartedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var enabled int
	var current string
	var leaseRaw any
	if err := tx.QueryRowContext(ctx, `SELECT enabled, current_run_id, lease_until
		FROM feishu_ingestion_state WHERE id = 1`).Scan(&enabled, &current, &leaseRaw); err != nil {
		return nil, err
	}
	leaseUntil := parseSQLiteTime(leaseRaw)
	if enabled == 0 {
		return nil, errors.New("Feishu chat ingestion is disabled")
	}
	if current != "" && leaseUntil.After(now) {
		return nil, fmt.Errorf("%w: ingestion run %s holds lease until %s", ErrConflict, current, leaseUntil.Format(time.RFC3339))
	}
	if current != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE feishu_ingestion_runs SET status = 'error',
			error = 'run lease expired after service interruption', finished_at = ?
			WHERE id = ? AND status = 'running'`, now, current); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO feishu_ingestion_runs
		(id, trigger, status, window_start, window_end, started_at) VALUES (?, ?, 'running', ?, ?, ?)`,
		run.ID, run.Trigger, run.WindowStart, run.WindowEnd, run.StartedAt); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE feishu_ingestion_state SET status = 'running',
		last_error = '', current_run_id = ?, lease_until = ?, requested = 0, updated_at = ? WHERE id = 1`,
		run.ID, now.Add(lease), now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Store) FailFeishuIngestionRun(ctx context.Context, runID, message string, seen, candidates int, inputTokens, cachedInputTokens, outputTokens int64) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE feishu_ingestion_runs SET status = 'error',
		messages_seen = ?, messages_candidate = ?, input_tokens = ?, cached_input_tokens = ?, output_tokens = ?,
		error = ?, finished_at = ? WHERE id = ?`, seen, candidates, inputTokens, cachedInputTokens, outputTokens,
		truncateError(message), now, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE feishu_ingestion_state SET
		status = CASE WHEN enabled = 1 THEN 'error' ELSE 'disabled' END,
		last_error = ?, current_run_id = '', lease_until = NULL, updated_at = ?
		WHERE id = 1 AND current_run_id = ?`, truncateError(message), now, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RenewFeishuIngestionLease(ctx context.Context, runID string, lease time.Duration) error {
	if lease <= 0 {
		lease = 20 * time.Minute
	}
	result, err := s.db.ExecContext(ctx, `UPDATE feishu_ingestion_state SET lease_until = ?, updated_at = ?
		WHERE id = 1 AND enabled = 1 AND current_run_id = ?`, time.Now().UTC().Add(lease), time.Now().UTC(), runID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("%w: ingestion run %s no longer holds the lease", ErrConflict, runID)
	}
	return nil
}

func (s *Store) FilterNewFeishuMessageIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	result := make(map[string]bool, len(ids))
	for _, batch := range chunkStrings(normalizeStrings(ids), 400) {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i := range batch {
			args[i] = batch[i]
			result[batch[i]] = true
		}
		rows, err := s.db.QueryContext(ctx, `SELECT message_id FROM feishu_ingestion_seen WHERE message_id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			result[id] = false
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func chunkStrings(values []string, size int) [][]string {
	if size <= 0 {
		size = len(values)
	}
	result := make([][]string, 0, (len(values)+size-1)/size)
	for len(values) > 0 {
		end := min(size, len(values))
		result = append(result, values[:end])
		values = values[end:]
	}
	return result
}

func truncateError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4096 {
		return value[:4096]
	}
	return value
}

func (s *Store) CompleteFeishuIngestion(ctx context.Context, commit model.FeishuIngestionCommit) (*model.FeishuIngestionRun, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM feishu_ingestion_runs WHERE id = ?`, commit.RunID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != "running" {
		return nil, fmt.Errorf("%w: ingestion run is %s", ErrConflict, status)
	}
	created, updated, skipped, reviews := 0, 0, commit.Skipped, 0
	mutated := false
	for index, raw := range commit.NewDemands {
		if raw == nil {
			return nil, fmt.Errorf("new demand %d is null", index)
		}
		demand := cloneDemand(raw)
		demand.Status = model.DemandStatusPending
		demand.Priority = model.DemandPriorityNone
		demand.ProjectID = nil
		existingID, findErr := findImportedDemandTx(ctx, tx, demand)
		if findErr != nil {
			return nil, findErr
		}
		if existingID != "" {
			skipped++
			continue
		}
		if err := normalizeNewDemand(demand, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("new demand %d: %w", index, err)
		}
		if err := insertDemandTx(ctx, tx, demand); err != nil {
			if errors.Is(err, ErrConflict) {
				skipped++
				continue
			}
			return nil, err
		}
		created++
		mutated = true
	}
	for _, update := range commit.ProgressUpdates {
		action, review, applyErr := applyAutomaticProgressTx(ctx, tx, update)
		if applyErr != nil {
			return nil, applyErr
		}
		switch action {
		case "updated":
			updated++
			mutated = true
		case "review":
			if review != nil {
				inserted, insertErr := insertDemandReviewTx(ctx, tx, review)
				if insertErr != nil {
					return nil, insertErr
				}
				if inserted {
					reviews++
				}
			}
		case "skipped":
			skipped++
		}
	}
	for i := range commit.Reviews {
		inserted, insertErr := insertDemandReviewTx(ctx, tx, &commit.Reviews[i])
		if insertErr != nil {
			return nil, insertErr
		}
		if inserted {
			reviews++
		} else {
			skipped++
		}
	}
	if mutated {
		if err := markFeishuDirtyTx(ctx, tx, time.Now().UTC()); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	for _, id := range normalizeStrings(commit.ProcessedMessageIDs) {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO feishu_ingestion_seen
			(message_id, seen_at) VALUES (?, ?)`, id, now); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE feishu_ingestion_runs SET status = 'success',
		messages_seen = ?, messages_candidate = ?, created = ?, updated = ?, skipped = ?,
		review_count = ?, missing_context_count = ?, input_tokens = ?, cached_input_tokens = ?,
		output_tokens = ?, finished_at = ?, error = '' WHERE id = ?`, commit.MessagesSeen,
		commit.MessagesCandidate, created, updated, skipped, reviews, commit.MissingContextCount,
		commit.InputTokens, commit.CachedInputTokens, commit.OutputTokens, now, commit.RunID); err != nil {
		return nil, err
	}
	var backfill any
	if commit.BackfillAt != nil {
		backfill = commit.BackfillAt.UTC()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE feishu_ingestion_state SET
		last_success_end = ?, last_backfill_at = COALESCE(?, last_backfill_at),
		status = CASE WHEN enabled = 1 THEN 'idle' ELSE 'disabled' END,
		last_error = '', current_run_id = '', lease_until = NULL, updated_at = ?
		WHERE id = 1 AND current_run_id = ?`, commit.WindowEnd.UTC(), backfill, now, commit.RunID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetFeishuIngestionRun(ctx, commit.RunID)
}

func applyAutomaticProgressTx(ctx context.Context, tx *sql.Tx, update model.DemandProgressUpdate) (string, *model.DemandReview, error) {
	update.Text = strings.TrimSpace(update.Text)
	update.DedupeKey = strings.TrimSpace(update.DedupeKey)
	update.Links = progressLinksWithSource(update.Links, &update.Source)
	if update.DemandID == "" || update.Text == "" || update.DedupeKey == "" {
		return "", nil, errors.New("automatic progress requires demand_id, text and dedupe_key")
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM demands WHERE id = ?`, update.DemandID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return "", nil, fmt.Errorf("progress demand %s: %w", update.DemandID, ErrNotFound)
		}
		return "", nil, err
	}
	var duplicateID string
	duplicateErr := tx.QueryRowContext(ctx, `SELECT id FROM demand_progress WHERE dedupe_key = ?`, update.DedupeKey).Scan(&duplicateID)
	if duplicateErr != nil && duplicateErr != sql.ErrNoRows {
		return "", nil, duplicateErr
	}
	if duplicateID != "" {
		links, err := normalizeProgressLinks(update.Links, update.Text)
		if err != nil {
			return "", nil, err
		}
		changed, err := insertProgressLinksTx(ctx, tx, duplicateID, links)
		if err != nil {
			return "", nil, err
		}
		if changed {
			return "updated", nil, nil
		}
		return "skipped", nil, nil
	}
	anchorCount := len(normalizeStrings(update.Anchors))
	sourceMatch := false
	if key := strings.TrimSpace(update.Source.DedupeKey); key != "" {
		_ = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM demand_sources WHERE demand_id = ? AND dedupe_key = ?)`, update.DemandID, key).Scan(&sourceMatch)
	}
	if update.Confidence < 0.9 || (!sourceMatch && anchorCount < 2) {
		review := &model.DemandReview{Kind: "progress", SuggestedDemandID: update.DemandID,
			ProgressText: update.Text, Source: update.Source, ProgressDedupeKey: update.DedupeKey,
			Links: update.Links, Confidence: update.Confidence,
			Rationale: "automatic match did not meet the service-side confidence anchors"}
		return "review", review, nil
	}
	progress := model.Progress{ID: uuid.NewString(), DemandID: update.DemandID, Text: update.Text,
		DedupeKey: update.DedupeKey, Links: update.Links, CreatedAt: time.Now().UTC()}
	if err := insertProgressTx(ctx, tx, &progress); err != nil {
		if isUniqueConstraint(err) {
			return "skipped", nil, nil
		}
		return "", nil, err
	}
	if update.Source.Kind != "" {
		var sourceExists int
		if update.Source.DedupeKey != "" {
			_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM demand_sources WHERE dedupe_key = ?`, update.Source.DedupeKey).Scan(&sourceExists)
		}
		if sourceExists == 0 {
			if err := insertSourceTx(ctx, tx, update.DemandID, &update.Source); err != nil && !errors.Is(err, ErrConflict) {
				return "", nil, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE demands SET updated_at = ? WHERE id = ?`, progress.CreatedAt, update.DemandID); err != nil {
		return "", nil, err
	}
	return "updated", nil, nil
}

func insertDemandReviewTx(ctx context.Context, tx *sql.Tx, review *model.DemandReview) (bool, error) {
	if review == nil {
		return false, nil
	}
	review.ProgressText = strings.TrimSpace(review.ProgressText)
	review.ProgressDedupeKey = strings.TrimSpace(review.ProgressDedupeKey)
	if review.ProgressText == "" || review.ProgressDedupeKey == "" || review.Source.Kind == "" {
		return false, errors.New("review requires progress_text, progress_dedupe_key and source")
	}
	if review.ID == "" {
		review.ID = uuid.NewString()
	}
	if review.Kind == "" {
		review.Kind = "progress"
	}
	if review.Status == "" {
		review.Status = "pending"
	}
	now := time.Now().UTC()
	if review.CreatedAt.IsZero() {
		review.CreatedAt = now
	}
	review.UpdatedAt = now
	review.Links = progressLinksWithSource(review.Links, &review.Source)
	links, err := normalizeProgressLinks(review.Links, review.ProgressText)
	if err != nil {
		return false, err
	}
	review.Links = links
	raw, err := json.Marshal(demandReviewEvidence{Source: review.Source, Links: review.Links})
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO demand_reviews
		(id, kind, status, suggested_demand_id, progress_text, source_json,
		 progress_dedupe_key, confidence, rationale, created_at, updated_at)
		VALUES (?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?)`, review.ID, review.Kind,
		nullableString(review.SuggestedDemandID), review.ProgressText, string(raw),
		review.ProgressDedupeKey, review.Confidence, review.Rationale,
		review.CreatedAt.UTC(), review.UpdatedAt.UTC())
	if err != nil {
		if isUniqueConstraint(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func (s *Store) GetFeishuIngestionRun(ctx context.Context, id string) (*model.FeishuIngestionRun, error) {
	return scanIngestionRun(s.db.QueryRowContext(ctx, `SELECT `+ingestionRunColumns+
		` FROM feishu_ingestion_runs WHERE id = ?`, id).Scan)
}

func scanIngestionRun(scan func(...any) error) (*model.FeishuIngestionRun, error) {
	var run model.FeishuIngestionRun
	var startRaw, endRaw, startedRaw, finishedRaw any
	if err := scan(&run.ID, &run.Trigger, &run.Status, &startRaw, &endRaw,
		&run.MessagesSeen, &run.MessagesCandidate, &run.Created, &run.Updated,
		&run.Skipped, &run.ReviewCount, &run.MissingContextCount, &run.InputTokens,
		&run.CachedInputTokens, &run.OutputTokens, &startedRaw, &finishedRaw, &run.Error); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	run.WindowStart = parseSQLiteTime(startRaw)
	run.WindowEnd = parseSQLiteTime(endRaw)
	run.StartedAt = parseSQLiteTime(startedRaw)
	run.FinishedAt = optionalSQLiteTime(finishedRaw)
	return &run, nil
}

func (s *Store) ListFeishuIngestionRuns(ctx context.Context, limit int) ([]model.FeishuIngestionRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+ingestionRunColumns+
		` FROM feishu_ingestion_runs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.FeishuIngestionRun, 0)
	for rows.Next() {
		run, scanErr := scanIngestionRun(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, *run)
	}
	return result, rows.Err()
}

func (s *Store) ListDemandReviews(ctx context.Context, status string) ([]model.DemandReview, error) {
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "accepted" && status != "dismissed" && status != "all" {
		return nil, errors.New("review status must be pending, accepted, dismissed or all")
	}
	query := `SELECT id, kind, status, suggested_demand_id, progress_text, source_json,
		progress_dedupe_key, confidence, rationale, created_at, updated_at, resolved_at
		FROM demand_reviews`
	args := []any{}
	if status != "all" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.DemandReview, 0)
	for rows.Next() {
		review, scanErr := scanDemandReview(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, *review)
	}
	return result, rows.Err()
}

func scanDemandReview(scan func(...any) error) (*model.DemandReview, error) {
	var review model.DemandReview
	var suggested sql.NullString
	var sourceRaw string
	var createdRaw, updatedRaw, resolvedRaw any
	if err := scan(&review.ID, &review.Kind, &review.Status, &suggested,
		&review.ProgressText, &sourceRaw, &review.ProgressDedupeKey, &review.Confidence,
		&review.Rationale, &createdRaw, &updatedRaw, &resolvedRaw); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if suggested.Valid {
		review.SuggestedDemandID = suggested.String
	}
	var evidence demandReviewEvidence
	if err := json.Unmarshal([]byte(sourceRaw), &evidence); err == nil && evidence.Source.Kind != "" {
		review.Source = evidence.Source
		review.Links = evidence.Links
	} else if err := json.Unmarshal([]byte(sourceRaw), &review.Source); err != nil {
		return nil, fmt.Errorf("decode review source: %w", err)
	}
	if review.Links == nil {
		review.Links = []model.ProgressLink{}
	}
	review.CreatedAt = parseSQLiteTime(createdRaw)
	review.UpdatedAt = parseSQLiteTime(updatedRaw)
	review.ResolvedAt = optionalSQLiteTime(resolvedRaw)
	return &review, nil
}

func (s *Store) ResolveDemandReview(ctx context.Context, id, action, demandID string) (*model.DemandReview, error) {
	if action != "accept" && action != "dismiss" {
		return nil, errors.New("review action must be accept or dismiss")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	review, err := scanDemandReview(tx.QueryRowContext(ctx, `SELECT id, kind, status,
		suggested_demand_id, progress_text, source_json, progress_dedupe_key, confidence,
		rationale, created_at, updated_at, resolved_at FROM demand_reviews WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, err
	}
	if review.Status != "pending" {
		return nil, fmt.Errorf("%w: review is already %s", ErrConflict, review.Status)
	}
	now := time.Now().UTC()
	status := "dismissed"
	if action == "accept" {
		status = "accepted"
		if strings.TrimSpace(demandID) == "" {
			demandID = review.SuggestedDemandID
		}
		if demandID == "" {
			return nil, errors.New("demand_id is required to accept this review")
		}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM demands WHERE id = ?`, demandID).Scan(&exists); err != nil {
			if err == sql.ErrNoRows {
				return nil, ErrNotFound
			}
			return nil, err
		}
		progress := model.Progress{ID: uuid.NewString(), DemandID: demandID,
			Text: review.ProgressText, DedupeKey: review.ProgressDedupeKey,
			Links: review.Links, CreatedAt: now}
		err := insertProgressTx(ctx, tx, &progress)
		if err != nil && !isUniqueConstraint(err) {
			return nil, err
		}
		if review.Source.Kind != "" {
			var sourceExists int
			if review.Source.DedupeKey != "" {
				_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM demand_sources WHERE dedupe_key = ?`, review.Source.DedupeKey).Scan(&sourceExists)
			}
			if sourceExists == 0 {
				if err := insertSourceTx(ctx, tx, demandID, &review.Source); err != nil && !errors.Is(err, ErrConflict) {
					return nil, err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE demands SET updated_at = ? WHERE id = ?`, now, demandID); err != nil {
			return nil, err
		}
		if err := markFeishuDirtyTx(ctx, tx, now); err != nil {
			return nil, err
		}
		review.SuggestedDemandID = demandID
	}
	if _, err := tx.ExecContext(ctx, `UPDATE demand_reviews SET status = ?,
		suggested_demand_id = ?, updated_at = ?, resolved_at = ? WHERE id = ?`, status,
		nullableString(review.SuggestedDemandID), now, now, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	review.Status = status
	review.UpdatedAt = now
	review.ResolvedAt = &now
	return review, nil
}

type demandReviewEvidence struct {
	Source model.Source         `json:"source"`
	Links  []model.ProgressLink `json:"links,omitempty"`
}
