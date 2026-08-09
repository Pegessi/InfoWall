package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infowall/infowall/internal/model"
)

type DemandListOptions struct {
	Status           model.DemandStatus
	ProjectID        string
	Query            string
	IncludeDismissed bool
}

type DemandUpdate struct {
	Title         *string
	Description   *string
	Status        *model.DemandStatus
	Priority      *model.DemandPriority
	ProjectIDSet  bool
	ProjectID     *string
	ProjectHint   *string
	NextAction    *string
	BlockedReason *string
}

func (u DemandUpdate) Empty() bool {
	return u.Title == nil && u.Description == nil && u.Status == nil &&
		u.Priority == nil && !u.ProjectIDSet && u.ProjectHint == nil &&
		u.NextAction == nil && u.BlockedReason == nil
}

type ImportDemandResult struct {
	Action string        `json:"action"`
	Demand *model.Demand `json:"demand"`
}

type ImportDemandsResult struct {
	Created int                  `json:"created"`
	Updated int                  `json:"updated"`
	Skipped int                  `json:"skipped"`
	Results []ImportDemandResult `json:"results"`
}

var ErrConflict = errors.New("conflict")

const demandColumns = `id, title, description, status, priority, project_id,
	project_hint, next_action, blocked_reason, created_at, updated_at, completed_at`

type demandQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) CreateDemand(ctx context.Context, demand *model.Demand) (*model.Demand, error) {
	if demand == nil {
		return nil, fmt.Errorf("demand is required")
	}
	d := cloneDemand(demand)
	now := time.Now().UTC()
	if err := normalizeNewDemand(d, now); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := resolveDemandProject(ctx, tx, d); err != nil {
		return nil, err
	}
	if err := insertDemandTx(ctx, tx, d); err != nil {
		return nil, err
	}
	if err := markFeishuDirtyTx(ctx, tx, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetDemand(ctx, d.ID)
}

func (s *Store) ListDemands(ctx context.Context, opts DemandListOptions) ([]*model.Demand, error) {
	if opts.Status != "" && !opts.Status.Valid() {
		return nil, fmt.Errorf("invalid demand status %q", opts.Status)
	}
	query := `SELECT ` + demandColumns + ` FROM demands`
	where := make([]string, 0, 4)
	args := make([]any, 0, 8)
	if opts.Status != "" {
		where = append(where, "status = ?")
		args = append(args, opts.Status)
	} else if !opts.IncludeDismissed {
		where = append(where, "status <> 'dismissed'")
	}
	if strings.TrimSpace(opts.ProjectID) != "" {
		where = append(where, "project_id = ?")
		args = append(args, strings.TrimSpace(opts.ProjectID))
	}
	for _, term := range searchTerms(opts.Query) {
		where = append(where, `lower(title || ' ' || description || ' ' || project_hint || ' ' || next_action || ' ' || blocked_reason)
			LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(strings.ToLower(term))+"%")
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY CASE priority WHEN 'p0' THEN 0 WHEN 'p1' THEN 1 WHEN 'p2' THEN 2
		WHEN 'p3' THEN 3 ELSE 4 END, updated_at DESC, id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	demands := make([]*model.Demand, 0)
	for rows.Next() {
		demand, err := scanDemand(rows.Scan)
		if err != nil {
			rows.Close()
			return nil, err
		}
		demands = append(demands, demand)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, demand := range demands {
		if err := loadDemandRelations(ctx, s.db, demand); err != nil {
			return nil, err
		}
	}
	return demands, nil
}

func (s *Store) GetDemand(ctx context.Context, id string) (*model.Demand, error) {
	return getDemand(ctx, s.db, id)
}

func getDemand(ctx context.Context, q demandQueryer, id string) (*model.Demand, error) {
	demand, err := scanDemand(q.QueryRowContext(ctx,
		`SELECT `+demandColumns+` FROM demands WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, err
	}
	if err := loadDemandRelations(ctx, q, demand); err != nil {
		return nil, err
	}
	return demand, nil
}

func scanDemand(scan func(...any) error) (*model.Demand, error) {
	var demand model.Demand
	var projectID sql.NullString
	var createdRaw, updatedRaw, completedRaw any
	if err := scan(
		&demand.ID, &demand.Title, &demand.Description, &demand.Status,
		&demand.Priority, &projectID, &demand.ProjectHint, &demand.NextAction,
		&demand.BlockedReason, &createdRaw, &updatedRaw, &completedRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if projectID.Valid {
		id := projectID.String
		demand.ProjectID = &id
	}
	demand.CreatedAt = parseSQLiteTime(createdRaw)
	demand.UpdatedAt = parseSQLiteTime(updatedRaw)
	if completed := parseSQLiteTime(completedRaw); !completed.IsZero() {
		demand.CompletedAt = &completed
	}
	demand.Sources = []model.Source{}
	demand.Progress = []model.Progress{}
	return &demand, nil
}

func loadDemandRelations(ctx context.Context, q demandQueryer, demand *model.Demand) error {
	rows, err := q.QueryContext(ctx, `SELECT id, demand_id, kind, external_id, chat_id,
		chat_name, sender_id, sender_name, message_time, url, excerpt, dedupe_key, created_at
		FROM demand_sources WHERE demand_id = ? ORDER BY created_at, id`, demand.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var source model.Source
		var messageRaw, createdRaw any
		if err := rows.Scan(&source.ID, &source.DemandID, &source.Kind, &source.ExternalID,
			&source.ChatID, &source.ChatName, &source.SenderID, &source.SenderName,
			&messageRaw, &source.URL, &source.Excerpt, &source.DedupeKey, &createdRaw); err != nil {
			rows.Close()
			return err
		}
		source.MessageTime = parseSQLiteTime(messageRaw)
		source.CreatedAt = parseSQLiteTime(createdRaw)
		demand.Sources = append(demand.Sources, source)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	rows, err = q.QueryContext(ctx, `SELECT id, demand_id, text, dedupe_key, created_at
		FROM demand_progress WHERE demand_id = ? ORDER BY created_at, id`, demand.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var progress model.Progress
		var createdRaw any
		if err := rows.Scan(&progress.ID, &progress.DemandID, &progress.Text,
			&progress.DedupeKey, &createdRaw); err != nil {
			return err
		}
		progress.CreatedAt = parseSQLiteTime(createdRaw)
		progress.Links = []model.ProgressLink{}
		demand.Progress = append(demand.Progress, progress)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	linksByProgress := make(map[string][]model.ProgressLink)
	rows, err = q.QueryContext(ctx, `SELECT links.progress_id, links.kind, links.external_id,
		links.title, links.url, links.state, links.dedupe_key
		FROM demand_progress_links AS links
		JOIN demand_progress AS progress ON progress.id = links.progress_id
		WHERE progress.demand_id = ?
		ORDER BY progress.created_at, links.position, links.dedupe_key`, demand.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var progressID string
		var link model.ProgressLink
		if err := rows.Scan(&progressID, &link.Kind, &link.ExternalID, &link.Title,
			&link.URL, &link.State, &link.DedupeKey); err != nil {
			rows.Close()
			return err
		}
		linksByProgress[progressID] = append(linksByProgress[progressID], link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for i := range demand.Progress {
		if links := linksByProgress[demand.Progress[i].ID]; links != nil {
			demand.Progress[i].Links = links
		}
	}
	return nil
}

func (s *Store) UpdateDemand(ctx context.Context, id string, update DemandUpdate) (*model.Demand, error) {
	if update.Empty() {
		return nil, fmt.Errorf("demand patch has no fields")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	demand, err := getDemand(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if update.Title != nil {
		demand.Title = strings.TrimSpace(*update.Title)
		if demand.Title == "" {
			return nil, fmt.Errorf("demand title is required")
		}
	}
	if update.Description != nil {
		demand.Description = *update.Description
	}
	if update.Status != nil {
		if !update.Status.Valid() {
			return nil, fmt.Errorf("invalid demand status %q", *update.Status)
		}
		demand.Status = *update.Status
	}
	if update.Priority != nil {
		if !update.Priority.Valid() {
			return nil, fmt.Errorf("invalid demand priority %q", *update.Priority)
		}
		demand.Priority = *update.Priority
	}
	if update.ProjectHint != nil {
		demand.ProjectHint = strings.TrimSpace(*update.ProjectHint)
	}
	if update.ProjectIDSet {
		demand.ProjectID = update.ProjectID
		if demand.ProjectID == nil || strings.TrimSpace(*demand.ProjectID) == "" {
			demand.ProjectID = nil
			if update.ProjectHint == nil {
				demand.ProjectHint = ""
			}
		} else if update.ProjectHint == nil {
			demand.ProjectHint = ""
		}
		if err := resolveDemandProject(ctx, tx, demand); err != nil {
			return nil, err
		}
	}
	if update.NextAction != nil {
		demand.NextAction = *update.NextAction
	}
	if update.BlockedReason != nil {
		demand.BlockedReason = *update.BlockedReason
	}
	now := time.Now().UTC()
	demand.UpdatedAt = now
	if demand.Status == model.DemandStatusDone {
		if demand.CompletedAt == nil {
			demand.CompletedAt = &now
		}
	} else {
		demand.CompletedAt = nil
	}
	if err := updateDemandRowTx(ctx, tx, demand); err != nil {
		return nil, err
	}
	if err := markFeishuDirtyTx(ctx, tx, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetDemand(ctx, id)
}

func (s *Store) AddDemandProgress(ctx context.Context, demandID, text string) (*model.Progress, error) {
	return s.AddDemandProgressWithSource(ctx, demandID, text, nil)
}

func (s *Store) AddDemandProgressWithSource(ctx context.Context, demandID, text string, source *model.Source) (*model.Progress, error) {
	return s.AddDemandProgressWithSourceAndLinks(ctx, demandID, text, source, nil)
}

func (s *Store) AddDemandProgressWithSourceAndLinks(ctx context.Context, demandID, text string, source *model.Source, links []model.ProgressLink) (*model.Progress, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("progress text is required")
	}
	now := time.Now().UTC()
	progress := &model.Progress{ID: uuid.NewString(), DemandID: demandID, Text: text,
		Links: progressLinksWithSource(links, source), CreatedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM demands WHERE id = ?`, demandID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := insertProgressTx(ctx, tx, progress); err != nil {
		return nil, err
	}
	if source != nil {
		sourceCopy := *source
		if err := insertSourceTx(ctx, tx, demandID, &sourceCopy); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE demands SET updated_at = ? WHERE id = ?`, now, demandID); err != nil {
		return nil, err
	}
	if err := markFeishuDirtyTx(ctx, tx, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return progress, nil
}

func (s *Store) ImportDemands(ctx context.Context, incoming []*model.Demand) (*ImportDemandsResult, error) {
	result := &ImportDemandsResult{Results: make([]ImportDemandResult, 0, len(incoming))}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	mutated := false
	for index, raw := range incoming {
		if raw == nil {
			return nil, fmt.Errorf("demand %d is null", index)
		}
		demand := cloneDemand(raw)
		existingID, err := findImportedDemandTx(ctx, tx, demand)
		if err != nil {
			return nil, fmt.Errorf("demand %d: %w", index, err)
		}
		if existingID == "" {
			now := time.Now().UTC()
			if err := normalizeNewDemand(demand, now); err != nil {
				return nil, fmt.Errorf("demand %d: %w", index, err)
			}
			if err := resolveDemandProject(ctx, tx, demand); err != nil {
				return nil, fmt.Errorf("demand %d: %w", index, err)
			}
			if err := insertDemandTx(ctx, tx, demand); err != nil {
				return nil, fmt.Errorf("demand %d: %w", index, err)
			}
			result.Created++
			result.Results = append(result.Results, ImportDemandResult{Action: "created", Demand: demand})
			mutated = true
			continue
		}

		existing, err := getDemand(ctx, tx, existingID)
		if err != nil {
			return nil, fmt.Errorf("demand %d: %w", index, err)
		}
		changed, err := mergeImportedDemandTx(ctx, tx, existing, demand)
		if err != nil {
			return nil, fmt.Errorf("demand %d: %w", index, err)
		}
		if changed {
			existing.UpdatedAt = time.Now().UTC()
			if err := updateDemandRowTx(ctx, tx, existing); err != nil {
				return nil, fmt.Errorf("demand %d: %w", index, err)
			}
			result.Updated++
			result.Results = append(result.Results, ImportDemandResult{Action: "updated", Demand: existing})
			mutated = true
		} else {
			result.Skipped++
			result.Results = append(result.Results, ImportDemandResult{Action: "skipped", Demand: existing})
		}
	}
	if mutated {
		if err := markFeishuDirtyTx(ctx, tx, time.Now().UTC()); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for i := range result.Results {
		demand, err := s.GetDemand(ctx, result.Results[i].Demand.ID)
		if err != nil {
			return nil, err
		}
		result.Results[i].Demand = demand
	}
	return result, nil
}

func normalizeNewDemand(demand *model.Demand, now time.Time) error {
	demand.Title = strings.TrimSpace(demand.Title)
	if demand.Title == "" {
		return fmt.Errorf("demand title is required")
	}
	if demand.ID == "" {
		demand.ID = uuid.NewString()
	}
	if demand.Status == "" {
		demand.Status = model.DemandStatusPending
	}
	if !demand.Status.Valid() {
		return fmt.Errorf("invalid demand status %q", demand.Status)
	}
	if demand.Priority == "" {
		demand.Priority = model.DemandPriorityNone
	}
	if !demand.Priority.Valid() {
		return fmt.Errorf("invalid demand priority %q", demand.Priority)
	}
	if demand.CreatedAt.IsZero() {
		demand.CreatedAt = now
	}
	demand.UpdatedAt = now
	if demand.Status == model.DemandStatusDone {
		if demand.CompletedAt == nil {
			demand.CompletedAt = &now
		}
	} else {
		demand.CompletedAt = nil
	}
	return nil
}

func resolveDemandProject(ctx context.Context, tx *sql.Tx, demand *model.Demand) error {
	projectID, hint, err := resolveProjectTx(ctx, tx, demand.ProjectID, demand.ProjectHint)
	if err != nil {
		return err
	}
	demand.ProjectID = projectID
	demand.ProjectHint = hint
	return nil
}

func insertDemandTx(ctx context.Context, tx *sql.Tx, demand *model.Demand) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO demands
		(id, title, description, status, priority, project_id, project_hint,
		 next_action, blocked_reason, created_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, demand.ID, demand.Title,
		demand.Description, demand.Status, demand.Priority, demand.ProjectID,
		demand.ProjectHint, demand.NextAction, demand.BlockedReason,
		demand.CreatedAt.UTC(), demand.UpdatedAt.UTC(), demand.CompletedAt); err != nil {
		if isUniqueConstraint(err) {
			return fmt.Errorf("%w: demand already exists", ErrConflict)
		}
		return fmt.Errorf("insert demand: %w", err)
	}
	for i := range demand.Sources {
		if err := insertSourceTx(ctx, tx, demand.ID, &demand.Sources[i]); err != nil {
			return err
		}
	}
	for i := range demand.Progress {
		progress := &demand.Progress[i]
		progress.Text = strings.TrimSpace(progress.Text)
		if progress.Text == "" {
			return fmt.Errorf("progress text is required")
		}
		if progress.ID == "" {
			progress.ID = uuid.NewString()
		}
		progress.DemandID = demand.ID
		if progress.CreatedAt.IsZero() {
			progress.CreatedAt = demand.CreatedAt
		}
		if err := insertProgressTx(ctx, tx, progress); err != nil {
			return fmt.Errorf("insert demand progress: %w", err)
		}
	}
	return nil
}

func insertSourceTx(ctx context.Context, tx *sql.Tx, demandID string, source *model.Source) error {
	source.Kind = strings.TrimSpace(source.Kind)
	if source.Kind == "" {
		return fmt.Errorf("source kind is required")
	}
	if source.ID == "" {
		source.ID = uuid.NewString()
	}
	source.DemandID = demandID
	if source.CreatedAt.IsZero() {
		source.CreatedAt = time.Now().UTC()
	}
	var messageTime any
	if !source.MessageTime.IsZero() {
		messageTime = source.MessageTime.UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO demand_sources
		(id, demand_id, kind, external_id, chat_id, chat_name, sender_id, sender_name,
		 message_time, url, excerpt, dedupe_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, source.ID, demandID,
		source.Kind, source.ExternalID, source.ChatID, source.ChatName, source.SenderID,
		source.SenderName, messageTime, source.URL, source.Excerpt,
		strings.TrimSpace(source.DedupeKey), source.CreatedAt.UTC())
	if err != nil {
		if isUniqueConstraint(err) {
			return fmt.Errorf("%w: source dedupe_key already exists", ErrConflict)
		}
		return fmt.Errorf("insert demand source: %w", err)
	}
	return nil
}

func updateDemandRowTx(ctx context.Context, tx *sql.Tx, demand *model.Demand) error {
	_, err := tx.ExecContext(ctx, `UPDATE demands SET title = ?, description = ?, status = ?,
		priority = ?, project_id = ?, project_hint = ?, next_action = ?, blocked_reason = ?,
		updated_at = ?, completed_at = ? WHERE id = ?`, demand.Title, demand.Description,
		demand.Status, demand.Priority, demand.ProjectID, demand.ProjectHint,
		demand.NextAction, demand.BlockedReason, demand.UpdatedAt.UTC(), demand.CompletedAt, demand.ID)
	return err
}

func findImportedDemandTx(ctx context.Context, tx *sql.Tx, demand *model.Demand) (string, error) {
	// An explicit demand ID is authoritative for reconciled updates. Looking at
	// source keys first could silently attach evidence to a different demand if
	// a caller made a bad match; with ID-first resolution that situation becomes
	// a unique-key conflict and the whole import transaction rolls back safely.
	if strings.TrimSpace(demand.ID) != "" {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM demands WHERE id = ?`, demand.ID).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}
	for _, source := range demand.Sources {
		if key := strings.TrimSpace(source.DedupeKey); key != "" {
			var id string
			err := tx.QueryRowContext(ctx, `SELECT demand_id FROM demand_sources WHERE dedupe_key = ?`, key).Scan(&id)
			if err == nil {
				return id, nil
			}
			if err != sql.ErrNoRows {
				return "", err
			}
		}
	}
	return "", nil
}

func mergeImportedDemandTx(ctx context.Context, tx *sql.Tx, current, incoming *model.Demand) (bool, error) {
	sourceChanged, err := mergeImportedSourcesTx(ctx, tx, current.ID, current.Sources, incoming.Sources)
	if err != nil {
		return false, err
	}
	progressChanged, err := mergeImportedProgressTx(ctx, tx, current.ID, current.Progress, incoming.Progress)
	if err != nil {
		return false, err
	}
	return sourceChanged || progressChanged, nil
}

func mergeImportedProgressTx(ctx context.Context, tx *sql.Tx, demandID string, existing, incoming []model.Progress) (bool, error) {
	changed := false
	for i := range incoming {
		progress := incoming[i]
		progress.Text = strings.TrimSpace(progress.Text)
		if progress.Text == "" {
			continue
		}
		var duplicate *model.Progress
		for currentIndex := range existing {
			current := &existing[currentIndex]
			if progress.DedupeKey != "" && progress.DedupeKey == current.DedupeKey {
				duplicate = current
				break
			}
			if progress.ID != "" && progress.ID == current.ID {
				duplicate = current
				break
			}
			if progress.Text == current.Text && progress.CreatedAt.Equal(current.CreatedAt) {
				duplicate = current
				break
			}
		}
		if duplicate != nil {
			links, normalizeErr := normalizeProgressLinks(progress.Links, progress.Text)
			if normalizeErr != nil {
				return false, normalizeErr
			}
			linksChanged, insertErr := insertProgressLinksTx(ctx, tx, duplicate.ID, links)
			if insertErr != nil {
				return false, insertErr
			}
			changed = changed || linksChanged
			continue
		}
		if progress.ID == "" {
			progress.ID = uuid.NewString()
		}
		progress.DemandID = demandID
		if progress.CreatedAt.IsZero() {
			progress.CreatedAt = time.Now().UTC()
		}
		if err := insertProgressTx(ctx, tx, &progress); err != nil {
			if isUniqueConstraint(err) {
				continue
			}
			return false, err
		}
		existing = append(existing, progress)
		changed = true
	}
	return changed, nil
}

func mergeImportedSourcesTx(ctx context.Context, tx *sql.Tx, demandID string, existing, incoming []model.Source) (bool, error) {
	changed := false
	for i := range incoming {
		source := incoming[i]
		var match *model.Source
		for j := range existing {
			if source.DedupeKey != "" && source.DedupeKey == existing[j].DedupeKey {
				match = &existing[j]
				break
			}
			if source.DedupeKey == "" && source.ExternalID != "" &&
				source.Kind == existing[j].Kind && source.ExternalID == existing[j].ExternalID {
				match = &existing[j]
				break
			}
		}
		if match == nil {
			if err := insertSourceTx(ctx, tx, demandID, &source); err != nil {
				return false, err
			}
			existing = append(existing, source)
			changed = true
			continue
		}
		// A matching dedupe key/external id is the same immutable evidence record.
		// Import never rewrites it; only genuinely new sources are appended.
	}
	return changed, nil
}

func cloneDemand(source *model.Demand) *model.Demand {
	copy := *source
	copy.ProjectID = cloneStringPointer(source.ProjectID)
	copy.Sources = append([]model.Source(nil), source.Sources...)
	copy.Progress = append([]model.Progress(nil), source.Progress...)
	for i := range copy.Progress {
		copy.Progress[i].Links = append([]model.ProgressLink(nil), source.Progress[i].Links...)
	}
	return &copy
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func isUniqueConstraint(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
