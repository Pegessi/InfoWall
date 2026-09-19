package feishuingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/model"
)

func TestAnalysisSchemaRestrictsEveryReturnedMessageIDToCandidates(t *testing.T) {
	allowed := analysisCandidateMessageIDs([]AnalysisInput{{
		Messages:   []Message{{ID: "om_context"}, {ID: "om_candidate"}},
		Candidates: []Message{{ID: "om_candidate"}},
	}})
	if strings.Join(allowed, ",") != "om_candidate" {
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
	if _, present := schema["$schema"]; present {
		t.Fatalf("generated schema must not declare $schema; it disables Claude Code's native structured output: %s", raw)
	}
	definitions := schema["$defs"].(map[string]any)
	messageID := definitions["message_id"].(map[string]any)
	enum := messageID["enum"].([]any)
	if len(enum) != 1 || enum[0] != "om_candidate" {
		t.Fatalf("message ID enum = %+v", enum)
	}
	source := definitions["source"].(map[string]any)
	externalID := source["properties"].(map[string]any)["external_id"].(map[string]any)
	if externalID["$ref"] != "#/$defs/message_id" {
		t.Fatalf("source external_id is not allowlisted: %+v", externalID)
	}
}

func TestAnalysisWorkspaceKeepsContextReadableButNotAllowedAsOutput(t *testing.T) {
	directory := t.TempDir()
	batches := []AnalysisInput{{
		Messages:   []Message{{ID: "om_context", Content: "earlier context"}, {ID: "om_candidate", Content: "new update"}},
		Candidates: []Message{{ID: "om_candidate", Content: "new update"}},
	}}
	allowed := analysisCandidateMessageIDs(batches)
	workspace, err := writeAnalysisWorkspace(directory, batches, allowed)
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw, err := os.ReadFile(workspace.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest analysisManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	if strings.Join(manifest.AllowedMessageIDs, ",") != "om_candidate" {
		t.Fatalf("allowed IDs = %v", manifest.AllowedMessageIDs)
	}
	batchRaw, err := os.ReadFile(manifest.CandidateFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(batchRaw), "om_context") || !strings.Contains(string(batchRaw), "earlier context") {
		t.Fatalf("context message is not readable: %s", batchRaw)
	}
}

func TestAnalysisWorkspacePreselectsBoundedRelevantDemands(t *testing.T) {
	directory := t.TempDir()
	relevantByLink := &model.Demand{ID: "demand-link", Title: "修复请求迁移", Sources: []model.Source{{ExternalID: "seed/llmserver!2817"}}}
	relevantByText := &model.Demand{ID: "demand-text", Title: "优化 M15 CUDA Graph 批处理稳定性"}
	demands := []*model.Demand{relevantByLink, relevantByText}
	for index := 0; index < 64; index++ {
		demands = append(demands, &model.Demand{ID: fmt.Sprintf("unrelated-%02d", index), Title: fmt.Sprintf("无关需求 %02d", index)})
	}
	batches := []AnalysisInput{{
		Messages:   []Message{{ID: "candidate", Content: "M15 CUDA Graph 出现异常"}},
		Candidates: []Message{{ID: "candidate", Content: "M15 CUDA Graph 出现异常", Links: []model.ProgressLink{{ExternalID: "seed/llmserver!2817"}}}},
		Snapshot:   Snapshot{Demands: demands},
	}}
	workspace, err := writeAnalysisWorkspace(directory, batches, analysisCandidateMessageIDs(batches))
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw, err := os.ReadFile(workspace.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest analysisManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.DemandFiles) == 0 || len(manifest.DemandFiles) > analysisDemandFileLimit {
		t.Fatalf("demand file count = %d, want 1..%d", len(manifest.DemandFiles), analysisDemandFileLimit)
	}
	selected := make(map[string]struct{}, len(manifest.DemandFiles))
	for _, path := range manifest.DemandFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var demand model.Demand
		if err := json.Unmarshal(raw, &demand); err != nil {
			t.Fatal(err)
		}
		selected[demand.ID] = struct{}{}
	}
	for _, id := range []string{"demand-link", "demand-text"} {
		if _, ok := selected[id]; !ok {
			t.Fatalf("relevant demand %q not preselected: %v", id, selected)
		}
	}
	if _, ok := selected["unrelated-00"]; ok {
		t.Fatalf("unrelated demand was exposed: %v", selected)
	}
}

func TestSelectAnalysisDemandsCapsBroadLexicalMatches(t *testing.T) {
	demands := make([]*model.Demand, 0, analysisDemandFileLimit+8)
	for index := 0; index < analysisDemandFileLimit+8; index++ {
		demands = append(demands, &model.Demand{ID: fmt.Sprintf("demand-%03d", index), Title: "M15 CUDA Graph 稳定性"})
	}
	batches := []AnalysisInput{{
		Candidates: []Message{{ID: "candidate", Content: "M15 CUDA Graph 出现异常"}},
		Snapshot:   Snapshot{Demands: demands},
	}}
	selected := selectAnalysisDemands(batches)
	if len(selected) != analysisDemandFileLimit {
		t.Fatalf("selected demand count = %d, want %d", len(selected), analysisDemandFileLimit)
	}
	wantLast := fmt.Sprintf("demand-%03d", analysisDemandFileLimit-1)
	if selected[0].ID != "demand-000" || selected[len(selected)-1].ID != wantLast {
		t.Fatalf("selection tie-break is not deterministic: first=%q last=%q", selected[0].ID, selected[len(selected)-1].ID)
	}
}

func TestAnalysisPromptRequiresRawSchemaJSON(t *testing.T) {
	for _, required := range []string{"OUTPUT PROTOCOL", "exactly one raw JSON object", "The first output byte must be {", "All five required top-level arrays"} {
		if !strings.Contains(analysisPrompt, required) {
			t.Fatalf("analysis prompt missing %q", required)
		}
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

func TestDecodeAnalysisResultRejectsContextMessageAsOutcome(t *testing.T) {
	contextMessage := Message{ID: "om_context", SenderType: "user", Content: "earlier context"}
	candidate := Message{ID: "om_candidate", SenderType: "user", Content: "new update"}
	batches := []AnalysisInput{{Messages: []Message{contextMessage, candidate}, Candidates: []Message{candidate}}}
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "new demand source",
			raw:  `{"new_demands":[{"title":"修复部署失败","description":"部署失败，需要恢复。","next_action":"定位失败日志","project_hint":"Server","sources":[{"external_id":"om_context","excerpt":"earlier context"}]}],"progress_updates":[],"reviews":[],"skipped_message_ids":["om_candidate"],"missing_context_message_ids":[]}`,
			want: "new_demands[0] source must be a new candidate message",
		},
		{
			name: "progress source",
			raw:  `{"new_demands":[],"progress_updates":[{"demand_id":"demand-1","text":"已完成修复。","dedupe_key":"progress-1","source":{"external_id":"om_context","excerpt":"earlier context"},"confidence":0.95,"anchors":["exact MR"]}],"reviews":[],"skipped_message_ids":["om_candidate"],"missing_context_message_ids":[]}`,
			want: "progress_updates[0] source must be a new candidate message",
		},
		{
			name: "review source",
			raw:  `{"new_demands":[],"progress_updates":[],"reviews":[{"suggested_demand_id":"demand-1","progress_text":"可能已完成修复。","progress_dedupe_key":"review-1","source":{"external_id":"om_context","excerpt":"earlier context"},"confidence":0.6,"rationale":"关联不唯一"}],"skipped_message_ids":["om_candidate"],"missing_context_message_ids":[]}`,
			want: "reviews[0] source must be a new candidate message",
		},
		{
			name: "skipped context",
			raw:  `{"new_demands":[],"progress_updates":[],"reviews":[],"skipped_message_ids":["om_context","om_candidate"],"missing_context_message_ids":[]}`,
			want: "skipped_message_ids[0] source must be a new candidate message",
		},
		{
			name: "missing context context",
			raw:  `{"new_demands":[],"progress_updates":[],"reviews":[],"skipped_message_ids":["om_candidate"],"missing_context_message_ids":["om_context"]}`,
			want: "missing_context_message_ids[0] source must be a new candidate message",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeAnalysisResult([]byte(test.raw), batches)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
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
