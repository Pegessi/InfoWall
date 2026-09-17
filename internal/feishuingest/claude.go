package feishuingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Claude0821Analyzer starts an ephemeral, restricted Claude Code process. It
// resolves one named local 0821 environment preset without calling or
// modifying Claude Hub, reusing a Hub tab, or persisting credentials.
type Claude0821Analyzer struct {
	Path        string
	PresetsPath string
	Preset      string
	Timeout     time.Duration
}

type claudePresetFile struct {
	CustomPresets []claudeEnvPreset `json:"custom_presets"`
}

type claudeEnvPreset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

type claude0821Profile struct {
	ID          string
	Model       string
	Fingerprint string
	Env         map[string]string
}

type analyzerProfileProvider interface {
	AnalyzerProfile() (string, string, error)
}

// AnalyzerProfile resolves only non-secret identity metadata. Credentials are
// never returned to the caller or persisted by InfoWall.
func (analyzer Claude0821Analyzer) AnalyzerProfile() (string, string, error) {
	profile, err := loadClaude0821Profile(analyzer.PresetsPath, analyzer.Preset)
	if err != nil {
		return "", "", err
	}
	return profile.ID, profile.Fingerprint, nil
}

func (analyzer Claude0821Analyzer) Analyze(ctx context.Context, batches []AnalysisInput) (Result, error) {
	if len(analysisCandidateMessageIDs(batches)) == 0 {
		return Result{}, nil
	}
	profile, err := loadClaude0821Profile(analyzer.PresetsPath, analyzer.Preset)
	if err != nil {
		return Result{}, err
	}
	path, err := findClaudePath(analyzer.Path)
	if err != nil {
		return Result{}, err
	}
	temporary, err := os.MkdirTemp("", "infowall-claude-ingestion-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(temporary)
	allowed := analysisCandidateMessageIDs(batches)
	workspace, err := writeAnalysisWorkspace(temporary, batches, allowed)
	if err != nil {
		return Result{}, err
	}
	schema, err := analysisSchemaFor(allowed)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(workspace.SchemaPath, schema, 0o600); err != nil {
		return Result{}, err
	}
	timeout := analyzer.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	runContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	allowedRead := "Read(" + workspace.Dir + "/**)"
	allowedGlob := "Glob(" + workspace.Dir + "/**)"
	command := exec.Command(path,
		"--print", "--output-format", "json", "--no-session-persistence",
		"--disable-slash-commands", "--setting-sources", "",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--permission-mode", "dontAsk", "--tools", "Read,Glob",
		"--allowedTools", allowedRead+","+allowedGlob,
		"--disallowedTools", "Bash,Edit,Write,WebFetch,WebSearch,NotebookEdit",
		"--effort", "low", "--model", profile.Model,
		"--system-prompt", analysisPrompt, "--json-schema", string(schema),
	)
	command.Dir = workspace.Dir
	command.Env = claudeCommandEnvironment(profile.Env)
	command.Env = append(command.Env, "INFOWALL_SUMMARY_RUNNER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
	command.Stdin = strings.NewReader("Read and classify this InfoWall manifest using the exact output shapes in the system prompt: " + workspace.ManifestPath)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("start Claude 0821 analysis: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return Result{}, fmt.Errorf("Claude 0821 analysis failed: %s", safeClaudeError(stderr.String(), err))
		}
	case <-runContext.Done():
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-done
		if errors.Is(runContext.Err(), context.DeadlineExceeded) {
			return Result{}, fmt.Errorf("Claude 0821 analysis timed out after %s", timeout)
		}
		return Result{}, runContext.Err()
	}
	output, usage, err := decodeClaudeEnvelope(stdout.Bytes())
	if err != nil {
		return Result{}, err
	}
	result, err := decodeAnalysisResult(output, batches)
	if err != nil {
		return Result{InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens, OutputTokens: usage.OutputTokens}, err
	}
	result.InputTokens = usage.InputTokens
	result.CachedInputTokens = usage.CachedInputTokens
	result.OutputTokens = usage.OutputTokens
	result.AnalyzerRoute = "claude-0821"
	result.AnalyzerProfileID = profile.ID
	result.AnalyzerProfileFingerprint = profile.Fingerprint
	result.AnalyzerHealthy = true
	return result, nil
}

var claudePresetEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func loadClaude0821Profile(path, requestedPreset string) (claude0821Profile, error) {
	if strings.TrimSpace(path) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return claude0821Profile{}, err
		}
		path = filepath.Join(home, ".claude_hub", "env_presets.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return claude0821Profile{}, fmt.Errorf("read Claude Hub environment presets: %w", err)
	}
	var presets claudePresetFile
	if err := json.Unmarshal(raw, &presets); err != nil {
		return claude0821Profile{}, fmt.Errorf("decode Claude Hub environment presets: %w", err)
	}
	requestedPreset = strings.TrimSpace(requestedPreset)
	if requestedPreset == "" {
		requestedPreset = "0821"
	}
	for _, preset := range presets.CustomPresets {
		if preset.ID != requestedPreset && !strings.EqualFold(preset.Name, requestedPreset) {
			continue
		}
		env, err := parseClaudePresetEnvironment(preset.Text)
		if err != nil {
			return claude0821Profile{}, fmt.Errorf("decode Claude Hub environment preset %q: %w", preset.ID, err)
		}
		env = filterClaudeEnvironment(env)
		model := strings.TrimSpace(env["ANTHROPIC_MODEL"])
		if !isClaude0821Model(model) {
			return claude0821Profile{}, fmt.Errorf("Claude Hub environment preset %q does not select an 0821 model", preset.ID)
		}
		if !hasClaudeCredential(env) {
			return claude0821Profile{}, fmt.Errorf("Claude Hub environment preset %q has no usable Claude credential", preset.ID)
		}
		return claude0821Profile{ID: preset.ID, Model: model, Fingerprint: profileFingerprint(preset.ID, env), Env: env}, nil
	}
	return claude0821Profile{}, fmt.Errorf("no usable local Claude 0821 environment preset named %q", requestedPreset)
}

