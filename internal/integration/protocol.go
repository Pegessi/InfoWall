// Package integration defines the vendor-neutral contracts used to extend
// InfoWall's collection, enrichment, analysis, and export pipeline. Missing
// list fields are equivalent to empty lists; JSON encoders omit both nil and
// empty slices to keep a single wire representation.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ProtocolVersion is the integration contract version implemented by this
// package. Components must advertise this version in their descriptor.
const ProtocolVersion = 1

// ComponentID is a stable, installation-independent component identifier.
type ComponentID string

// Role identifies the operation a component implements.
type Role string

const (
	RoleConnector Role = "connector"
	RoleEnricher  Role = "enricher"
	RoleAnalyzer  Role = "analyzer"
	RoleExporter  Role = "exporter"
)

// Descriptor describes an integration component without exposing its
// implementation or vendor-specific configuration.
type Descriptor struct {
	ID              ComponentID `json:"id"`
	Role            Role        `json:"role"`
	Name            string      `json:"name"`
	Version         string      `json:"version,omitempty"`
	ProtocolVersion int         `json:"protocol_version"`
}

// Validate checks the descriptor fields shared by all component roles.
func (d Descriptor) Validate() error {
	if strings.TrimSpace(string(d.ID)) == "" {
		return fmt.Errorf("component id is empty")
	}
	if !d.Role.valid() {
		return fmt.Errorf("component %q has invalid role %q", d.ID, d.Role)
	}
	if d.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("component %q uses protocol version %d; want %d", d.ID, d.ProtocolVersion, ProtocolVersion)
	}
	return nil
}

func (r Role) valid() bool {
	switch r {
	case RoleConnector, RoleEnricher, RoleAnalyzer, RoleExporter:
		return true
	default:
		return false
	}
}

// ObservationRef identifies one observation within a connector. Its fields
// are comparable, so it can be used directly as a map key.
type ObservationRef struct {
	ConnectorID ComponentID `json:"connector_id"`
	ExternalID  string      `json:"external_id"`
}

// Key returns an unambiguous stable text form of the observation reference.
func (r ObservationRef) Key() string {
	return fmt.Sprintf("%d:%s%d:%s", len(r.ConnectorID), r.ConnectorID, len(r.ExternalID), r.ExternalID)
}

// Observation is a normalized fact collected from an external source.
type Observation struct {
	Ref           ObservationRef  `json:"ref"`
	Kind          string          `json:"kind"`
	ContainerID   string          `json:"container_id,omitempty"`
	ContainerName string          `json:"container_name,omitempty"`
	ThreadID      string          `json:"thread_id,omitempty"`
	ActorID       string          `json:"actor_id,omitempty"`
	ActorName     string          `json:"actor_name,omitempty"`
	Title         string          `json:"title,omitempty"`
	Content       string          `json:"content"`
	URL           string          `json:"url,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Deleted       bool            `json:"deleted"`
	Links         []Link          `json:"links,omitempty"`
	Attributes    json.RawMessage `json:"attributes,omitempty"`
}

// Link is a named external resource associated with an observation or a
// proposed progress update.
type Link struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id,omitempty"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url"`
	State      string `json:"state,omitempty"`
	DedupeKey  string `json:"dedupe_key,omitempty"`
}

// CollectRequest bounds one connector collection pass. Checkpoint and
// Settings are opaque JSON owned by the connector.
type CollectRequest struct {
	WindowStart time.Time       `json:"window_start"`
	WindowEnd   time.Time       `json:"window_end"`
	Limit       int             `json:"limit,omitempty"`
	Checkpoint  json.RawMessage `json:"checkpoint,omitempty"`
	Settings    json.RawMessage `json:"settings,omitempty"`
}

// Collection is the normalized output of one connector collection pass.
type Collection struct {
	Observations   []Observation    `json:"observations,omitempty"`
	CandidateRefs  []ObservationRef `json:"candidate_refs,omitempty"`
	NextCheckpoint json.RawMessage  `json:"next_checkpoint,omitempty"`
	AckRefs        []ObservationRef `json:"ack_refs,omitempty"`
	Seen           int              `json:"seen"`
	Warnings       []Warning        `json:"warnings,omitempty"`
}

