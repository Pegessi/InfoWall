package conversationingest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/model"
)

func TestNormalizeHookPayloadIsStableAndKeepsOnlyDirectLinks(t *testing.T) {
	raw := []byte(`{"session_id":"session-1","turn_id":"turn-1","cwd":"/tmp/project","prompt":"检查 [MR](https://code.example/repo/merge_requests/42）)","timestamp":"2026-08-12T09:00:00+08:00"}`)
	first, err := NormalizeHookPayload("codex", "UserPromptSubmit", raw, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeHookPayload("codex", "UserPromptSubmit", raw, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || !strings.HasPrefix(first.ID, "conversation-hook:") {
		t.Fatalf("duplicate hook IDs differ: %q %q", first.ID, second.ID)
	}
	if len(first.Links) != 1 || first.Links[0].URL != "https://code.example/repo/merge_requests/42" || first.Links[0].Kind != "codebase-mr" {
		t.Fatalf("links = %+v", first.Links)
	}
}

func TestClaudeTranscriptCursorReadsOnlyNewAssistantText(t *testing.T) {
	stateDir := t.TempDir()
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	old := `{"type":"assistant","message":{"content":[{"type":"text","text":"old answer"}]}}` + "\n"
	if err := os.WriteFile(transcript, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordTranscriptCursor(stateDir, transcript); err != nil {
		t.Fatal(err)
	}
	tool := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"private tool log"}}]}}` + "\n"
	newAnswer := `{"type":"assistant","message":{"content":[{"type":"text","text":"new final answer"}]}}` + "\n"
	file, _ := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = file.WriteString(tool + newAnswer)
	_ = file.Close()
	result, err := ReadClaudeTranscriptIncrement(stateDir, transcript)
	if err != nil {
		t.Fatal(err)
	}
	if result != "new final answer" || strings.Contains(result, "old answer") || strings.Contains(result, "private tool log") {
		t.Fatalf("increment = %q", result)
	}
}

func TestClaudeRepeatedStopUsesTranscriptCursorIdentity(t *testing.T) {
	stateDir := t.TempDir()
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	promptRaw := []byte(`{"session_id":"session-1","transcript_path":"` + transcript + `","prompt":"same goal"}`)
	prompt, err := NormalizeHookPayload("claude", "UserPromptSubmit", promptRaw, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	firstRaw := []byte(`{"session_id":"session-1","transcript_path":"` + transcript + `","last_assistant_message":"done","timestamp":"2026-08-12T09:00:00+08:00"}`)
	secondRaw := []byte(`{"session_id":"session-1","transcript_path":"` + transcript + `","last_assistant_message":"done","timestamp":"2026-08-12T09:00:01+08:00"}`)
	first, err := NormalizeHookPayload("claude", "Stop", firstRaw, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeHookPayload("claude", "Stop", secondRaw, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if prompt.TurnID == "" || prompt.TurnID != first.TurnID || first.TurnID != second.TurnID || first.ID != second.ID {
		t.Fatalf("cursor identity prompt=%+v first=%+v second=%+v", prompt, first, second)
	}
}

func TestSummaryRunnerIsExcluded(t *testing.T) {
	t.Setenv("INFOWALL_SUMMARY_RUNNER", "1")
	_, err := NormalizeHookPayload("claude", "Stop", []byte(`{"session_id":"session-1","last_assistant_message":"done"}`), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Fatalf("summary runner error = %v", err)
	}
}

func TestSpoolIsPrivateRetrySafeAndExpires(t *testing.T) {
	stateDir := t.TempDir()
	event := model.ConversationHookEvent{ID: "event-1", Source: "codex", EventName: "Stop", SessionID: "session-1", Result: "done", OccurredAt: time.Now()}
	if err := WriteSpool(stateDir, event); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(SpoolDir(stateDir), "event-1.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("spool mode = %v err=%v", info.Mode().Perm(), err)
	}
	calls := 0
	count, err := DrainSpool(context.Background(), stateDir, func(_ context.Context, got model.ConversationHookEvent) error {
		calls++
		if got.ID != event.ID {
			t.Fatalf("event = %+v", got)
		}
		return nil
	})
	if err != nil || count != 1 || calls != 1 {
		t.Fatalf("drain count=%d calls=%d err=%v", count, calls, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("imported spool still exists: %v", err)
	}

	stale := event
	stale.ID = "stale"
	raw, _ := json.Marshal(stale)
	stalePath := filepath.Join(SpoolDir(stateDir), "stale.json")
	if err := os.WriteFile(stalePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	_ = os.Chtimes(stalePath, old, old)
	count, err = DrainSpool(context.Background(), stateDir, func(context.Context, model.ConversationHookEvent) error {
		t.Fatal("expired spool was imported")
		return nil
	})
	if err != nil || count != 0 {
		t.Fatalf("expired drain count=%d err=%v", count, err)
	}
}
