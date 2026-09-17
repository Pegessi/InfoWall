// Command infowall is the CLI entry point for the infowall personal information wall.
// Subcommands:
//
//	serve     start the HTTP + SSE server
//	push      push one or more markdown items (from files, stdin, or heredoc)
//	list      list recent items
//	get       fetch a single item by id
//	export    export items for portable archive/audit
//	pin/unpin toggle the pinned flag on an item
//	delete    delete an item
//	health    check that a running server is reachable
//	doctor    run read-only local operation diagnostics
//	demand    create, import, inspect, and update tracked demands
//	project   create and manage demand projects
//	sync      configure and run external integrations
//	agent     print the machine-readable CLI contract
//	version   print version info
//
// The push, list, get, export, pin, unpin, delete, health, and doctor commands
// all accept --json for machine-readable output, making the CLI suitable for
// scripting and agents.
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
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/infowall/infowall/internal/conversationingest"
	"github.com/infowall/infowall/internal/server"
	"github.com/infowall/infowall/internal/store"
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
	case "export":
		return cmdExport(rest)
	case "pin":
		return cmdPin(rest, true)
	case "unpin":
		return cmdPin(rest, false)
	case "delete":
		return cmdDelete(rest)
	case "health":
		return cmdHealth(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "db":
		return cmdDB(rest)
	case "demand":
		return cmdDemand(rest)
	case "project":
		return cmdProject(rest)
	case "sync":
		return cmdSync(rest)
	case "scan":
		return cmdScan(rest)
	case "agent":
		return cmdAgent(rest)
	case "hook":
		return cmdHook(rest)
	case "hooks":
		return cmdHooks(rest)
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
  infowall serve   [--addr :8899] [--db infowall.db] [--default-view infowall|workbench] [--dev] [--api-key KEY]
  infowall push    [file|- ...] [-t/--topic TOPIC] [--type TYPE] [--pin] [--server URL] [--api-key KEY] [--json]
  infowall list    [--limit N] [--topic TOPIC] [--type TYPE] [--server URL] [--api-key KEY] [--json]
  infowall get     <id> [--raw] [--server URL] [--api-key KEY] [--json]
  infowall export  [id ...] [--format jsonl|json] [--out PATH] [--limit N] [--topic TOPIC] [--type TYPE] [--server URL] [--api-key KEY] [--json]
  infowall pin     <id> [--server URL] [--api-key KEY] [--json]
  infowall unpin   <id> [--server URL] [--api-key KEY] [--json]
  infowall delete  <id> [--yes] [--server URL] [--api-key KEY] [--json]
  infowall health  [--server URL] [--api-key KEY] [--json]
  infowall doctor  [--server URL] [--api-key KEY] [--json]
  infowall db info   [--db infowall.db] [--json]
  infowall db backup --out PATH [--db infowall.db] [--json]
  infowall demand <apply|create|import|list|get|update|progress|dismiss|restore|review> [flags]
  infowall project <create|list|update|archive> [flags]
  infowall sync feishu <setup|status|now|disable> [flags]
  infowall scan activity <setup|status|now|runs|disable> [flags]
  infowall scan feishu <setup|status|now|runs|disable> [flags]  # compatibility alias
  infowall agent spec [--json]
  infowall hooks <install|status> [--bin PATH] [--server URL] [--json]
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
  infowall export --out archive.jsonl         # read-only portable archive
  infowall health --json                      # verify the configured server
  infowall doctor --json                      # health + auth diagnostic
  infowall db info --json                     # report local DB path/size/count
  infowall db backup --out backups/wall.db    # safe live backup (VACUUM INTO)
  infowall demand create --title "排查吞吐下降" --status pending --json
  infowall demand apply --input demands.json --json
  infowall demand progress DEMAND_ID --text "已收集日志" --link "https://logs.example/run/1" --json
  infowall project create --name "M15 性能" --json
  infowall sync feishu setup --create --json
  infowall hooks install --json
  infowall scan activity setup --json

Agent-friendly notes:
  * Run infowall agent spec --json to discover commands, enums, input shapes,
    idempotency guarantees, and stable error codes without parsing this text.
  * Every server-talking client command supports --json for structured stdout;
    on failure structured JSON is written to stderr, stdout stays empty, and
    the exit code is non-zero.
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
	defaultView := fs.String("default-view", envOr("INFOWALL_DEFAULT_VIEW", "workbench"), "default frontend: infowall or workbench")
	claudePath := fs.String("claude-path", os.Getenv("INFOWALL_CLAUDE_PATH"), "Claude Code executable (auto-detected by default)")
	claudePresets := fs.String("claude-presets", os.Getenv("INFOWALL_CLAUDE_PRESETS"), "read-only Claude Hub env_presets.json path")
	claudePreset := fs.String("claude-0821-preset", envOr("INFOWALL_CLAUDE_0821_PRESET", "0821"), "named local Claude 0821 environment preset")
	fs.Parse(args)

	ctx := context.Background()
	s, err := server.New(ctx, server.Config{
		Addr:              *addr,
		DBPath:            *db,
		Dev:               *dev,
		APIKey:            *apiKey,
		DistFS:            distFS(),
		DefaultView:       *defaultView,
		ClaudePath:        *claudePath,
		ClaudePresetsPath: *claudePresets,
		ClaudePreset:      *claudePreset,
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

// --- export ---

const exportPageSize = 200

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	format := fs.String("format", "jsonl", "archive format: jsonl or json")
	out := fs.String("out", "", "write archive to this path instead of stdout (refuses to overwrite)")
	limit := fs.Int("limit", 0, "max items to export; 0 means all")
	typeFilter := fs.String("type", "", "compatibility alias for --topic")
	topicFilter := fs.String("topic", "", "filter by topic when exporting from the list API")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)

	if *topicFilter != "" {
		*typeFilter = *topicFilter
	}
	if *limit < 0 {
		return cfg.fail(errors.New("export --limit must be >= 0"))
	}
	formatSet := flagWasSet(fs, "format")
	if cfg.asJSON && *out == "" && !formatSet {
		*format = "json"
	}

	opts := exportOptions{
		Limit:      *limit,
		TypeFilter: *typeFilter,
		IDs:        fs.Args(),
	}
	items, err := collectExportItems(cfg, opts)
	if err != nil {
		return cfg.fail(err)
	}

	normalizedFormat, err := normalizeExportFormat(*format)
	if err != nil {
		return cfg.fail(err)
	}
	if *out == "" {
		if err := writeExport(os.Stdout, normalizedFormat, items); err != nil {
			return cfg.fail(err)
		}
		return nil
	}

	archive, err := writeExportFile(*out, normalizedFormat, items)
	if err != nil {
		return cfg.fail(err)
	}
	summary := exportSummary{
		OK:     true,
		Format: normalizedFormat,
		Count:  len(items),
		Out:    archive.Out,
		Bytes:  archive.SizeBytes,
		Server: strings.TrimRight(cfg.server, "/"),
		Topic:  opts.TypeFilter,
		IDs:    opts.IDs,
	}
	if cfg.asJSON {
		writeJSONStdout(summary)
		return nil
	}
	fmt.Printf("exported %d item(s) to %s (%s, %d bytes)\n", summary.Count, summary.Out, summary.Format, summary.Bytes)
	return nil
}

type exportOptions struct {
	Limit      int
	TypeFilter string
	IDs        []string
}

type exportSummary struct {
	OK     bool     `json:"ok"`
	Format string   `json:"format"`
	Count  int      `json:"count"`
	Out    string   `json:"out,omitempty"`
	Bytes  int64    `json:"bytes,omitempty"`
	Server string   `json:"server"`
	Topic  string   `json:"topic,omitempty"`
	IDs    []string `json:"ids,omitempty"`
}

type exportArchive struct {
	Format string       `json:"format"`
	Count  int          `json:"count"`
	Items  []exportItem `json:"items"`
}

type exportFileResult struct {
	Out       string
	SizeBytes int64
}

type exportItem struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Tags      []string       `json:"tags"`
	Pinned    bool           `json:"pinned"`
	Meta      map[string]any `json:"meta"`
	Body      string         `json:"body"`
	Raw       string         `json:"raw"`
	CreatedAt time.Time      `json:"created_at"`
}

