package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/infowall/infowall/internal/model"
)

const progressLinksSchema = `
CREATE TABLE IF NOT EXISTS demand_progress_links (
    progress_id TEXT NOT NULL REFERENCES demand_progress(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL DEFAULT 'link',
    external_id TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT '',
    dedupe_key  TEXT NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (progress_id, dedupe_key)
);
CREATE INDEX IF NOT EXISTS idx_demand_progress_links_progress
    ON demand_progress_links(progress_id, position, dedupe_key);
`

var progressURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

func normalizeProgressLinks(links []model.ProgressLink, text string) ([]model.ProgressLink, error) {
	result := make([]model.ProgressLink, 0, len(links)+2)
	seen := make(map[string]struct{}, len(links)+2)
	appendLink := func(link model.ProgressLink, strict bool) error {
		link.Kind = strings.TrimSpace(link.Kind)
		link.ExternalID = strings.TrimSpace(link.ExternalID)
		link.Title = strings.TrimSpace(link.Title)
		link.URL = strings.TrimSpace(link.URL)
		link.State = strings.TrimSpace(link.State)
		link.DedupeKey = strings.TrimSpace(link.DedupeKey)
		parsed, err := url.Parse(link.URL)
		if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			if strict {
				return fmt.Errorf("progress link %q must be an absolute http(s) URL", link.URL)
			}
			return nil
		}
		link.URL = parsed.String()
		if _, ok := seen[link.URL]; ok {
			return nil
		}
		if link.Kind == "" {
			link.Kind = "link"
		}
		if link.Title == "" {
			link.Title = progressLinkTitle(link)
		}
		if link.DedupeKey == "" {
			sum := sha256.Sum256([]byte(link.URL))
			link.DedupeKey = "url:" + hex.EncodeToString(sum[:12])
		}
		seen[link.URL] = struct{}{}
		result = append(result, link)
		return nil
	}

	if len(links) > 32 {
		return nil, fmt.Errorf("progress accepts at most 32 links")
	}
	for _, link := range links {
		if err := appendLink(link, true); err != nil {
			return nil, err
		}
	}
	for _, raw := range progressURLPattern.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,;:!?，。；：！？)]}）】》")
		if err := appendLink(model.ProgressLink{Kind: "link", URL: raw}, false); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func progressLinkTitle(link model.ProgressLink) string {
	externalID := strings.TrimSpace(link.ExternalID)
	switch strings.ToLower(strings.TrimSpace(link.Kind)) {
	case "feishu-im":
		return "飞书原消息"
	case "feishu-doc", "doc", "document":
		return joinLinkTitle("飞书文档", externalID)
	case "feishu-wiki", "wiki":
		return joinLinkTitle("飞书 Wiki", externalID)
	case "feishu-minutes", "minutes":
		return joinLinkTitle("飞书妙记", externalID)
	case "codebase-mr":
		return joinLinkTitle("Codebase MR", externalID)
	case "seed-jobrun", "jobrun":
		return joinLinkTitle("JobRun", externalID)
	case "trial":
		return joinLinkTitle("Trial", externalID)
	case "arena-eval", "arena":
		return joinLinkTitle("Arena 评测", externalID)
	case "model-card":
		return joinLinkTitle("Model Card", externalID)
	case "insight":
		return joinLinkTitle("Insight", externalID)
	default:
		parsed, err := url.Parse(link.URL)
		if err == nil && parsed.Host != "" {
			return parsed.Host
		}
		return "相关链接"
	}
}

func joinLinkTitle(prefix, externalID string) string {
	if externalID == "" {
		return prefix
	}
	return prefix + " · " + externalID
}

func progressLinkFromSource(source model.Source) model.ProgressLink {
	title := sourceResourceTitle(source)
	return model.ProgressLink{
		Kind:       source.Kind,
		ExternalID: source.ExternalID,
		Title:      title,
		URL:        source.URL,
		DedupeKey:  source.DedupeKey,
	}
}

func progressLinksWithSource(links []model.ProgressLink, source *model.Source) []model.ProgressLink {
	result := append([]model.ProgressLink(nil), links...)
	if source != nil && strings.TrimSpace(source.URL) != "" {
		result = append(result, progressLinkFromSource(*source))
	}
	return result
}

func sourceResourceTitle(source model.Source) string {
	if source.Kind == "feishu-im" {
		if source.ChatName != "" {
			return "飞书消息 · " + source.ChatName
		}
		return "飞书原消息"
	}
	parts := strings.Split(source.Excerpt, " · ")
	if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[1])
	}
	return progressLinkTitle(model.ProgressLink{Kind: source.Kind, ExternalID: source.ExternalID, URL: source.URL})
}

