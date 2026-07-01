package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandSources(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "b.md"), "# B")
	mustWrite(t, filepath.Join(dir, "a.markdown"), "# A")

	single := filepath.Join(dir, "b.md")
	other := filepath.Join(dir, "a.markdown")

	t.Run("directory is rejected as a per-source error", func(t *testing.T) {
		srcs, err := expandSources([]string{dir})
		if err != nil {
			t.Fatalf("expandSources should not hard-fail: %v", err)
		}
		if len(srcs) != 1 || srcs[0].err == nil {
			t.Fatalf("expected a single errored source for a directory, got %+v", srcs)
		}
		if _, rerr := srcs[0].read(); rerr == nil {
			t.Fatal("read() should report the directory error")
		}
	})

	t.Run("explicit file used as-is", func(t *testing.T) {
		srcs, err := expandSources([]string{single})
		if err != nil {
			t.Fatal(err)
		}
		if len(srcs) != 1 || srcs[0].path != single {
			t.Fatalf("unexpected: %+v", srcs)
		}
	})

	t.Run("dash means stdin", func(t *testing.T) {
		srcs, err := expandSources([]string{"-"})
		if err != nil {
			t.Fatal(err)
		}
		if len(srcs) != 1 || !srcs[0].isStdin {
			t.Fatalf("expected stdin source, got %+v", srcs)
		}
	})

	t.Run("multiple files + stdin preserve order", func(t *testing.T) {
		srcs, err := expandSources([]string{single, "-", other})
		if err != nil {
			t.Fatal(err)
		}
		if len(srcs) != 3 {
			t.Fatalf("want 3 sources, got %d: %+v", len(srcs), srcs)
		}
		if srcs[0].path != single || !srcs[1].isStdin || srcs[2].path != other {
			t.Fatalf("order not preserved: %+v", srcs)
		}
	})

	t.Run("missing path surfaces as per-source error", func(t *testing.T) {
		srcs, err := expandSources([]string{filepath.Join(dir, "nope.md")})
		if err != nil {
			t.Fatalf("expandSources should not hard-fail: %v", err)
		}
		if len(srcs) != 1 || srcs[0].err == nil {
			t.Fatalf("expected a single source carrying an error, got %+v", srcs)
		}
		if _, rerr := srcs[0].read(); rerr == nil {
			t.Fatal("read() should report the missing-file error")
		}
	})

	t.Run("bad path does not drop later valid sources", func(t *testing.T) {
		srcs, err := expandSources([]string{filepath.Join(dir, "nope.md"), single})
		if err != nil {
			t.Fatal(err)
		}
		if len(srcs) != 2 {
			t.Fatalf("want 2 sources, got %d: %+v", len(srcs), srcs)
		}
		if srcs[0].err == nil {
			t.Fatal("first source should carry an error")
		}
		if srcs[1].err != nil || srcs[1].path != single {
			t.Fatalf("second valid source damaged: %+v", srcs[1])
		}
	})
}