type exportListResponse struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
}

func collectExportItems(cfg *clientConfig, opts exportOptions) ([]exportItem, error) {
	ids := opts.IDs
	if len(ids) == 0 {
		listed, err := listExportIDs(cfg, opts)
		if err != nil {
			return nil, err
		}
		ids = listed
	}

	items := make([]exportItem, 0, len(ids))
	for _, id := range ids {
		it, err := fetchExportItem(cfg, id)
		if err != nil {
			return nil, err
		}
		items = append(items, *it)
	}
	return items, nil
}

func listExportIDs(cfg *clientConfig, opts exportOptions) ([]string, error) {
	var ids []string
	offset := 0
	for {
		pageLimit := exportPageSize
		if opts.Limit > 0 {
			remaining := opts.Limit - len(ids)
			if remaining <= 0 {
				break
			}
			if remaining < pageLimit {
				pageLimit = remaining
			}
		}

		url := fmt.Sprintf("%s?limit=%d&offset=%d", cfg.url("/api/items"), pageLimit, offset)
		if opts.TypeFilter != "" {
			url += "&type=" + neturl.QueryEscape(opts.TypeFilter)
		}
		resp, err := cfg.do(http.MethodGet, url, "", nil)
		if err != nil {
			return nil, fmt.Errorf("list export ids: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read export list response: %w", readErr)
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("list export ids failed (server %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var page exportListResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode export list response: %w", err)
		}
		for _, it := range page.Items {
			if it.ID != "" {
				ids = append(ids, it.ID)
			}
		}
		if len(page.Items) < pageLimit {
			break
		}
		offset += len(page.Items)
	}
	return ids, nil
}

