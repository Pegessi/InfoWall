// Package store provides SQLite persistence for Items.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/infowall/infowall/internal/model"
)

type Store struct {
	db *sql.DB
}

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
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_time_format=sqlite")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// modernc defaults are safe, but _time_format=sqlite asks it to store in
	// the "2006-01-02 15:04:05" form preferred by the spec.
	db.SetMaxOpenConns(1) // SQLite + WAL is safest in WAL mode with a single writer.
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

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

// List returns items ordered by pinned DESC, created_at DESC.
func (s *Store) List(ctx context.Context, limit, offset int, typeFilter string) ([]*model.Item, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT id, type, title, tags, meta, body, pinned, created_at
	      FROM items`
	args := []any{}
	if typeFilter != "" {
		q += " WHERE type = ?"
		args = append(args, typeFilter)
	}
	q += " ORDER BY pinned DESC, created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

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
	return items, rows.Err()
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

var ErrNotFound = errors.New("not found")
