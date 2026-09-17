package feishuingest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeClaudePresets(t *testing.T, path string, presets []claudeEnvPreset) {
	t.Helper()
	raw, err := json.Marshal(claudePresetFile{CustomPresets: presets})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadClaude0821ProfileUsesOnlyNamed0821Preset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env_presets.json")
	writeClaudePresets(t, path, []claudeEnvPreset{
		{ID: "day1", Name: "day1", Text: "ANTHROPIC_MODEL=auto_model/alwaysday1\nANTHROPIC_AUTH_TOKEN=day1-secret"},
		{ID: "0821-id", Name: "0821", Text: "# local-only preset\nANTHROPIC_MODEL='model_api/experimental_0821'\nANTHROPIC_AUTH_TOKEN=local-secret\nUNRELATED_SECRET=drop-me"},
	})
	profile, err := loadClaude0821Profile(path, "0821")
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != "0821-id" || profile.Model != "model_api/experimental_0821" || len(profile.Fingerprint) != 16 {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Env["UNRELATED_SECRET"] != "" || profile.Env["ANTHROPIC_AUTH_TOKEN"] != "local-secret" {
		t.Fatalf("environment filtering failed: %+v", profile.Env)
	}
}

func TestLoadClaude0821ProfileRejectsNamedNon0821Preset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env_presets.json")
	writeClaudePresets(t, path, []claudeEnvPreset{{ID: "day1", Name: "day1", Text: "ANTHROPIC_MODEL=auto_model/alwaysday1\nANTHROPIC_AUTH_TOKEN=test-token"}})
	_, err := loadClaude0821Profile(path, "day1")
	if err == nil || !strings.Contains(err.Error(), "0821") {
		t.Fatalf("expected 0821 profile rejection, got %v", err)
	}
}

func TestLoadClaude0821ProfileRejectsInvalidPresetLineWithoutLeakingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env_presets.json")
	writeClaudePresets(t, path, []claudeEnvPreset{{ID: "0821-id", Name: "0821", Text: "ANTHROPIC_MODEL=model_api/experimental_0821\nnot an assignment secret-value"}})
	_, err := loadClaude0821Profile(path, "0821")
	if err == nil || !strings.Contains(err.Error(), "line 2") || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("expected safe parse error, got %v", err)
	}
}

func TestClaudeProfileFingerprintDoesNotPersistCredentialMaterial(t *testing.T) {
	first := profileFingerprint("0821", map[string]string{"ANTHROPIC_MODEL": "model_api/experimental_0821", "ANTHROPIC_AUTH_TOKEN": "secret-a"})
	second := profileFingerprint("0821", map[string]string{"ANTHROPIC_MODEL": "model_api/experimental_0821", "ANTHROPIC_AUTH_TOKEN": "secret-b"})
	if first != second || strings.Contains(first, "secret") {
		t.Fatalf("credential value changed non-secret profile fingerprint: %q %q", first, second)
	}
}