func fetchExportItem(cfg *clientConfig, id string) (*exportItem, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("export id must not be empty")
	}
	url := cfg.url("/api/items/" + neturl.PathEscape(id) + "?raw=1")
	resp, err := cfg.do(http.MethodGet, url, "", nil)
	if err != nil {
		return nil, fmt.Errorf("fetch export item %s: %w", id, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read export item %s response: %w", id, err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch export item %s failed (server %d): %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var item exportItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("decode export item %s: %w", id, err)
	}
	return &item, nil
}

func normalizeExportFormat(format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "jsonl", "ndjson":
		return "jsonl", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported export format %q (want jsonl or json)", format)
	}
}

func writeExport(w io.Writer, format string, items []exportItem) error {
	switch format {
	case "jsonl":
		enc := json.NewEncoder(w)
		for _, it := range items {
			if err := enc.Encode(it); err != nil {
				return fmt.Errorf("write jsonl export: %w", err)
			}
		}
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(exportArchive{Format: "json", Count: len(items), Items: items}); err != nil {
			return fmt.Errorf("write json export: %w", err)
		}
	default:
		return fmt.Errorf("unsupported export format %q", format)
	}
	return nil
}

func writeExportFile(outPath, format string, items []exportItem) (*exportFileResult, error) {
	if strings.TrimSpace(outPath) == "" {
		return nil, errors.New("export --out path must not be empty")
	}
	if dir := filepath.Dir(outPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create export parent directory %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("export target %s already exists (refusing to overwrite; choose a new path)", outPath)
		}
		return nil, fmt.Errorf("create export target %s: %w", outPath, err)
	}
	writeErr := writeExport(f, format, items)
	closeErr := f.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close export target %s: %w", outPath, closeErr)
	}
	absOut, _ := filepath.Abs(outPath)
	res := &exportFileResult{Out: absOut}
	if fi, err := os.Stat(outPath); err == nil {
		res.SizeBytes = fi.Size()
	}
	return res, nil
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
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

