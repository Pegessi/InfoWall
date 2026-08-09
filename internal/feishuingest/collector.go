package feishuingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/infowall/infowall/internal/feishusync"
)

type LarkCollector struct {
	Runner   feishusync.CommandRunner
	Timezone string
}

type larkSearchResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		HasMore   bool          `json:"has_more"`
		PageToken string        `json:"page_token"`
		Total     int           `json:"total"`
		Messages  []larkMessage `json:"messages"`
	} `json:"data"`
}

type larkMessage struct {
	ChatID      string `json:"chat_id"`
	ChatName    string `json:"chat_name"`
	ChatType    string `json:"chat_type"`
	Content     string `json:"content"`
	CreateTime  string `json:"create_time"`
	Deleted     bool   `json:"deleted"`
	MessageLink string `json:"message_app_link"`
	MessageID   string `json:"message_id"`
	MessageType string `json:"msg_type"`
	ThreadID    string `json:"thread_id"`
	Sender      struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		SenderType string `json:"sender_type"`
	} `json:"sender"`
}

func (c LarkCollector) Collect(ctx context.Context, start, end time.Time, excluded []string) (Collection, error) {
	if c.Runner == nil {
		return Collection{}, errors.New("lark runner is required")
	}
	raw, err := c.Runner.Run(ctx, "", "im", "+messages-search", "--query", "",
		"--start", start.Format(time.RFC3339), "--end", end.Format(time.RFC3339),
		"--page-all", "--page-size", "50", "--page-limit", "40", "--no-reactions",
		"--format", "json", "--as", "user")
	if err != nil {
		return Collection{}, err
	}
	var response larkSearchResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return Collection{}, fmt.Errorf("decode lark message search: %w", err)
	}
	if !response.OK {
		return Collection{}, errors.New("lark message search returned ok=false")
	}
	if response.Data.HasMore || response.Data.PageToken != "" || response.Data.Total > MessageLimit || len(response.Data.Messages) > MessageLimit {
		return Collection{}, fmt.Errorf("message limit reached for window (%d, max %d); split the window before retrying", response.Data.Total, MessageLimit)
	}
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, id := range excluded {
		excludedSet[strings.TrimSpace(id)] = struct{}{}
	}
	result := Collection{Messages: make([]Message, 0, len(response.Data.Messages)), Candidates: []Message{}, Seen: len(response.Data.Messages)}
	seen := make(map[string]struct{}, len(response.Data.Messages))
	location := start.Location()
	timezone := c.Timezone
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	if configured, locationErr := time.LoadLocation(timezone); locationErr == nil {
		location = configured
	}
	for _, item := range response.Data.Messages {
		if item.MessageID == "" {
			continue
		}
		if _, duplicate := seen[item.MessageID]; duplicate {
			continue
		}
		seen[item.MessageID] = struct{}{}
		createdAt, parseErr := parseLarkTime(item.CreateTime, location)
		if parseErr != nil {
			continue
		}
		message := Message{ID: item.MessageID, ChatID: item.ChatID, ChatName: item.ChatName,
			ChatType: item.ChatType, ThreadID: item.ThreadID, SenderID: item.Sender.ID,
			SenderName: item.Sender.Name, SenderType: item.Sender.SenderType,
			MessageType: item.MessageType, Content: strings.TrimSpace(item.Content),
			URL: item.MessageLink, CreatedAt: createdAt, Deleted: item.Deleted}
		if _, skip := excludedSet[message.ChatID]; skip {
			continue
		}
		if message.Deleted || emptyContent(message.Content) {
			continue
		}
		result.Messages = append(result.Messages, message)
		if humanMessage(message) {
			result.Candidates = append(result.Candidates, message)
		}
	}
	sort.Slice(result.Messages, func(i, j int) bool { return result.Messages[i].CreatedAt.Before(result.Messages[j].CreatedAt) })
	sort.Slice(result.Candidates, func(i, j int) bool { return result.Candidates[i].CreatedAt.Before(result.Candidates[j].CreatedAt) })
	return result, nil
}

func parseLarkTime(value string, location *time.Location) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if layout == time.RFC3339Nano || layout == time.RFC3339 {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed, nil
			}
			continue
		}
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported lark time %q", value)
}

func humanMessage(message Message) bool {
	typeName := strings.ToLower(message.SenderType)
	return typeName == "user" || typeName == ""
}

func emptyContent(value string) bool {
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return false
		}
	}
	return true
}
