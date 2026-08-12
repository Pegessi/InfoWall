package feishusync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type scriptedRunner struct {
	responses [][]byte
	calls     [][]string
	inputs    []string
}

func (r *scriptedRunner) Run(_ context.Context, stdin string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	r.inputs = append(r.inputs, stdin)
	if len(r.responses) == 0 {
		return nil, fmt.Errorf("unexpected call")
	}
	response := r.responses[0]
	r.responses = r.responses[1:]
	return response, nil
}

func envelope(id, url, content string, revision int) []byte {
	return []byte(fmt.Sprintf(`{"ok":true,"data":{"document":{"document_id":%q,"url":%q,"revision_id":%d,"content":%q}}}`, id, url, revision, content))
}

func TestExecRunnerTimeoutKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are Unix-specific")
	}
	directory := t.TempDir()
	script := filepath.Join(directory, "fake-lark-cli")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := ExecRunner{Path: script, Timeout: 100 * time.Millisecond}
	started := time.Now()
	_, err := runner.Run(context.Background(), "", "im", "+messages-search")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("process group was not terminated promptly: %s", elapsed)
	}
}

func TestSyncManagedSectionRefetchesAndCleansStaleBlocks(t *testing.T) {
	rendered := Render(Snapshot{})
	runner := &scriptedRunner{responses: [][]byte{
		envelope("doc", "url", `<fragment><h1 id="old-heading">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 10),
		envelope("doc", "url", "", 11),
		envelope("doc", "url", `<fragment><h1 id="new-heading">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 11),
		envelope("doc", "url", `<fragment><h1 id="new-heading">InfoWall 自动同步</h1><p id="new-body">body</p><p id="marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p><p id="stale">old</p></fragment>`, 11),
		envelope("doc", "url", "", 12),
		envelope("doc", "url", `<fragment><h1 id="verified-heading">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 12),
		envelope("doc", "url", `<fragment><h1 id="verified-heading">InfoWall 自动同步</h1><p id="marker2">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 12),
	}}
	client := Client{Runner: runner}
	result, err := client.SyncManagedSection(context.Background(), "doc", rendered)
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 12 || result.Hash != rendered.Hash {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(runner.calls) != 7 {
		t.Fatalf("calls=%d", len(runner.calls))
	}
	if got := strings.Join(runner.calls[1], " "); !strings.Contains(got, "block_replace") || !strings.Contains(got, "old-heading") {
		t.Fatalf("unexpected replace call: %s", got)
	}
	if got := strings.Join(runner.calls[4], " "); !strings.Contains(got, "block_delete") || !strings.Contains(got, "stale") {
		t.Fatalf("unexpected delete call: %s", got)
	}
	if got := strings.Join(runner.calls[4], " "); strings.Contains(got, "old-heading") {
		t.Fatalf("reused stale heading id: %s", got)
	}
}

func TestSyncManagedSectionCleansDuplicateMatchingMarker(t *testing.T) {
	rendered := Render(Snapshot{})
	runner := &scriptedRunner{responses: [][]byte{
		envelope("doc", "url", `<fragment><h1 id="old-heading">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 20),
		envelope("doc", "url", "", 21),
		envelope("doc", "url", `<fragment><h1 id="new-heading">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 21),
		envelope("doc", "url", `<fragment><h1 id="new-heading">InfoWall 自动同步</h1><p id="new-body">new</p><p id="new-marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p><p id="old-body">old</p><p id="old-marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 21),
		envelope("doc", "url", "", 22),
		envelope("doc", "url", `<fragment><h1 id="verified-heading">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 22),
		envelope("doc", "url", `<fragment><h1 id="verified-heading">InfoWall 自动同步</h1><p id="verified-body">new</p><p id="verified-marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 22),
	}}
	client := Client{Runner: runner}
	result, err := client.SyncManagedSection(context.Background(), "doc", rendered)
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 22 || result.Hash != rendered.Hash {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(runner.calls) != 7 {
		t.Fatalf("calls=%d", len(runner.calls))
	}
	deleteCall := strings.Join(runner.calls[4], " ")
	if !strings.Contains(deleteCall, "block_delete") || !strings.Contains(deleteCall, "old-body,old-marker") {
		t.Fatalf("unexpected delete call: %s", deleteCall)
	}
	if strings.Contains(deleteCall, "new-marker") {
		t.Fatalf("deleted the new managed marker: %s", deleteCall)
	}
}

func TestCreateUsesStdinAndUserIdentity(t *testing.T) {
	rendered := Render(Snapshot{})
	runner := &scriptedRunner{responses: [][]byte{
		envelope("docx123", "https://example/docx/docx123", "", 1),
		envelope("docx123", "https://example/docx/docx123", `<fragment><h1 id="managed">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 1),
		envelope("docx123", "https://example/docx/docx123", `<fragment><h1 id="managed">InfoWall 自动同步</h1><p id="marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 1),
	}}
	client := Client{Runner: runner}
	doc, err := client.Create(context.Background(), rendered)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "docx123" || len(runner.calls) != 3 || !strings.Contains(strings.Join(runner.calls[0], " "), "--as user") {
		t.Fatalf("unexpected create result/call: %+v %#v", doc, runner.calls)
	}
	if !strings.Contains(runner.inputs[0], ManagedHeading) {
		t.Fatal("initial document not supplied on stdin")
	}
}

func TestBindAppendsManagedSectionWhenMissing(t *testing.T) {
	rendered := Render(Snapshot{})
	runner := &scriptedRunner{responses: [][]byte{
		envelope("doc", "url", `<fragment><h1 id="existing">Existing</h1></fragment>`, 4),
		envelope("doc", "url", "", 5),
		envelope("doc", "url", `<fragment><h1 id="existing">Existing</h1><h1 id="managed">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 5),
		envelope("doc", "url", `<fragment><h1 id="existing">Existing</h1><h1 id="managed2">InfoWall 自动同步</h1><h1 id="boundary">同步说明</h1></fragment>`, 5),
		envelope("doc", "url", `<fragment><h1 id="managed2">InfoWall 自动同步</h1><p id="marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 5),
	}}
	client := Client{Runner: runner}
	result, err := client.Bind(context.Background(), "doc", rendered)
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 5 || result.Hash != rendered.Hash {
		t.Fatalf("unexpected bind result: %+v", result)
	}
	if got := strings.Join(runner.calls[1], " "); !strings.Contains(got, "block_insert_after") || !strings.Contains(got, "--block-id -1") {
		t.Fatalf("unexpected bind call: %s", got)
	}
	if !strings.Contains(runner.inputs[1], EndMarkerPrefix+rendered.Hash) {
		t.Fatal("bound content missing generated marker")
	}
}