// --- health ---

func cmdHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(errors.New("usage: infowall health [--server URL] [--api-key KEY] [--json]"))
	}

	result, err := checkHealth(cfg)
	if err != nil {
		return cfg.fail(err)
	}
	if cfg.asJSON {
		writeJSONStdout(result)
		return nil
	}
	fmt.Printf("ok: %s (%d)\n", result.Server, result.HTTPStatus)
	if result.ServiceStatus != "" {
		fmt.Printf("status: %s\n", result.ServiceStatus)
	}
	if result.TS != "" {
		fmt.Printf("server time: %s\n", result.TS)
	}
	return nil
}

type healthResult struct {
	OK            bool   `json:"ok"`
	Reachable     bool   `json:"reachable"`
	Server        string `json:"server"`
	HTTPStatus    int    `json:"http_status"`
	Service       string `json:"service,omitempty"`
	ServiceStatus string `json:"service_status,omitempty"`
	Version       string `json:"version,omitempty"`
	Commit        string `json:"commit,omitempty"`
	TS            string `json:"ts,omitempty"`
}

type healthPayload struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
	Status  string `json:"status"`
	TS      string `json:"ts"`
}

func checkHealth(cfg *clientConfig) (*healthResult, error) {
	serverURL := strings.TrimRight(cfg.server, "/")
	resp, err := cfg.do(http.MethodGet, cfg.url("/api/health"), "", nil)
	if err != nil {
		return nil, fmt.Errorf("health check %s: %w", serverURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read health response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("health check %s failed (server %d): %s", serverURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload healthPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode health response: %w", err)
	}
	if !payload.OK {
		return nil, fmt.Errorf("health check %s reported not ok", serverURL)
	}

	return &healthResult{
		OK:            true,
		Reachable:     true,
		Server:        serverURL,
		HTTPStatus:    resp.StatusCode,
		Service:       payload.Service,
		ServiceStatus: payload.Status,
		Version:       version,
		Commit:        commit,
		TS:            payload.TS,
	}, nil
}

// --- doctor ---

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(errors.New("usage: infowall doctor [--server URL] [--api-key KEY] [--json]"))
	}

	health, err := checkHealth(cfg)
	if err != nil {
		return cfg.fail(fmt.Errorf("server health: %w", err))
	}
	auth, err := checkAuthRead(cfg)
	if err != nil {
		return cfg.fail(err)
	}

	result := doctorResult{
		OK:        true,
		Server:    strings.TrimRight(cfg.server, "/"),
		Health:    *health,
		Auth:      auth,
		NextSteps: doctorNextSteps(),
	}
	if cfg.asJSON {
		writeJSONStdout(result)
		return nil
	}

	fmt.Printf("ok: %s\n", result.Server)
	fmt.Printf("health: %s (%d)\n", health.ServiceStatus, health.HTTPStatus)
	fmt.Printf("auth: %s (%d)\n", auth.Status, auth.HTTPStatus)
	fmt.Println("next:")
	for _, step := range result.NextSteps {
		fmt.Printf("  - %s\n", step)
	}
	return nil
}

type doctorResult struct {
	OK        bool             `json:"ok"`
	Server    string           `json:"server"`
	Health    healthResult     `json:"health"`
	Auth      doctorAuthResult `json:"auth"`
	NextSteps []string         `json:"next_steps"`
}

type doctorAuthResult struct {
	OK             bool   `json:"ok"`
	Checked        bool   `json:"checked"`
	HTTPStatus     int    `json:"http_status,omitempty"`
	Status         string `json:"status"`
	Path           string `json:"path"`
	APIKeyProvided bool   `json:"api_key_provided"`
	Message        string `json:"message,omitempty"`
}

