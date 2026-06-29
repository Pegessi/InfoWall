// Command infowall is the CLI entry point for the infowall personal information wall.
// Subcommands:
//
//	serve     start the HTTP + SSE server
//	push      push one or more markdown items (from files, stdin, or heredoc)
//	list      list recent items
//	get       fetch a single item by id
//	pin/unpin toggle the pinned flag on an item
//	delete    delete an item
//	version   print version info
//
// The push, list, get, pin, unpin, and delete commands all accept --json for
// machine-readable output, making the CLI suitable for scripting and agents.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/infowall/infowall/internal/server"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		// errEmitted means the message was already written (e.g. as JSON); just exit.
		var emitted errEmitted
		if !errors.As(err, &emitted) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "serve":
		return cmdServe(rest)
	case "push":
		return cmdPush(rest)
	case "list":
		return cmdList(rest)
	case "get":
		return cmdGet(rest)
	case "pin":
		return cmdPin(rest, true)
	case "unpin":
		return cmdPin(rest, false)
	case "delete":
		return cmdDelete(rest)
	case "version", "--version", "-v":
		fmt.Printf("infowall v%s (commit %s)\n", version, commit)
		return nil
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage() {
	fmt.Println(`infowall — personal information wall

Usage:
  infowall serve   [--addr :8899] [--db infowall.db] [--dev] [--api-key KEY]
  infowall push    [file|- ...] [-t/--topic TOPIC] [--type TYPE] [--pin] [--server URL] [--api-key KEY] [--json]
  infowall list    [--limit N] [--topic TOPIC] [--type TYPE] [--server URL] [--api-key KEY] [--json]
  infowall get     <id> [--raw] [--server URL] [--api-key KEY] [--json]
  infowall pin     <id> [--server URL] [--api-key KEY] [--json]
  infowall unpin   <id> [--server URL] [--api-key KEY] [--json]
  infowall delete  <id> [--yes] [--server URL] [--api-key KEY] [--json]
  infowall version

Examples:
  infowall push paper.md                    # push one file
  infowall push notes/*.md                   # push every *.md via a shell glob
  infowall push a.md b.md c.md               # push several files as separate items
  echo "# done" | infowall push -            # push from stdin
  infowall push - --topic link < link.md      # set the topic explicitly
  infowall push docs/*.md --json             # machine-readable result array
  infowall list --topic paper                 # filter a topic panel
  infowall list --json | jq '.items[].id'     # script-friendly output
  infowall get 1a2b3c4d --json               # fetch one item as JSON

Agent-friendly notes:
  * Every command supports --json for structured stdout; on failure a JSON
    object {"error": "..."} is written to stderr and the exit code is non-zero.
  * push is fully non-interactive when given file arguments, so it never
    blocks waiting for input. Use it to upload complex markdown from files
    instead of squeezing content onto the command line. Push whole folders
    with a shell glob (e.g. notes/*.md); directory arguments are rejected.`)
}

// --- serve ---

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", envOr("INFOWALL_ADDR", ":8899"), "listen address")
	db := fs.String("db", envOr("INFOWALL_DB", "infowall.db"), "SQLite database path")
	dev := fs.Bool("dev", false, "dev mode: proxy frontend to Vite on :5173")
	apiKey := fs.String("api-key", os.Getenv("INFOWALL_API_KEY"), "API key for write/auth")
	fs.Parse(args)

	ctx := context.Background()
	s, err := server.New(ctx, server.Config{
		Addr:   *addr,
		DBPath: *db,
		Dev:    *dev,
		APIKey: *apiKey,
		DistFS: distFS(),
	})
	if err != nil {
		return err
	}
	return s.ListenAndServe()
}

// --- push ---

func cmdPush(args []string) error {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	typ := fs.String("type", "", "compatibility alias for --topic")
	topic := fs.String("topic", "", "override/set item topic (e.g. note, paper)")
	forceTopic := fs.String("t", "", "shorthand for --topic")
	pin := fs.Bool("pin", false, "push as pinned")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if *topic != "" {
		*typ = *topic
	}
	if *forceTopic != "" {
		*typ = *forceTopic
	}

	sources, err := expandSources(fs.Args())
	if err != nil {
		return cfg.fail(err)
	}

	results := make([]pushResult, 0, len(sources))
	var failures int
	for _, src := range sources {
		res := pushOne(src, *typ, *pin, cfg)
		if res.Error != "" {
			failures++
		}
		results = append(results, res)
	}

	if cfg.asJSON {
		// A single source still returns an array for predictable shape.
		writeJSONStdout(results)
	} else {
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintf(os.Stderr, "✗ %s: %s\n", r.Source, r.Error)
				continue
			}
			fmt.Printf("pushed %s (%s) %q  ← %s\n", shortID(r.ID), r.Type, r.Title, r.Source)
		}
	}

	if failures > 0 {
		return errEmitted{fmt.Errorf("%d of %d source(s) failed", failures, len(results))}
	}
	return nil
}

