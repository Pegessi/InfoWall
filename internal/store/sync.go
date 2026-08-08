package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/infowall/infowall/internal/model"
)

const feishuSyncStateColumns = `enabled, doc_token, doc_url, dirty,
	desired_version, synced_version, status, last_revision, last_hash,
	last_success_at, last_error, retry_count, next_retry_at, updated_at`

func markFeishuDirtyTx(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE feishu_sync_state
		SET dirty = 1,
			desired_version = desired_version + 1,
			status = CASE WHEN enabled = 1 THEN 'pending' ELSE status END,
			retry_count = 0,
			next_retry_at = NULL,
			updated_at = ?
		WHERE id = 1`, now.UTC())
	if err != nil {
		return fmt.Errorf("mark Feishu sync dirty: %w", err)
	}
	return nil
}

func (s *Store) GetFeishuSyncState(ctx context.Context) (*model.FeishuSyncState, error) {
	return scanFeishuSyncState(s.db.QueryRowContext(ctx,
		`SELECT `+feishuSyncStateColumns+` FROM feishu_sync_state WHERE id = 1`).Scan)
}

func scanFeishuSyncState(scan func(...any) error) (*model.FeishuSyncState, error) {
	var (
		state                        model.FeishuSyncState
		enabled, dirty               int
		lastSuccessRaw, nextRetryRaw any
		updatedAtRaw                 any
	)
	if err := scan(
		&enabled, &state.DocToken, &state.DocURL, &dirty,
		&state.DesiredVersion, &state.SyncedVersion, &state.Status,
		&state.LastRevision, &state.LastHash, &lastSuccessRaw, &state.LastError,
		&state.RetryCount, &nextRetryRaw, &updatedAtRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	state.Enabled = enabled != 0
	state.Dirty = dirty != 0
	state.UpdatedAt = parseSQLiteTime(updatedAtRaw)
	if t := parseSQLiteTime(lastSuccessRaw); !t.IsZero() {
		state.LastSuccessAt = &t
	}
	if t := parseSQLiteTime(nextRetryRaw); !t.IsZero() {
		state.NextRetryAt = &t
	}
	return &state, nil
}

func (s *Store) ConfigureFeishuSync(ctx context.Context, docToken, docURL string, enabled bool) error {
	now := time.Now().UTC()
	status := "disabled"
	if enabled {
		status = "pending"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE feishu_sync_state
		SET enabled = ?, doc_token = ?, doc_url = ?, dirty = ?,
			desired_version = desired_version + 1, status = ?,
			last_error = '', retry_count = 0, next_retry_at = NULL, updated_at = ?
		WHERE id = 1`, boolInt(enabled), docToken, docURL, boolInt(enabled), status, now)
	if err != nil {
		return fmt.Errorf("configure Feishu sync: %w", err)
	}
	return nil
}

func (s *Store) MarkFeishuSyncRunning(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE feishu_sync_state
		SET status = 'running', next_retry_at = NULL, updated_at = ? WHERE id = 1`, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("mark Feishu sync running: %w", err)
	}
	return nil
}

func (s *Store) CompleteFeishuSync(ctx context.Context, desiredVersion int64, revision, hash string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE feishu_sync_state
		SET synced_version = max(synced_version, min(?, desired_version)),
			dirty = CASE WHEN desired_version <= ? THEN 0 ELSE 1 END,
			status = CASE WHEN enabled = 0 THEN 'disabled'
				WHEN desired_version <= ? THEN 'idle' ELSE 'pending' END,
			last_revision = ?, last_hash = ?, last_success_at = ?,
			last_error = '', retry_count = 0, next_retry_at = NULL, updated_at = ?
		WHERE id = 1`, desiredVersion, desiredVersion,
		desiredVersion, revision, hash, now, now)
	if err != nil {
		return fmt.Errorf("complete Feishu sync: %w", err)
	}
	return nil
}

func (s *Store) FailFeishuSync(ctx context.Context, syncError string, nextRetry time.Time) error {
	var nextRetryArg any
	if !nextRetry.IsZero() {
		nextRetryArg = nextRetry.UTC()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE feishu_sync_state
		SET dirty = 1, status = CASE WHEN enabled = 1 THEN 'error' ELSE 'disabled' END,
			last_error = ?, retry_count = retry_count + 1, next_retry_at = ?, updated_at = ?
		WHERE id = 1`, syncError, nextRetryArg, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("fail Feishu sync: %w", err)
	}
	return nil
}

func (s *Store) DisableFeishuSync(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE feishu_sync_state
		SET enabled = 0, dirty = 0, status = 'disabled', last_error = '',
			retry_count = 0, next_retry_at = NULL,
			updated_at = ? WHERE id = 1`, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("disable Feishu sync: %w", err)
	}
	return nil
}