func parseClaudePresetEnvironment(text string) (map[string]string, error) {
	env := make(map[string]string)
	for lineNumber, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) >= len("export ") && strings.EqualFold(line[:len("export ")], "export ") {
			line = strings.TrimSpace(line[len("export "):])
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !claudePresetEnvKey.MatchString(key) {
			return nil, fmt.Errorf("invalid environment assignment at line %d", lineNumber+1)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		env[key] = value
	}
	return env, nil
}

func isClaude0821Model(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "0821")
}

func hasClaudeCredential(env map[string]string) bool {
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_ACCESS_KEY_ID", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"} {
		if strings.TrimSpace(env[key]) != "" {
			return true
		}
	}
	return false
}

func filterClaudeEnvironment(input map[string]string) map[string]string {
	result := make(map[string]string)
	for key, value := range input {
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_CODE_") || strings.HasPrefix(key, "AWS_") || strings.HasPrefix(key, "GOOGLE_") {
			result[key] = value
		}
	}
	return result
}

func profileFingerprint(tabID string, env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	hash.Write([]byte(tabID))
	for _, key := range keys {
		hash.Write([]byte{0})
		hash.Write([]byte(key))
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

func claudeCommandEnvironment(profile map[string]string) []string {
	result := make([]string, 0, len(profile)+5)
	for _, key := range []string{"HOME", "PATH", "TMPDIR", "SHELL", "LANG"} {
		if value := os.Getenv(key); value != "" {
			result = append(result, key+"="+value)
		}
	}
	keys := make([]string, 0, len(profile))
	for key := range profile {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+profile[key])
	}
	return result
}

func findClaudePath(configured string) (string, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured, nil
	}
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	candidates, _ := filepath.Glob(filepath.Join(home, ".local", "share", "claude", "versions", "*"))
	sort.Strings(candidates)
	for index := len(candidates) - 1; index >= 0; index-- {
		if info, err := os.Stat(candidates[index]); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidates[index], nil
		}
	}
	return "", errors.New("find Claude Code executable: not found")
}

type claudeUsage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
}

