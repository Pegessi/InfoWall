// Package store provides SQLite persistence for Items.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/infowall/infowall/internal/model"
)

type Store struct {
	db *sql.DB
}

// schemaVersion is the SQLite schema version this binary understands, tracked in
// the database via PRAGMA user_version. Databases created before versioning was
// introduced report user_version = 0 and are structurally identical to v1, so
// migrating 0 -> 1 only stamps the version (no data change). Bump this and add a
// case in migrate() when the schema changes in a future release.
const schemaVersion = 5

const schema = `
CREATE TABLE IF NOT EXISTS items (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL DEFAULT 'note',
    title      TEXT NOT NULL DEFAULT '',
    tags       TEXT NOT NULL DEFAULT '[]',
    meta       TEXT NOT NULL DEFAULT '{}',
    body       TEXT NOT NULL DEFAULT '',
    raw        TEXT NOT NULL DEFAULT '',
    pinned     INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_items_created ON items(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_pinned  ON items(pinned DESC, created_at DESC);
`

const workbenchSchema = `
CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL COLLATE NOCASE UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    color       TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_projects_status_name ON projects(status, name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS demands (
    id             TEXT PRIMARY KEY,
    title          TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'planned', 'active', 'waiting', 'done', 'dismissed')),
    priority       TEXT NOT NULL DEFAULT 'none' CHECK (priority IN ('p0', 'p1', 'p2', 'p3', 'none')),
    project_id     TEXT REFERENCES projects(id) ON DELETE SET NULL,
    project_hint   TEXT NOT NULL DEFAULT '',
    next_action    TEXT NOT NULL DEFAULT '',
    blocked_reason TEXT NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL,
    completed_at   DATETIME
);
CREATE INDEX IF NOT EXISTS idx_demands_status_updated ON demands(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_demands_project_updated ON demands(project_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS demand_sources (
    id           TEXT PRIMARY KEY,
    demand_id    TEXT NOT NULL REFERENCES demands(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    external_id  TEXT NOT NULL DEFAULT '',
    chat_id      TEXT NOT NULL DEFAULT '',
    chat_name    TEXT NOT NULL DEFAULT '',
    sender_id    TEXT NOT NULL DEFAULT '',
    sender_name  TEXT NOT NULL DEFAULT '',
    message_time DATETIME,
    url          TEXT NOT NULL DEFAULT '',
    excerpt      TEXT NOT NULL DEFAULT '',
    dedupe_key   TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_demand_sources_demand ON demand_sources(demand_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_demand_sources_dedupe
    ON demand_sources(dedupe_key) WHERE dedupe_key <> '';

CREATE TABLE IF NOT EXISTS demand_progress (
    id         TEXT PRIMARY KEY,
    demand_id  TEXT NOT NULL REFERENCES demands(id) ON DELETE CASCADE,
    text       TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_demand_progress_demand ON demand_progress(demand_id, created_at DESC);

CREATE TABLE IF NOT EXISTS feishu_sync_state (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    enabled         INTEGER NOT NULL DEFAULT 0,
    doc_token       TEXT NOT NULL DEFAULT '',
    doc_url         TEXT NOT NULL DEFAULT '',
    dirty           INTEGER NOT NULL DEFAULT 0,
    desired_version INTEGER NOT NULL DEFAULT 0,
    synced_version  INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'disabled',
    last_revision   TEXT NOT NULL DEFAULT '',
    last_hash       TEXT NOT NULL DEFAULT '',
    last_success_at DATETIME,
    last_error      TEXT NOT NULL DEFAULT '',
    retry_count     INTEGER NOT NULL DEFAULT 0,
    next_retry_at   DATETIME,
    updated_at      DATETIME NOT NULL
);
INSERT OR IGNORE INTO feishu_sync_state (id, updated_at) VALUES (1, CURRENT_TIMESTAMP);
`

const appSettingsSchema = `
CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);
`

