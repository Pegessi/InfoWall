package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/model"
)

func TestHealthDoesNotRequireAuth(t *testing.T) {
	srv, err := New(context.Background(), Config{
		DBPath: filepath.Join(t.TempDir(), "infowall.db"),
		APIKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	httpSrv := httptest.NewServer(srv.mux)
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("health ok = %#v, want true", payload["ok"])
	}
	if payload["service"] != "infowall" || payload["status"] != "ok" {
		t.Fatalf("unexpected health payload: %#v", payload)
	}
	for key := range payload {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "key") || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "item") || strings.Contains(lower, "db") {
			t.Fatalf("health payload exposes unsafe key %q in %#v", key, payload)
		}
	}

	protectedResp, err := http.Get(httpSrv.URL + "/api/items")
	if err != nil {
		t.Fatal(err)
	}
	defer protectedResp.Body.Close()
	if protectedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/items without auth status = %d, want 401", protectedResp.StatusCode)
	}
}

func TestListItemsCursorPagination(t *testing.T) {
	srv, err := New(context.Background(), Config{
		DBPath: filepath.Join(t.TempDir(), "infowall.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		insertServerItem(t, srv, id, base.Add(time.Duration(i)*time.Minute), false)
	}

	httpSrv := httptest.NewServer(srv.mux)
	defer httpSrv.Close()

	first := getItemsPage(t, httpSrv.URL+"/api/items?limit=2")
	if got := pageIDs(first); strings.Join(got, ",") != "e,d" {
		t.Fatalf("first page ids = %v, want e,d", got)
	}
	if !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page should have cursor: %+v", first)
	}

	insertServerItem(t, srv, "newest", base.Add(10*time.Minute), false)
	second := getItemsPage(t, httpSrv.URL+"/api/items?limit=2&cursor="+first.NextCursor)
	if got := pageIDs(second); strings.Join(got, ",") != "c,b" {
		t.Fatalf("second page ids = %v, want c,b", got)
	}
	if !second.HasMore || second.NextCursor == "" {
		t.Fatalf("second page should have cursor: %+v", second)
	}
}

func TestListItemsInvalidCursor(t *testing.T) {
	srv, err := New(context.Background(), Config{
		DBPath: filepath.Join(t.TempDir(), "infowall.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	httpSrv := httptest.NewServer(srv.mux)
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/api/items?cursor=not-a-valid-cursor")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestListItemsSearchAndPinnedParams(t *testing.T) {
	srv, err := New(context.Background(), Config{
		DBPath: filepath.Join(t.TempDir(), "infowall.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	// insertServerItem uses id as the title, so we can search by it.
	insertServerItem(t, srv, "alpha", base, true)
	insertServerItem(t, srv, "alphabet", base.Add(time.Minute), false)
	insertServerItem(t, srv, "beta", base.Add(2*time.Minute), false)

	httpSrv := httptest.NewServer(srv.mux)
	defer httpSrv.Close()

	// Backward-compat: no new params returns everything (3 items).
	all := getItemsPage(t, httpSrv.URL+"/api/items?limit=50")
	if len(all.Items) != 3 {
		t.Fatalf("no-param request should return 3, got %d", len(all.Items))
	}

	// q= searches the full history (substring, case-insensitive): alpha, alphabet.
	byQuery := getItemsPage(t, httpSrv.URL+"/api/items?limit=50&q=alpha")
	if got := pageIDs(byQuery); strings.Join(sortedCopy(got), ",") != "alpha,alphabet" {
		t.Fatalf("q=alpha ids = %v, want alpha,alphabet", got)
	}

	// pinned=1 restricts to pinned items.
	byPinned := getItemsPage(t, httpSrv.URL+"/api/items?limit=50&pinned=1")
	if got := pageIDs(byPinned); strings.Join(got, ",") != "alpha" {
		t.Fatalf("pinned=1 ids = %v, want alpha", got)
	}

	// q + pinned compose.
	combo := getItemsPage(t, httpSrv.URL+"/api/items?limit=50&q=alpha&pinned=1")
	if got := pageIDs(combo); strings.Join(got, ",") != "alpha" {
		t.Fatalf("q=alpha&pinned=1 ids = %v, want alpha", got)
	}

	// No match -> empty, no cursor.
	none := getItemsPage(t, httpSrv.URL+"/api/items?limit=50&q=zzznotfound")
	if len(none.Items) != 0 || none.HasMore {
		t.Fatalf("no-match query should be empty, got %d hasMore=%v", len(none.Items), none.HasMore)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

type itemsPage struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

func getItemsPage(t *testing.T, endpoint string) itemsPage {
	t.Helper()
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", endpoint, resp.StatusCode)
	}
	var page itemsPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	return page
}

func pageIDs(page itemsPage) []string {
	ids := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func insertServerItem(t *testing.T, srv *Server, id string, createdAt time.Time, pinned bool) {
	t.Helper()
	if err := srv.store.Insert(context.Background(), &model.Item{
		ID:        id,
		Type:      model.TypeNote,
		Title:     id,
		Tags:      []string{},
		Meta:      map[string]any{},
		CreatedAt: createdAt,
		Pinned:    pinned,
	}); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}
