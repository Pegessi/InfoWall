package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	demandsPath           = "/api/demands"
	projectsPath          = "/api/projects"
	feishuIntegrationPath = "/api/integrations/feishu-doc"
	feishuChatPath        = "/api/integrations/feishu-chat"
	demandReviewsPath     = "/api/demand-reviews"
)

// --- demand ---

func cmdDemand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: infowall demand <apply|create|import|list|get|update|progress|dismiss|restore|review> [flags]")
	}

	switch args[0] {
	case "apply":
		return cmdDemandApply("apply", args[1:])
	case "create":
		return cmdDemandCreate(args[1:])
	case "import":
		return cmdDemandApply("import", args[1:])
	case "list":
		return cmdDemandList(args[1:])
	case "get":
		return cmdDemandGet(args[1:])
	case "update":
		return cmdDemandUpdate(args[1:])
	case "progress":
		return cmdDemandProgress(args[1:])
	case "dismiss":
		return cmdDemandSetStatus("dismiss", "dismissed", args[1:])
	case "restore":
		return cmdDemandSetStatus("restore", "pending", args[1:])
	case "review":
		return cmdDemandReview(args[1:])
	case "help", "-h", "--help":
		printDemandUsage()
		return nil
	default:
		return fmt.Errorf("unknown demand subcommand %q", args[0])
	}
}

func printDemandUsage() {
	fmt.Println(`infowall demand — track work at one consistent granularity

Usage:
  infowall demand apply --input FILE|- [client flags]  # preferred retry-safe agent write
  infowall demand create --title TEXT [--description TEXT] [--status STATUS] [--priority P0..P3|none]
      [--project ID] [--project-hint TEXT] [--next-action TEXT] [--blocked-reason TEXT] [client flags]
  infowall demand import --input FILE|- [client flags]
  infowall demand list [--status STATUS] [--project ID] [--q TEXT] [--include-dismissed] [client flags]
  infowall demand get ID [client flags]
  infowall demand update ID [the same editable fields as create] [client flags]
  infowall demand progress ID --text TEXT [--link URL ...] [--links JSON|--links-input FILE|-]
      [--source JSON|--source-input FILE|-] [client flags]
  infowall demand dismiss ID [client flags]
  infowall demand restore ID [client flags]
  infowall demand review list [--status pending|accepted|dismissed|all] [client flags]
  infowall demand review accept REVIEW_ID [--demand DEMAND_ID] [client flags]
  infowall demand review dismiss REVIEW_ID [client flags]

Client flags: --server URL --api-key KEY --json`)
}

type demandFields struct {
	title         *string
	description   *string
	status        *string
	priority      *string
	project       *string
	projectHint   *string
	nextAction    *string
	blockedReason *string
}

func addDemandFields(fs *flag.FlagSet, create bool) *demandFields {
	statusDefault := ""
	priorityDefault := ""
	if create {
		statusDefault = "pending"
		priorityDefault = "none"
	}
	return &demandFields{
		title:         fs.String("title", "", "demand title"),
		description:   fs.String("description", "", "demand description"),
		status:        fs.String("status", statusDefault, "pending, planned, active, waiting, done, or dismissed"),
		priority:      fs.String("priority", priorityDefault, "p0, p1, p2, p3, or none"),
		project:       fs.String("project", "", "project id; use --project-hint when the project is not known yet"),
		projectHint:   fs.String("project-hint", "", "unresolved project name or grouping hint"),
		nextAction:    fs.String("next-action", "", "next concrete action"),
		blockedReason: fs.String("blocked-reason", "", "reason the demand is waiting"),
	}
}