// EnrichRequest contains observations and opaque enricher settings.
type EnrichRequest struct {
	Observations []Observation   `json:"observations,omitempty"`
	Settings     json.RawMessage `json:"settings,omitempty"`
}

// EnrichmentResult contains resources resolved from observations.
type EnrichmentResult struct {
	Resources []Resource `json:"resources,omitempty"`
	Warnings  []Warning  `json:"warnings,omitempty"`
}

// Warning is a non-fatal component diagnostic.
type Warning struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Ref     *ObservationRef `json:"ref,omitempty"`
}

// ResourceRef identifies a resource within an enricher.
type ResourceRef struct {
	EnricherID ComponentID `json:"enricher_id"`
	ExternalID string      `json:"external_id"`
}

// Resource is external context resolved by an enricher.
type Resource struct {
	Ref             ResourceRef      `json:"ref"`
	ObservationRefs []ObservationRef `json:"observation_refs,omitempty"`
	Kind            string           `json:"kind"`
	URL             string           `json:"url,omitempty"`
	Title           string           `json:"title,omitempty"`
	State           string           `json:"state,omitempty"`
	Excerpt         string           `json:"excerpt,omitempty"`
	DedupeKey       string           `json:"dedupe_key,omitempty"`
	Accessible      bool             `json:"accessible"`
	Attributes      json.RawMessage  `json:"attributes,omitempty"`
}

// AnalysisBatch is one analyzer input batch. Settings are opaque JSON owned
// by the analyzer.
type AnalysisBatch struct {
	WindowStart  time.Time        `json:"window_start"`
	WindowEnd    time.Time        `json:"window_end"`
	Observations []Observation    `json:"observations,omitempty"`
	Candidates   []Observation    `json:"candidates,omitempty"`
	Resources    []Resource       `json:"resources,omitempty"`
	Snapshot     AnalysisSnapshot `json:"snapshot"`
	Settings     json.RawMessage  `json:"settings,omitempty"`
}

// AnalysisSnapshot is the current workbench state visible to an analyzer.
type AnalysisSnapshot struct {
	Demands  []DemandView  `json:"demands,omitempty"`
	Projects []ProjectView `json:"projects,omitempty"`
}

// DemandView is the vendor-neutral subset of a demand needed by components.
type DemandView struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Status        string         `json:"status"`
	Priority      string         `json:"priority"`
	ProjectID     string         `json:"project_id,omitempty"`
	ProjectHint   string         `json:"project_hint,omitempty"`
	NextAction    string         `json:"next_action,omitempty"`
	BlockedReason string         `json:"blocked_reason,omitempty"`
	Sources       []SourceView   `json:"sources,omitempty"`
	Progress      []ProgressView `json:"progress,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	CompletedAt   *time.Time     `json:"completed_at,omitempty"`
}

// SourceView is source evidence attached to an existing demand.
type SourceView struct {
	Kind       string    `json:"kind"`
	ExternalID string    `json:"external_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	URL        string    `json:"url,omitempty"`
	Excerpt    string    `json:"excerpt,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ProgressView is one existing demand progress entry.
type ProgressView struct {
	Text      string    `json:"text"`
	Links     []Link    `json:"links,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ProjectView is the vendor-neutral subset of a project needed by components.
