package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/infowall/infowall/internal/model"
)

func TestEnsureParentDir(t *testing.T) {
	t.Run("creates nested parent directories", func(t *testing.T) {
		base := t.TempDir()
		path := filepath.Join(base, "a", "b", "c", "wall.db")
		if err := EnsureParentDir(path); err != nil {
			t.Fatalf("EnsureParentDir: %v", err)
		}
		if fi, err := os.Stat(filepath.Dir(path)); err != nil || !fi.IsDir() {
			t.Fatalf("parent dir not created: err=%v", err)
		}
	})

	t.Run("path that is an existing directory errors", func(t *testing.T) {
		dir := t.TempDir()
		if err := EnsureParentDir(dir); err == nil {
			t.Fatal("expected error when path is a directory")
		}
	})

	t.Run("parent component that is a file errors", func(t *testing.T) {
		base := t.TempDir()
		file := filepath.Join(base, "afile")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := EnsureParentDir(filepath.Join(file, "wall.db")); err == nil {
			t.Fatal("expected error when a parent component is a file")
		}
	})

	t.Run("plain filename in cwd is fine", func(t *testing.T) {
		if err := EnsureParentDir("infowall.db"); err != nil {
			t.Fatalf("plain filename should be accepted: %v", err)
		}
	})

	t.Run("empty path errors", func(t *testing.T) {
		if err := EnsureParentDir(""); err == nil {
			t.Fatal("expected error for empty path")
		}
	})
}

func TestOpenCreatesNestedPath(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "nested", "dir", "wall.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open nested: %v", err)
	}
	defer st.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("db file not created at nested path: %v", err)
	}
	n, err := st.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("fresh db should have 0 items, got %d", n)
	}
}

func TestCount(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if n, _ := st.Count(ctx); n != 0 {
		t.Fatalf("want 0, got %d", n)
	}
	insertItem(t, st, "a")
	insertItem(t, st, "b")
	if n, _ := st.Count(ctx); n != 2 {
		t.Fatalf("want 2, got %d", n)
	}
}

func TestBackup(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	insertItem(t, st, "a")
	insertItem(t, st, "b")
	insertItem(t, st, "c")

	out := filepath.Join(t.TempDir(), "sub", "backup.db")

	t.Run("creates a consistent backup with the same rows", func(t *testing.T) {
		if err := st.Backup(ctx, out); err != nil {
			t.Fatalf("Backup: %v", err)
		}
		// Parent dir should have been created.
		if _, err := os.Stat(out); err != nil {
			t.Fatalf("backup file missing: %v", err)
		}
		// Open the backup independently and verify the row count matches.
		bk, err := Open(out)
		if err != nil {
			t.Fatalf("open backup: %v", err)
		}
		defer bk.Close()
		n, err := bk.Count(ctx)
		if err != nil {
			t.Fatalf("count backup: %v", err)
		}
		if n != 3 {
			t.Fatalf("backup should have 3 rows, got %d", n)
		}
	})

	t.Run("refuses to overwrite an existing file", func(t *testing.T) {
		// out already exists from the previous subtest.
		err := st.Backup(ctx, out)
		if err == nil {
			t.Fatal("expected refuse-overwrite error")
		}
	})

	t.Run("empty out path errors", func(t *testing.T) {
		if err := st.Backup(ctx, ""); err == nil {
			t.Fatal("expected error for empty out path")
		}
	})
}

