package feishuingest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAnalysisSchemaRestrictsEveryReturnedMessageID(t *testing.T) {
	allowed := analysisMessageIDs([]AnalysisInput{{
		Messages:   []Message{{ID: "om_context"}, {ID: "om_candidate"}},
		Candidates: []Message{{ID: "om_candidate"}},
	}})
	if strings.Join(allowed, ",") != "om_candidate,om_context" {
		t.Fatalf("allowed IDs = %v", allowed)
	}
	raw, err := analysisSchemaFor(allowed)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	definitions := schema["$defs"].(map[string]any)
	messageID := definitions["message_id"].(map[string]any)
	enum := messageID["enum"].([]any)
	if len(enum) != 2 || enum[0] != "om_candidate" || enum[1] != "om_context" {
		t.Fatalf("message ID enum = %+v", enum)
	}
	source := definitions["source"].(map[string]any)
	externalID := source["properties"].(map[string]any)["external_id"].(map[string]any)
	if externalID["$ref"] != "#/$defs/message_id" {
		t.Fatalf("source external_id is not allowlisted: %+v", externalID)
	}
}

func TestCodexTimeoutKillsLauncherProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are Unix-specific")
	}
	directory := t.TempDir()
	script := filepath.Join(directory, "fake-codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	analyzer := CodexAnalyzer{Path: script, CWD: directory, Timeout: 100 * time.Millisecond}
	started := time.Now()
	_, err := analyzer.Analyze(context.Background(), []AnalysisInput{{Candidates: []Message{{ID: "message-1"}}}})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("process group was not terminated promptly: %s", elapsed)
	}
}

func TestDecodeAnalysisResultUsesServerOwnedEvidenceMetadata(t *testing.T) {
	message := Message{ID: "om_1", ChatID: "chat_1", SenderName: "用户", CreatedAt: time.Now()}
	raw := []byte(`{"new_demands":[{"title":"修复部署失败","description":"部署当前失败，需要定位并恢复。","next_action":"检查失败日志并提交修复","project_hint":"Server","sources":[{"external_id":"om_1","excerpt":"部署失败了"}]}],"progress_updates":[],"reviews":[],"skipped_message_ids":[],"missing_context_message_ids":[]}`)
	result, err := decodeAnalysisResult(raw, []AnalysisInput{{Messages: []Message{message}, Candidates: []Message{message}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewDemands) != 1 || result.NewDemands[0].Sources[0].ExternalID != message.ID {
		t.Fatalf("result = %+v", result)
	}
}

func TestDecodeAnalysisResultStillRejectsUnknownMessageID(t *testing.T) {
	message := Message{ID: "om_allowed", SenderType: "user", Content: "需求"}
	raw := []byte(`{"new_demands":[],"progress_updates":[],"reviews":[],"skipped_message_ids":["om_unknown"],"missing_context_message_ids":["om_allowed"]}`)
	if _, err := decodeAnalysisResult(raw, []AnalysisInput{{Messages: []Message{message}, Candidates: []Message{message}}}); err == nil || !strings.Contains(err.Error(), "unknown message") {
		t.Fatalf("unknown message ID was accepted: %v", err)
	}
}