func checkAuthRead(cfg *clientConfig) (doctorAuthResult, error) {
	serverURL := strings.TrimRight(cfg.server, "/")
	res := doctorAuthResult{
		Checked:        true,
		Path:           "/api/items?limit=1",
		APIKeyProvided: cfg.apiKey != "",
	}

	resp, err := cfg.do(http.MethodGet, cfg.url(res.Path), "", nil)
	if err != nil {
		res.Status = "unreachable"
		return res, fmt.Errorf("auth check %s%s: %w", serverURL, res.Path, err)
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	res.HTTPStatus = resp.StatusCode

	switch {
	case resp.StatusCode == http.StatusOK:
		res.OK = true
		res.Status = "ok"
		res.Message = "read API accepted configured credentials"
		return res, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		res.Status = "unauthorized"
		res.Message = "read API rejected configured credentials"
		return res, fmt.Errorf("auth check %s%s failed: server %d unauthorized", serverURL, res.Path, resp.StatusCode)
	case resp.StatusCode >= 300:
		res.Status = "api_error"
		res.Message = "read API returned a non-success status"
		return res, fmt.Errorf("auth check %s%s failed: server %d", serverURL, res.Path, resp.StatusCode)
	default:
		res.Status = "unexpected"
		return res, fmt.Errorf("auth check %s%s returned unexpected status %d", serverURL, res.Path, resp.StatusCode)
	}
}

func doctorNextSteps() []string {
	return []string{
		"Verify the local DB with `infowall db info --db <path> --json`.",
		"Back up the local DB with `infowall db backup --db <path> --out <backup.db> --json`.",
		"See README formal local operation sections for startup, always-on service, backup, and restore guidance.",
	}
}

// --- db (local SQLite persistence: info + backup) ---

// cmdDB dispatches the local database subcommands. These operate directly on the
// SQLite file (via --db / INFOWALL_DB), not over HTTP, so they work whether or
// not a server is running.
func cmdDB(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: infowall db <info|backup> [flags]")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "info":
		return cmdDBInfo(rest)
	case "backup":
		return cmdDBBackup(rest)
	case "-h", "--help", "help":
		fmt.Println(`infowall db — local SQLite persistence

Usage:
  infowall db info   [--db PATH] [--json]
  infowall db backup --out PATH [--db PATH] [--json]

Both operate on the local database file (--db, or INFOWALL_DB, default
infowall.db); they do not talk to a running server. backup uses SQLite
VACUUM INTO, which is safe to run while the server is live.`)
		return nil
	default:
		return fmt.Errorf("unknown db subcommand %q (want info or backup)", sub)
	}
}

// dbInfo is the machine-readable result of `infowall db info`.
type dbInfo struct {
	Path        string `json:"path"`        // absolute, resolved path
	Exists      bool   `json:"exists"`      // does the main DB file exist?
	SizeBytes   int64  `json:"size_bytes"`  // size of the main DB file (0 if absent)
	WAL         bool   `json:"wal"`         // is a -wal sidecar present?
	SHM         bool   `json:"shm"`         // is a -shm sidecar present?
	Items       int64  `json:"items"`       // row count in items table
	Initialized bool   `json:"initialized"` // schema present / openable
	APIKeySet   bool   `json:"api_key_set"` // is INFOWALL_API_KEY configured?
}

func cmdDBInfo(args []string) error {
	fs := flag.NewFlagSet("db info", flag.ExitOnError)
	dbPath := fs.String("db", envOr("INFOWALL_DB", "infowall.db"), "SQLite database path")
	asJSON := fs.Bool("json", false, "output machine-readable JSON")
	parseFlags(fs, args)

	info, err := collectDBInfo(*dbPath)
	if err != nil {
		return failLocal(*asJSON, err)
	}
	if *asJSON {
		writeJSONStdout(info)
		return nil
	}
	fmt.Printf("path:        %s\n", info.Path)
	fmt.Printf("exists:      %v\n", info.Exists)
	fmt.Printf("size:        %d bytes\n", info.SizeBytes)
	fmt.Printf("wal/shm:     %v / %v\n", info.WAL, info.SHM)
	fmt.Printf("items:       %d\n", info.Items)
	fmt.Printf("initialized: %v\n", info.Initialized)
	fmt.Printf("api key set: %v\n", info.APIKeySet)
	return nil
}