func TestApplyFrontmatter(t *testing.T) {
	t.Run("no type/pin returns input unchanged", func(t *testing.T) {
		in := []byte("# hello\n")
		out := applyFrontmatter(in, "", false)
		if !bytes.Equal(in, out) {
			t.Fatalf("expected unchanged, got %q", out)
		}
	})

	t.Run("prepends topic frontmatter when none present", func(t *testing.T) {
		out := string(applyFrontmatter([]byte("# hello\n"), "link", true))
		if !strings.HasPrefix(out, "---\n") {
			t.Fatalf("expected frontmatter fence, got %q", out)
		}
		if !strings.Contains(out, "topic: link") || !strings.Contains(out, "pinned: true") {
			t.Fatalf("missing injected fields: %q", out)
		}
		if strings.Contains(out, "type:") {
			t.Fatalf("new frontmatter should use topic, got %q", out)
		}
		if !strings.Contains(out, "# hello") {
			t.Fatalf("body lost: %q", out)
		}
	})

	t.Run("injects into existing type frontmatter without duplicating", func(t *testing.T) {
		in := []byte("---\ntitle: Hi\ntype: paper\n---\n\nbody\n")
		out := string(applyFrontmatter(in, "note", true))
		// type already present → must NOT be overridden or duplicated.
		if strings.Count(out, "type:") != 1 {
			t.Fatalf("type should not be duplicated/overridden: %q", out)
		}
		if !strings.Contains(out, "type: paper") {
			t.Fatalf("existing type changed: %q", out)
		}
		if !strings.Contains(out, "pinned: true") {
			t.Fatalf("pinned not injected: %q", out)
		}
		if !strings.Contains(out, "title: Hi") || !strings.Contains(out, "body") {
			t.Fatalf("frontmatter/body damaged: %q", out)
		}
	})

	t.Run("injects into existing topic frontmatter without duplicating", func(t *testing.T) {
		in := []byte("---\ntitle: Hi\ntopic: paper\n---\n\nbody\n")
		out := string(applyFrontmatter(in, "note", true))
		if strings.Count(out, "topic:") != 1 {
			t.Fatalf("topic should not be duplicated/overridden: %q", out)
		}
		if !strings.Contains(out, "topic: paper") {
			t.Fatalf("existing topic changed: %q", out)
		}
		if strings.Contains(out, "type:") {
			t.Fatalf("existing topic should not gain type alias: %q", out)
		}
		if !strings.Contains(out, "pinned: true") {
			t.Fatalf("pinned not injected: %q", out)
		}
	})
}

func TestPushOneEndToEnd(t *testing.T) {
	var gotBody []byte
	var gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "abcdef1234567890", "type": "note", "title": "Hello"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "n.md")
	mustWrite(t, file, "# Hello\n\nworld")

	cfg := &clientConfig{server: srv.URL, apiKey: "secret"}
	res := pushOne(source{label: file, path: file}, "", false, cfg)
	if res.Error != "" {
		t.Fatalf("push failed: %s", res.Error)
	}
	if res.ID != "abcdef1234567890" || res.Title != "Hello" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth header not sent: %q", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "text/markdown") {
		t.Fatalf("unexpected content-type: %q", gotCT)
	}
	if !strings.Contains(string(gotBody), "# Hello") {
		t.Fatalf("body not forwarded: %q", gotBody)
	}
}

func TestPushOneServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"unauthorized"}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "n.md")
	mustWrite(t, file, "# Hello")

	cfg := &clientConfig{server: srv.URL}
	res := pushOne(source{label: file, path: file}, "", false, cfg)
	if res.Error == "" {
		t.Fatal("expected error result for 401")
	}
	if !strings.Contains(res.Error, "401") {
		t.Fatalf("error should mention status: %q", res.Error)
	}
}

func TestExportCommandJSONLinesFetchesRawItems(t *testing.T) {
	var sawList bool
	var gotItemIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("auth header not sent: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/items":
			sawList = true
			if r.Method != http.MethodGet {
				t.Fatalf("list should be GET, got %s", r.Method)
			}
			if r.URL.Query().Get("limit") != "200" || r.URL.Query().Get("offset") != "0" {
				t.Fatalf("unexpected list pagination: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("type") != "paper" {
				t.Fatalf("topic filter should be sent as type=paper, got %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "paper-1"},
					{"id": "paper-2"},
				},
			})
		case "/api/items/paper-1", "/api/items/paper-2":
			if r.URL.Query().Get("raw") != "1" {
				t.Fatalf("item fetch should include raw=1, got %s", r.URL.RawQuery)
			}
			id := strings.TrimPrefix(r.URL.Path, "/api/items/")
			gotItemIDs = append(gotItemIDs, id)
			json.NewEncoder(w).Encode(map[string]any{
				"id":         id,
				"type":       "paper",
				"title":      "Paper " + id,
				"tags":       []string{"ml"},
				"pinned":     id == "paper-1",
				"meta":       map[string]any{"url": "https://example.test/" + id},
				"body":       "Rendered " + id,
				"raw":        "# Raw " + id,
				"created_at": "2026-07-01T00:00:00Z",
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdExport([]string{"--server", srv.URL + "/", "--api-key", "secret", "--topic", "paper"})
	})
	if err != nil {
		t.Fatalf("export failed: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty on success, got %q", stderr)
	}
	if strings.Contains(stdout, "secret") {
		t.Fatalf("export output leaked secret: %q", stdout)
	}
	if !sawList || strings.Join(gotItemIDs, ",") != "paper-1,paper-2" {
		t.Fatalf("unexpected export requests: sawList=%v ids=%v", sawList, gotItemIDs)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two JSONL lines, got %d: %q", len(lines), stdout)
	}
	var first exportItem
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode first JSONL line: %v", err)
	}
	if first.ID != "paper-1" || first.Raw != "# Raw paper-1" || first.Body == "" || !first.Pinned {
		t.Fatalf("raw/body/pinned metadata not preserved: %+v", first)
	}
	if first.Meta["url"] != "https://example.test/paper-1" {
		t.Fatalf("meta url not preserved: %+v", first.Meta)
	}
}

