// Package parser turns raw markdown bytes (with optional YAML frontmatter) into a model.Item.
package parser

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infowall/infowall/internal/model"
	"github.com/yuin/goldmark"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// md is the shared goldmark instance with GFM + YAML frontmatter support.
var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		meta.Meta,
	),
)

var (
	// firstHeading matches the first markdown H1 heading line: "# Title"
	firstHeading = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	// firstNonEmpty matches the first non-empty line.
	firstNonEmpty = regexp.MustCompile(`(?m)^\s*(.+?)\s*$`)
)

// utf8BOM is the UTF-8 byte-order mark that some editors prepend.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Parse reads raw markdown bytes (with optional YAML frontmatter delimited by ---) and produces an Item.
// The caller owns the memory of `raw` (we copy what we need).
func Parse(raw []byte) (*model.Item, error) {
	rawCopy := append([]byte(nil), raw...)

	var buf bytes.Buffer
	ctx := parser.NewContext()
	if err := md.Convert(rawCopy, &buf, parser.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("markdown parse: %w", err)
	}

	metaMap := meta.Get(ctx) // may contain map[interface{}]interface{} from yaml.v2
	if metaMap == nil {
		metaMap = map[string]any{}
	}
	metaMap, _ = sanitizeMeta(metaMap).(map[string]any)
	if metaMap == nil {
		metaMap = map[string]any{}
	}
	body := extractBody(rawCopy)

	it := &model.Item{
		ID:        uuid.New().String(),
		Type:      model.TypeNote,
		Title:     "",
		Tags:      nil,
		Pinned:    false,
		Meta:      metaMap,
		Body:      body,
		Raw:       string(rawCopy),
		CreatedAt: time.Now().UTC(),
	}

	// Type/topic. "topic" is the user-facing grouping name; "type" remains the
	// stored/API field for compatibility.
	if v, ok := metaMap["type"].(string); ok && v != "" {
		it.Type = v
	} else if v, ok := metaMap["topic"].(string); ok && v != "" {
		it.Type = v
	}

	// Title
	if v, ok := metaMap["title"].(string); ok && v != "" {
		it.Title = v
	} else {
		it.Title = deriveTitle(body)
	}

	// Tags
	it.Tags = asStringSlice(metaMap["tags"])

	// Pinned
	if v, ok := metaMap["pinned"]; ok && isTruthy(v) {
		it.Pinned = true
	}

	// Created at (allow override from frontmatter, RFC3339)
	if v, ok := metaMap["created_at"].(string); ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			it.CreatedAt = t.UTC()
		}
	}

	return it, nil
}

// extractBody returns the markdown after the frontmatter block.
// goldmark-meta doesn't expose the post-frontmatter text directly, so we do a quick scan:
// if the document starts with `---\n`, find the next closing `---` at the start of a line and
// return everything after.
func extractBody(raw []byte) string {
	s := string(bytes.TrimPrefix(raw, utf8BOM))
	s = strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return s
	}
	rest := s[3:]
	// The opening fence can be "---" with optional trailing whitespace followed by newline.
	// Accept "\n" or "\r\n" after the opening fence.
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		// no newline after opening fence: treat whole doc as body (no real frontmatter)
		return s
	}
	bodyStart := -1
	searchFrom := nl + 1
	for {
		idx := strings.Index(s[searchFrom:], "\n---")
		if idx < 0 {
			break
		}
		pos := searchFrom + idx
		// Examine the "---..." part after the newline.
		tail := s[pos+1:]
		eol := strings.IndexByte(tail, '\n')
		var line string
		if eol < 0 {
			line = strings.TrimRight(tail, "\r")
		} else {
			line = strings.TrimRight(tail[:eol], "\r")
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || trimmed == "..." {
			// Body starts after the closing fence line.
			if eol < 0 {
				bodyStart = pos + 1 + len(line)
			} else {
				bodyStart = pos + 1 + eol + 1
			}
			// Strip leading newline(s).
			for bodyStart < len(s) && (s[bodyStart] == '\n' || s[bodyStart] == '\r') {
				bodyStart++
			}
			break
		}
		searchFrom = pos + 1
	}
	if bodyStart < 0 {
		// no closing fence; treat whole doc as body
		return s
	}
	return s[bodyStart:]
}

// deriveTitle picks a title from body: first H1, else first non-empty line (truncated).
func deriveTitle(body string) string {
	if m := firstHeading.FindStringSubmatch(body); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := firstNonEmpty.FindStringSubmatch(body); len(m) == 2 {
		t := strings.TrimSpace(m[1])
		// Use runes so truncation respects UTF-8 character boundaries.
		r := []rune(t)
		if len(r) > 60 {
			t = string(r[:60]) + "…"
		}
		return t
	}
	return "(untitled)"
}

func isTruthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "yes" || x == "1"
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}

func asStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return x
	case []interface{}:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			} else if e != nil {
				out = append(out, fmt.Sprint(e))
			}
		}
		return out
	case string:
		// Allow comma-separated string
		parts := strings.Split(x, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		return nil
	}
}

// sanitizeMeta recursively converts map[interface{}]interface{} (produced by yaml.v2 when parsing
// nested YAML structures) to map[string]interface{} so the map is JSON-serializable.
func sanitizeMeta(v any) any {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = sanitizeMeta(vv)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[fmt.Sprint(k)] = sanitizeMeta(vv)
		}
		return out
	case []interface{}:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = sanitizeMeta(e)
		}
		return out
	default:
		return v
	}
}