const feishuIngestionSchema = `
ALTER TABLE demand_progress ADD COLUMN dedupe_key TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_demand_progress_dedupe
    ON demand_progress(dedupe_key) WHERE dedupe_key <> '';

CREATE TABLE IF NOT EXISTS feishu_ingestion_state (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    enabled              INTEGER NOT NULL DEFAULT 0,
    timezone             TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    active_start         TEXT NOT NULL DEFAULT '09:00',
    active_end           TEXT NOT NULL DEFAULT '23:00',
    interval_minutes     INTEGER NOT NULL DEFAULT 30,
    overlap_minutes      INTEGER NOT NULL DEFAULT 5,
    excluded_chat_ids    TEXT NOT NULL DEFAULT '[]',
    last_success_end     DATETIME,
    last_backfill_at     DATETIME,
    next_run_at          DATETIME,
    status               TEXT NOT NULL DEFAULT 'disabled',
    last_error           TEXT NOT NULL DEFAULT '',
    current_run_id       TEXT NOT NULL DEFAULT '',
    lease_until          DATETIME,
    requested            INTEGER NOT NULL DEFAULT 0,
    updated_at           DATETIME NOT NULL
);
INSERT OR IGNORE INTO feishu_ingestion_state (id, updated_at) VALUES (1, CURRENT_TIMESTAMP);

CREATE TABLE IF NOT EXISTS feishu_ingestion_runs (
    id                    TEXT PRIMARY KEY,
    trigger               TEXT NOT NULL,
    status                TEXT NOT NULL,
    window_start          DATETIME NOT NULL,
    window_end            DATETIME NOT NULL,
    messages_seen         INTEGER NOT NULL DEFAULT 0,
    messages_candidate    INTEGER NOT NULL DEFAULT 0,
    created               INTEGER NOT NULL DEFAULT 0,
    updated               INTEGER NOT NULL DEFAULT 0,
    skipped               INTEGER NOT NULL DEFAULT 0,
    review_count          INTEGER NOT NULL DEFAULT 0,
    missing_context_count INTEGER NOT NULL DEFAULT 0,
    input_tokens          INTEGER NOT NULL DEFAULT 0,
    cached_input_tokens   INTEGER NOT NULL DEFAULT 0,
    output_tokens         INTEGER NOT NULL DEFAULT 0,
    started_at            DATETIME NOT NULL,
    finished_at           DATETIME,
    error                 TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_feishu_ingestion_runs_started
    ON feishu_ingestion_runs(started_at DESC);

CREATE TABLE IF NOT EXISTS feishu_ingestion_seen (
    message_id TEXT PRIMARY KEY,
    seen_at    DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_feishu_ingestion_seen_at
    ON feishu_ingestion_seen(seen_at DESC);

CREATE TABLE IF NOT EXISTS demand_reviews (
    id                  TEXT PRIMARY KEY,
    kind                TEXT NOT NULL DEFAULT 'progress',
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'dismissed')),
    suggested_demand_id TEXT REFERENCES demands(id) ON DELETE SET NULL,
    progress_text       TEXT NOT NULL,
    source_json         TEXT NOT NULL,
    progress_dedupe_key TEXT NOT NULL DEFAULT '',
    confidence          REAL NOT NULL DEFAULT 0,
    rationale           TEXT NOT NULL DEFAULT '',
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL,
    resolved_at         DATETIME
);
CREATE INDEX IF NOT EXISTS idx_demand_reviews_status_created
    ON demand_reviews(status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_demand_reviews_progress_dedupe
    ON demand_reviews(progress_dedupe_key) WHERE progress_dedupe_key <> '';
`

// SQLite time formats we accept when scanning. modernc.org/sqlite serializes
// time.Time as RFC3339Nano by default, but we also accept the Go stdlib
// "2006-01-02 15:04:05" form in case rows were inserted by other tooling.
var sqliteTimeFormats = []string{
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05Z07:00",
}