func (f *demandFields) createPayload() map[string]any {
	payload := map[string]any{
		"title":       strings.TrimSpace(*f.title),
		"description": *f.description,
		"status":      strings.ToLower(strings.TrimSpace(*f.status)),
		"priority":    strings.ToLower(strings.TrimSpace(*f.priority)),
	}
	putNonEmpty(payload, "project_id", *f.project)
	putNonEmpty(payload, "project_hint", *f.projectHint)
	putNonEmpty(payload, "next_action", *f.nextAction)
	putNonEmpty(payload, "blocked_reason", *f.blockedReason)
	return payload
}

func (f *demandFields) updatePayload(fs *flag.FlagSet) map[string]any {
	payload := make(map[string]any)
	putFlagString(fs, payload, "title", "title", *f.title, false)
	putFlagString(fs, payload, "description", "description", *f.description, false)
	putFlagString(fs, payload, "status", "status", strings.ToLower(strings.TrimSpace(*f.status)), false)
	putFlagString(fs, payload, "priority", "priority", strings.ToLower(strings.TrimSpace(*f.priority)), false)
	putFlagString(fs, payload, "project", "project_id", *f.project, true)
	putFlagString(fs, payload, "project-hint", "project_hint", *f.projectHint, false)
	putFlagString(fs, payload, "next-action", "next_action", *f.nextAction, false)
	putFlagString(fs, payload, "blocked-reason", "blocked_reason", *f.blockedReason, false)
	return payload
}

func cmdDemandCreate(args []string) error {
	fs := flag.NewFlagSet("demand create", flag.ExitOnError)
	fields := addDemandFields(fs, true)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if strings.TrimSpace(*fields.title) == "" {
		return cfg.fail(errors.New("demand create requires --title"))
	}
	return requestAndPrint(cfg, http.MethodPost, demandsPath, fields.createPayload())
}

func cmdDemandImport(args []string) error { return cmdDemandApply("import", args) }

func cmdDemandApply(command string, args []string) error {
	fs := flag.NewFlagSet("demand "+command, flag.ExitOnError)
	input := fs.String("input", "-", "JSON file to import, or - for stdin")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(fmt.Errorf("usage: infowall demand %s --input FILE|- [client flags]", command))
	}

	raw, err := readJSONInput(*input)
	if err != nil {
		return cfg.fail(err)
	}
	payload, err := normalizeDemandImport(raw)
	if err != nil {
		return cfg.fail(err)
	}
	return requestAndPrint(cfg, http.MethodPost, demandsPath+"/import", payload)
}

func normalizeDemandImport(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode demand import JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}

	switch typed := value.(type) {
	case []any:
		return map[string]any{"demands": typed}, nil
	case map[string]any:
		if rawDemands, exists := typed["demands"]; exists {
			demands, ok := rawDemands.([]any)
			if !ok {
				return nil, errors.New("demand import demands field must be an array")
			}
			return map[string]any{"demands": demands}, nil
		}
		_, hasID := typed["id"]
		title, hasTitle := typed["title"].(string)
		if !hasID && (!hasTitle || strings.TrimSpace(title) == "") {
			return nil, errors.New("single demand import requires a non-empty title or an existing demand id")
		}
		return map[string]any{"demands": []any{typed}}, nil
	default:
		return nil, errors.New("demand import must be a demand object, demand array, or an object containing a demands array")
	}
}

func cmdDemandList(args []string) error {
	fs := flag.NewFlagSet("demand list", flag.ExitOnError)
	status := fs.String("status", "", "filter by status")
	project := fs.String("project", "", "filter by project id")
	queryText := fs.String("q", "", "search title, description, project hint, next action, and blocked reason")
	includeDismissed := fs.Bool("include-dismissed", false, "include dismissed demands when status is not specified")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(errors.New("usage: infowall demand list [--status STATUS] [--project ID] [--q TEXT] [--include-dismissed] [client flags]"))
	}

	query := url.Values{}
	if strings.TrimSpace(*status) != "" {
		query.Set("status", strings.ToLower(strings.TrimSpace(*status)))
	}
	if strings.TrimSpace(*project) != "" {
		query.Set("project_id", strings.TrimSpace(*project))
	}
	if strings.TrimSpace(*queryText) != "" {
		query.Set("q", strings.TrimSpace(*queryText))
	}
	if *includeDismissed {
		query.Set("include_dismissed", "true")
	}
	path := demandsPath
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return requestAndPrint(cfg, http.MethodGet, path, nil)
}

