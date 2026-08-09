package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemandCreateCLIRequest(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/demands" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer secret" {
			t.Fatalf("authorization = %q", auth)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("content-type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":"d-1","title":"排查吞吐","status":"pending"}`)
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{
			"demand", "create",
			"--title", "排查吞吐",
			"--description", "M15 周期性下降",
			"--priority", "P1",
			"--project-hint", "M15 性能",
			"--next-action", "收集多 rank 堆栈",
			"--server", srv.URL,
			"--api-key", "secret",
			"--json",
		})
	})
	if err != nil {
		t.Fatalf("create failed: %v; stderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if got["title"] != "排查吞吐" || got["status"] != "pending" || got["priority"] != "p1" {
		t.Fatalf("unexpected create body: %#v", got)
	}
	if got["project_hint"] != "M15 性能" || got["next_action"] != "收集多 rank 堆栈" {
		t.Fatalf("tracking fields lost: %#v", got)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(stdout), &response); err != nil || response["id"] != "d-1" {
		t.Fatalf("invalid JSON stdout %q: %v", stdout, err)
	}
}

func TestScanFeishuSetupAndReviewAcceptCLI(t *testing.T) {
	requests := make([]struct {
		method string
		path   string
		body   map[string]any
	}, 0, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
		}
		requests = append(requests, struct {
			method string
			path   string
			body   map[string]any
		}{r.Method, r.URL.Path, body})
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	if _, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"scan", "feishu", "setup", "--exclude-chat", "oc_skip", "--server", srv.URL, "--json"})
	}); err != nil {
		t.Fatalf("scan setup failed: %v stderr=%s", err, stderr)
	}
	if _, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"demand", "review", "accept", "review-1", "--demand", "demand-1", "--server", srv.URL, "--json"})
	}); err != nil {
		t.Fatalf("review accept failed: %v stderr=%s", err, stderr)
	}
	if len(requests) != 2 || requests[0].method != http.MethodPatch || requests[0].path != "/api/integrations/feishu-chat" {
		t.Fatalf("unexpected setup request: %+v", requests)
	}
	if requests[0].body["enabled"] != true || requests[0].body["interval_minutes"] != float64(30) {
		t.Fatalf("unexpected setup body: %+v", requests[0].body)
	}
	if requests[1].path != "/api/demand-reviews/review-1/accept" || requests[1].body["demand_id"] != "demand-1" {
		t.Fatalf("unexpected review request: %+v", requests[1])
	}
}

func TestDemandImportCLINormalizesBareArray(t *testing.T) {
	input := filepath.Join(t.TempDir(), "demands.json")
	raw := `[{"title":"跟进发布","status":"pending","sources":[{"kind":"feishu-im","external_id":"om_1","dedupe_key":"feishu-im:om_1:follow-release"}]}]`
	if err := os.WriteFile(input, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Demands []struct {
			Title   string `json:"title"`
			Sources []struct {
				DedupeKey string `json:"dedupe_key"`
			} `json:"sources"`
		} `json:"demands"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/demands/import" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"created":1,"updated":0,"skipped":0,"results":[]}`)
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"demand", "import", "--input", input, "--server", srv.URL, "--json"})
	})
	if err != nil {
		t.Fatalf("import failed: %v; stderr=%s", err, stderr)
	}
	if len(got.Demands) != 1 || got.Demands[0].Title != "跟进发布" {
		t.Fatalf("bare array was not normalized: %+v", got)
	}
	if len(got.Demands[0].Sources) != 1 || got.Demands[0].Sources[0].DedupeKey == "" {
		t.Fatalf("source dedupe evidence lost: %+v", got)
	}
	if !strings.Contains(stdout, `"created":1`) {
		t.Fatalf("server result not forwarded: %q", stdout)
	}
}

func TestDemandApplyAcceptsSingleDemandObject(t *testing.T) {
	input := filepath.Join(t.TempDir(), "demand.json")
	raw := `{"title":"跟进发布","status":"pending","sources":[{"kind":"manual","dedupe_key":"agent:release:1"}]}`
	if err := os.WriteFile(input, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Demands []map[string]any `json:"demands"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/demands/import" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"created":1,"updated":0,"skipped":0,"results":[]}`)
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"demand", "apply", "--input", input, "--server", srv.URL, "--json"})
	})
	if err != nil || stderr != "" {
		t.Fatalf("apply failed: err=%v stderr=%q", err, stderr)
	}
	if len(got.Demands) != 1 || got.Demands[0]["title"] != "跟进发布" {
		t.Fatalf("single demand was not normalized: %+v", got)
	}
	if !strings.Contains(stdout, `"created":1`) {
		t.Fatalf("result was not forwarded: %q", stdout)
	}
}

func TestDemandListAgentFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/demands" || r.URL.Query().Get("q") != "吞吐下降" ||
			r.URL.Query().Get("project_id") != "p1" || r.URL.Query().Get("include_dismissed") != "true" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"demands":[]}`)
	}))
	defer srv.Close()

	_, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"demand", "list", "--q", "吞吐下降", "--project", "p1", "--include-dismissed", "--server", srv.URL, "--json"})
	})
	if err != nil || stderr != "" {
		t.Fatalf("list failed: err=%v stderr=%q", err, stderr)
	}
}

func TestDemandUpdateProgressDismissRestoreCLI(t *testing.T) {
	type request struct {
		method string
		path   string
		body   map[string]any
	}
	var requests []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		requests = append(requests, request{method: r.Method, path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	commands := [][]string{
		{"demand", "update", "d1", "--status", "waiting", "--project=", "--blocked-reason", "等待日志", "--server", srv.URL, "--json"},
		{"demand", "progress", "d1", "--text", "日志已到", "--source", `{"kind":"feishu-im","external_id":"om_2"}`, "--server", srv.URL, "--json"},
		{"demand", "dismiss", "d1", "--server", srv.URL, "--json"},
		{"demand", "restore", "d1", "--server", srv.URL, "--json"},
	}
	for _, command := range commands {
		_, stderr, err := captureCommandOutput(t, func() error { return run(command) })
		if err != nil {
			t.Fatalf("%v failed: %v; stderr=%s", command, err, stderr)
		}
	}
	if len(requests) != 4 {
		t.Fatalf("got %d requests: %+v", len(requests), requests)
	}
	if requests[0].method != http.MethodPatch || requests[0].path != "/api/demands/d1" {
		t.Fatalf("bad update request: %+v", requests[0])
	}
	if value, exists := requests[0].body["project_id"]; !exists || value != nil {
		t.Fatalf("--project= should explicitly clear project: %#v", requests[0].body)
	}
	if requests[1].path != "/api/demands/d1/progress" || requests[1].body["text"] != "日志已到" {
		t.Fatalf("bad progress request: %+v", requests[1])
	}
	if source, ok := requests[1].body["source"].(map[string]any); !ok || source["external_id"] != "om_2" {
		t.Fatalf("progress source lost: %#v", requests[1].body)
	}
	if requests[2].body["status"] != "dismissed" || requests[3].body["status"] != "pending" {
		t.Fatalf("dismiss/restore statuses wrong: %+v", requests)
	}
}

func TestProjectAndFeishuSyncCLIContracts(t *testing.T) {
	type request struct {
		method string
		path   string
		body   map[string]any
	}
	var requests []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		requests = append(requests, request{method: r.Method, path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	commands := [][]string{
		{"project", "create", "--name", "M15", "--color", "#2563eb", "--server", srv.URL, "--json"},
		{"project", "update", "p1", "--description", "性能专项", "--server", srv.URL, "--json"},
		{"project", "archive", "p1", "--server", srv.URL, "--json"},
		{"sync", "feishu", "setup", "--doc", "https://example.feishu.cn/docx/abc", "--server", srv.URL, "--json"},
		{"sync", "feishu", "status", "--server", srv.URL, "--json"},
		{"sync", "feishu", "now", "--server", srv.URL, "--json"},
		{"sync", "feishu", "disable", "--server", srv.URL, "--json"},
	}
	for _, command := range commands {
		_, stderr, err := captureCommandOutput(t, func() error { return run(command) })
		if err != nil {
			t.Fatalf("%v failed: %v; stderr=%s", command, err, stderr)
		}
	}

	want := []request{
		{method: http.MethodPost, path: "/api/projects"},
		{method: http.MethodPatch, path: "/api/projects/p1"},
		{method: http.MethodPatch, path: "/api/projects/p1"},
		{method: http.MethodPost, path: "/api/integrations/feishu-doc"},
		{method: http.MethodGet, path: "/api/integrations/feishu-doc"},
		{method: http.MethodPost, path: "/api/integrations/feishu-doc/sync"},
		{method: http.MethodDelete, path: "/api/integrations/feishu-doc"},
	}
	if len(requests) != len(want) {
		t.Fatalf("request count=%d want=%d: %+v", len(requests), len(want), requests)
	}
	for i := range want {
		if requests[i].method != want[i].method || requests[i].path != want[i].path {
			t.Fatalf("request %d = %s %s, want %s %s", i, requests[i].method, requests[i].path, want[i].method, want[i].path)
		}
	}
	if requests[0].body["name"] != "M15" || requests[2].body["status"] != "archived" {
		t.Fatalf("project bodies wrong: %+v", requests[:3])
	}
	if requests[3].body["doc_url"] != "https://example.feishu.cn/docx/abc" {
		t.Fatalf("setup body wrong: %#v", requests[3].body)
	}
}

func TestDemandCLIJSONErrorGoesToStderr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid status"}`)
	}))
	defer srv.Close()

	stdout, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"demand", "list", "--server", srv.URL, "--json"})
	})
	if err == nil {
		t.Fatal("expected command error")
	}
	if stdout != "" {
		t.Fatalf("stdout should be empty on error: %q", stdout)
	}
	var envelope cliErrorEnvelope
	if decodeErr := json.Unmarshal([]byte(stderr), &envelope); decodeErr != nil {
		t.Fatalf("stderr is not JSON: %q: %v", stderr, decodeErr)
	}
	if !strings.Contains(envelope.Error, "server 400") || !strings.Contains(envelope.Error, "invalid status") {
		t.Fatalf("unexpected JSON error: %#v", envelope)
	}
	if envelope.ErrorCode != "api_error" || envelope.Retryable || envelope.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("unexpected structured error metadata: %#v", envelope)
	}
}

func TestNormalizeDemandImportRejectsInvalidShape(t *testing.T) {
	for _, raw := range []string{`{}`, `{"demands":{}}`, `"not-an-array"`, `[] {}`} {
		if _, err := normalizeDemandImport([]byte(raw)); err == nil {
			t.Fatalf("expected invalid import %q to fail", raw)
		}
	}
}
