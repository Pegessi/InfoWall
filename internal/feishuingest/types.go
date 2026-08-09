package feishuingest

import (
	"context"
	"time"

	"github.com/infowall/infowall/internal/model"
)

const MessageLimit = 2000

type Message struct {
	ID          string    `json:"message_id"`
	ChatID      string    `json:"chat_id"`
	ChatName    string    `json:"chat_name"`
	ChatType    string    `json:"chat_type,omitempty"`
	ThreadID    string    `json:"thread_id,omitempty"`
	SenderID    string    `json:"sender_id"`
	SenderName  string    `json:"sender_name"`
	SenderType  string    `json:"sender_type"`
	MessageType string    `json:"message_type"`
	Content     string    `json:"content"`
	URL         string    `json:"url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Deleted     bool      `json:"deleted"`
}

type Collection struct {
	Messages   []Message
	Candidates []Message
	Seen       int
}

type Snapshot struct {
	Demands  []*model.Demand  `json:"demands"`
	Projects []*model.Project `json:"projects"`
}

type Resource struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
	Title      string `json:"title,omitempty"`
	State      string `json:"state,omitempty"`
	Excerpt    string `json:"excerpt,omitempty"`
	DedupeKey  string `json:"dedupe_key"`
	Accessible bool   `json:"accessible"`
}

type AnalysisInput struct {
	WindowStart time.Time  `json:"window_start"`
	WindowEnd   time.Time  `json:"window_end"`
	Messages    []Message  `json:"messages"`
	Candidates  []Message  `json:"candidate_messages"`
	Snapshot    Snapshot   `json:"existing_snapshot"`
	Resources   []Resource `json:"linked_resources"`
}

// EvidenceRef is the only source shape the model may return. InfoWall owns all
// canonical metadata (sender, chat, timestamp, URL and dedupe key) and resolves
// it from the referenced collected message after strict validation.
type EvidenceRef struct {
	ExternalID string `json:"external_id"`
	Excerpt    string `json:"excerpt"`
}

type NewDemand struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	NextAction  string        `json:"next_action"`
	ProjectHint string        `json:"project_hint"`
	Sources     []EvidenceRef `json:"sources"`
}

type ProgressUpdate struct {
	DemandID   string      `json:"demand_id"`
	Text       string      `json:"text"`
	DedupeKey  string      `json:"dedupe_key"`
	Source     EvidenceRef `json:"source"`
	Confidence float64     `json:"confidence"`
	Anchors    []string    `json:"anchors"`
}

type Review struct {
	SuggestedDemandID string      `json:"suggested_demand_id"`
	ProgressText      string      `json:"progress_text"`
	ProgressDedupeKey string      `json:"progress_dedupe_key"`
	Source            EvidenceRef `json:"source"`
	Confidence        float64     `json:"confidence"`
	Rationale         string      `json:"rationale"`
}

type Result struct {
	NewDemands        []NewDemand      `json:"new_demands"`
	ProgressUpdates   []ProgressUpdate `json:"progress_updates"`
	Reviews           []Review         `json:"reviews"`
	SkippedMessageIDs []string         `json:"skipped_message_ids"`
	MissingContextIDs []string         `json:"missing_context_message_ids"`
	InputTokens       int64            `json:"input_tokens,omitempty"`
	CachedInputTokens int64            `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64            `json:"output_tokens,omitempty"`
}

type Analyzer interface {
	Analyze(context.Context, []AnalysisInput) (Result, error)
}

type Collector interface {
	Collect(context.Context, time.Time, time.Time, []string) (Collection, error)
}

type Enricher interface {
	Enrich(context.Context, []Message) []Resource
}
