package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandSources(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "b.md"), "# B")
	mustWrite(t, filepath.Join(dir, "a.markdown"), "# A")

	single := filepath.Join(dir, "b.md")
	other := filepath.Join(dir, "a.markdown")

	t.Run("directory is rejected as a per-source error", func(t *testing.T) {
		srcs, err := expandSources([]string{dir})
		if err != nil {
			t.Fatalf("expandSources should not hard-fail: %v", err)
		}
		if len(srcs) != 1 || srcs[0].err == nil {
			t.Fatalf("expected a single errored source for a directory, got %+v", srcs)
		}
		if _, rerr := srcs[0].read(); rerr == nil {
			t.Fatal("read() should report the directory error")
		}
	})

	t.Run("explicit file used as-is", func(t *testing.T) {
		srcs, err := expandSources([]string{single})
		if err != nil {
			t.Fatal(err)
		}
		if len(srcs) != 1 || srcs[0].path != single {
			t.Fatalf("unexpected: %+v", srcs)
		}
	})

	t.Run("dash means stdin", func(t *testing.T) {
		srcs, err := expandSources([]string{"-"})
		if err != nil {
			t.Fatal(err)
		}
		if len(srcs) != 1 || !srcs[0].isStdin {
			t.Fatalf("expected stdin source, got %+v", srcs)
		}
	})

	t.Run("multiple files + stdin preserve order", func(t *testing.T) {
		srcs, err := expandSources([]string{single, "-", other})
		if err != nil {
			t.Fatal(err)
		}
		if len(srcs) != 3 {
			t.Fatalf("want 3 sources, got %d: %+v", len(srcs), srcs)
		}
		if srcs[0].path != single || !srcs[1].isStdin || srcs[2].path != other {
			t.Fatalf("order not preserved: %+v", srcs)
		}
	})

	t.Run("missing path surfaces as per-source error", func(t *testing.T) {
		srcs, err := expandSources([]string{filepath.Join(dir, "nope.md")})
		if err != nil {
			t.Fatalf("expandSources should not hard-fail: %v", err)
		}
		if len(srcs) != 1 || srcs[0].err == nil {
			t.Fatalf("expected a single source carrying an error, got %+v", srcs)
		}
		if _, rerr := srcs[0].read(); rerr == nil {
			t.Fatal("read() should report the missing-file error")
		}
	})

	t.Run("bad path does not drop later valid sources", func(t *testing.T) {
		srcs, err := expandSources([]string{filepath.Join(dir, "nope.md"), single})
		if err != nil {
			t.Fatal(err)
		}
		if len(srcs) != 2 {
			t.Fatalf("want 2 sources, got %d: %+v", len(srcs), srcs)
		}
		if srcs[0].err == nil {
			t.Fatal("first source should carry an error")
		}
		if srcs[1].err != nil || srcs[1].path != single {
			t.Fatalf("second valid source damaged: %+v", srcs[1])
		}
	})
}

func TestApplyFrontmatter(t *testing.T) {
	t.Run("no type/pin returns input unchanged", func(t *testing.T) {
		in := []byte("# hello\n")
		out := applyFrontmatter(in, "", false)
		if !bytes.Equal(in, out) {
			t.Fatalf("expected unchanged, got %q", out)
		}
	})

	t.Run("prepends topic frontmatter when none present", func(t *testing.T) {
		out := string(applyFrontmatter([]byte("# hello\n"), "link", true))
		if !strings.HasPrefix(out, "---\n") {
			t.Fatalf("expected frontmatter fence, got %q", out)
		}
		if !strings.Contains(out, "topic: link") || !strings.Contains(out, "pinned: true") {
			t.Fatalf("missing injected fields: %q", out)
		}
		if strings.Contains(out, "type:") {
			t.Fatalf("new frontmatter should use topic, got %q", out)
		}
		if !strings.Contains(out, "# hello") {
			t.Fatalf("body lost: %q", out)
		}
	})

	t.Run("injects into existing type frontmatter without duplicating", func(t *testing.T) {
		in := []byte("---\ntitle: Hi\ntype: paper\n---\n\nbody\n")
		out := string(applyFrontmatter(in, "note", true))
		// type already present → must NOT be overridden or duplicated.
		if strings.Count(out, "type:") != 1 {
			t.Fatalf("type should not be duplicated/overridden: %q", out)
		}
		if !strings.Contains(out, "type: paper") {
			t.Fatalf("existing type changed: %q", out)
		}
		if !strings.Contains(out, "pinned: true") {
			t.Fatalf("pinned not injected: %q", out)
		}
		if !strings.Contains(out, "title: Hi") || !strings.Contains(out, "body") {
			t.Fatalf("frontmatter/body damaged: %q", out)
		}
	})

	t.Run("injects into existing topic frontmatter without duplicating", func(t *testing.T) {
		in := []byte("---\ntitle: Hi\ntopic: paper\n---\n\nbody\n")
		out := string(applyFrontmatter(in, "note", true))
		if strings.Count(out, "topic:") != 1 {
			t.Fatalf("topic should not be duplicated/overridden: %q", out)
		}
		if !strings.Contains(out, "topic: paper") {
			t.Fatalf("existing topic changed: %q", out)
		}
		if strings.Contains(out, "type:") {
			t.Fatalf("existing topic should not gain type alias: %q", out)
		}
		if !strings.Contains(out, "pinned: true") {
			t.Fatalf("pinned not injected: %q", out)
		}
	})
}