type ProjectView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Color       string    `json:"color,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// EvidenceRef identifies collected evidence used by an analyzer proposal.
type EvidenceRef struct {
	Observation ObservationRef `json:"observation"`
	Excerpt     string         `json:"excerpt"`
}

// DemandProposal describes a demand that the analyzer proposes creating.
type DemandProposal struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	NextAction  string        `json:"next_action,omitempty"`
	ProjectHint string        `json:"project_hint,omitempty"`
	Evidence    []EvidenceRef `json:"evidence,omitempty"`
}

// ProgressProposal describes progress that the analyzer proposes appending.
type ProgressProposal struct {
	DemandID   string      `json:"demand_id"`
	Text       string      `json:"text"`
	DedupeKey  string      `json:"dedupe_key"`
	Evidence   EvidenceRef `json:"evidence"`
	Links      []Link      `json:"links,omitempty"`
	Confidence float64     `json:"confidence"`
	Anchors    []string    `json:"anchors,omitempty"`
}

// ReviewProposal describes an ambiguous progress match requiring review.
type ReviewProposal struct {
	SuggestedDemandID string      `json:"suggested_demand_id,omitempty"`
	ProgressText      string      `json:"progress_text"`
	ProgressDedupeKey string      `json:"progress_dedupe_key"`
	Evidence          EvidenceRef `json:"evidence"`
	Links             []Link      `json:"links,omitempty"`
	Confidence        float64     `json:"confidence"`
	Rationale         string      `json:"rationale,omitempty"`
}

// Usage reports analyzer resource usage.
type Usage struct {
	InputTokens       int64 `json:"input_tokens,omitempty"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64 `json:"output_tokens,omitempty"`
}

// AnalyzerRoute reports which analyzer route handled a batch.
type AnalyzerRoute struct {
	Route              string `json:"route,omitempty"`
	ProfileID          string `json:"profile_id,omitempty"`
	ProfileFingerprint string `json:"profile_fingerprint,omitempty"`
	Healthy            bool   `json:"healthy"`
	FallbackUsed       bool   `json:"fallback_used"`
	PrimaryError       string `json:"primary_error,omitempty"`
}

// AnalysisResult contains analyzer proposals and diagnostics.
type AnalysisResult struct {
	Demands        []DemandProposal   `json:"demand_proposals,omitempty"`
	Progress       []ProgressProposal `json:"progress_proposals,omitempty"`
	Reviews        []ReviewProposal   `json:"review_proposals,omitempty"`
	Skipped        []ObservationRef   `json:"skipped,omitempty"`
	MissingContext []ObservationRef   `json:"missing_context,omitempty"`
	Usage          Usage              `json:"usage"`
	Route          AnalyzerRoute      `json:"route"`
	Warnings       []Warning          `json:"warnings,omitempty"`
}

// ExportRequest contains a complete export snapshot and opaque exporter
// settings.
type ExportRequest struct {
	Destination    string          `json:"destination"`
	DesiredVersion int64           `json:"desired_version"`
	Snapshot       ExportSnapshot  `json:"snapshot"`
	Settings       json.RawMessage `json:"settings,omitempty"`
}

// ExportSnapshot is the current workbench state to publish externally.
type ExportSnapshot struct {
	Demands     []DemandView  `json:"demands,omitempty"`
	Projects    []ProjectView `json:"projects,omitempty"`
	GeneratedAt time.Time     `json:"generated_at"`
}

// ExportReceipt records the durable identity returned by an exporter.
type ExportReceipt struct {
	Revision   string    `json:"revision,omitempty"`
	URL        string    `json:"url,omitempty"`
	Hash       string    `json:"hash,omitempty"`
	ExportedAt time.Time `json:"exported_at"`
	Warnings   []Warning `json:"warnings,omitempty"`
}

// Connector collects normalized observations from an external source.
type Connector interface {
	Descriptor() Descriptor
	Collect(context.Context, CollectRequest) (Collection, error)
}

// Enricher resolves additional resources referenced by observations.
type Enricher interface {
	Descriptor() Descriptor
	Enrich(context.Context, EnrichRequest) (EnrichmentResult, error)
}

// Analyzer proposes workbench changes from a normalized analysis batch.
type Analyzer interface {
	Descriptor() Descriptor
	Analyze(context.Context, AnalysisBatch) (AnalysisResult, error)
}

// Exporter publishes a workbench snapshot to an external destination.
type Exporter interface {
	Descriptor() Descriptor
	Export(context.Context, ExportRequest) (ExportReceipt, error)
}