func insertProgressTx(ctx context.Context, tx *sql.Tx, progress *model.Progress) error {
	links, err := normalizeProgressLinks(progress.Links, progress.Text)
	if err != nil {
		return err
	}
	progress.Links = links
	if _, err := tx.ExecContext(ctx, `INSERT INTO demand_progress
		(id, demand_id, text, dedupe_key, created_at) VALUES (?, ?, ?, ?, ?)`,
		progress.ID, progress.DemandID, progress.Text, strings.TrimSpace(progress.DedupeKey), progress.CreatedAt.UTC()); err != nil {
		return err
	}
	_, err = insertProgressLinksTx(ctx, tx, progress.ID, links)
	return err
}

func insertProgressLinksTx(ctx context.Context, tx *sql.Tx, progressID string, links []model.ProgressLink) (bool, error) {
	changed := false
	for position, link := range links {
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO demand_progress_links
			(progress_id, kind, external_id, title, url, state, dedupe_key, position)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, progressID, link.Kind, link.ExternalID,
			link.Title, link.URL, link.State, link.DedupeKey, position)
		if err != nil {
			return false, err
		}
		if rows, _ := result.RowsAffected(); rows > 0 {
			changed = true
		}
	}
	return changed, nil
}

func migrateProgressLinks(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(progressLinksSchema); err != nil {
		return fmt.Errorf("create progress links schema: %w", err)
	}

	type legacyProgress struct {
		id, demandID, text, dedupeKey string
	}
	progressRows, err := tx.Query(`SELECT id, demand_id, text, dedupe_key FROM demand_progress`)
	if err != nil {
		return err
	}
	progresses := make([]legacyProgress, 0)
	for progressRows.Next() {
		var progress legacyProgress
		if err := progressRows.Scan(&progress.id, &progress.demandID, &progress.text, &progress.dedupeKey); err != nil {
			progressRows.Close()
			return err
		}
		progresses = append(progresses, progress)
	}
	if err := progressRows.Close(); err != nil {
		return err
	}

	sourceRows, err := tx.Query(`SELECT id, demand_id, kind, external_id, chat_id,
		chat_name, sender_id, sender_name, message_time, url, excerpt, dedupe_key, created_at
		FROM demand_sources WHERE url <> ''`)
	if err != nil {
		return err
	}
	sourcesByDemand := make(map[string][]model.Source)
	for sourceRows.Next() {
		var source model.Source
		var messageRaw, createdRaw any
		if err := sourceRows.Scan(&source.ID, &source.DemandID, &source.Kind, &source.ExternalID,
			&source.ChatID, &source.ChatName, &source.SenderID, &source.SenderName,
			&messageRaw, &source.URL, &source.Excerpt, &source.DedupeKey, &createdRaw); err != nil {
			sourceRows.Close()
			return err
		}
		sourcesByDemand[source.DemandID] = append(sourcesByDemand[source.DemandID], source)
	}
	if err := sourceRows.Close(); err != nil {
		return err
	}

	for _, progress := range progresses {
		links, err := normalizeProgressLinks(nil, progress.text)
		if err != nil {
			return err
		}
		kindCount := make(map[string]int)
		for _, source := range sourcesByDemand[progress.demandID] {
			kindCount[source.Kind]++
		}
		for _, source := range sourcesByDemand[progress.demandID] {
			if legacyProgressMatchesSource(progress.text, progress.dedupeKey, source, kindCount[source.Kind]) {
				links = append(links, progressLinkFromSource(source))
			}
		}
		links, err = normalizeProgressLinks(links, progress.text)
		if err != nil {
			return err
		}
		if _, err := insertProgressLinksTx(context.Background(), tx, progress.id, links); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func legacyProgressMatchesSource(text, progressKey string, source model.Source, sameKindCount int) bool {
	textLower := strings.ToLower(text)
	keyLower := strings.ToLower(progressKey)
	if source.ExternalID != "" && (strings.Contains(textLower, strings.ToLower(source.ExternalID)) ||
		strings.Contains(keyLower, strings.ToLower(source.ExternalID))) {
		return true
	}
	if source.DedupeKey != "" && strings.Contains(keyLower, strings.ToLower(source.DedupeKey)) {
		return true
	}
	if sameKindCount != 1 {
		return false
	}
	switch source.Kind {
	case "model-card":
		return strings.Contains(textLower, "model card")
	case "seed-jobrun":
		return strings.Contains(textLower, "jobrun") || strings.Contains(textLower, "trial")
	case "arena-eval":
		return strings.Contains(textLower, "arena") || strings.Contains(text, "评测")
	case "codebase-mr":
		return strings.Contains(textLower, " mr ") || strings.HasPrefix(textLower, "mr ")
	case "feishu-doc", "feishu-wiki", "feishu-minutes":
		return strings.Contains(text, "文档") || strings.Contains(text, "飞书") || strings.Contains(text, "妙记")
	default:
		return false
	}
}