func TestPushOneEndToEnd(t *testing.T) {
	var gotBody []byte
	var gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "abcdef1234567890", "type": "note", "title": "Hello"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "n.md")
	mustWrite(t, file, "# Hello\n\nworld")

	cfg := &clientConfig{server: srv.URL, apiKey: "secret"}
	res := pushOne(source{label: file, path: file}, "", false, cfg)
	if res.Error != "" {
		t.Fatalf("push failed: %s", res.Error)
	}
	if res.ID != "abcdef1234567890" || res.Title != "Hello" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth header not sent: %q", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "text/markdown") {
		t.Fatalf("unexpected content-type: %q", gotCT)
	}
	if !strings.Contains(string(gotBody), "# Hello") {
		t.Fatalf("body not forwarded: %q", gotBody)
	}
}

func TestPushOneServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"unauthorized"}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "n.md")
	mustWrite(t, file, "# Hello")

	cfg := &clientConfig{server: srv.URL}
	res := pushOne(source{label: file, path: file}, "", false, cfg)
	if res.Error == "" {
		t.Fatal("expected error result for 401")
	}
	if !strings.Contains(res.Error, "401") {
		t.Fatalf("error should mention status: %q", res.Error)
	}
}

func TestClientConfigURL(t *testing.T) {
	cfg := &clientConfig{server: "http://x:1/"}
	if got := cfg.url("/api/items"); got != "http://x:1/api/items" {
		t.Fatalf("trailing slash not trimmed: %q", got)
	}
}

func TestParseFlagsPermutesFlagsAfterPositional(t *testing.T) {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	cfg := addClientFlags(fs)
	// Flags appear both before and after positional args.
	parseFlags(fs, []string{"a.md", "--json", "b.md", "--server", "http://x:9", "c.md"})
	if !cfg.asJSON {
		t.Fatal("--json after positional not parsed")
	}
	if cfg.server != "http://x:9" {
		t.Fatalf("--server value after positional not parsed: %q", cfg.server)
	}
	got := fs.Args()
	want := []string{"a.md", "b.md", "c.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("positional args = %v, want %v", got, want)
	}
}

func TestParseFlagsEqualsForm(t *testing.T) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, []string{"--server=http://y:8", "x"})
	if cfg.server != "http://y:8" {
		t.Fatalf("--server=value not parsed: %q", cfg.server)
	}
	if fs.Arg(0) != "x" {
		t.Fatalf("positional lost: %v", fs.Args())
	}
}

func TestParseFlagsDoubleDashStopsParsing(t *testing.T) {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	cfg := addClientFlags(fs)
	// After "--", a literal "--json" must be treated as a positional (e.g. a filename).
	parseFlags(fs, []string{"--", "--json"})
	if cfg.asJSON {
		t.Fatal("--json after -- should be positional, not a flag")
	}
	if fs.Arg(0) != "--json" {
		t.Fatalf("expected literal positional, got %v", fs.Args())
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("abcdef1234"); got != "abcdef12" {
		t.Fatalf("want abcdef12, got %q", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Fatalf("short id should pass through, got %q", got)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
