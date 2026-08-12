// Package conversationingest implements the small, non-blocking boundary
// between local agent lifecycle hooks and InfoWall's scheduled demand worker.
package conversationingest

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/infowall/infowall/internal/model"
)

const maxHookTextRunes = 12000

var httpURLPattern = regexp.MustCompile(`https?://[^\s<>"'\]\)]+`)

// NormalizeHookPayload converts either a Codex or Claude hook payload into the
// single server-side event shape. The raw payload is never persisted.
func NormalizeHookPayload(source, eventName string, raw []byte, stateDir string) (model.ConversationHookEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return model.ConversationHookEvent{}, fmt.Errorf("decode hook payload: %w", err)
	}
	source = strings.ToLower(strings.TrimSpace(source))
	eventName = strings.TrimSpace(eventName)
	if source != "codex" && source != "claude" {
		return model.ConversationHookEvent{}, errors.New("hook source must be codex or claude")
	}
	if eventName != "UserPromptSubmit" && eventName != "Stop" {
		return model.ConversationHookEvent{}, errors.New("hook event must be UserPromptSubmit or Stop")
	}
	if os.Getenv("INFOWALL_SUMMARY_RUNNER") == "1" {
		return model.ConversationHookEvent{}, errors.New("summary runner events are excluded")
	}

	transcriptPath := firstString(payload, "transcript_path", "transcriptPath")
	turnID := firstString(payload, "turn_id", "turnId")
	if source == "claude" && transcriptPath != "" && stateDir != "" {
		if eventName == "UserPromptSubmit" {
			_ = RecordTranscriptCursor(stateDir, transcriptPath)
		}
		if offset, cursorErr := transcriptCursorOffset(stateDir, transcriptPath); cursorErr == nil && turnID == "" {
			turnID = "transcript-byte-" + strconv.FormatInt(offset, 10)
		}
	}
	prompt := firstString(payload, "prompt", "user_prompt", "userPrompt")
	result := firstString(payload, "last_assistant_message", "lastAssistantMessage", "result")
	if source == "claude" && eventName == "Stop" && strings.TrimSpace(result) == "" && transcriptPath != "" && stateDir != "" {
		result, _ = ReadClaudeTranscriptIncrement(stateDir, transcriptPath)
	}
	sessionID := firstString(payload, "session_id", "sessionId")
	if sessionID == "" {
		return model.ConversationHookEvent{}, errors.New("hook payload is missing session_id")
	}
	occurredAt := time.Now().UTC()
	if rawTime := firstString(payload, "timestamp", "occurred_at"); rawTime != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTime); err == nil {
			occurredAt = parsed.UTC()
		}
	}
	combined := prompt + "\n" + result
	stableIdentity := source + "\x00" + eventName + "\x00" + sessionID + "\x00" + turnID
	if turnID == "" {
		stableIdentity += "\x00" + string(raw)
	}
	eventHash := sha256.Sum256([]byte(stableIdentity))
	return model.ConversationHookEvent{
		ID: "conversation-hook:" + hex.EncodeToString(eventHash[:16]), Source: source, EventName: eventName, SessionID: sessionID,
		TurnID: turnID, CWD: firstString(payload, "cwd"),
		URL:    firstString(payload, "url", "thread_url", "threadUrl", "conversation_url", "conversationUrl"),
		Prompt: truncateRunes(prompt, maxHookTextRunes), Result: truncateRunes(result, maxHookTextRunes),
		TranscriptPath: transcriptPath, Links: ExtractLinks(combined), OccurredAt: occurredAt,
	}, nil
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// ExtractLinks retains only verified direct URLs already present in the hook
// text. It never manufactures a route from an opaque identifier.
func ExtractLinks(text string) []model.ProgressLink {
	seen := map[string]struct{}{}
	result := make([]model.ProgressLink, 0)
	for _, raw := range httpURLPattern.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,;:!?)]}，。；：！？）】")
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		hash := sha256.Sum256([]byte(raw))
		result = append(result, model.ProgressLink{Kind: linkKind(raw), Title: linkTitle(raw), URL: raw,
			DedupeKey: "conversation-link:" + hex.EncodeToString(hash[:8])})
	}
	return result
}

func linkKind(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "merge_requests") || strings.Contains(lower, "pull/"):
		return "codebase-mr"
	case strings.Contains(lower, "evaluation") || strings.Contains(lower, "arena"):
		return "arena-eval"
	case strings.Contains(lower, "trial") || strings.Contains(lower, "jobrun"):
		return "seed-jobrun"
	case strings.Contains(lower, "feishu") || strings.Contains(lower, "larksuite"):
		return "feishu-doc"
	default:
		return "link"
	}
}

