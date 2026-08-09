package feishuingest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/infowall/infowall/internal/feishusync"
)

var linkPattern = regexp.MustCompile(`https?://[^\s<>"')]+`)
var mrPattern = regexp.MustCompile(`/([^/]+/[^/]+)/merge_requests/(\d+)`)

type CommandEnricher struct {
	LarkRunner  feishusync.CommandRunner
	BytedRunner feishusync.CommandRunner
	BytedPath   string
	Timeout     time.Duration
}

func (enricher CommandEnricher) Enrich(ctx context.Context, messages []Message) []Resource {
	urls := make(map[string]struct{})
	for _, message := range messages {
		for _, raw := range linkPattern.FindAllString(message.Content, -1) {
			urls[strings.TrimRight(raw, ".,;，。；")] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(urls))
	for raw := range urls {
		ordered = append(ordered, raw)
	}
	sort.Strings(ordered)
	if len(ordered) > 12 {
		ordered = ordered[:12]
	}
	result := make([]Resource, 0, len(ordered))
	for _, raw := range ordered {
		if resource, ok := enricher.read(ctx, raw); ok {
			result = append(result, resource)
		}
	}
	return result
}

func (enricher CommandEnricher) read(ctx context.Context, raw string) (Resource, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return Resource{}, false
	}
	if match := mrPattern.FindStringSubmatch(parsed.Path); len(match) == 3 && strings.Contains(parsed.Host, "code") {
		resource := Resource{Kind: "codebase-mr", ExternalID: match[1] + "!" + match[2], URL: raw,
			DedupeKey: "codebase-mr:" + match[1] + ":" + match[2]}
		output, runErr := enricher.runByted(ctx, "--json", "codebase", "mr", "get", raw)
		return enrichResource(resource, output, runErr), true
	}
	if enricher.LarkRunner == nil {
		return Resource{}, false
	}
	path := strings.Trim(parsed.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return Resource{}, false
	}
	token := parts[len(parts)-1]
	switch parts[0] {
	case "docx", "docs":
		resource := Resource{Kind: "feishu-doc", ExternalID: token, URL: raw, DedupeKey: "feishu-doc:" + token}
		output, runErr := enricher.LarkRunner.Run(ctx, "", "docs", "+fetch", "--as", "user", "--doc", raw,
			"--scope", "outline", "--detail", "simple", "--doc-format", "markdown", "--format", "json")
		return enrichResource(resource, output, runErr), true
	case "wiki":
		resource := Resource{Kind: "feishu-wiki", ExternalID: token, URL: raw, DedupeKey: "feishu-wiki:" + token}
		output, runErr := enricher.LarkRunner.Run(ctx, "", "wiki", "+node-get", "--as", "user", "--node-token", raw, "--format", "json")
		return enrichResource(resource, output, runErr), true
	}
	if strings.Contains(parsed.Host, "meetings") || strings.Contains(path, "minutes") {
		resource := Resource{Kind: "feishu-minutes", ExternalID: token, URL: raw, DedupeKey: "feishu-minutes:" + token}
		output, runErr := enricher.LarkRunner.Run(ctx, "", "minutes", "+detail", "--as", "user", "--minute-tokens", token, "--summary", "--format", "json")
		return enrichResource(resource, output, runErr), true
	}
	return Resource{}, false
}

func (enricher CommandEnricher) runByted(ctx context.Context, args ...string) ([]byte, error) {
	if enricher.BytedRunner != nil {
		return enricher.BytedRunner.Run(ctx, "", args...)
	}
	path := enricher.BytedPath
	if path == "" {
		var err error
		path, err = exec.LookPath("bytedcli")
		if err != nil {
			return nil, err
		}
	}
	timeout := enricher.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(runContext, path, args...)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	if err := command.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func enrichResource(resource Resource, raw []byte, err error) Resource {
	if err != nil {
		resource.Accessible = false
		return resource
	}
	resource.Accessible = true
	var value any
	if json.Unmarshal(raw, &value) != nil {
		resource.Excerpt = truncateRunes(strings.TrimSpace(string(raw)), 500)
		return resource
	}
	metadata := make(map[string]string)
	collectMetadata(value, metadata)
	resource.Title = firstMetadata(metadata, "title", "name", "subject")
	resource.State = firstMetadata(metadata, "state", "status")
	parts := make([]string, 0, 5)
	for _, key := range []string{"description", "summary", "source_branch", "target_branch", "content"} {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			parts = append(parts, key+": "+truncateRunes(value, 300))
		}
	}
	resource.Excerpt = truncateRunes(strings.Join(parts, " · "), 700)
	return resource
}

func collectMetadata(value any, destination map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lower := strings.ToLower(key)
			switch lower {
			case "title", "name", "subject", "state", "status", "description", "summary", "source_branch", "target_branch", "content":
				if text, ok := item.(string); ok && destination[lower] == "" {
					destination[lower] = text
				}
			}
			collectMetadata(item, destination)
		}
	case []any:
		for _, item := range typed {
			collectMetadata(item, destination)
		}
	}
}

func firstMetadata(metadata map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			return truncateRunes(value, 200)
		}
	}
	return ""
}

func resourcesForMessage(message Message, resources []Resource) []Resource {
	result := make([]Resource, 0)
	for _, resource := range resources {
		if strings.Contains(message.Content, resource.URL) {
			result = append(result, resource)
		}
	}
	return result
}
