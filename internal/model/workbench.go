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