func TestListPageCursorDoesNotSkipAfterNewerInsert(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		insertItemAt(t, st, id, base.Add(time.Duration(i)*time.Minute), false)
	}

	first, err := st.ListPage(ctx, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if got := itemIDs(first.Items); strings.Join(got, ",") != "e,d" {
		t.Fatalf("first page ids = %v, want e,d", got)
	}
	if !first.HasMore || first.NextCursor == nil {
		t.Fatalf("first page should have a next cursor: %+v", first)
	}

	// Simulate an item.new SSE arriving between the initial page and Load more.
	// Offset pagination would now shift and duplicate/skip around the page
	// boundary; the cursor should continue strictly after "d".
	insertItemAt(t, st, "newest", base.Add(10*time.Minute), false)

	next, err := st.ListPage(ctx, ListOptions{Limit: 2, After: first.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if got := itemIDs(next.Items); strings.Join(got, ",") != "c,b" {
		t.Fatalf("second page ids = %v, want c,b", got)
	}
	if !next.HasMore || next.NextCursor == nil {
		t.Fatalf("second page should have a next cursor: %+v", next)
	}
}

func TestListPageCursorUsesStableIDTieBreak(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	at := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	insertItemAt(t, st, "a", at, false)
	insertItemAt(t, st, "b", at, false)
	insertItemAt(t, st, "c", at, false)

	first, err := st.ListPage(ctx, ListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if got := itemIDs(first.Items); strings.Join(got, ",") != "c" {
		t.Fatalf("first page ids = %v, want c", got)
	}
	if first.NextCursor == nil {
		t.Fatal("expected next cursor")
	}

	second, err := st.ListPage(ctx, ListOptions{Limit: 2, After: first.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if got := itemIDs(second.Items); strings.Join(got, ",") != "b,a" {
		t.Fatalf("second page ids = %v, want b,a", got)
	}
	if second.HasMore {
		t.Fatalf("second page should be end-of-feed: %+v", second)
	}
}

func TestListPageCursorKeepsPinnedOrdering(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	insertItemAt(t, st, "unpinned-new", base.Add(3*time.Minute), false)
	insertItemAt(t, st, "pinned-old", base, true)
	insertItemAt(t, st, "unpinned-old", base.Add(time.Minute), false)

	page, err := st.ListPage(ctx, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if got := itemIDs(page.Items); strings.Join(got, ",") != "pinned-old,unpinned-new" {
		t.Fatalf("first page ids = %v, want pinned-old,unpinned-new", got)
	}
	if page.NextCursor == nil {
		t.Fatal("expected next cursor")
	}
	next, err := st.ListPage(ctx, ListOptions{Limit: 2, After: page.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if got := itemIDs(next.Items); strings.Join(got, ",") != "unpinned-old" {
		t.Fatalf("second page ids = %v, want unpinned-old", got)
	}
}

// --- schema version / migration ---

// userVersion reads PRAGMA user_version from a standalone sqlite handle at path.
func userVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite %s: %v", path, err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return v
}

func TestFreshDBGetsCurrentSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wall.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	st.Close()
	if got := userVersion(t, path); got != schemaVersion {
		t.Fatalf("fresh DB user_version = %d, want %d", got, schemaVersion)
	}

	// Idempotent: reopening an already-current DB keeps the version.
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st2.Close()
	if got := userVersion(t, path); got != schemaVersion {
		t.Fatalf("after reopen user_version = %d, want %d", got, schemaVersion)
	}
}

func TestLegacyUnversionedDBMigratesAndPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Build a legacy database the way earlier releases did: create the current
	// items schema but DO NOT set user_version (so it reads 0), and seed rows
	// including a pinned item with non-empty raw content.
	raw, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(schema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO items (id, type, title, tags, meta, body, raw, pinned, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-1", "paper", "Legacy Paper", `["ml","x"]`, `{"k":"v"}`,
		"body text", "---\ntype: paper\n---\nraw source", 1, "2026-01-02 03:04:05",
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO items (id, type, title, tags, meta, body, raw, pinned, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-2", "note", "Legacy Note", `[]`, `{}`, "second", "", 0, "2026-01-03 03:04:05",
	); err != nil {
		t.Fatalf("seed legacy row 2: %v", err)
	}
	raw.Close()

	if got := userVersion(t, path); got != 0 {
		t.Fatalf("precondition: legacy DB should read user_version 0, got %d", got)
	}

	// Open through the store: it should migrate 0 -> current without data loss.
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy DB: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	if n, err := st.Count(ctx); err != nil || n != 2 {
		t.Fatalf("Count after migrate = %d (err %v), want 2", n, err)
	}

	// The pinned item with raw content must be intact field-for-field.
	got, err := st.Get(ctx, "legacy-1")
	if err != nil {
		t.Fatalf("Get legacy-1: %v", err)
	}
	if !got.Pinned {
		t.Fatalf("legacy-1 should remain pinned")
	}
	if got.Raw != "---\ntype: paper\n---\nraw source" {
		t.Fatalf("legacy-1 raw content changed: %q", got.Raw)
	}
	if got.Title != "Legacy Paper" || got.Type != "paper" {
		t.Fatalf("legacy-1 title/type changed: %q/%q", got.Title, got.Type)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "ml" {
		t.Fatalf("legacy-1 tags changed: %v", got.Tags)
	}

	if got := userVersion(t, path); got != schemaVersion {
		t.Fatalf("after migrate user_version = %d, want %d", got, schemaVersion)
	}
}

func TestNewerThanSupportedVersionFailsWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")

	raw, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO items (id, type, title, tags, meta, body, raw, pinned, created_at)
		 VALUES ('sentinel','note','Sentinel','[]','{}','b','',0,'2026-01-01 00:00:00')`,
	); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}
	future := schemaVersion + 1
	if _, err := raw.Exec(fmt.Sprintf("PRAGMA user_version = %d", future)); err != nil {
		t.Fatalf("set future version: %v", err)
	}
	raw.Close()

	st, err := Open(path)
	if err == nil {
		if st != nil {
			st.Close()
		}
		t.Fatal("expected Open to fail for a newer-than-supported schema version")
	}
	if st != nil {
		t.Fatalf("store should be nil on version error, got %v", st)
	}
	// Error should be contextual: mention both versions.
	msg := err.Error()
	if !strings.Contains(msg, fmt.Sprint(future)) || !strings.Contains(msg, fmt.Sprint(schemaVersion)) {
		t.Fatalf("error should name found (%d) and supported (%d) versions: %q", future, schemaVersion, msg)
	}

	// The file must be untouched: version still `future`, sentinel row present.
	if got := userVersion(t, path); got != future {
		t.Fatalf("user_version mutated on failed open: got %d, want %d", got, future)
	}
	verify, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen raw: %v", err)
	}
	defer verify.Close()
	var n int
	if err := verify.QueryRow("SELECT count(*) FROM items").Scan(&n); err != nil {
		t.Fatalf("count sentinel: %v", err)
	}
	if n != 1 {
		t.Fatalf("data mutated on failed open: %d rows, want 1", n)
	}
}

// --- helpers ---

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "wall.db"))
	if err != nil {
		t.Fatalf("open temp store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func insertItem(t *testing.T, st *Store, id string) {
	t.Helper()
	it := &model.Item{ID: id, Type: model.TypeNote, Title: id}
	if err := st.Insert(context.Background(), it); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func insertItemAt(t *testing.T, st *Store, id string, createdAt time.Time, pinned bool) {
	t.Helper()
	it := &model.Item{
		ID:        id,
		Type:      model.TypeNote,
		Title:     id,
		Tags:      []string{},
		Meta:      map[string]any{},
		CreatedAt: createdAt,
		Pinned:    pinned,
	}
	if err := st.Insert(context.Background(), it); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func itemIDs(items []*model.Item) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

// --- full-history search / filter ---

func insertFull(t *testing.T, st *Store, it *model.Item) {
	t.Helper()
	if it.Tags == nil {
		it.Tags = []string{}
	}
	if it.Meta == nil {
		it.Meta = map[string]any{}
	}
	if err := st.Insert(context.Background(), it); err != nil {
		t.Fatalf("insert %s: %v", it.ID, err)
	}
}

func TestListPageTextSearchAcrossFields(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	insertFull(t, st, &model.Item{ID: "title-hit", Type: "paper", Title: "Attention Transformer", Body: "seq", CreatedAt: base})
	insertFull(t, st, &model.Item{ID: "body-hit", Type: "note", Title: "Grocery", Body: "milk and a transformer toy", CreatedAt: base.Add(time.Minute)})
	insertFull(t, st, &model.Item{ID: "tag-hit", Type: "link", Title: "Deal", Tags: []string{"transformer"}, CreatedAt: base.Add(2 * time.Minute)})
	insertFull(t, st, &model.Item{ID: "meta-hit", Type: "link", Title: "Ref", Meta: map[string]any{"summary": "about transformers"}, CreatedAt: base.Add(3 * time.Minute)})
	insertFull(t, st, &model.Item{ID: "miss", Type: "note", Title: "Unrelated", Body: "nothing here", CreatedAt: base.Add(4 * time.Minute)})

	page, err := st.ListPage(ctx, ListOptions{Limit: 50, Query: "transformer"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	got := map[string]bool{}
	for _, id := range itemIDs(page.Items) {
		got[id] = true
	}
	for _, want := range []string{"title-hit", "body-hit", "tag-hit", "meta-hit"} {
		if !got[want] {
			t.Fatalf("query 'transformer' should match %s; got %v", want, itemIDs(page.Items))
		}
	}
	if got["miss"] {
		t.Fatalf("query 'transformer' should not match 'miss'")
	}
	if len(page.Items) != 4 {
		t.Fatalf("want 4 matches, got %d (%v)", len(page.Items), itemIDs(page.Items))
	}
}

func TestListPageSearchIsCaseInsensitiveAndMultiTermAnd(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	insertFull(t, st, &model.Item{ID: "both", Title: "Alpha Beta", CreatedAt: base})
	insertFull(t, st, &model.Item{ID: "one", Title: "Alpha only", CreatedAt: base.Add(time.Minute)})

	page, err := st.ListPage(ctx, ListOptions{Limit: 50, Query: "ALPHA beta"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if ids := itemIDs(page.Items); len(ids) != 1 || ids[0] != "both" {
		t.Fatalf("multi-term AND (case-insensitive) failed: got %v", ids)
	}
}

func TestListPageTypeAndPinnedComposeWithQuery(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	insertFull(t, st, &model.Item{ID: "paper-pin", Type: "paper", Title: "report alpha", Pinned: true, CreatedAt: base})
	insertFull(t, st, &model.Item{ID: "paper-nopin", Type: "paper", Title: "report beta", CreatedAt: base.Add(time.Minute)})
	insertFull(t, st, &model.Item{ID: "note-pin", Type: "note", Title: "report gamma", Pinned: true, CreatedAt: base.Add(2 * time.Minute)})

	page, err := st.ListPage(ctx, ListOptions{Limit: 50, Query: "report", TypeFilter: "paper", PinnedOnly: true})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if ids := itemIDs(page.Items); len(ids) != 1 || ids[0] != "paper-pin" {
		t.Fatalf("type+pinned+query compose failed: got %v", ids)
	}

	pinnedPage, _ := st.ListPage(ctx, ListOptions{Limit: 50, PinnedOnly: true})
	if len(pinnedPage.Items) != 2 {
		t.Fatalf("pinnedOnly should return 2, got %d", len(pinnedPage.Items))
	}
}

func TestListPageSearchPaginatesAcrossFilteredSet(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	// 5 matching items interleaved with 5 non-matching ones.
	for i := 0; i < 5; i++ {
		insertFull(t, st, &model.Item{ID: fmt.Sprintf("m%d", i), Title: fmt.Sprintf("keeper %d", i), CreatedAt: base.Add(time.Duration(2*i) * time.Minute)})
		insertFull(t, st, &model.Item{ID: fmt.Sprintf("x%d", i), Title: fmt.Sprintf("other %d", i), CreatedAt: base.Add(time.Duration(2*i+1) * time.Minute)})
	}

	seen := map[string]int{}
	var cursor *ListCursor
	pages := 0
	for {
		page, err := st.ListPage(ctx, ListOptions{Limit: 2, Query: "keeper", After: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, it := range page.Items {
			seen[it.ID]++
			if !strings.HasPrefix(it.Title, "keeper") {
				t.Fatalf("non-matching item leaked into filtered page: %s", it.ID)
			}
		}
		pages++
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
		if cursor == nil {
			t.Fatal("HasMore true but nil cursor")
		}
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("expected 5 distinct matches across pages, got %d (%v)", len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("item %s returned %d times (skip/dup across filtered pages)", id, n)
		}
	}
}

func TestListPageSearchTreatsWildcardsLiterally(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	insertFull(t, st, &model.Item{ID: "pct", Title: "50%_off sale", CreatedAt: base})
	insertFull(t, st, &model.Item{ID: "plain", Title: "regular price", CreatedAt: base.Add(time.Minute)})

	page, err := st.ListPage(ctx, ListOptions{Limit: 50, Query: "50%_off"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if ids := itemIDs(page.Items); len(ids) != 1 || ids[0] != "pct" {
		t.Fatalf("wildcard-literal match failed: got %v", ids)
	}
}

func TestListPageEmptyQueryIsUnfiltered(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	insertFull(t, st, &model.Item{ID: "a", Title: "one", CreatedAt: base})
	insertFull(t, st, &model.Item{ID: "b", Title: "two", CreatedAt: base.Add(time.Minute)})

	for _, q := range []string{"", "   "} {
		page, err := st.ListPage(ctx, ListOptions{Limit: 50, Query: q})
		if err != nil {
			t.Fatalf("empty query %q: %v", q, err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("empty query %q should return all 2, got %d", q, len(page.Items))
		}
	}
}

func TestListPageSearchNoMatches(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	insertFull(t, st, &model.Item{ID: "a", Title: "hello", CreatedAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)})
	page, err := st.ListPage(ctx, ListOptions{Limit: 50, Query: "zzznotfound"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 0 || page.HasMore {
		t.Fatalf("no-match query should return empty page, got %d items hasMore=%v", len(page.Items), page.HasMore)
	}
}
