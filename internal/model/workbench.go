package model

import "time"

type DemandStatus string

const (
	DemandStatusPending   DemandStatus = "pending"
	DemandStatusPlanned   DemandStatus = "planned"
	DemandStatusActive    DemandStatus = "active"
	DemandStatusWaiting   DemandStatus = "waiting"
	DemandStatusDone      DemandStatus = "done"
	DemandStatusDismissed DemandStatus = "dismissed"
)

func (s DemandStatus) Valid() bool {
	switch s {
	case DemandStatusPending, DemandStatusPlanned, DemandStatusActive,
		DemandStatusWaiting, DemandStatusDone, DemandStatusDismissed:
		return true
	default:
		return false
	}
}

type DemandPriority string

const (
	DemandPriorityP0   DemandPriority = "p0"
	DemandPriorityP1   DemandPriority = "p1"
	DemandPriorityP2   DemandPriority = "p2"
	DemandPriorityP3   DemandPriority = "p3"
	DemandPriorityNone DemandPriority = "none"
)

func (p DemandPriority) Valid() bool {
	switch p {
	case DemandPriorityP0, DemandPriorityP1, DemandPriorityP2,
		DemandPriorityP3, DemandPriorityNone:
		return true
	default:
		return false
	}
}

type Source struct {
	ID          string    `json:"id"`
	DemandID    string    `json:"demand_id,omitempty"`
	Kind        string    `json:"kind"`
	ExternalID  string    `json:"external_id,omitempty"`
	ChatID      string    `json:"chat_id,omitempty"`
	ChatName    string    `json:"chat_name,omitempty"`
	SenderID    string    `json:"sender_id,omitempty"`
	SenderName  string    `json:"sender_name,omitempty"`
	MessageTime time.Time `json:"message_time,omitempty"`
	URL         string    `json:"url,omitempty"`
	Excerpt     string    `json:"excerpt,omitempty"`
	DedupeKey   string    `json:"dedupe_key,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Progress struct {
	ID        string    `json:"id"`
	DemandID  string    `json:"demand_id,omitempty"`
	Text      string    `json:"text"`
	DedupeKey string    `json:"dedupe_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Demand struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Status        DemandStatus   `json:"status"`
	Priority      DemandPriority `json:"priority"`
	ProjectID     *string        `json:"project_id"`
	ProjectHint   string         `json:"project_hint,omitempty"`
	NextAction    string         `json:"next_action,omitempty"`
	BlockedReason string         `json:"blocked_reason,omitempty"`
	Sources       []Source       `json:"sources"`
	Progress      []Progress     `json:"progress"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	CompletedAt   *time.Time     `json:"completed_at"`
}

type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusArchived ProjectStatus = "archived"
)

func (s ProjectStatus) Valid() bool {
	return s == ProjectStatusActive || s == ProjectStatusArchived
}

type Project struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Color       string        `json:"color"`
	Status      ProjectStatus `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type FeishuSyncState struct {
	Enabled        bool       `json:"enabled"`
	DocToken       string     `json:"doc_token,omitempty"`
	DocURL         string     `json:"doc_url,omitempty"`
	Dirty          bool       `json:"dirty"`
	DesiredVersion int64      `json:"desired_version"`
	SyncedVersion  int64      `json:"synced_version"`
	Status         string     `json:"status"`
	LastRevision   string     `json:"last_revision,omitempty"`
	LastHash       string     `json:"last_hash,omitempty"`
	LastSuccessAt  *time.Time `json:"last_success_at"`
	LastError      string     `json:"last_error,omitempty"`
	RetryCount     int        `json:"retry_count"`
	NextRetryAt    *time.Time `json:"next_retry_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type FeishuIngestionState struct {
	Enabled         bool       `json:"enabled"`
	Timezone        string     `json:"timezone"`
	ActiveStart     string     `json:"active_start"`
	ActiveEnd       string     `json:"active_end"`
	IntervalMinutes int        `json:"interval_minutes"`
	OverlapMinutes  int        `json:"overlap_minutes"`
	ExcludedChatIDs []string   `json:"excluded_chat_ids"`
	LastSuccessEnd  *time.Time `json:"last_success_end"`
	LastBackfillAt  *time.Time `json:"last_backfill_at"`
	NextRunAt       *time.Time `json:"next_run_at"`
	Status          string     `json:"status"`
	LastError       string     `json:"last_error,omitempty"`
	CurrentRunID    string     `json:"current_run_id,omitempty"`
	LeaseUntil      *time.Time `json:"lease_until"`
	Requested       bool       `json:"requested"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type FeishuIngestionRun struct {
	ID                  string     `json:"id"`
	Trigger             string     `json:"trigger"`
	Status              string     `json:"status"`
	WindowStart         time.Time  `json:"window_start"`
	WindowEnd           time.Time  `json:"window_end"`
	MessagesSeen        int        `json:"messages_seen"`
	MessagesCandidate   int        `json:"messages_candidate"`
	Created             int        `json:"created"`
	Updated             int        `json:"updated"`
	Skipped             int        `json:"skipped"`
	ReviewCount         int        `json:"review_count"`
	MissingContextCount int        `json:"missing_context_count"`
	InputTokens         int64      `json:"input_tokens"`
	CachedInputTokens   int64      `json:"cached_input_tokens"`
	OutputTokens        int64      `json:"output_tokens"`
	StartedAt           time.Time  `json:"started_at"`
	FinishedAt          *time.Time `json:"finished_at"`
	Error               string     `json:"error,omitempty"`
}

type DemandReview struct {
	ID                string     `json:"id"`
	Kind              string     `json:"kind"`
	Status            string     `json:"status"`
	SuggestedDemandID string     `json:"suggested_demand_id,omitempty"`
	ProgressText      string     `json:"progress_text"`
	Source            Source     `json:"source"`
	ProgressDedupeKey string     `json:"progress_dedupe_key"`
	Confidence        float64    `json:"confidence"`
	Rationale         string     `json:"rationale,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ResolvedAt        *time.Time `json:"resolved_at"`
}

// DemandProgressUpdate is an ingestion-only append. Automated collection must
// never rewrite the matched demand's user-managed fields.
type DemandProgressUpdate struct {
	DemandID   string   `json:"demand_id"`
	Text       string   `json:"text"`
	DedupeKey  string   `json:"dedupe_key"`
	Source     Source   `json:"source"`
	Confidence float64  `json:"confidence"`
	Anchors    []string `json:"anchors,omitempty"`
}

type FeishuIngestionCommit struct {
	RunID               string                 `json:"run_id"`
	WindowEnd           time.Time              `json:"window_end"`
	BackfillAt          *time.Time             `json:"backfill_at,omitempty"`
	MessagesSeen        int                    `json:"messages_seen"`
	MessagesCandidate   int                    `json:"messages_candidate"`
	NewDemands          []*Demand              `json:"new_demands"`
	ProgressUpdates     []DemandProgressUpdate `json:"progress_updates"`
	Reviews             []DemandReview         `json:"reviews"`
	ProcessedMessageIDs []string               `json:"processed_message_ids"`
	Skipped             int                    `json:"skipped"`
	MissingContextCount int                    `json:"missing_context_count"`
	InputTokens         int64                  `json:"input_tokens"`
	CachedInputTokens   int64                  `json:"cached_input_tokens"`
	OutputTokens        int64                  `json:"output_tokens"`
}