// collectDBInfo gathers status about the local DB file. Opening the store also
// initializes the schema (CREATE TABLE IF NOT EXISTS) — matching what `serve`
// would do — so `db info` on a fresh path reports a usable, initialized DB.
func collectDBInfo(dbPath string) (*dbInfo, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		abs = dbPath
	}
	info := &dbInfo{Path: abs, APIKeySet: os.Getenv("INFOWALL_API_KEY") != ""}

	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	info.Initialized = true

	n, err := st.Count(context.Background())
	if err != nil {
		return nil, fmt.Errorf("count items in %s: %w", abs, err)
	}
	info.Items = n

	if fi, err := os.Stat(dbPath); err == nil {
		info.Exists = true
		info.SizeBytes = fi.Size()
	}
	if _, err := os.Stat(dbPath + "-wal"); err == nil {
		info.WAL = true
	}
	if _, err := os.Stat(dbPath + "-shm"); err == nil {
		info.SHM = true
	}
	return info, nil
}

// dbBackupResult is the machine-readable result of `infowall db backup`.
type dbBackupResult struct {
	Source    string `json:"source"`     // resolved source DB path
	Out       string `json:"out"`        // resolved backup path
	SizeBytes int64  `json:"size_bytes"` // size of the written backup
}

func cmdDBBackup(args []string) error {
	fs := flag.NewFlagSet("db backup", flag.ExitOnError)
	dbPath := fs.String("db", envOr("INFOWALL_DB", "infowall.db"), "SQLite database path to back up")
	out := fs.String("out", "", "destination path for the backup (required; must not exist)")
	asJSON := fs.Bool("json", false, "output machine-readable JSON")
	parseFlags(fs, args)

	if *out == "" {
		return failLocal(*asJSON, errors.New("usage: infowall db backup --out PATH [--db PATH] [--json]"))
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return failLocal(*asJSON, err)
	}
	defer st.Close()

	if err := st.Backup(context.Background(), *out); err != nil {
		return failLocal(*asJSON, err)
	}

	absSrc, _ := filepath.Abs(*dbPath)
	absOut, _ := filepath.Abs(*out)
	res := &dbBackupResult{Source: absSrc, Out: absOut}
	if fi, err := os.Stat(*out); err == nil {
		res.SizeBytes = fi.Size()
	}
	if *asJSON {
		writeJSONStdout(res)
		return nil
	}
	fmt.Printf("backed up %s → %s (%d bytes)\n", res.Source, res.Out, res.SizeBytes)
	return nil
}

// --- local agent hooks ---

func cmdHook(args []string) error {
	if len(args) == 0 || args[0] != "ingest" {
		return errors.New("usage: infowall hook ingest --source codex|claude --event UserPromptSubmit|Stop")
	}
	fs := flag.NewFlagSet("hook ingest", flag.ContinueOnError)
	source := fs.String("source", "", "hook source")
	eventName := fs.String("event", "", "hook event name")
	stateDir := fs.String("state-dir", conversationingest.DefaultStateDir(), "private hook state directory")
	cfg := addClientFlags(fs)
	parseFlags(fs, args[1:])
	// Hooks must never interrupt the user's agent session. All failures become a
	// best-effort spool write and the protocol response remains an empty object.
	defer fmt.Fprintln(os.Stdout, "{}")
	if os.Getenv("INFOWALL_SUMMARY_RUNNER") == "1" {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 2<<20))
	if err != nil {
		return nil
	}
	event, err := conversationingest.NormalizeHookPayload(*source, *eventName, raw, *stateDir)
	if err != nil {
		return nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.url("/api/integrations/conversations/events"), bytes.NewReader(payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		if cfg.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
		}
		response, requestErr := (&http.Client{Timeout: 750 * time.Millisecond}).Do(req)
		if requestErr == nil {
			io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
	}
	_ = conversationingest.WriteSpool(*stateDir, event)
	return nil
}

