package feishusync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/infowall/infowall/internal/integration"
)

// DefaultExporterID is the recommended stable component ID for the Feishu
// document exporter. Callers may supply a different ID when they need more
// than one independently configured Feishu exporter.
const DefaultExporterID integration.ComponentID = "feishu.doc"

// ExportSettings selects the Feishu document operation for one export. Exactly
// one of Create or DocRef must be set. DocRef may be a document URL or token;
// Client.Bind decides whether the first export binds an unmanaged document or
// synchronizes an already managed one.
type ExportSettings struct {
	Create bool   `json:"create,omitempty"`
	DocRef string `json:"doc_ref,omitempty"`
}

// IntegrationExporter adapts the Feishu document Client to the vendor-neutral
// integration.Exporter contract. Constructing it performs no external work;
// the client is invoked only by Export.
type IntegrationExporter struct {
	client     Client
	descriptor integration.Descriptor
	now        func() time.Time
}

// NewIntegrationExporter returns a Feishu integration exporter. Empty ID,
// name, role, and protocol fields receive the recommended defaults; explicit
// role or protocol values must match the exporter contract.
func NewIntegrationExporter(client Client, descriptor integration.Descriptor) (*IntegrationExporter, error) {
	if descriptor.ID == "" {
		descriptor.ID = DefaultExporterID
	}
	if strings.TrimSpace(descriptor.Name) == "" {
		descriptor.Name = "Feishu document"
	}
	if descriptor.Role == "" {
		descriptor.Role = integration.RoleExporter
	}
	if descriptor.ProtocolVersion == 0 {
		descriptor.ProtocolVersion = integration.ProtocolVersion
	}
	if err := descriptor.Validate(); err != nil {
		return nil, fmt.Errorf("Feishu exporter descriptor: %w", err)
	}
	if descriptor.Role != integration.RoleExporter {
		return nil, fmt.Errorf("Feishu exporter descriptor has role %q; want %q", descriptor.Role, integration.RoleExporter)
	}
	return &IntegrationExporter{client: client, descriptor: descriptor, now: time.Now}, nil
}

// Descriptor describes this Feishu exporter component.
func (e *IntegrationExporter) Descriptor() integration.Descriptor {
	return e.descriptor
}

// Export renders the neutral workbench snapshot and creates, binds, or
// synchronizes the document selected by request settings.
func (e *IntegrationExporter) Export(ctx context.Context, request integration.ExportRequest) (integration.ExportReceipt, error) {
	settings, err := decodeExportSettings(request.Settings)
	if err != nil {
		return integration.ExportReceipt{}, err
	}
	exportedAt := e.now()
	snapshot := snapshotFromExportSnapshot(request.Snapshot)
	if snapshot.Generated.IsZero() {
		snapshot.Generated = exportedAt
	}
	rendered := Render(snapshot)

	var receipt integration.ExportReceipt
	if settings.Create {
		document, createErr := e.client.Create(ctx, rendered)
		if createErr != nil {
			return integration.ExportReceipt{}, fmt.Errorf("create Feishu document: %w", createErr)
		}
		receipt.Revision = strconv.FormatInt(document.Revision, 10)
		receipt.Hash = rendered.Hash
		receipt.URL = safeReceiptURL(document.URL)
	} else {
		result, bindErr := e.client.Bind(ctx, settings.DocRef, rendered)
		if bindErr != nil {
			return integration.ExportReceipt{}, documentExportError{cause: bindErr, docRef: settings.DocRef}
		}
		receipt.Revision = strconv.FormatInt(result.Revision, 10)
		receipt.Hash = result.Hash
		receipt.URL = safeReceiptURL(settings.DocRef)
	}
	receipt.ExportedAt = exportedAt
	return receipt, nil
}

type documentExportError struct {
	cause  error
	docRef string
}

func (e documentExportError) Error() string {
	message := strings.ReplaceAll(e.cause.Error(), e.docRef, "[redacted]")
	return "bind or sync Feishu document: " + message
}

