package main

import (
	"encoding/json"
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
	if spec.SpecVersion != "6" {
		t.Fatalf("expected agent spec v6, got %q", spec.SpecVersion)
	}
	if got := strings.Join(spec.Enums["demand_status"], ","); !strings.Contains(got, "dismissed") {
		t.Fatalf("demand status enum is incomplete: %q", got)
	}
	var sawApply bool
	for _, command := range spec.Commands {
		if strings.HasPrefix(command.Path, "demand apply ") {
			sawApply = command.Writes && strings.Contains(command.Idempotency, "dedupe_key")
		}
	}
	if !sawApply {
		t.Fatalf("spec does not identify retry-safe demand apply: %+v", spec.Commands)
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
	autoQuality, ok := spec.Quality["automatic_feishu_ingestion"].(map[string]any)
	if !ok || !strings.Contains(autoQuality["window"].(string), "minus 5 minutes") {
		t.Fatalf("missing automatic ingestion contract: %+v", spec.Quality)
	}
	if !strings.Contains(autoQuality["self_relevance"].(string), "before Codex") ||
		!strings.Contains(autoQuality["identity"].(string), "open_id") ||
		!strings.Contains(autoQuality["identity"].(string), "needs_refresh") {
		t.Fatalf("missing self-relevance gate: %+v", autoQuality)
	}
	linkQuality, ok := spec.Quality["progress_links"].(map[string]any)
	if !ok || !strings.Contains(linkQuality["rule"].(string), "direct http(s) link") {
		t.Fatalf("missing progress link contract: %+v", spec.Quality)
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
	}
	for _, test := range tests {
		got := classifyCLIError(test.err)
		if got.ErrorCode != test.code || got.Retryable != test.retryable || got.HTTPStatus != test.status {
			t.Fatalf("classify %v = %+v", test.err, got)
		}
	}
}
