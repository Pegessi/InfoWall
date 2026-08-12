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

	"github.com/infowall/infowall/internal/model"
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

func TestMarshalAnalysisInputSerializesSharedSnapshotOnce(t *testing.T) {
	snapshot := Snapshot{Demands: []*model.Demand{{ID: "demand-1", Title: "snapshot-title"}}}
	batches := []AnalysisInput{
		{Messages: []Message{{ID: "message-1"}}, Candidates: []Message{{ID: "message-1"}}, Snapshot: snapshot},
		{Messages: []Message{{ID: "message-2"}}, Candidates: []Message{{ID: "message-2"}}, Snapshot: snapshot},
	}
	raw, err := marshalAnalysisInput(batches, []string{"message-1", "message-2"})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	if count := strings.Count(encoded, "snapshot-title"); count != 1 {
		t.Fatalf("snapshot serialized %d times: %s", count, encoded)
	}
	if count := strings.Count(encoded, "existing_snapshot"); count != 1 {
		t.Fatalf("existing_snapshot key count = %d", count)
	}
}

func TestCodexReadsEphemeralInputFileInsteadOfEmbeddingChatInPrompt(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "fake-codex")
	contents := `#!/bin/sh
prompt=$(cat)
printf '%s' "$prompt" > "$PWD/captured-prompt.txt"
manifest_path=$(printf '%s' "$prompt" | sed -n 's#.*<infowall_ingestion_manifest>\(.*\)</infowall_ingestion_manifest>.*#\1#p')
cp "$manifest_path" "$PWD/captured-manifest.json"
candidate_path=$(sed -n 's#.*"candidate_files":\["\([^"]*\)".*#\1#p' "$manifest_path")
cp "$candidate_path" "$PWD/captured-input.json"
output_path=''
while [ "$#" -gt 0 ]; do
  if [ "$1" = '--output-last-message' ]; then
    shift
    output_path=$1
  fi
  shift
done
printf '%s\n' '{"new_demands":[],"progress_updates":[],"reviews":[],"skipped_message_ids":[],"missing_context_message_ids":["message-1"]}' > "$output_path"
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}'
`
	if err := os.WriteFile(script, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	analyzer := CodexAnalyzer{Path: script, CWD: directory}
	result, err := analyzer.Analyze(context.Background(), []AnalysisInput{{
		Messages:   []Message{{ID: "message-1", Content: "secret-chat-content"}},
		Candidates: []Message{{ID: "message-1", Content: "secret-chat-content"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.InputTokens != 10 || result.OutputTokens != 2 {
		t.Fatalf("usage = %+v", result)
	}
	prompt, err := os.ReadFile(filepath.Join(directory, "captured-prompt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(prompt), "secret-chat-content") {
		t.Fatalf("chat content was embedded in prompt: %s", prompt)
	}
	if !strings.Contains(string(prompt), "<infowall_ingestion_manifest>") {
		t.Fatalf("prompt is missing input file path: %s", prompt)
	}
	input, err := os.ReadFile(filepath.Join(directory, "captured-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(input), "secret-chat-content") {
		t.Fatalf("temporary input did not contain chat content: %s", input)
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

func TestCodexFailureClassifiesContextLimitWithoutPersistingOutput(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "fake-codex")
	contents := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"error\",\"message\":\"maximum context length exceeded near private chat text\"}'\nexit 1\n"
	if err := os.WriteFile(script, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	analyzer := CodexAnalyzer{Path: script, CWD: directory}
	_, err := analyzer.Analyze(context.Background(), []AnalysisInput{{Candidates: []Message{{ID: "message-1"}}}})
	if err == nil || !strings.Contains(err.Error(), "Codex input exceeded context window") {
		t.Fatalf("context error = %v", err)
	}
	if strings.Contains(err.Error(), "private chat text") {
		t.Fatalf("error leaked command output: %v", err)
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

func TestDecodeAnalysisResultDoesNotLeakUnknownFieldName(t *testing.T) {
	message := Message{ID: "om_allowed", SenderType: "user", Content: "需求"}
	privateField := "private-chat-text-must-not-enter-errors"
	raw := []byte(`{"new_demands":[],"progress_updates":[],"reviews":[],"skipped_message_ids":["om_allowed"],"missing_context_message_ids":[],"` + privateField + `":true}`)
	_, err := decodeAnalysisResult(raw, []AnalysisInput{{Messages: []Message{message}, Candidates: []Message{message}}})
	if err == nil || strings.Contains(err.Error(), privateField) {
		t.Fatalf("schema error leaked unknown field: %v", err)
	}
}

func TestDecodeAnalysisResultRejectsNewDemandFromLocalConversation(t *testing.T) {
	message := Message{ID: "codex-turn:session:turn", SourceKind: "codex-conversation", Content: "实现完成"}
	raw := []byte(`{"new_demands":[{"title":"新增一个需求","description":"仅来自本地对话。","next_action":"等待确认","project_hint":"Server","sources":[{"external_id":"codex-turn:session:turn","excerpt":"实现完成"}]}],"progress_updates":[],"reviews":[],"skipped_message_ids":[],"missing_context_message_ids":[]}`)
	_, err := decodeAnalysisResult(raw, []AnalysisInput{{Messages: []Message{message}, Candidates: []Message{message}}})
	if err == nil || !strings.Contains(err.Error(), "only use Feishu") {
		t.Fatalf("local conversation created a demand: %v", err)
	}
}