func Open(path string) (*Store, error) {
	if err := EnsureParentDir(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_time_format=sqlite")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// modernc defaults are safe, but _time_format=sqlite asks it to store in
	// the "2006-01-02 15:04:05" form preferred by the spec.
	db.SetMaxOpenConns(1) // SQLite + WAL is safest in WAL mode with a single writer.
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// migrate brings the database schema up to schemaVersion, tracked via
// PRAGMA user_version. It is the single centralized init/migration path:
//
//   - A database at the current version is left untouched (idempotent no-op).
//   - A database newer than this binary supports is rejected with a clear
//     contextual error and is NOT modified — no silent downgrade or data change.
//   - Older/unversioned databases (user_version = 0) are migrated forward in
//     ordered steps. The 0 -> 1 step applies the baseline schema
//     (CREATE IF NOT EXISTS) and stamps the version, preserving any existing
//     rows created by earlier releases.
func migrate(db *sql.DB) error {
	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current == schemaVersion {
		return nil
	}
	if current > schemaVersion {
		return fmt.Errorf(
			"database schema version %d is newer than supported version %d; "+
				"upgrade infowall or restore a compatible backup (the database was left unchanged)",
			current, schemaVersion,
		)
	}

	// Apply ordered migration steps from `current` up to schemaVersion.
	for v := current; v < schemaVersion; v++ {
		if err := migrateStep(db, v); err != nil {
			return fmt.Errorf("migrate schema %d -> %d: %w", v, v+1, err)
		}
		// user_version does not accept a bound parameter; the value is our own
		// trusted integer constant, so formatting it inline is safe.
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			return fmt.Errorf("stamp schema version %d: %w", v+1, err)
		}
	}
	return nil
}

// migrateStep applies the change that moves the schema from version `from` to
// `from+1`. Add a new case here (and bump schemaVersion) for each future change.
func migrateStep(db *sql.DB, from int) error {
	switch from {
	case 0:
		// 0 -> 1: baseline schema. CREATE IF NOT EXISTS is safe against both a
		// brand-new database and an unversioned one that earlier releases already
		// populated, so existing items/pins/raw content are preserved.
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("apply baseline schema: %w", err)
		}
		return nil
	case 1:
		// 1 -> 2: add the personal-workbench tables alongside the existing
		// feed items. No items table is rebuilt or rewritten.
		if _, err := db.Exec(workbenchSchema); err != nil {
			return fmt.Errorf("apply workbench schema: %w", err)
		}
		return nil
	case 2:
		// 2 -> 3: add small, globally persisted application preferences. This
		// table is independent of feed and workbench data.
		if _, err := db.Exec(appSettingsSchema); err != nil {
			return fmt.Errorf("apply app settings schema: %w", err)
		}
		return nil
	case 3:
		// 3 -> 4: add automated Feishu chat ingestion state, run audit,
		// ambiguous-progress review items, and retry-safe progress keys.
		if _, err := db.Exec(feishuIngestionSchema); err != nil {
			return fmt.Errorf("apply Feishu ingestion schema: %w", err)
		}
		return nil
	case 4:
		// 4 -> 5: store named, directly navigable resources on individual
		// progress entries and backfill them from existing evidence URLs.
		if err := migrateProgressLinks(db); err != nil {
			return fmt.Errorf("apply demand progress links schema: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("no migration defined from version %d", from)
	}
}

// EnsureParentDir makes the parent directory of a SQLite DB path exist, so a
// nested target like "data/sub/infowall.db" is handled deliberately rather than
// failing obscurely at open time. It returns a clear, path-contextual error if
// the path is unusable (e.g. the target itself is an existing directory, or a
// parent component is a file). Special in-memory/empty paths are passed through
// untouched — there is no silent temp/in-memory fallback for real file paths.
func EnsureParentDir(path string) error {
	if path == "" {
		return fmt.Errorf("empty database path")
	}
	// Leave SQLite special targets (":memory:", "file::memory:", DSN URIs) alone.
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return fmt.Errorf("database path %s is a directory, expected a file", path)
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if fi, err := os.Stat(dir); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("database parent %s exists but is not a directory", dir)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat database parent %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database parent %s: %w", dir, err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// Count returns the number of items currently stored.
func (s *Store) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM items`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Backup writes a consistent snapshot of the database to outPath using SQLite's
// `VACUUM INTO`. This is safe to run against a live, WAL-mode database (it takes
// a read transaction and writes a fully-checkpointed, defragmented copy), unlike
// a raw file copy which may miss un-checkpointed WAL pages. VACUUM INTO refuses
// to write to a path that already exists, so we surface that as a clear error
// rather than risk clobbering an existing backup. The parent directory of
// outPath is created if needed.
func (s *Store) Backup(ctx context.Context, outPath string) error {
	if outPath == "" {
		return fmt.Errorf("empty backup output path")
	}
	if _, err := os.Stat(outPath); err == nil {
		return fmt.Errorf("backup target %s already exists (refusing to overwrite; choose a new path)", outPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat backup target %s: %w", outPath, err)
	}
	if err := EnsureParentDir(outPath); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, outPath); err != nil {
		return fmt.Errorf("backup to %s: %w", outPath, err)
	}
	return nil
}

func (s *Store) Insert(ctx context.Context, it *model.Item) error {
	tagsJSON, _ := json.Marshal(it.Tags)
	metaJSON, _ := json.Marshal(it.Meta)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO items (id, type, title, tags, meta, body, raw, pinned, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ID, it.Type, it.Title, string(tagsJSON), string(metaJSON), it.Body, it.Raw,
		boolInt(it.Pinned), it.CreatedAt.UTC(),
	)
	return err
}

// List returns items ordered by pinned DESC, created_at DESC, id DESC.
func (s *Store) List(ctx context.Context, limit, offset int, typeFilter string) ([]*model.Item, error) {
	page, err := s.ListPage(ctx, ListOptions{
		Limit:      limit,
		Offset:     offset,
		TypeFilter: typeFilter,
	})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// ListOptions controls a page of items from the feed history.
type ListOptions struct {
	Limit      int
	Offset     int
	TypeFilter string
	// Query is a free-text search. Each whitespace-separated term must match
	// (case-insensitive substring) somewhere in title/body/type/tags/meta.
	Query string
	// PinnedOnly restricts results to pinned items when true.
	PinnedOnly bool
	After      *ListCursor
}

// ListCursor identifies the last item from a previous page. It follows the same
// ordering as ListPage: pinned DESC, created_at DESC, id DESC.
type ListCursor struct {
	Pinned    bool
	CreatedAt time.Time
	ID        string
}

// ListPageResult is one deterministic page of items plus the next cursor.
type ListPageResult struct {
	Items      []*model.Item
	NextCursor *ListCursor
	HasMore    bool
}

// ListPage returns items ordered by pinned DESC, created_at DESC, id DESC. If
// After is set, it uses keyset pagination from that cursor; otherwise Offset is
// honored for backwards-compatible clients.
func (s *Store) ListPage(ctx context.Context, opts ListOptions) (*ListPageResult, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	q := `SELECT id, type, title, tags, meta, body, pinned, created_at
	      FROM items`
	args := []any{}
	var where []string
	if opts.TypeFilter != "" {
		where = append(where, "type = ?")
		args = append(args, opts.TypeFilter)
	}
	if opts.PinnedOnly {
		where = append(where, "pinned = 1")
	}
	// Full-history text search: each whitespace-separated term must appear
	// (case-insensitive substring) in the concatenated searchable columns
	// (title/body/type/tags/meta). tags/meta are stored as JSON text, so this
	// also matches their string contents. ESCAPE makes %/_ in a term literal.
	for _, term := range searchTerms(opts.Query) {
		where = append(where,
			`lower(title || ' ' || body || ' ' || type || ' ' || tags || ' ' || meta) LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(strings.ToLower(term))+"%")
	}
	if opts.After != nil {
		where = append(where, `(pinned < ? OR (pinned = ? AND (created_at < ? OR (created_at = ? AND id < ?))))`)
		pinned := boolInt(opts.After.Pinned)
		args = append(args, pinned, pinned, opts.After.CreatedAt.UTC(), opts.After.CreatedAt.UTC(), opts.After.ID)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY pinned DESC, created_at DESC, id DESC LIMIT ?"
	args = append(args, limit+1)
	if opts.After == nil {
		q += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*model.Item
	for rows.Next() {
		it, err := scanItem(rows.Scan, false)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	res := &ListPageResult{}
	if len(items) > limit {
		res.HasMore = true
		items = items[:limit]
	}
	res.Items = items
	if res.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		res.NextCursor = &ListCursor{
			Pinned:    last.Pinned,
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		}
	}
	return res, nil
}

func (s *Store) Get(ctx context.Context, id string) (*model.Item, error) {
	return scanItem(func(dests ...any) error {
		return s.db.QueryRowContext(ctx,
			`SELECT id, type, title, tags, meta, body, raw, pinned, created_at FROM items WHERE id = ?`, id,
		).Scan(dests...)
	}, true)
}

// scanItem is a helper that knows how to scan a row into a *model.Item, tolerating
// either a time.Time or a string for created_at depending on driver behaviour.
func scanItem(scan func(...any) error, withRaw bool) (*model.Item, error) {
	var (
		it           model.Item
		tagsJSON     string
		metaJSON     string
		pinnedInt    int
		createdAtRaw any
	)
	dests := []any{&it.ID, &it.Type, &it.Title, &tagsJSON, &metaJSON, &it.Body}
	if withRaw {
		dests = append(dests, &it.Raw)
	}
	dests = append(dests, &pinnedInt, &createdAtRaw)
	if err := scan(dests...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !withRaw {
		it.Raw = ""
	}
	if err := json.Unmarshal([]byte(tagsJSON), &it.Tags); err != nil {
		it.Tags = nil
	}
	if err := json.Unmarshal([]byte(metaJSON), &it.Meta); err != nil {
		it.Meta = map[string]any{}
	}
	it.Pinned = pinnedInt != 0
	it.CreatedAt = parseSQLiteTime(createdAtRaw)
	return &it, nil
}

func parseSQLiteTime(v any) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x.UTC()
	case string:
		for _, f := range sqliteTimeFormats {
			if t, err := time.Parse(f, x); err == nil {
				return t.UTC()
			}
		}
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func (s *Store) SetPinned(ctx context.Context, id string, pinned bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE items SET pinned = ? WHERE id = ?`, boolInt(pinned), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// LatestID returns the most recent item's id and creation time (for SSE catch-up).
func (s *Store) LatestID(ctx context.Context) (id string, at time.Time, err error) {
	var createdAtRaw any
	err = s.db.QueryRowContext(ctx,
		`SELECT id, created_at FROM items ORDER BY created_at DESC LIMIT 1`,
	).Scan(&id, &createdAtRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return id, parseSQLiteTime(createdAtRaw), nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// searchTerms splits a free-text query into non-empty whitespace-separated terms.
func searchTerms(query string) []string {
	return strings.Fields(query)
}

// likeEscape escapes the SQL LIKE wildcards so a term containing %, _, or the
// escape char itself is matched literally. Pair with `ESCAPE '\'` in the query.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

var ErrNotFound = errors.New("not found")
