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
	Runner       feishusync.CommandRunner
	Timezone     string
	SelfUserID   string
	SelfUserName string
}

type selfIdentity struct {
	OpenID string
	Name   string
}

type larkAuthStatus struct {
	Identities struct {
		User struct {
			Status    string `json:"status"`
			Available bool   `json:"available"`
			OpenID    string `json:"openId"`
			UserName  string `json:"userName"`
		} `json:"user"`
	} `json:"identities"`
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
	Mentions    []struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"mentions"`
	Sender struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		SenderType string `json:"sender_type"`
	} `json:"sender"`
}

func (c LarkCollector) Collect(ctx context.Context, start, end time.Time, excluded []string) (Collection, error) {
	if c.Runner == nil {
		return Collection{}, errors.New("lark runner is required")
	}
	identity, err := c.resolveSelfIdentity(ctx)
	if err != nil {
		return Collection{}, err
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
	allMessages := make([]Message, 0, len(response.Data.Messages))
	result := Collection{Messages: []Message{}, Candidates: []Message{}, Seen: len(response.Data.Messages)}
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
		for _, mention := range item.Mentions {
			message.Mentions = append(message.Mentions, Mention{ID: mention.ID, Key: mention.Key, Name: mention.Name})
		}
		if _, skip := excludedSet[message.ChatID]; skip {
			continue
		}
		if message.Deleted || emptyContent(message.Content) {
			continue
		}
		allMessages = append(allMessages, message)
	}
	result.Messages, result.Candidates = selfRelevantMessages(allMessages, identity)
	sort.Slice(result.Messages, func(i, j int) bool { return result.Messages[i].CreatedAt.Before(result.Messages[j].CreatedAt) })
	sort.Slice(result.Candidates, func(i, j int) bool { return result.Candidates[i].CreatedAt.Before(result.Candidates[j].CreatedAt) })
	return result, nil
}

func (c LarkCollector) resolveSelfIdentity(ctx context.Context) (selfIdentity, error) {
	identity := selfIdentity{OpenID: strings.TrimSpace(c.SelfUserID), Name: strings.TrimSpace(c.SelfUserName)}
	if identity.OpenID != "" {
		return identity, nil
	}
	raw, err := c.Runner.Run(ctx, "", "auth", "status", "--json")
	if err != nil {
		return selfIdentity{}, fmt.Errorf("resolve current Feishu user: %w", err)
	}
	var status larkAuthStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return selfIdentity{}, fmt.Errorf("decode current Feishu user: %w", err)
	}
	user := status.Identities.User
	// needs_refresh is a usable user identity: lark-cli refreshes its token on
	// the following user API call. Treat availability plus a stable open_id as
	// the identity contract and let the actual search surface refresh/auth
	// failures without advancing the ingestion watermark.
	if !user.Available || strings.TrimSpace(user.OpenID) == "" {
		return selfIdentity{}, errors.New("current Feishu user identity is unavailable; group ingestion is fail-closed")
	}
	return selfIdentity{OpenID: strings.TrimSpace(user.OpenID), Name: strings.TrimSpace(user.UserName)}, nil
}

// selfRelevantMessages applies the non-negotiable pre-Codex relevance gate.
// Direct conversations remain eligible. Group/topic conversations only become
// candidates when the current user authored the message, was explicitly
// mentioned, or participated in that exact thread. Unrelated group messages
// are not even included as context.
func selfRelevantMessages(messages []Message, identity selfIdentity) ([]Message, []Message) {
	selfThreads := make(map[string]struct{})
	for _, message := range messages {
		if isGroupConversation(message) && message.ThreadID != "" && sentBySelf(message, identity) {
			selfThreads[threadKey(message)] = struct{}{}
		}
	}

	candidateIDs := make(map[string]struct{})
	contextThreads := make(map[string]struct{})
	for _, message := range messages {
		if !humanMessage(message) {
			continue
		}
		relevant := isDirectConversation(message) || sentBySelf(message, identity) || mentionsSelf(message, identity)
		if !relevant && isGroupConversation(message) && message.ThreadID != "" {
			_, relevant = selfThreads[threadKey(message)]
		}
		if !relevant {
			continue
		}
		candidateIDs[message.ID] = struct{}{}
		if isGroupConversation(message) && message.ThreadID != "" {
			contextThreads[threadKey(message)] = struct{}{}
		}
	}

	contextMessages := make([]Message, 0, len(messages))
	candidates := make([]Message, 0, len(candidateIDs))
	for _, message := range messages {
		_, candidate := candidateIDs[message.ID]
		includeContext := isDirectConversation(message) || candidate
		if !includeContext && isGroupConversation(message) && message.ThreadID != "" {
			_, includeContext = contextThreads[threadKey(message)]
		}
		if includeContext {
			contextMessages = append(contextMessages, message)
		}
		if candidate {
			candidates = append(candidates, message)
		}
	}
	return contextMessages, candidates
}

func isDirectConversation(message Message) bool {
	return strings.EqualFold(strings.TrimSpace(message.ChatType), "p2p")
}

func isGroupConversation(message Message) bool {
	chatType := strings.ToLower(strings.TrimSpace(message.ChatType))
	return chatType == "group" || chatType == "topic"
}

func sentBySelf(message Message, identity selfIdentity) bool {
	if senderID := strings.TrimSpace(message.SenderID); senderID != "" {
		return senderID == identity.OpenID
	}
	return identity.Name != "" && strings.EqualFold(strings.TrimSpace(message.SenderName), identity.Name)
}

func mentionsSelf(message Message, identity selfIdentity) bool {
	for _, mention := range message.Mentions {
		if id := strings.TrimSpace(mention.ID); id != "" {
			if id == identity.OpenID {
				return true
			}
			continue
		}
		if identity.Name != "" && strings.EqualFold(strings.TrimSpace(mention.Name), identity.Name) {
			return true
		}
	}
	return false
}

func threadKey(message Message) string {
	return message.ChatID + "\x00" + message.ThreadID
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