func cmdDemandGet(args []string) error {
	fs := flag.NewFlagSet("demand get", flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return cfg.fail(errors.New("usage: infowall demand get ID [client flags]"))
	}
	return requestAndPrint(cfg, http.MethodGet, demandPath(fs.Arg(0)), nil)
}

func cmdDemandUpdate(args []string) error {
	fs := flag.NewFlagSet("demand update", flag.ExitOnError)
	fields := addDemandFields(fs, false)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return cfg.fail(errors.New("usage: infowall demand update ID [fields] [client flags]"))
	}
	payload := fields.updatePayload(fs)
	if len(payload) == 0 {
		return cfg.fail(errors.New("demand update requires at least one editable field"))
	}
	return requestAndPrint(cfg, http.MethodPatch, demandPath(fs.Arg(0)), payload)
}

func cmdDemandProgress(args []string) error {
	fs := flag.NewFlagSet("demand progress", flag.ExitOnError)
	text := fs.String("text", "", "progress text; may also follow ID as positional text")
	sourceJSON := fs.String("source", "", "optional source object as inline JSON")
	sourceInput := fs.String("source-input", "", "optional source object JSON file, or - for stdin")
	linksJSON := fs.String("links", "", "optional structured progress link array as inline JSON")
	linksInput := fs.String("links-input", "", "optional structured progress link array JSON file, or - for stdin")
	var linkURLs repeatedString
	fs.Var(&linkURLs, "link", "related http(s) URL; repeat for multiple links")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() < 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return cfg.fail(errors.New("usage: infowall demand progress ID --text TEXT [--link URL ...] [--links JSON|--links-input FILE|-] [--source JSON|--source-input FILE|-] [client flags]"))
	}
	if *text == "" && fs.NArg() > 1 {
		*text = strings.Join(fs.Args()[1:], " ")
	} else if fs.NArg() > 1 {
		return cfg.fail(errors.New("progress text must be supplied either by --text or as positional text, not both"))
	}
	if strings.TrimSpace(*text) == "" {
		return cfg.fail(errors.New("demand progress requires non-empty text"))
	}
	if *sourceJSON != "" && *sourceInput != "" {
		return cfg.fail(errors.New("use only one of --source and --source-input"))
	}
	if *linksJSON != "" && *linksInput != "" {
		return cfg.fail(errors.New("use only one of --links and --links-input"))
	}
	if *sourceInput == "-" && *linksInput == "-" {
		return cfg.fail(errors.New("stdin can supply either --source-input - or --links-input -, not both"))
	}

	payload := map[string]any{"text": strings.TrimSpace(*text)}
	if *sourceJSON != "" || *sourceInput != "" {
		raw := []byte(*sourceJSON)
		var err error
		if *sourceInput != "" {
			raw, err = readJSONInput(*sourceInput)
			if err != nil {
				return cfg.fail(err)
			}
		}
		source, err := decodeJSONObject(raw, "progress source")
		if err != nil {
			return cfg.fail(err)
		}
		payload["source"] = source
	}
	links := make([]any, 0, len(linkURLs))
	for _, raw := range linkURLs {
		links = append(links, map[string]any{"url": strings.TrimSpace(raw)})
	}
	if *linksJSON != "" || *linksInput != "" {
		raw := []byte(*linksJSON)
		var err error
		if *linksInput != "" {
			raw, err = readJSONInput(*linksInput)
			if err != nil {
				return cfg.fail(err)
			}
		}
		structured, err := decodeJSONArray(raw, "progress links")
		if err != nil {
			return cfg.fail(err)
		}
		links = append(links, structured...)
	}
	if len(links) > 0 {
		payload["links"] = links
	}

	path := demandPath(fs.Arg(0)) + "/progress"
	return requestAndPrint(cfg, http.MethodPost, path, payload)
}

