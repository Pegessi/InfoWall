// Command infowall is the CLI entry point for the infowall personal information wall.
// Subcommands:
//
//	serve     start the HTTP + SSE server
//	push      push a markdown item (from file, stdin, or heredoc)
//	list      list recent items
//	pin/unpin toggle the pinned flag on an item
//	delete    delete an item
//	version   print version info
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
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
		fmt.Fprintln(os.Stderr, "error:", err)
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
	case "pin":
		if len(rest) < 1 {
			return fmt.Errorf("usage: infowall pin <id>")
		}
		return cmdPin(rest[0], true)
	case "unpin":
		if len(rest) < 1 {
			return fmt.Errorf("usage: infowall unpin <id>")
		}
		return cmdPin(rest[0], false)
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
  infowall push    [file|-] [-t/--type TYPE] [--server URL] [--api-key KEY] [--pin]
  infowall list    [--limit N] [--type TYPE] [--json]
  infowall pin     <id>
  infowall unpin   <id>
  infowall delete  <id> [--yes]
  infowall version

Examples:
  infowall push paper.md
  echo "# done" | infowall push -
  cat <<'EOF' | infowall push -
  ---
  type: paper
  title: Attention Is All You Need
  ---
  The dominant sequence transduction models...
  EOF`)
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
	typ := fs.String("type", "", "override/set item type (e.g. note, paper)")
	serverURL := fs.String("server", envOr("INFOWALL_URL", "http://localhost:8899"), "server base URL")
	apiKey := fs.String("api-key", os.Getenv("INFOWALL_API_KEY"), "API key")
	pin := fs.Bool("pin", false, "push as pinned")
	forceType := fs.String("t", "", "shorthand for --type")
	fs.Parse(args)
	if *forceType != "" {
		*typ = *forceType
	}

	file := "-"
	fileGiven := false
	if rest := fs.Args(); len(rest) > 0 {
		file = rest[0]
		fileGiven = true
	}

	raw, err := readInput(file, fileGiven)
	if err != nil {
		return err
	}

	// If --type is set and the input does not start with a frontmatter fence,
	// wrap it with a synthetic frontmatter block.
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	hasFM := bytes.HasPrefix(trimmed, []byte("---"))
	if *typ != "" || *pin {
		if !hasFM {
			var fm bytes.Buffer
			fm.WriteString("---\n")
			if *typ != "" {
				fmt.Fprintf(&fm, "type: %s\n", *typ)
			}
			if *pin {
				fm.WriteString("pinned: true\n")
			}
			fm.WriteString("---\n\n")
			raw = append(fm.Bytes(), raw...)
		} else {
			// Inject type/pinned into existing frontmatter (simple text manipulation, not full YAML parse).
			raw = injectIntoFrontmatter(raw, *typ, *pin)
		}
	}

	url := strings.TrimRight(*serverURL, "/") + "/api/items"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/markdown; charset=utf-8")
	if *apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+*apiKey)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var created struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return err
	}
	shortID := created.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	fmt.Printf("pushed %s (%s) %q\n", shortID, created.Type, created.Title)
	return nil
}

// --- list ---

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max items to return")
	typeFilter := fs.String("type", "", "filter by type")
	asJSON := fs.Bool("json", false, "output raw JSON")
	serverURL := fs.String("server", envOr("INFOWALL_URL", "http://localhost:8899"), "server URL")
	apiKey := fs.String("api-key", os.Getenv("INFOWALL_API_KEY"), "API key")
	fs.Parse(args)

	url := fmt.Sprintf("%s/api/items?limit=%d", strings.TrimRight(*serverURL, "/"), *limit)
	if *typeFilter != "" {
		url += "&type=" + *typeFilter
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if *apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+*apiKey)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("list failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var list struct {
		Items []listItem `json:"items"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if *asJSON {
		fmt.Println(string(body))
		return nil
	}
	if len(list.Items) == 0 {
		fmt.Println("(no items — push something with `infowall push <file>`)")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTYPE\tPINNED\tAGE\tTITLE")
	for _, it := range list.Items {
		shortID := it.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		pin := " "
		if it.Pinned {
			pin = "📌"
		}
		title := it.Title
		if len(title) > 60 {
			title = title[:57] + "…"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", shortID, it.Type, pin, relativeTime(it.CreatedAt), title)
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

// --- pin/unpin ---

func cmdPin(id string, pinned bool) error {
	serverURL := envOr("INFOWALL_URL", "http://localhost:8899")
	apiKey := os.Getenv("INFOWALL_API_KEY")
	action := "pin"
	if !pinned {
		action = "unpin"
	}
	body, _ := json.Marshal(map[string]any{"pinned": pinned})
	url := fmt.Sprintf("%s/api/items/%s/pin", strings.TrimRight(serverURL, "/"), id)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s failed (%d): %s", action, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	fmt.Printf("%sned %s\n", action, id)
	return nil
}

// --- delete ---

func cmdDelete(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	yes := fs.Bool("yes", false, "skip confirmation")
	serverURL := fs.String("server", envOr("INFOWALL_URL", "http://localhost:8899"), "server URL")
	apiKey := fs.String("api-key", os.Getenv("INFOWALL_API_KEY"), "API key")
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: infowall delete <id> [--yes]")
	}
	id := fs.Arg(0)
	if !*yes {
		fmt.Printf("delete %s? [y/N] ", id)
		var ans string
		fmt.Fscanln(os.Stdin, &ans)
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			fmt.Println("aborted")
			return nil
		}
	}
	url := fmt.Sprintf("%s/api/items/%s", strings.TrimRight(*serverURL, "/"), id)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	if *apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+*apiKey)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	fmt.Printf("deleted %s\n", id)
	return nil
}

// --- helpers ---

func readInput(file string, fileGiven bool) ([]byte, error) {
	if file == "-" || file == "" {
		// If no file argument was provided and stdin is a TTY, nudge the user.
		if !fileGiven && isTTY() {
			fmt.Fprintln(os.Stderr, "(reading from stdin; type or pipe markdown, then Ctrl-D to end)")
		}
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(file)
}

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

// injectIntoFrontmatter does a dumb line-based insertion of type/pinned into an existing frontmatter.
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
	if typ != "" && !strings.Contains(fmBody, "\ntype:") && !strings.HasPrefix(fmBody, "type:") {
		inject = append(inject, "type: "+typ)
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
