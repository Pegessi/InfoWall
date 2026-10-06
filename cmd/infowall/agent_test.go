package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSpecJSONIsMachineDiscoverable(t *testing.T) {
	stdout, stderr, err := captureCommandOutput(t, func() error {
		return run([]string{"agent", "spec", "--json"})
	})
	if err != nil || stderr != "" {
		t.Fatalf("agent spec failed: err=%v stderr=%q", err, stderr)
	}
	var spec agentSpec
	if err := json.Unmarshal([]byte(stdout), &spec); err != nil {
		t.Fatalf("invalid spec JSON: %v\n%s", err, stdout)
	}
	if spec.SpecVersion != agentSpecVersion || len(spec.Commands) == 0 {
		t.Fatalf("incomplete spec: %+v", spec)
	}
	if spec.SpecVersion != "12" {
		t.Fatalf("expected agent spec v12, got %q", spec.SpecVersion)
	}
	if spec.Transport["read_access"] != "public" || !strings.Contains(spec.Transport["write_auth"].(string), "Bearer") ||
		!strings.Contains(spec.Transport["remote_access"].(string), "HTTPS") {
		t.Fatalf("missing direct client/server authorization contract: %+v", spec.Transport)
	}
	if spec.Transport["integrations_env"] != "INFOWALL_INTEGRATIONS" {
		t.Fatalf("missing integration environment contract: %+v", spec.Transport)
	}
	if got := strings.Join(spec.Enums["integration_mode"], ","); got != "builtin,none" {
		t.Fatalf("unexpected integration modes: %q", got)
	}
	if got := strings.Join(spec.Enums["demand_status"], ","); !strings.Contains(got, "dismissed") {
		t.Fatalf("demand status enum is incomplete: %q", got)
	}
	var sawApply, sawServe bool
	for _, command := range spec.Commands {
		if strings.HasPrefix(command.Path, "demand apply ") {
			sawApply = command.Writes && strings.Contains(command.Idempotency, "dedupe_key")
		}
		if strings.HasPrefix(command.Path, "serve ") {
			sawServe = strings.Contains(command.Path, "--integrations builtin|none") &&
				strings.Contains(command.Notes, "does not remove adapters")
		}
	}
	if !sawApply {
		t.Fatalf("spec does not identify retry-safe demand apply: %+v", spec.Commands)
	}
	if !sawServe {
		t.Fatalf("spec does not describe pure Core startup: %+v", spec.Commands)
	}
	titleQuality, ok := spec.Quality["demand_title"].(map[string]any)
	if !ok {
		t.Fatalf("missing demand title quality contract: %+v", spec.Quality)
	}
	if got := titleQuality["pattern"]; !strings.Contains(got.(string), "concrete outcome") {
		t.Fatalf("title pattern is not semantic enough: %v", got)
	}
	if got := titleQuality["recommended_max_display_characters"]; got != float64(56) {
		t.Fatalf("unexpected title display limit: %v", got)
	}
	contextQuality, ok := spec.Quality["context_enrichment"].(map[string]any)
	if !ok || !strings.Contains(contextQuality["codebase_mr_read"].(string), "mr get") {
		t.Fatalf("missing linked-content enrichment contract: %+v", spec.Quality)
	}
	autoQuality, ok := spec.Quality["automatic_activity_ingestion"].(map[string]any)
	if !ok || !strings.Contains(autoQuality["window"].(string), "5 minutes") {
		t.Fatalf("missing automatic ingestion contract: %+v", spec.Quality)
	}
	if !strings.Contains(autoQuality["sources"].(string), "Remote Hub agents are never collected") ||
		!strings.Contains(autoQuality["self_relevance"].(string), "before Codex") ||
		!strings.Contains(autoQuality["identity"].(string), "open_id") ||
		!strings.Contains(autoQuality["identity"].(string), "needs_refresh") ||
		!strings.Contains(autoQuality["message_ids"].(string), "JSON Schema") {
		t.Fatalf("missing self-relevance gate: %+v", autoQuality)
	}
	linkQuality, ok := spec.Quality["progress_links"].(map[string]any)
	if !ok || !strings.Contains(linkQuality["rule"].(string), "direct http(s) link") {
		t.Fatalf("missing progress link contract: %+v", spec.Quality)
	}
}

func TestInstallConversationHooksPreservesExistingHooksAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{"theme":"dark","hooks":{"Stop":[{"hooks":[{"type":"command","command":"existing-hook"}]}]}}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installConversationHooks(path, "codex", "/old/infowall", "http://127.0.0.1:8899", ""); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := installConversationHooks(path, "codex", "/opt/infowall", "http://127.0.0.1:18999", "/keys/test-key"); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "existing-hook") || !strings.Contains(text, `"theme": "dark"`) {
		t.Fatalf("existing settings were overwritten: %s", text)
	}
	for _, eventName := range []string{"UserPromptSubmit", "Stop"} {
		marker := "hook ingest --source codex --event " + eventName
		if strings.Count(text, marker) != 1 {
			t.Fatalf("hook %s count = %d: %s", eventName, strings.Count(text, marker), text)
		}
	}
	if strings.Contains(text, "/old/infowall") || strings.Contains(text, "127.0.0.1:8899") {
		t.Fatalf("stale hook configuration was retained: %s", text)
	}
	if !strings.Contains(text, "/opt/infowall") || !strings.Contains(text, "127.0.0.1:18999") || !strings.Contains(text, "--api-key-file '/keys/test-key'") {
		t.Fatalf("updated hook configuration is incomplete: %s", text)
	}
	installed, current, err := conversationHooksStatus(path, "codex", "/opt/infowall", "http://127.0.0.1:18999", "/keys/test-key")
	if err != nil || !installed || !current {
		t.Fatalf("hook status = installed:%v current:%v err:%v", installed, current, err)
	}
	_, stale, err := conversationHooksStatus(path, "codex", "/opt/infowall", "http://127.0.0.1:8899", "")
	if err != nil || stale {
		t.Fatalf("stale hook configuration reported current: %v err:%v", stale, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings mode = %v err=%v", info.Mode().Perm(), err)
	}
}

func TestClassifyCLIError(t *testing.T) {
	tests := []struct {
		err       error
		code      string
		retryable bool
		status    int
	}{
		{err: &apiResponseError{StatusCode: 409, Message: "duplicate"}, code: "conflict", status: 409},
		{err: &apiResponseError{StatusCode: 503, Message: "starting"}, code: "server_error", retryable: true, status: 503},
		{err: &apiResponseError{StatusCode: 401, Message: "unauthorized"}, code: "unauthorized", status: 401},
		{err: &apiResponseError{StatusCode: 403, Message: "read-only mirror"}, code: "read_only", status: 403},
	}
	for _, test := range tests {
		got := classifyCLIError(test.err)
		if got.ErrorCode != test.code || got.Retryable != test.retryable || got.HTTPStatus != test.status {
			t.Fatalf("classify %v = %+v", test.err, got)
		}
	}
}