func cmdDemandSetStatus(command, status string, args []string) error {
	fs := flag.NewFlagSet("demand "+command, flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return cfg.fail(fmt.Errorf("usage: infowall demand %s ID [client flags]", command))
	}
	return requestAndPrint(cfg, http.MethodPatch, demandPath(fs.Arg(0)), map[string]any{"status": status})
}

func demandPath(id string) string {
	return demandsPath + "/" + url.PathEscape(strings.TrimSpace(id))
}

func cmdDemandReview(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: infowall demand review <list|accept|dismiss> [flags]")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("demand review list", flag.ExitOnError)
		status := fs.String("status", "pending", "pending, accepted, dismissed, or all")
		cfg := addClientFlags(fs)
		parseFlags(fs, args[1:])
		if fs.NArg() != 0 {
			return cfg.fail(errors.New("usage: infowall demand review list [--status STATUS] [client flags]"))
		}
		query := url.Values{"status": []string{strings.ToLower(strings.TrimSpace(*status))}}
		return requestAndPrint(cfg, http.MethodGet, demandReviewsPath+"?"+query.Encode(), nil)
	case "accept":
		fs := flag.NewFlagSet("demand review accept", flag.ExitOnError)
		demandID := fs.String("demand", "", "override the suggested demand id")
		cfg := addClientFlags(fs)
		parseFlags(fs, args[1:])
		if fs.NArg() != 1 {
			return cfg.fail(errors.New("usage: infowall demand review accept REVIEW_ID [--demand DEMAND_ID] [client flags]"))
		}
		payload := map[string]any{}
		putNonEmpty(payload, "demand_id", *demandID)
		return requestAndPrint(cfg, http.MethodPost, demandReviewsPath+"/"+url.PathEscape(fs.Arg(0))+"/accept", payload)
	case "dismiss":
		fs := flag.NewFlagSet("demand review dismiss", flag.ExitOnError)
		cfg := addClientFlags(fs)
		parseFlags(fs, args[1:])
		if fs.NArg() != 1 {
			return cfg.fail(errors.New("usage: infowall demand review dismiss REVIEW_ID [client flags]"))
		}
		return requestAndPrint(cfg, http.MethodPost, demandReviewsPath+"/"+url.PathEscape(fs.Arg(0))+"/dismiss", nil)
	default:
		return fmt.Errorf("unknown demand review subcommand %q", args[0])
	}
}

// --- project ---

func cmdProject(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: infowall project <create|list|update|archive> [flags]")
	}
	switch args[0] {
	case "create":
		return cmdProjectCreate(args[1:])
	case "list":
		return cmdProjectList(args[1:])
	case "update":
		return cmdProjectUpdate(args[1:])
	case "archive":
		return cmdProjectArchive(args[1:])
	case "help", "-h", "--help":
		printProjectUsage()
		return nil
	default:
		return fmt.Errorf("unknown project subcommand %q", args[0])
	}
}

func printProjectUsage() {
	fmt.Println(`infowall project — group related demands

Usage:
  infowall project create --name TEXT [--description TEXT] [--color CSS_COLOR] [client flags]
  infowall project list [client flags]
  infowall project update ID [--name TEXT] [--description TEXT] [--color CSS_COLOR] [--status STATUS] [client flags]
  infowall project archive ID [client flags]

Client flags: --server URL --api-key KEY --json`)
}

type projectFields struct {
	name        *string
	description *string
	color       *string
	status      *string
}

