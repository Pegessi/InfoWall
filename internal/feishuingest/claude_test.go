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

func writeClaudeTabs(t *testing.T, path string, tabs []claudeHubTab) {
	t.Helper()
	raw, err := json.Marshal(tabs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadClaude0821ProfileUsesOnlyLocalClaude0821Tab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabs.json")
	writeClaudeTabs(t, path, []claudeHubTab{
		{ID: "remote-0821", AgentType: "claude", Target: "remote", Env: map[string]string{"ANTHROPIC_MODEL": "model_api/experimental_0821", "ANTHROPIC_AUTH_TOKEN": "remote-secret"}},
		{ID: "local-codex-0821", AgentType: "codex", Target: "local", Env: map[string]string{"ANTHROPIC_MODEL": "model_api/experimental_0821", "ANTHROPIC_AUTH_TOKEN": "codex-secret"}},
		{ID: "local-day1", AgentType: "claude", Target: "local", Env: map[string]string{"ANTHROPIC_MODEL": "auto_model/alwaysday1", "ANTHROPIC_AUTH_TOKEN": "day1-secret"}},
		{ID: "local-0821", AgentType: "claude", Target: "local", Env: map[string]string{"ANTHROPIC_MODEL": "model_api/experimental_0821", "ANTHROPIC_AUTH_TOKEN": "local-secret", "UNRELATED_SECRET": "drop-me"}},
	})
	profile, err := loadClaude0821Profile(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if profile.TabID != "local-0821" || profile.Model != "model_api/experimental_0821" || len(profile.Fingerprint) != 16 {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Env["UNRELATED_SECRET"] != "" || profile.Env["ANTHROPIC_AUTH_TOKEN"] != "local-secret" {
		t.Fatalf("environment filtering failed: %+v", profile.Env)
	}
}

func TestLoadClaude0821ProfileRejectsPinnedNon0821Tab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabs.json")
	writeClaudeTabs(t, path, []claudeHubTab{{ID: "local-day1", AgentType: "claude", Target: "local", Env: map[string]string{
		"ANTHROPIC_MODEL": "auto_model/alwaysday1", "ANTHROPIC_AUTH_TOKEN": "test-token",
	}}})
	_, err := loadClaude0821Profile(path, "local-day1")
	if err == nil || !strings.Contains(err.Error(), "0821") {
		t.Fatalf("expected 0821 profile rejection, got %v", err)
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
	tabsPath := filepath.Join(directory, "tabs.json")
	capturePath := filepath.Join(directory, "capture.txt")
	writeClaudeTabs(t, tabsPath, []claudeHubTab{{ID: "0821", AgentType: "claude", Target: "local", Env: map[string]string{
		"ANTHROPIC_MODEL": "model_api/experimental_0821", "ANTHROPIC_AUTH_TOKEN": "test-token", "CLAUDE_CODE_TEST_CAPTURE": capturePath,
	}}})
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
	result, err := (Claude0821Analyzer{Path: script, HubTabsPath: tabsPath}).Analyze(context.Background(), []AnalysisInput{
		{Messages: []Message{message}, Candidates: []Message{message}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AnalyzerRoute != "claude-0821" || result.AnalyzerProfileID != "0821" ||
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
	tabsPath := filepath.Join(directory, "tabs.json")
	writeClaudeTabs(t, tabsPath, []claudeHubTab{{ID: "0821", AgentType: "claude", Target: "local", Env: map[string]string{
		"ANTHROPIC_MODEL": "model_api/experimental_0821", "ANTHROPIC_AUTH_TOKEN": "test-token",
	}}})
	script := filepath.Join(directory, "fake-claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err := (Claude0821Analyzer{Path: script, HubTabsPath: tabsPath, Timeout: 100 * time.Millisecond}).Analyze(context.Background(), []AnalysisInput{
		{Messages: []Message{{ID: "message-1"}}, Candidates: []Message{{ID: "message-1"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("process group was not terminated promptly: %s", elapsed)
	}
}
