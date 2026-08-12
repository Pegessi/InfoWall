package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/infowall/infowall/internal/model"
)

// PutConversationHookEvent persists a retry-safe hook envelope. Replayed hook
// IDs are accepted as successful no-ops so a client may spool after an HTTP
// timeout without creating duplicate turns.
func (s *Store) PutConversationHookEvent(ctx context.Context, event model.ConversationHookEvent) (bool, error) {
	event.ID = strings.TrimSpace(event.ID)
	event.Source = strings.ToLower(strings.TrimSpace(event.Source))
	event.EventName = strings.TrimSpace(event.EventName)
	event.SessionID = strings.TrimSpace(event.SessionID)
	if event.ID == "" || event.SessionID == "" {
		return false, errors.New("conversation hook event requires id and session_id")
	}
	if event.Source != "codex" && event.Source != "claude" {
		return false, errors.New("conversation hook source must be codex or claude")
	}
	if event.EventName != "UserPromptSubmit" && event.EventName != "Stop" {
		return false, errors.New("conversation hook event_name must be UserPromptSubmit or Stop")
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	links, err := json.Marshal(event.Links)
	if err != nil {
		return false, fmt.Errorf("encode conversation hook links: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO conversation_hook_events
		(id, source, event_name, session_id, turn_id, cwd, url, prompt, result,
		 transcript_path, links_json, occurred_at, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.ID, event.Source,
		event.EventName, event.SessionID, strings.TrimSpace(event.TurnID),
		strings.TrimSpace(event.CWD), strings.TrimSpace(event.URL), event.Prompt, event.Result,
		strings.TrimSpace(event.TranscriptPath), string(links), event.OccurredAt.UTC(), time.Now().UTC())
	if err != nil {
		return false, fmt.Errorf("store conversation hook event: %w", err)
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

type pendingHookEvent struct {
	model.ConversationHookEvent
	processed bool
}

// ListPendingConversationTurns pairs pending prompts with their subsequent
// final results. Unpaired prompts remain pending for a later Stop event.
func (s *Store) ListPendingConversationTurns(ctx context.Context, end time.Time) ([]model.ConversationTurn, error) {
	// The supported recovery horizon is 12 hours. Clearing still-unpaired hook
	// envelopes after 24 hours prevents abandoned prompts/stops from retaining
	// conversation bodies indefinitely while leaving a generous retry margin.
	if _, err := s.db.ExecContext(ctx, `UPDATE conversation_hook_events SET
		processed_at = ?, prompt = '', result = '', transcript_path = '', url = '', links_json = '[]'
		WHERE processed_at IS NULL AND occurred_at < ?`, end.UTC(), end.UTC().Add(-24*time.Hour)); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, source, event_name, session_id,
		turn_id, cwd, url, prompt, result, transcript_path, links_json, occurred_at, received_at
		FROM conversation_hook_events
		WHERE processed_at IS NULL AND occurred_at <= ?
		ORDER BY occurred_at,
			CASE event_name WHEN 'UserPromptSubmit' THEN 0 ELSE 1 END,
			received_at, id`, end.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]pendingHookEvent, 0)
	for rows.Next() {
		var event pendingHookEvent
		var linksRaw string
		var occurredRaw, receivedRaw any
		if err := rows.Scan(&event.ID, &event.Source, &event.EventName, &event.SessionID,
			&event.TurnID, &event.CWD, &event.URL, &event.Prompt, &event.Result, &event.TranscriptPath,
			&linksRaw, &occurredRaw, &receivedRaw); err != nil {
			return nil, err
		}
		event.OccurredAt = parseSQLiteTime(occurredRaw)
		event.ReceivedAt = parseSQLiteTime(receivedRaw)
		if err := json.Unmarshal([]byte(linksRaw), &event.Links); err != nil {
			return nil, fmt.Errorf("decode conversation hook links: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	turns := make([]model.ConversationTurn, 0)
	for stopIndex := range events {
		stop := &events[stopIndex]
		if stop.EventName != "Stop" || strings.TrimSpace(stop.Result) == "" {
			continue
		}
		promptIndex := -1
		searchStart := stopIndex - 1
		if stop.TurnID != "" {
			searchStart = len(events) - 1
		}
		for index := searchStart; index >= 0; index-- {
			if index == stopIndex {
				continue
			}
			candidate := &events[index]
			if candidate.processed || candidate.EventName != "UserPromptSubmit" ||
				candidate.Source != stop.Source || candidate.SessionID != stop.SessionID {
				continue
			}
			if stop.TurnID != "" && candidate.TurnID != "" && candidate.TurnID != stop.TurnID {
				continue
			}
			promptIndex = index
			break
		}
		if promptIndex < 0 {
			// A final result without its user goal is not useful demand evidence.
			// Leave it pending so an out-of-order prompt can still complete it.
			continue
		}
		ids := []string{stop.ID}
		prompt, cwd, url, turnID := "", stop.CWD, stop.URL, stop.TurnID
		links := append([]model.ProgressLink(nil), stop.Links...)
		candidate := &events[promptIndex]
		candidate.processed = true
		ids = append(ids, candidate.ID)
		prompt = candidate.Prompt
		if cwd == "" {
			cwd = candidate.CWD
		}
		if url == "" {
			url = candidate.URL
		}
		if turnID == "" {
			turnID = candidate.TurnID
		}
		links = append(links, candidate.Links...)
		stop.processed = true
		turnIDForKey := turnID
		if turnIDForKey == "" {
			turnIDForKey = stop.ID
		}
		turns = append(turns, model.ConversationTurn{
			ID:     stop.Source + "-turn:" + stop.SessionID + ":" + turnIDForKey,
			Source: stop.Source, SessionID: stop.SessionID, TurnID: turnID, CWD: cwd, URL: url,
			Prompt: prompt, Result: stop.Result, Links: dedupeProgressLinks(links),
			OccurredAt: stop.OccurredAt, HookEventIDs: ids,
		})
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i].OccurredAt.Before(turns[j].OccurredAt) })
	return turns, nil
}

func dedupeProgressLinks(links []model.ProgressLink) []model.ProgressLink {
	seen := make(map[string]struct{}, len(links))
	result := make([]model.ProgressLink, 0, len(links))
	for _, link := range links {
		key := strings.TrimSpace(link.DedupeKey)
		if key == "" {
			key = strings.TrimSpace(link.URL)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, link)
	}
	return result
}

func markConversationHookEventsProcessedTx(ctx context.Context, tx *sql.Tx, ids []string, at time.Time) error {
	for _, batch := range chunkStrings(normalizeStrings(ids), 300) {
		if len(batch) == 0 {
			continue
		}
		placeholders := strings.TrimRight(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch)+1)
		args = append(args, at.UTC())
		for _, id := range batch {
			args = append(args, id)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversation_hook_events SET
			processed_at = ?, prompt = '', result = '', transcript_path = '', url = '', links_json = '[]'
			WHERE id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
	}
	return nil
}

func updateConversationWatermarksTx(ctx context.Context, tx *sql.Tx, end time.Time) error {
	for _, source := range []string{"feishu", "codex", "claude"} {
		if _, err := tx.ExecContext(ctx, `UPDATE conversation_source_state SET
			last_success_end = ?, updated_at = ? WHERE source = ?`, end.UTC(), time.Now().UTC(), source); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) conversationWatermarks(ctx context.Context) (map[string]*time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source, last_success_end FROM conversation_source_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]*time.Time{}
	for rows.Next() {
		var source string
		var raw any
		if err := rows.Scan(&source, &raw); err != nil {
			return nil, err
		}
		result[source] = optionalSQLiteTime(raw)
	}
	return result, rows.Err()
}