func addProjectFields(fs *flag.FlagSet, create bool) *projectFields {
	statusDefault := ""
	colorDefault := ""
	if create {
		statusDefault = "active"
		colorDefault = "#64748b"
	}
	return &projectFields{
		name:        fs.String("name", "", "project name"),
		description: fs.String("description", "", "project description"),
		color:       fs.String("color", colorDefault, "project CSS color"),
		status:      fs.String("status", statusDefault, "active or archived"),
	}
}

func cmdProjectCreate(args []string) error {
	fs := flag.NewFlagSet("project create", flag.ExitOnError)
	fields := addProjectFields(fs, true)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if strings.TrimSpace(*fields.name) == "" {
		return cfg.fail(errors.New("project create requires --name"))
	}
	payload := map[string]any{
		"name":        strings.TrimSpace(*fields.name),
		"description": *fields.description,
		"color":       strings.TrimSpace(*fields.color),
		"status":      strings.ToLower(strings.TrimSpace(*fields.status)),
	}
	return requestAndPrint(cfg, http.MethodPost, projectsPath, payload)
}

func cmdProjectList(args []string) error {
	fs := flag.NewFlagSet("project list", flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(errors.New("usage: infowall project list [client flags]"))
	}
	return requestAndPrint(cfg, http.MethodGet, projectsPath, nil)
}

func cmdProjectUpdate(args []string) error {
	fs := flag.NewFlagSet("project update", flag.ExitOnError)
	fields := addProjectFields(fs, false)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return cfg.fail(errors.New("usage: infowall project update ID [fields] [client flags]"))
	}
	payload := make(map[string]any)
	putFlagString(fs, payload, "name", "name", *fields.name, false)
	putFlagString(fs, payload, "description", "description", *fields.description, false)
	putFlagString(fs, payload, "color", "color", *fields.color, false)
	putFlagString(fs, payload, "status", "status", strings.ToLower(strings.TrimSpace(*fields.status)), false)
	if len(payload) == 0 {
		return cfg.fail(errors.New("project update requires at least one editable field"))
	}
	return requestAndPrint(cfg, http.MethodPatch, projectPath(fs.Arg(0)), payload)
}

func cmdProjectArchive(args []string) error {
	fs := flag.NewFlagSet("project archive", flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return cfg.fail(errors.New("usage: infowall project archive ID [client flags]"))
	}
	return requestAndPrint(cfg, http.MethodPatch, projectPath(fs.Arg(0)), map[string]any{"status": "archived"})
}

func projectPath(id string) string {
	return projectsPath + "/" + url.PathEscape(strings.TrimSpace(id))
}

// --- sync feishu ---

func cmdSync(args []string) error {
	if len(args) == 0 || args[0] != "feishu" {
		return errors.New("usage: infowall sync feishu <setup|status|now|disable> [flags]")
	}
	if len(args) == 1 {
		return errors.New("usage: infowall sync feishu <setup|status|now|disable> [flags]")
	}
	switch args[1] {
	case "setup":
		return cmdSyncFeishuSetup(args[2:])
	case "status":
		return cmdSyncFeishuSimple("status", http.MethodGet, feishuIntegrationPath, args[2:])
	case "now":
		return cmdSyncFeishuSimple("now", http.MethodPost, feishuIntegrationPath+"/sync", args[2:])
	case "disable":
		return cmdSyncFeishuSimple("disable", http.MethodDelete, feishuIntegrationPath, args[2:])
	case "help", "-h", "--help":
		printSyncFeishuUsage()
		return nil
	default:
		return fmt.Errorf("unknown sync feishu subcommand %q", args[1])
	}
}

func printSyncFeishuUsage() {
	fmt.Println(`infowall sync feishu — maintain the rendered progress document

Usage:
  infowall sync feishu setup --create [client flags]
  infowall sync feishu setup --doc URL [client flags]
  infowall sync feishu status [client flags]
  infowall sync feishu now [client flags]
  infowall sync feishu disable [client flags]

Client flags: --server URL --api-key KEY --json`)
}