func TestExportCommandJSONSelectedIDsSkipsList(t *testing.T) {
	var sawList bool
	var gotIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/items":
			sawList = true
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/items/a", "/api/items/b":
			if r.URL.Query().Get("raw") != "1" {
				t.Fatalf("item fetch should include raw=1, got %s", r.URL.RawQuery)
			}
			id := strings.TrimPrefix(r.URL.Path, "/api/items/")
			gotIDs = append(gotIDs, id)
			json.NewEncoder(w).Encode(map[string]any{
				"id":         id,
				"type":       "note",
				"title":      "Title " + id,
				"tags":       []string{},
				"pinned":     false,
				"meta":       map[string]any{"source": "selected"},
				"body":       "Body " + id,
				"raw":        "# Title " + id,
				"created_at": "2026-07-01T00:00:00Z",
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdExport([]string{"--server", srv.URL, "--json", "a", "b"})
	})
	if err != nil {
		t.Fatalf("export failed: %v\nstderr=%s", err, stderr)
	}
	if sawList {
		t.Fatal("explicit id export should not call list endpoint")
	}
	if strings.Join(gotIDs, ",") != "a,b" {
		t.Fatalf("selected ids fetched out of order: %v", gotIDs)
	}

	var archive exportArchive
	if err := json.Unmarshal([]byte(stdout), &archive); err != nil {
		t.Fatalf("decode export JSON: %v\nstdout=%s", err, stdout)
	}
	if archive.Format != "json" || archive.Count != 2 || len(archive.Items) != 2 {
		t.Fatalf("unexpected export archive: %+v", archive)
	}
	if archive.Items[1].Raw != "# Title b" || archive.Items[1].Meta["source"] != "selected" {
		t.Fatalf("selected item metadata not preserved: %+v", archive.Items[1])
	}
}

func TestExportCommandOutWritesJSONLAndRefusesOverwrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/items":
			json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "x"}}})
		case "/api/items/x":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "x",
				"type":       "link",
				"title":      "Link",
				"tags":       []string{"archive"},
				"pinned":     false,
				"meta":       map[string]any{"url": "https://example.test"},
				"body":       "Body",
				"raw":        "---\ntopic: link\n---\n\nBody",
				"created_at": "2026-07-01T00:00:00Z",
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "archive", "items.jsonl")
	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdExport([]string{"--server", srv.URL, "--out", out, "--json"})
	})
	if err != nil {
		t.Fatalf("export failed: %v\nstderr=%s", err, stderr)
	}
	var summary exportSummary
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil {
		t.Fatalf("decode summary JSON: %v\nstdout=%s", err, stdout)
	}
	if !summary.OK || summary.Count != 1 || summary.Format != "jsonl" || summary.Bytes <= 0 {
		t.Fatalf("unexpected export summary: %+v", summary)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	if !strings.Contains(string(data), `"raw":"---\ntopic: link`) || !strings.Contains(string(data), `"url":"https://example.test"`) {
		t.Fatalf("export file did not preserve raw/meta: %s", data)
	}

	stdout2, stderr2, err2 := captureCommandOutput(t, func() error {
		return cmdExport([]string{"--server", srv.URL, "--out", out, "--json"})
	})
	if err2 == nil {
		t.Fatal("expected refuse-overwrite error")
	}
	if strings.TrimSpace(stdout2) != "" {
		t.Fatalf("stdout should be empty on error, got %q", stdout2)
	}
	if !strings.Contains(stderr2, "\"error\"") || !strings.Contains(stderr2, "refusing to overwrite") {
		t.Fatalf("expected JSON refuse-overwrite error, got %q", stderr2)
	}
}

func TestExportCommandJSONFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"unauthorized"}`)
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdExport([]string{"--server", srv.URL, "--json"})
	})
	if err == nil {
		t.Fatal("expected export auth failure")
	}
	if stdout != "" {
		t.Fatalf("stdout should be empty on JSON failure, got %q", stdout)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(stderr), &got); err != nil {
		t.Fatalf("stderr should be JSON error, got %q: %v", stderr, err)
	}
	if !strings.Contains(got["error"], "server 401") {
		t.Fatalf("error should mention status, got %q", got["error"])
	}
}

func TestHealthCommandJSONSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/health" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("auth header not sent through shared client flags: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"service": "infowall",
			"status":  "ok",
			"ts":      "2026-07-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdHealth([]string{"--server", srv.URL + "/", "--api-key", "secret", "--json"})
	})
	if err != nil {
		t.Fatalf("health failed: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty on success, got %q", stderr)
	}
	if strings.Contains(stdout, "secret") {
		t.Fatalf("health output leaked secret: %q", stdout)
	}

	var got healthResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid health JSON %q: %v", stdout, err)
	}
	if !got.OK || !got.Reachable || got.HTTPStatus != http.StatusOK {
		t.Fatalf("unexpected health result: %+v", got)
	}
	if got.Server != srv.URL || got.Service != "infowall" || got.ServiceStatus != "ok" {
		t.Fatalf("unexpected health fields: %+v", got)
	}
	if got.Version == "" || got.Commit == "" {
		t.Fatalf("version/commit should be included when available: %+v", got)
	}
}

func TestHealthCommandJSONFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"starting"}`)
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdHealth([]string{"--server", srv.URL, "--json"})
	})
	if err == nil {
		t.Fatal("expected health failure")
	}
	if stdout != "" {
		t.Fatalf("stdout should be empty on JSON failure, got %q", stdout)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stderr), &got); err != nil {
		t.Fatalf("stderr should be JSON error, got %q: %v", stderr, err)
	}
	if !strings.Contains(got["error"], "server 503") {
		t.Fatalf("error should mention server status, got %q", got["error"])
	}
}

func TestDoctorCommandJSONSuccess(t *testing.T) {
	var sawHealth, sawItems bool
	var gotHealthAuth, gotItemsAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			sawHealth = true
			gotHealthAuth = r.Header.Get("Authorization")
			json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"service": "infowall",
				"status":  "ok",
				"ts":      "2026-07-01T00:00:00Z",
			})
		case "/api/items":
			sawItems = true
			gotItemsAuth = r.Header.Get("Authorization")
			if r.URL.Query().Get("limit") != "1" {
				t.Fatalf("items probe should use limit=1, got %q", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdDoctor([]string{"--server", srv.URL + "/", "--api-key", "secret", "--json"})
	})
	if err != nil {
		t.Fatalf("doctor failed: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty on success, got %q", stderr)
	}
	if strings.Contains(stdout, "secret") {
		t.Fatalf("doctor output leaked secret: %q", stdout)
	}
	if !sawHealth || !sawItems {
		t.Fatalf("doctor should call both health and items, saw health=%v items=%v", sawHealth, sawItems)
	}
	if gotHealthAuth != "Bearer secret" || gotItemsAuth != "Bearer secret" {
		t.Fatalf("shared auth header not sent to probes: health=%q items=%q", gotHealthAuth, gotItemsAuth)
	}

	var got doctorResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid doctor JSON %q: %v", stdout, err)
	}
	if !got.OK || got.Server != srv.URL {
		t.Fatalf("unexpected doctor result: %+v", got)
	}
	if !got.Health.OK || got.Health.ServiceStatus != "ok" {
		t.Fatalf("unexpected health summary: %+v", got.Health)
	}
	if !got.Auth.OK || got.Auth.Status != "ok" || got.Auth.HTTPStatus != http.StatusOK {
		t.Fatalf("unexpected auth summary: %+v", got.Auth)
	}
	if !got.Auth.APIKeyProvided {
		t.Fatalf("api_key_provided should be true when --api-key is set: %+v", got.Auth)
	}
	if len(got.NextSteps) == 0 {
		t.Fatalf("doctor should include operator next steps: %+v", got)
	}
}

func TestDoctorCommandAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"service": "infowall",
				"status":  "ok",
				"ts":      "2026-07-01T00:00:00Z",
			})
		case "/api/items":
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"unauthorized"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdDoctor([]string{"--server", srv.URL, "--json"})
	})
	if err == nil {
		t.Fatal("expected doctor auth failure")
	}
	if stdout != "" {
		t.Fatalf("stdout should be empty on JSON failure, got %q", stdout)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stderr), &got); err != nil {
		t.Fatalf("stderr should be JSON error, got %q: %v", stderr, err)
	}
	if !strings.Contains(got["error"], "auth check") || !strings.Contains(got["error"], "server 401") {
		t.Fatalf("error should distinguish auth failure, got %q", got["error"])
	}
}

func TestDoctorCommandHealthFailureSkipsAuthProbe(t *testing.T) {
	var sawItems bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":"starting"}`)
		case "/api/items":
			sawItems = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return cmdDoctor([]string{"--server", srv.URL, "--json"})
	})
	if err == nil {
		t.Fatal("expected doctor health failure")
	}
	if sawItems {
		t.Fatal("doctor should not run auth probe when health fails")
	}
	if stdout != "" {
		t.Fatalf("stdout should be empty on JSON failure, got %q", stdout)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stderr), &got); err != nil {
		t.Fatalf("stderr should be JSON error, got %q: %v", stderr, err)
	}
	if !strings.Contains(got["error"], "server health") || !strings.Contains(got["error"], "server 503") {
		t.Fatalf("error should distinguish health failure, got %q", got["error"])
	}
}

func TestClientConfigURL(t *testing.T) {
	cfg := &clientConfig{server: "http://x:1/"}
	if got := cfg.url("/api/items"); got != "http://x:1/api/items" {
		t.Fatalf("trailing slash not trimmed: %q", got)
	}
}

func TestParseFlagsPermutesFlagsAfterPositional(t *testing.T) {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	cfg := addClientFlags(fs)
	// Flags appear both before and after positional args.
	parseFlags(fs, []string{"a.md", "--json", "b.md", "--server", "http://x:9", "c.md"})
	if !cfg.asJSON {
		t.Fatal("--json after positional not parsed")
	}
	if cfg.server != "http://x:9" {
		t.Fatalf("--server value after positional not parsed: %q", cfg.server)
	}
	got := fs.Args()
	want := []string{"a.md", "b.md", "c.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("positional args = %v, want %v", got, want)
	}
}

func TestParseFlagsEqualsForm(t *testing.T) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, []string{"--server=http://y:8", "x"})
	if cfg.server != "http://y:8" {
		t.Fatalf("--server=value not parsed: %q", cfg.server)
	}
	if fs.Arg(0) != "x" {
		t.Fatalf("positional lost: %v", fs.Args())
	}
}