// pushResult is the per-source outcome of a batch push, suitable for JSON output.
type pushResult struct {
	Source string `json:"source"`
	ID     string `json:"id,omitempty"`
	Type   string `json:"type,omitempty"`
	Title  string `json:"title,omitempty"`
	Error  string `json:"error,omitempty"`
}

// pushOne reads, prepares, and uploads a single markdown source. It never returns
// an error; failures are captured in the result so a batch can continue.
func pushOne(src source, typ string, pin bool, cfg *clientConfig) pushResult {
	raw, err := src.read()
	if err != nil {
		return pushResult{Source: src.label, Error: err.Error()}
	}
	raw = applyFrontmatter(raw, typ, pin)

	url := cfg.url("/api/items")
	resp, err := cfg.do(http.MethodPost, url, "text/markdown; charset=utf-8", bytes.NewReader(raw))
	if err != nil {
		return pushResult{Source: src.label, Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return pushResult{Source: src.label, Error: fmt.Sprintf("server %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
	var created struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return pushResult{Source: src.label, Error: fmt.Sprintf("decode response: %v", err)}
	}
	return pushResult{Source: src.label, ID: created.ID, Type: created.Type, Title: created.Title}
}

// applyFrontmatter injects topic/pinned into the markdown when requested. If the
// document already begins with a frontmatter fence the fields are merged in,
// otherwise a minimal frontmatter block is prepended.
func applyFrontmatter(raw []byte, typ string, pin bool) []byte {
	if typ == "" && !pin {
		return raw
	}
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		var fm bytes.Buffer
		fm.WriteString("---\n")
		if typ != "" {
			fmt.Fprintf(&fm, "topic: %s\n", typ)
		}
		if pin {
			fm.WriteString("pinned: true\n")
		}
		fm.WriteString("---\n\n")
		return append(fm.Bytes(), raw...)
	}
	return injectIntoFrontmatter(raw, typ, pin)
}

// --- list ---

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max items to return")
	typeFilter := fs.String("type", "", "compatibility alias for --topic")
	topicFilter := fs.String("topic", "", "filter by topic")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)

	url := fmt.Sprintf("%s?limit=%d", cfg.url("/api/items"), *limit)
	if *topicFilter != "" {
		*typeFilter = *topicFilter
	}
	if *typeFilter != "" {
		url += "&type=" + neturl.QueryEscape(*typeFilter)
	}
	resp, err := cfg.do(http.MethodGet, url, "", nil)
	if err != nil {
		return cfg.fail(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return cfg.fail(fmt.Errorf("server %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}
	if cfg.asJSON {
		writeRawJSONStdout(body)
		return nil
	}

	var list struct {
		Items []listItem `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return cfg.fail(fmt.Errorf("decode: %w", err))
	}
	if len(list.Items) == 0 {
		fmt.Println("(no items — push something with `infowall push <file>`)")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTOPIC\tPINNED\tAGE\tTITLE")
	for _, it := range list.Items {
		pin := " "
		if it.Pinned {
			pin = "📌"
		}
		title := it.Title
		if len(title) > 60 {
			title = title[:57] + "…"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", shortID(it.ID), it.Type, pin, relativeTime(it.CreatedAt), title)
	}
	tw.Flush()
	return nil
}

type listItem struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Pinned    bool      `json:"pinned"`
	CreatedAt time.Time `json:"created_at"`
}

// --- get ---

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	raw := fs.Bool("raw", false, "include the raw markdown source")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() < 1 {
		return cfg.fail(errors.New("usage: infowall get <id> [--raw] [--json]"))
	}
	id := fs.Arg(0)

	url := cfg.url("/api/items/" + id)
	if *raw {
		url += "?raw=1"
	}
	resp, err := cfg.do(http.MethodGet, url, "", nil)
	if err != nil {
		return cfg.fail(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return cfg.fail(fmt.Errorf("server %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}
	if cfg.asJSON {
		writeRawJSONStdout(body)
		return nil
	}

	var it struct {
		ID        string    `json:"id"`
		Type      string    `json:"type"`
		Title     string    `json:"title"`
		Tags      []string  `json:"tags"`
		Pinned    bool      `json:"pinned"`
		Body      string    `json:"body"`
		Raw       string    `json:"raw"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(body, &it); err != nil {
		return cfg.fail(fmt.Errorf("decode: %w", err))
	}
	fmt.Printf("id:      %s\n", it.ID)
	fmt.Printf("topic:   %s\n", it.Type)
	fmt.Printf("title:   %s\n", it.Title)
	if len(it.Tags) > 0 {
		fmt.Printf("tags:    %s\n", strings.Join(it.Tags, ", "))
	}
	fmt.Printf("pinned:  %v\n", it.Pinned)
	fmt.Printf("created: %s\n", it.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Println("---")
	if *raw && it.Raw != "" {
		fmt.Println(it.Raw)
	} else {
		fmt.Println(it.Body)
	}
	return nil
}

// --- pin/unpin ---

func cmdPin(args []string, pinned bool) error {
	action := "pin"
	if !pinned {
		action = "unpin"
	}
	fs := flag.NewFlagSet(action, flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() < 1 {
		return cfg.fail(fmt.Errorf("usage: infowall %s <id> [--json]", action))
	}
	id := fs.Arg(0)

	body, _ := json.Marshal(map[string]any{"pinned": pinned})
	url := cfg.url("/api/items/" + id + "/pin")
	resp, err := cfg.do(http.MethodPost, url, "application/json", bytes.NewReader(body))
	if err != nil {
		return cfg.fail(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return cfg.fail(fmt.Errorf("%s failed (server %d): %s", action, resp.StatusCode, strings.TrimSpace(string(respBody))))
	}
	if cfg.asJSON {
		writeRawJSONStdout(respBody)
		return nil
	}
	fmt.Printf("%sned %s\n", action, id)
	return nil
}

// --- delete ---

func cmdDelete(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	yes := fs.Bool("yes", false, "skip confirmation")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() < 1 {
		return cfg.fail(errors.New("usage: infowall delete <id> [--yes] [--json]"))
	}
	id := fs.Arg(0)
	// In JSON mode we never prompt (agents can't answer); --yes is implied.
	if !*yes && !cfg.asJSON {
		fmt.Printf("delete %s? [y/N] ", id)
		var ans string
		fmt.Fscanln(os.Stdin, &ans)
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			fmt.Println("aborted")
			return nil
		}
	}
	url := cfg.url("/api/items/" + id)
	resp, err := cfg.do(http.MethodDelete, url, "", nil)
	if err != nil {
		return cfg.fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return cfg.fail(fmt.Errorf("delete failed (server %d): %s", resp.StatusCode, strings.TrimSpace(string(b))))
	}
	if cfg.asJSON {
		writeJSONStdout(map[string]any{"id": id, "deleted": true})
		return nil
	}
	fmt.Printf("deleted %s\n", id)
	return nil
}

// --- client config & shared HTTP helpers ---

// clientConfig holds the flags shared by every API-calling subcommand.
type clientConfig struct {
	server string
	apiKey string
	asJSON bool
}

// addClientFlags registers --server, --api-key, and --json on the given flag set
// and returns a clientConfig whose fields are populated after fs.Parse.
func addClientFlags(fs *flag.FlagSet) *clientConfig {
	cfg := &clientConfig{}
	fs.StringVar(&cfg.server, "server", envOr("INFOWALL_URL", "http://localhost:8899"), "server base URL")
	fs.StringVar(&cfg.apiKey, "api-key", os.Getenv("INFOWALL_API_KEY"), "API key")
	fs.BoolVar(&cfg.asJSON, "json", false, "output machine-readable JSON")
	return cfg
}

// boolFlag mirrors the unexported flag.boolFlag interface so we can detect
// boolean flags (which do not consume the following argument) when permuting.
type boolFlag interface {
	IsBoolFlag() bool
}

// parseFlags parses args while tolerating flags placed after positional
// arguments (e.g. `push a.md b.md --json`). The standard library's flag package
// stops at the first non-flag token; this permutes flags ahead of positionals
// first so order does not matter, which is friendlier for scripts and agents.
func parseFlags(fs *flag.FlagSet, args []string) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			// "--name=value" carries its own value.
			name := strings.TrimLeft(a, "-")
			if strings.ContainsRune(name, '=') {
				continue
			}
			// Non-boolean flags consume the next token as their value.
			if f := fs.Lookup(name); f != nil {
				if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
					continue
				}
				if i+1 < len(args) {
					flags = append(flags, args[i+1])
					i++
				}
			}
			continue
		}
		positional = append(positional, a)
	}
	// Append a "--" terminator so the flag package treats every permuted
	// positional literally (e.g. a filename that begins with "-").
	combined := append(flags, "--")
	combined = append(combined, positional...)
	fs.Parse(combined)
}

// url joins the configured server base with an API path.
func (c *clientConfig) url(path string) string {
	return strings.TrimRight(c.server, "/") + path
}

// do issues an HTTP request with auth applied. body may be nil.
func (c *clientConfig) do(method, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	return client.Do(req)
}

// fail emits an error in the configured format. In JSON mode it writes
// {"error": "..."} to stderr; otherwise it returns the error for main to print.
// Either way the returned error triggers a non-zero exit.
func (c *clientConfig) fail(err error) error {
	if c.asJSON {
		enc := json.NewEncoder(os.Stderr)
		enc.Encode(map[string]string{"error": err.Error()})
		return errEmitted{err}
	}
	return err
}

// errEmitted wraps an error whose message has already been shown to the user
// (e.g. as JSON), so main() exits non-zero without printing it again.
type errEmitted struct{ error }

func writeJSONStdout(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// writeRawJSONStdout writes server JSON through unchanged (already valid JSON).
func writeRawJSONStdout(b []byte) {
	os.Stdout.Write(bytes.TrimRight(b, "\n"))
	fmt.Fprintln(os.Stdout)
}

// --- source expansion (files / stdin) ---

// source is a single markdown input: either stdin or a file on disk.
type source struct {
	label   string // human/machine label shown in results
	path    string // file path (empty when stdin)
	isStdin bool
	err     error // expansion-time error (e.g. missing file); surfaced at read()
}

func (s source) read() ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.isStdin {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(s.path)
}

// expandSources turns push arguments into a flat, ordered list of sources.
// "-" (or no arguments at all) means stdin; every other argument is a single
// file used as-is. Directories are rejected (push files explicitly, e.g. via a
// shell glob like `push notes/*.md`). Per-argument problems (a missing path, a
// directory) are attached to the source as an error rather than aborting the
// whole batch, so a batch push still processes its remaining inputs.
func expandSources(args []string) ([]source, error) {
	if len(args) == 0 {
		if isTTY() {
			fmt.Fprintln(os.Stderr, "(reading from stdin; type or pipe markdown, then Ctrl-D to end)")
		}
		return []source{{label: "<stdin>", isStdin: true}}, nil
	}
	var out []source
	for _, arg := range args {
		if arg == "-" {
			out = append(out, source{label: "<stdin>", isStdin: true})
			continue
		}
		info, err := os.Stat(arg)
		if err != nil {
			out = append(out, source{label: arg, path: arg, err: err})
			continue
		}
		if info.IsDir() {
			out = append(out, source{label: arg, err: fmt.Errorf("is a directory (push files individually, e.g. %s/*.md)", strings.TrimRight(arg, "/"))})
			continue
		}
		out = append(out, source{label: arg, path: arg})
	}
	return out, nil
}

// --- helpers ---

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// injectIntoFrontmatter does a dumb line-based insertion of topic/pinned into an existing frontmatter.
// Finds the first "---" fence and the closing "---", then appends fields just before the closing fence.
func injectIntoFrontmatter(raw []byte, typ string, pinned bool) []byte {
	if typ == "" && !pinned {
		return raw
	}
	s := string(raw)
	lines := strings.SplitN(s, "\n", 3)
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return raw
	}
	// Find closing ---
	rest := lines[2]
	idx := strings.Index(rest, "\n---")
	var afterFM string
	if idx < 0 {
		// No closing fence found
		return raw
	}
	fmBody := rest[:idx]
	afterFM = rest[idx+4:]
	afterFM = strings.TrimPrefix(afterFM, "\n")

	var inject []string
	hasType := strings.Contains(fmBody, "\ntype:") || strings.HasPrefix(fmBody, "type:")
	hasTopic := strings.Contains(fmBody, "\ntopic:") || strings.HasPrefix(fmBody, "topic:")
	if typ != "" && !hasType && !hasTopic {
		inject = append(inject, "topic: "+typ)
	}
	if pinned && !strings.Contains(fmBody, "pinned:") {
		inject = append(inject, "pinned: true")
	}
	if len(inject) == 0 {
		return raw
	}
	newFM := fmBody
	if !strings.HasSuffix(newFM, "\n") {
		newFM += "\n"
	}
	newFM += strings.Join(inject, "\n")
	var out strings.Builder
	out.WriteString(lines[0])
	out.WriteString("\n")
	out.WriteString(lines[1])
	out.WriteString("\n")
	out.WriteString(newFM)
	out.WriteString("\n---")
	out.WriteString(afterFM)
	return []byte(out.String())
}