func decodeClaudeEnvelope(raw []byte) ([]byte, claudeUsage, error) {
	var envelope struct {
		Result         json.RawMessage `json:"structured_output"`
		FallbackResult json.RawMessage `json:"result"`
		Usage          map[string]any  `json:"usage"`
		IsError        bool            `json:"is_error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, claudeUsage{}, fmt.Errorf("decode Claude 0821 response: %w", err)
	}
	if envelope.IsError {
		return nil, claudeUsage{}, errors.New("Claude 0821 returned an error")
	}
	result := envelope.Result
	if len(result) == 0 || string(result) == "null" {
		result = envelope.FallbackResult
	}
	if len(result) == 0 {
		return nil, claudeUsage{}, errors.New("Claude 0821 returned no structured output")
	}
	if result[0] == '"' {
		var decoded string
		if err := json.Unmarshal(result, &decoded); err != nil {
			return nil, claudeUsage{}, err
		}
		result = []byte(decoded)
	}
	usage := claudeUsage{InputTokens: int64Value(envelope.Usage["input_tokens"]),
		CachedInputTokens: int64Value(envelope.Usage["cache_read_input_tokens"]),
		OutputTokens:      int64Value(envelope.Usage["output_tokens"])}
	return result, usage, nil
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}

// FallbackAnalyzer retries the primary once, then uses Codex without changing
// candidate IDs. The safe primary error is surfaced for UI degradation.
type FallbackAnalyzer struct {
	Primary  Analyzer
	Fallback Analyzer
}

func (analyzer FallbackAnalyzer) Analyze(ctx context.Context, batches []AnalysisInput) (Result, error) {
	profileID, profileFingerprint := "", ""
	if provider, ok := analyzer.Primary.(analyzerProfileProvider); ok {
		profileID, profileFingerprint, _ = provider.AnalyzerProfile()
	}
	var primaryErr error
	for attempt := 0; attempt < 2; attempt++ {
		result, err := analyzer.Primary.Analyze(ctx, batches)
		if err == nil {
			if result.AnalyzerRoute == "" {
				result.AnalyzerRoute = "claude-0821"
			}
			if result.AnalyzerProfileID == "" {
				result.AnalyzerProfileID = profileID
				result.AnalyzerProfileFingerprint = profileFingerprint
			}
			result.AnalyzerHealthy = true
			return result, nil
		}
		primaryErr = err
	}
	result, err := analyzer.Fallback.Analyze(ctx, batches)
	if err != nil {
		return result, fmt.Errorf("primary analyzer unavailable and Codex fallback failed: %w", err)
	}
	result.AnalyzerRoute = "codex"
	result.AnalyzerProfileID = profileID
	result.AnalyzerProfileFingerprint = profileFingerprint
	result.AnalyzerHealthy = false
	result.FallbackUsed = true
	result.PrimaryError = safeAnalyzerError(primaryErr)
	return result, nil
}

func safeClaudeError(stderr string, fallback error) string {
	lower := strings.ToLower(stderr)
	for _, signal := range []struct{ contains, message string }{
		{"authentication", "Claude 0821 authentication failed"},
		{"not logged in", "Claude 0821 authentication failed"},
		{"rate limit", "Claude 0821 rate limit reached"},
		{"context window", "Claude 0821 input exceeded context window"},
		{"too many tokens", "Claude 0821 input exceeded context window"},
		{"unknown option", "Claude 0821 rejected an unsupported CLI option"},
		{"invalid value", "Claude 0821 rejected an invalid CLI option value"},
		{"allowedtools", "Claude 0821 rejected the restricted tool allowlist"},
		{"allowed-tools", "Claude 0821 rejected the restricted tool allowlist"},
		{"permission mode", "Claude 0821 rejected the restricted permission mode"},
		{"mcp config", "Claude 0821 rejected the empty MCP configuration"},
		{"model not found", "Claude 0821 model route is unavailable"},
		{"invalid model", "Claude 0821 model route is unavailable"},
		{"model route", "Claude 0821 model route is unavailable"},
	} {
		if strings.Contains(lower, signal.contains) {
			return signal.message
		}
	}
	return fallback.Error()
}

func safeAnalyzerError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}
