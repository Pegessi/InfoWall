package feishuingest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

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