func cmdSyncFeishuSetup(args []string) error {
	fs := flag.NewFlagSet("sync feishu setup", flag.ExitOnError)
	create := fs.Bool("create", false, "create a new Feishu progress document")
	doc := fs.String("doc", "", "use an existing Feishu document URL")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 || (*create == (strings.TrimSpace(*doc) != "")) {
		return cfg.fail(errors.New("sync feishu setup requires exactly one of --create or --doc URL"))
	}
	payload := map[string]any{"create": true}
	if !*create {
		payload = map[string]any{"doc_url": strings.TrimSpace(*doc)}
	}
	return requestAndPrint(cfg, http.MethodPost, feishuIntegrationPath, payload)
}

func cmdSyncFeishuSimple(command, method, path string, args []string) error {
	fs := flag.NewFlagSet("sync feishu "+command, flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(fmt.Errorf("usage: infowall sync feishu %s [client flags]", command))
	}
	return requestAndPrint(cfg, method, path, nil)
}

// --- scan feishu ---

func cmdScan(args []string) error {
	if len(args) < 2 || args[0] != "feishu" {
		return errors.New("usage: infowall scan feishu <setup|status|now|runs|disable> [flags]")
	}
	switch args[1] {
	case "setup":
		return cmdScanFeishuSetup(args[2:])
	case "status":
		return cmdScanFeishuSimple("status", http.MethodGet, feishuChatPath, args[2:])
	case "now":
		return cmdScanFeishuSimple("now", http.MethodPost, feishuChatPath+"/scan", args[2:])
	case "runs":
		return cmdScanFeishuRuns(args[2:])
	case "disable":
		return cmdScanFeishuDisable(args[2:])
	case "help", "-h", "--help":
		printScanFeishuUsage()
		return nil
	default:
		return fmt.Errorf("unknown scan feishu subcommand %q", args[1])
	}
}

func printScanFeishuUsage() {
	fmt.Println(`infowall scan feishu — incrementally collect demand evidence from visible chats

Usage:
  infowall scan feishu setup [--exclude-chat CHAT_ID ...] [client flags]
  infowall scan feishu status [client flags]
  infowall scan feishu now [client flags]
  infowall scan feishu runs [--limit N] [client flags]
  infowall scan feishu disable [client flags]

The default schedule is Asia/Shanghai 09:00–23:00 every 30 minutes with a 5-minute overlap.
Direct chats are eligible; group/topic messages are analyzed only when sent by you, explicitly @mentioning you,
or in a thread where you participated. The current Feishu identity is resolved automatically from lark-cli auth status.
Use --resume-from RFC3339 only once when a prior manual scan has an audited completion watermark.
Client flags: --server URL --api-key KEY --json`)
}

type repeatedString []string

func (values *repeatedString) String() string { return strings.Join(*values, ",") }
func (values *repeatedString) Set(value string) error {
	*values = append(*values, strings.TrimSpace(value))
	return nil
}

func cmdScanFeishuSetup(args []string) error {
	fs := flag.NewFlagSet("scan feishu setup", flag.ExitOnError)
	timezone := fs.String("timezone", "Asia/Shanghai", "IANA timezone")
	start := fs.String("start", "09:00", "daily active start HH:MM")
	end := fs.String("end", "23:00", "daily active end HH:MM")
	interval := fs.Int("interval", 30, "scan interval in minutes")
	overlap := fs.Int("overlap", 5, "watermark overlap in minutes")
	resumeFrom := fs.String("resume-from", "", "one-time audited prior scan watermark (RFC3339)")
	var exclusions repeatedString
	fs.Var(&exclusions, "exclude-chat", "chat id to exclude; repeat for multiple chats")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(errors.New("usage: infowall scan feishu setup [flags]"))
	}
	payload := map[string]any{"enabled": true, "timezone": *timezone, "active_start": *start,
		"active_end": *end, "interval_minutes": *interval, "overlap_minutes": *overlap,
		"excluded_chat_ids": []string(exclusions)}
	putNonEmpty(payload, "resume_from", *resumeFrom)
	return requestAndPrint(cfg, http.MethodPatch, feishuChatPath, payload)
}