func linkTitle(raw string) string {
	kind := linkKind(raw)
	label := map[string]string{"codebase-mr": "Codebase MR", "arena-eval": "Arena 评测",
		"seed-jobrun": "JobRun / Trial", "feishu-doc": "飞书文档", "link": "相关链接"}[kind]
	parsed, err := url.Parse(raw)
	if err != nil {
		return label
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		locator := parts[len(parts)-1]
		if len(locator) <= 48 {
			return label + " · " + locator
		}
	}
	return label + " · " + parsed.Host
}

func cursorPath(stateDir, transcriptPath string) string {
	hash := sha256.Sum256([]byte(transcriptPath))
	return filepath.Join(stateDir, "hook-cursors", hex.EncodeToString(hash[:16])+".cursor")
}

// RecordTranscriptCursor snapshots the current transcript size at prompt time
// so Stop reads only records written for the current turn.
func RecordTranscriptCursor(stateDir, transcriptPath string) error {
	info, err := os.Stat(transcriptPath)
	if err != nil {
		return err
	}
	path := cursorPath(stateDir, transcriptPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.FormatInt(info.Size(), 10)), 0o600)
}

func transcriptCursorOffset(stateDir, transcriptPath string) (int64, error) {
	raw, err := os.ReadFile(cursorPath(stateDir, transcriptPath))
	if err != nil {
		return 0, err
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || offset < 0 {
		return 0, errors.New("Claude transcript cursor is invalid")
	}
	return offset, nil
}

// ReadClaudeTranscriptIncrement reads at most 2 MiB after the recorded prompt
// cursor and returns the last assistant text fragment in that increment.
func ReadClaudeTranscriptIncrement(stateDir, transcriptPath string) (string, error) {
	path := cursorPath(stateDir, transcriptPath)
	offset, cursorErr := transcriptCursorOffset(stateDir, transcriptPath)
	if cursorErr != nil {
		// Without a prompt-time cursor, fail closed instead of scanning a full
		// historical transcript.
		return "", errors.New("Claude transcript cursor is unavailable")
	}
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if offset < 0 || offset > info.Size() {
		return "", errors.New("Claude transcript cursor is outside the file")
	}
	readOffset := offset
	if available := info.Size() - offset; available > 2<<20 {
		// Tool-heavy turns can exceed the safety cap. Keep the tail where the
		// final assistant record lives rather than retaining intermediate logs.
		readOffset = info.Size() - (2 << 20)
	}
	if _, err := file.Seek(readOffset, io.SeekStart); err != nil {
		return "", err
	}
	reader := bufio.NewScanner(io.LimitReader(file, 2<<20))
	reader.Buffer(make([]byte, 64*1024), 2<<20)
	last := ""
	for reader.Scan() {
		if text := assistantText(reader.Bytes()); text != "" {
			last = text
		}
	}
	if err := reader.Err(); err != nil {
		return "", err
	}
	_ = os.WriteFile(path, []byte(strconv.FormatInt(info.Size(), 10)), 0o600)
	return truncateRunes(last, maxHookTextRunes), nil
}

func assistantText(line []byte) string {
	var record map[string]any
	if json.Unmarshal(line, &record) != nil || firstString(record, "type") != "assistant" {
		return ""
	}
	message, _ := record["message"].(map[string]any)
	content, _ := message["content"].([]any)
	parts := make([]string, 0)
	for _, item := range content {
		block, _ := item.(map[string]any)
		if text, _ := block["text"].(string); strings.TrimSpace(text) != "" {
			parts = append(parts, strings.TrimSpace(text))
		}
	}
	return strings.Join(parts, "\n")
}

// DefaultStateDir returns the private local state root used by hook cursors and
// the offline spool.
func DefaultStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".infowall")
}

func SpoolDir(stateDir string) string { return filepath.Join(stateDir, "spool", "conversations") }

// WriteSpool persists one normalized event after a local API failure.
func WriteSpool(stateDir string, event model.ConversationHookEvent) error {
	dir := SpoolDir(stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, event.ID+".json"), raw, 0o600)
}

// DrainSpool imports normalized events and deletes a file only after the store
// acknowledges it. Files older than 24 hours are removed without logging body
// contents.
func DrainSpool(ctx context.Context, stateDir string, put func(context.Context, model.ConversationHookEvent) error) (int, error) {
	dir := SpoolDir(stateDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, statErr := entry.Info()
		if statErr == nil && time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(path)
			continue
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return count, readErr
		}
		var event model.ConversationHookEvent
		if json.Unmarshal(raw, &event) != nil {
			_ = os.Remove(path)
			continue
		}
		if err := put(ctx, event); err != nil {
			return count, err
		}
		if err := os.Remove(path); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