func TestParseFlagsDoubleDashStopsParsing(t *testing.T) {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	cfg := addClientFlags(fs)
	// After "--", a literal "--json" must be treated as a positional (e.g. a filename).
	parseFlags(fs, []string{"--", "--json"})
	if cfg.asJSON {
		t.Fatal("--json after -- should be positional, not a flag")
	}
	if fs.Arg(0) != "--json" {
		t.Fatalf("expected literal positional, got %v", fs.Args())
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("abcdef1234"); got != "abcdef12" {
		t.Fatalf("want abcdef12, got %q", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Fatalf("short id should pass through, got %q", got)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDBInfoJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "wall.db")

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"db", "info", "--db", dbPath, "--json"})
	})
	if err != nil {
		t.Fatalf("db info failed: %v (stderr=%s)", err, stderr)
	}
	var info struct {
		Path        string `json:"path"`
		Exists      bool   `json:"exists"`
		Items       int64  `json:"items"`
		Initialized bool   `json:"initialized"`
	}
	if e := json.Unmarshal([]byte(stdout), &info); e != nil {
		t.Fatalf("decode db info json: %v (stdout=%q)", e, stdout)
	}
	if !info.Initialized || !info.Exists {
		t.Fatalf("fresh db should be initialized and exist: %+v", info)
	}
	if info.Items != 0 {
		t.Fatalf("fresh db should have 0 items, got %d", info.Items)
	}
	if !filepath.IsAbs(info.Path) {
		t.Fatalf("path should be absolute: %q", info.Path)
	}
}

func TestDBBackupJSONAndRefuseOverwrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wall.db")
	// Initialize the DB by opening it once via db info.
	if _, _, err := captureCommandOutput(t, func() error {
		return run([]string{"db", "info", "--db", dbPath, "--json"})
	}); err != nil {
		t.Fatalf("seed db info: %v", err)
	}

	out := filepath.Join(t.TempDir(), "sub", "backup.db")

	// First backup succeeds.
	stdout, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"db", "backup", "--db", dbPath, "--out", out, "--json"})
	})
	if err != nil {
		t.Fatalf("backup failed: %v (stderr=%s)", err, stderr)
	}
	var res struct {
		Out       string `json:"out"`
		SizeBytes int64  `json:"size_bytes"`
	}
	if e := json.Unmarshal([]byte(stdout), &res); e != nil {
		t.Fatalf("decode backup json: %v (stdout=%q)", e, stdout)
	}
	if res.SizeBytes <= 0 {
		t.Fatalf("backup size should be positive: %+v", res)
	}
	if _, e := os.Stat(out); e != nil {
		t.Fatalf("backup file missing: %v", e)
	}

	// Second backup to the same path must fail with JSON error on stderr.
	stdout2, stderr2, err2 := captureCommandOutput(t, func() error {
		return run([]string{"db", "backup", "--db", dbPath, "--out", out, "--json"})
	})
	if err2 == nil {
		t.Fatal("expected refuse-overwrite error")
	}
	if strings.TrimSpace(stdout2) != "" {
		t.Fatalf("stdout should be empty on error, got %q", stdout2)
	}
	if !strings.Contains(stderr2, "\"error\"") {
		t.Fatalf("expected JSON error on stderr, got %q", stderr2)
	}
}

func TestDBBackupMissingOut(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wall.db")
	_, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"db", "backup", "--db", dbPath, "--json"})
	})
	if err == nil {
		t.Fatal("expected error when --out is missing")
	}
	if !strings.Contains(stderr, "\"error\"") {
		t.Fatalf("expected JSON error on stderr, got %q", stderr)
	}
}

func TestDBUnknownSubcommand(t *testing.T) {
	if err := run([]string{"db", "frobnicate"}); err == nil {
		t.Fatal("expected error for unknown db subcommand")
	}
}

func captureCommandOutput(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	runErr := fn()
	stdoutW.Close()
	stderrW.Close()
	stdout, readOutErr := io.ReadAll(stdoutR)
	stderr, readErrErr := io.ReadAll(stderrR)
	if readOutErr != nil {
		t.Fatal(readOutErr)
	}
	if readErrErr != nil {
		t.Fatal(readErrErr)
	}
	return string(stdout), string(stderr), runErr
}
