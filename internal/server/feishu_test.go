package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infowall/infowall/internal/feishusync"
)

type fakeLarkRunner struct {
	responses [][]byte
	calls     [][]string
	inputs    []string
}

func (r *fakeLarkRunner) Run(_ context.Context, input string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	r.inputs = append(r.inputs, input)
	if len(r.responses) == 0 {
		return nil, fmt.Errorf("unexpected lark-cli call: %s", strings.Join(args, " "))
	}
	response := r.responses[0]
	r.responses = r.responses[1:]
	return response, nil
}

func TestFeishuDocSetupStatusAndDisable(t *testing.T) {
	rendered := feishusync.Render(feishusync.Snapshot{})
	runner := &fakeLarkRunner{responses: [][]byte{[]byte(`{
		"ok": true,
		"data": {"document": {
			"document_id": "docx_test",
			"url": "https://example.feishu.cn/docx/docx_test",
			"revision_id": 1
		}}
	}`),
		[]byte(`{"ok":true,"data":{"document":{"document_id":"docx_test","url":"https://example.feishu.cn/docx/docx_test","revision_id":1,"content":"<fragment><h1 id=\"managed\">InfoWall 自动同步</h1><h1 id=\"boundary\">同步说明</h1></fragment>"}}}`),
		[]byte(fmt.Sprintf(`{"ok":true,"data":{"document":{"document_id":"docx_test","url":"https://example.feishu.cn/docx/docx_test","revision_id":1,"content":%q}}}`, `<fragment><h1 id="managed">InfoWall 自动同步</h1><p id="marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`)),
	}}
	srv, err := New(context.Background(), Config{
		DBPath:     filepath.Join(t.TempDir(), "wall.db"),
		LarkRunner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	var initial map[string]any
	requestMuxJSON(t, "GET", "/api/integrations/feishu-doc", nil, 200, &initial, srv.mux)
	if initial["enabled"] != false || initial["status"] != "disabled" {
		t.Fatalf("unexpected initial state: %#v", initial)
	}

	var configured map[string]any
	requestMuxJSON(t, "POST", "/api/integrations/feishu-doc", map[string]any{"create": true}, 200, &configured, srv.mux)
	if configured["enabled"] != true || configured["status"] != "idle" || configured["doc_url"] != "https://example.feishu.cn/docx/docx_test" {
		t.Fatalf("unexpected configured state: %#v", configured)
	}
	if len(runner.calls) != 3 || !strings.Contains(strings.Join(runner.calls[0], " "), "docs +create --as user") {
		t.Fatalf("unexpected create call: %#v", runner.calls)
	}
	if !strings.Contains(runner.inputs[0], "InfoWall 自动同步") {
		t.Fatal("created document missing managed section")
	}

	var disabled map[string]any
	requestMuxJSON(t, "DELETE", "/api/integrations/feishu-doc", nil, 200, &disabled, srv.mux)
	if disabled["enabled"] != false || disabled["status"] != "disabled" {
		t.Fatalf("unexpected disabled state: %#v", disabled)
	}
}

func requestMuxJSON(t *testing.T, method, path string, body any, wantStatus int, destination any, handler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}) {
	// Kept in this file with a distinct signature so the HTTP-server helper in
	// workbench_test.go remains useful for network-level coverage.
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	if destination != nil {
		if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
			t.Fatal(err)
		}
	}
}