func cmdHooks(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: infowall hooks <install|status> [--bin PATH] [--server URL] [--json]")
	}
	fs := flag.NewFlagSet("hooks "+args[0], flag.ContinueOnError)
	binPath := fs.String("bin", "", "absolute infowall binary path")
	serverURL := fs.String("server", envOr("INFOWALL_URL", "http://localhost:8899"), "InfoWall server URL")
	asJSON := fs.Bool("json", false, "output machine-readable JSON")
	parseFlags(fs, args[1:])
	home, err := os.UserHomeDir()
	if err != nil {
		return failLocal(*asJSON, err)
	}
	if *binPath == "" {
		*binPath, err = os.Executable()
		if err != nil {
			return failLocal(*asJSON, err)
		}
	}
	*binPath, _ = filepath.Abs(*binPath)
	paths := map[string]string{"codex": filepath.Join(home, ".codex", "hooks.json"),
		"claude": filepath.Join(home, ".claude", "settings.json")}
	switch args[0] {
	case "install":
		for source, path := range paths {
			if err := installConversationHooks(path, source, *binPath, *serverURL); err != nil {
				return failLocal(*asJSON, err)
			}
		}
	case "status":
		// Read-only below.
	default:
		return failLocal(*asJSON, errors.New("hooks action must be install or status"))
	}
	status := map[string]any{"binary": *binPath, "server": *serverURL}
	for source, path := range paths {
		installed, readErr := conversationHooksInstalled(path, source)
		if readErr != nil {
			return failLocal(*asJSON, readErr)
		}
		entry := map[string]any{"installed": installed, "path": path}
		if source == "codex" {
			entry["trust_required"] = true
			entry["trust_hint"] = "Open /hooks in Codex and trust this hook before expecting events."
		}
		status[source] = entry
	}
	if *asJSON {
		writeJSONStdout(status)
	} else {
		fmt.Printf("Codex hooks: %v\nClaude hooks: %v\n", status["codex"], status["claude"])
	}
	return nil
}

func installConversationHooks(path, source, binPath, serverURL string) error {
	root := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	for _, eventName := range []string{"UserPromptSubmit", "Stop"} {
		entries, _ := hooks[eventName].([]any)
		marker := "hook ingest --source " + source + " --event " + eventName
		if jsonContainsString(entries, marker) {
			continue
		}
		guard := `[ "${INFOWALL_SUMMARY_RUNNER:-}" = "1" ]`
		command := "if " + guard + "; then cat >/dev/null 2>&1 || :; else " +
			shellQuote(binPath) + " hook ingest --source " + source + " --event " + eventName +
			" --server " + shellQuote(serverURL) + "; fi"
		entries = append(entries, map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": command, "timeout": 1,
		}}})
		hooks[eventName] = entries
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".infowall.tmp"
	if err := os.WriteFile(temporary, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func conversationHooksInstalled(path, source string) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return false, err
	}
	return jsonContainsString(root["hooks"], "hook ingest --source "+source), nil
}

func jsonContainsString(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case []any:
		for _, item := range typed {
			if jsonContainsString(item, needle) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if jsonContainsString(item, needle) {
				return true
			}
		}
	}
	return false
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// failLocal emits an error for the local (non-HTTP) db commands using the same
// contract as clientConfig.fail on stderr in --json mode,
// otherwise a plain returned error. Either way the process exits non-zero.
func failLocal(asJSON bool, err error) error {
	if asJSON {
		writeCLIErrorJSON(err)
		return errEmitted{err}
	}
	return err
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
// a structured envelope to stderr; otherwise it returns the error for main to print.
// Either way the returned error triggers a non-zero exit.
func (c *clientConfig) fail(err error) error {
	if c.asJSON {
		writeCLIErrorJSON(err)
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