func cmdScanFeishuSimple(command, method, path string, args []string) error {
	fs := flag.NewFlagSet("scan feishu "+command, flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(fmt.Errorf("usage: infowall scan feishu %s [client flags]", command))
	}
	return requestAndPrint(cfg, method, path, nil)
}

func cmdScanFeishuRuns(args []string) error {
	fs := flag.NewFlagSet("scan feishu runs", flag.ExitOnError)
	limit := fs.Int("limit", 20, "maximum runs to return")
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(errors.New("usage: infowall scan feishu runs [--limit N] [client flags]"))
	}
	return requestAndPrint(cfg, http.MethodGet, feishuChatPath+"/runs?limit="+url.QueryEscape(fmt.Sprint(*limit)), nil)
}

func cmdScanFeishuDisable(args []string) error {
	fs := flag.NewFlagSet("scan feishu disable", flag.ExitOnError)
	cfg := addClientFlags(fs)
	parseFlags(fs, args)
	if fs.NArg() != 0 {
		return cfg.fail(errors.New("usage: infowall scan feishu disable [client flags]"))
	}
	return requestAndPrint(cfg, http.MethodPatch, feishuChatPath, map[string]any{"enabled": false})
}

// --- shared workbench client helpers ---

func requestAndPrint(cfg *clientConfig, method, path string, payload any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return cfg.fail(fmt.Errorf("encode request: %w", err))
		}
		body = bytes.NewReader(encoded)
	}

	contentType := ""
	if body != nil {
		contentType = "application/json"
	}
	resp, err := cfg.do(method, cfg.url(path), contentType, body)
	if err != nil {
		return cfg.fail(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return cfg.fail(fmt.Errorf("read response: %w", err))
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(raw))
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &payload) == nil && strings.TrimSpace(payload.Error) != "" {
			message = strings.TrimSpace(payload.Error)
		}
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return cfg.fail(&apiResponseError{StatusCode: resp.StatusCode, Message: message})
	}
	return printAPIResult(cfg, raw)
}

func printAPIResult(cfg *clientConfig, raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte(`{"ok":true}`)
	}
	if !json.Valid(trimmed) {
		return cfg.fail(errors.New("server returned a non-JSON response"))
	}
	if cfg.asJSON {
		writeRawJSONStdout(trimmed)
		return nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, trimmed, "", "  "); err != nil {
		return cfg.fail(fmt.Errorf("format response: %w", err))
	}
	fmt.Fprintln(os.Stdout, pretty.String())
	return nil
}

func readJSONInput(path string) ([]byte, error) {
	if path == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read JSON from stdin: %w", err)
		}
		return raw, nil
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("JSON input path must not be empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read JSON input %s: %w", path, err)
	}
	return raw, nil
}

func decodeJSONObject(raw []byte, label string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode %s JSON: %w", label, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	return object, nil
}

func decodeJSONArray(raw []byte, label string) ([]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode %s JSON: %w", label, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON array", label)
	}
	for index, item := range array {
		if _, ok := item.(map[string]any); !ok {
			return nil, fmt.Errorf("%s item %d must be a JSON object", label, index)
		}
	}
	return array, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return errors.New("JSON input must contain exactly one value")
}

func putNonEmpty(payload map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		payload[key] = value
	}
}

func putFlagString(fs *flag.FlagSet, payload map[string]any, flagName, jsonName, value string, nullable bool) {
	if !flagWasSet(fs, flagName) {
		return
	}
	if nullable && strings.TrimSpace(value) == "" {
		payload[jsonName] = nil
		return
	}
	payload[jsonName] = value
}