func (e documentExportError) Unwrap() error {
	return e.cause
}

func decodeExportSettings(raw json.RawMessage) (ExportSettings, error) {
	var settings ExportSettings
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: %w", err)
	}
	if opening != json.Delim('{') {
		return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: expected JSON object")
	}
	seen := make(map[string]struct{}, 2)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: %w", tokenErr)
		}
		key, ok := token.(string)
		if !ok {
			return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: expected object key")
		}
		if _, duplicate := seen[key]; duplicate {
			return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: duplicate field %q", key)
		}
		seen[key] = struct{}{}
		switch key {
		case "create":
			value, valueErr := decoder.Token()
			if valueErr != nil {
				return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings field %q: %w", key, valueErr)
			}
			var valid bool
			settings.Create, valid = value.(bool)
			if !valid {
				return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings field %q: expected boolean", key)
			}
		case "doc_ref":
			value, valueErr := decoder.Token()
			if valueErr != nil {
				return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings field %q: %w", key, valueErr)
			}
			var valid bool
			settings.DocRef, valid = value.(string)
			if !valid {
				return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings field %q: expected string", key)
			}
		default:
			return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: unknown field %q", key)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: %w", err)
	}
	if closing != json.Delim('}') {
		return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: expected end of object")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ExportSettings{}, fmt.Errorf("decode Feishu exporter settings: %w", err)
	}
	settings.DocRef = strings.TrimSpace(settings.DocRef)
	if settings.Create == (settings.DocRef != "") {
		return ExportSettings{}, fmt.Errorf("Feishu exporter settings must provide exactly one of create=true or doc_ref")
	}
	return settings, nil
}

func safeReceiptURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return ""
	}
	return value
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func snapshotFromExportSnapshot(source integration.ExportSnapshot) Snapshot {
	snapshot := Snapshot{
		Generated: source.GeneratedAt,
		Projects:  make([]ProjectView, 0, len(source.Projects)),
		Demands:   make([]DemandView, 0, len(source.Demands)),
	}
	for _, project := range source.Projects {
		snapshot.Projects = append(snapshot.Projects, ProjectView{ID: project.ID, Name: project.Name})
	}
	for _, demand := range source.Demands {
		view := DemandView{
			ID:            demand.ID,
			Title:         demand.Title,
			Description:   demand.Description,
			Status:        demand.Status,
			Priority:      demand.Priority,
			ProjectID:     demand.ProjectID,
			ProjectHint:   demand.ProjectHint,
			NextAction:    demand.NextAction,
			BlockedReason: demand.BlockedReason,
			UpdatedAt:     demand.UpdatedAt,
			CompletedAt:   demand.CompletedAt,
		}
		latestProgress := -1
		for index := range demand.Progress {
			if latestProgress == -1 || !demand.Progress[index].CreatedAt.Before(demand.Progress[latestProgress].CreatedAt) {
				latestProgress = index
			}
		}
		if latestProgress >= 0 {
			latest := demand.Progress[latestProgress]
			view.LatestProgress = latest.Text
			view.ProgressLinks = make([]ProgressLinkView, 0, len(latest.Links))
			for _, link := range latest.Links {
				view.ProgressLinks = append(view.ProgressLinks, ProgressLinkView{
					Title: link.Title,
					URL:   link.URL,
					State: link.State,
				})
			}
		}
		latestSource := -1
		for index := range demand.Sources {
			if strings.TrimSpace(demand.Sources[index].URL) == "" {
				continue
			}
			if latestSource == -1 || !demand.Sources[index].OccurredAt.Before(demand.Sources[latestSource].OccurredAt) {
				latestSource = index
			}
		}
		if latestSource >= 0 {
			view.SourceURL = demand.Sources[latestSource].URL
		}
		snapshot.Demands = append(snapshot.Demands, view)
	}
	return snapshot
}

var _ integration.Exporter = (*IntegrationExporter)(nil)