func TestClaude0821AnalyzerUsesFileBackedRestrictedEphemeralRun(t *testing.T) {
	directory := t.TempDir()
	presetsPath := filepath.Join(directory, "env_presets.json")
	capturePath := filepath.Join(directory, "capture.txt")
	writeClaudePresets(t, presetsPath, []claudeEnvPreset{{ID: "preset-0821", Name: "0821", Text: "ANTHROPIC_MODEL=model_api/experimental_0821\nANTHROPIC_AUTH_TOKEN=test-token\nCLAUDE_CODE_TEST_CAPTURE=" + capturePath}})
	script := filepath.Join(directory, "fake-claude")
	contents := `#!/bin/sh
printf '%s\n' "$*" > "$CLAUDE_CODE_TEST_CAPTURE"
cat >> "$CLAUDE_CODE_TEST_CAPTURE"
printf '%s\n' '{"structured_output":{"new_demands":[],"progress_updates":[],"reviews":[],"skipped_message_ids":[],"missing_context_message_ids":["message-1"]},"usage":{"input_tokens":20,"cache_read_input_tokens":11,"output_tokens":3}}'
`
	if err := os.WriteFile(script, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	message := Message{ID: "message-1", SourceKind: "codex-conversation", Content: "private final result"}
	result, err := (Claude0821Analyzer{Path: script, PresetsPath: presetsPath, Preset: "0821"}).Analyze(context.Background(), []AnalysisInput{
		{Messages: []Message{message}, Candidates: []Message{message}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AnalyzerRoute != "claude-0821" || result.AnalyzerProfileID != "preset-0821" ||
		len(result.AnalyzerProfileFingerprint) != 16 || !result.AnalyzerHealthy ||
		result.InputTokens != 20 || result.CachedInputTokens != 11 || result.OutputTokens != 3 {
		t.Fatalf("result = %+v", result)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(captured)
	for _, required := range []string{"--no-session-persistence", "--disable-slash-commands", "--strict-mcp-config", "Read,Glob", "infowall-claude-ingestion-"} {
		if !strings.Contains(text, required) {
			t.Fatalf("Claude invocation is missing %q: %s", required, text)
		}
	}
	if strings.Contains(text, "private final result") {
		t.Fatalf("conversation content was embedded in the initial prompt: %s", text)
	}
}

func TestFallbackAnalyzerRetriesClaudeOnceThenUsesCodex(t *testing.T) {
	primary := &fakeAnalyzer{err: errors.New("0821 unavailable")}
	fallback := &fakeAnalyzer{result: Result{MissingContextIDs: []string{"message-1"}}}
	result, err := (FallbackAnalyzer{Primary: primary, Fallback: fallback}).Analyze(context.Background(), []AnalysisInput{
		{Messages: []Message{{ID: "message-1"}}, Candidates: []Message{{ID: "message-1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if primary.calls != 2 || fallback.calls != 1 || !result.FallbackUsed || result.AnalyzerRoute != "codex" || !strings.Contains(result.PrimaryError, "0821 unavailable") {
		t.Fatalf("calls primary=%d fallback=%d result=%+v", primary.calls, fallback.calls, result)
	}
}

func TestClaude0821TimeoutKillsProcessGroup(t *testing.T) {
	directory := t.TempDir()
	presetsPath := filepath.Join(directory, "env_presets.json")
	writeClaudePresets(t, presetsPath, []claudeEnvPreset{{ID: "preset-0821", Name: "0821", Text: "ANTHROPIC_MODEL=model_api/experimental_0821\nANTHROPIC_AUTH_TOKEN=test-token"}})
	script := filepath.Join(directory, "fake-claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err := (Claude0821Analyzer{Path: script, PresetsPath: presetsPath, Preset: "0821", Timeout: 100 * time.Millisecond}).Analyze(context.Background(), []AnalysisInput{
		{Messages: []Message{{ID: "message-1"}}, Candidates: []Message{{ID: "message-1"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("process group was not terminated promptly: %s", elapsed)
	}
}

// TestRealClaude0821SchemaProbe deliberately remains opt-in: it validates the
// real named preset and fresh no-session-persistence Claude process without
// reading any user transcript or writing to the InfoWall database.
func TestRealClaude0821SchemaProbe(t *testing.T) {
	if os.Getenv("INFOWALL_REAL_CLAUDE_0821_PROBE") != "1" {
		t.Skip("set INFOWALL_REAL_CLAUDE_0821_PROBE=1 to run the real Claude 0821 probe")
	}
	message := Message{
		ID:         "infowall-0821-schema-probe",
		SourceKind: "codex-conversation",
		Content:    "Schema probe only. This message carries no demand update and should be skipped or marked missing context.",
	}
	result, err := (Claude0821Analyzer{Preset: "0821", Timeout: 90 * time.Second}).Analyze(context.Background(), []AnalysisInput{{
		Messages:   []Message{message},
		Candidates: []Message{message},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.AnalyzerHealthy || result.AnalyzerRoute != "claude-0821" || result.AnalyzerProfileID == "" || len(result.AnalyzerProfileFingerprint) != 16 {
		t.Fatalf("unexpected probe result: %+v", result)
	}
	t.Logf("fresh Claude 0821 probe succeeded: profile=%s tokens=%d/%d/%d", result.AnalyzerProfileID, result.InputTokens, result.CachedInputTokens, result.OutputTokens)
}
