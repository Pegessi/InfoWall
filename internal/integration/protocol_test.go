package integration

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestProtocolJSONRoundTrip(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, time.October, 6, 11, 12, 13, 0, time.UTC)
	observationRef := ObservationRef{ConnectorID: "chat", ExternalID: "message-42"}
	observation := Observation{
		Ref:           observationRef,
		Kind:          "message",
		ContainerID:   "room-7",
		ContainerName: "Build room",
		ThreadID:      "thread-9",
		ActorID:       "user-3",
		ActorName:     "Ada",
		Title:         "Release update",
		Content:       "The release is ready.",
		URL:           "https://example.test/messages/42",
		OccurredAt:    occurredAt,
		Links: []Link{{
			Kind:       "change",
			ExternalID: "change-8",
			Title:      "Ship release",
			URL:        "https://example.test/changes/8",
			State:      "merged",
			DedupeKey:  "change:8",
		}},
		Attributes: json.RawMessage(`{"importance":"high"}`),
	}
	resource := Resource{
		Ref:             ResourceRef{EnricherID: "changes", ExternalID: "change-8"},
		ObservationRefs: []ObservationRef{observationRef},
		Kind:            "change",
		URL:             "https://example.test/changes/8",
		Title:           "Ship release",
		State:           "merged",
		Excerpt:         "All checks passed",
		DedupeKey:       "change:8",
		Accessible:      true,
		Attributes:      json.RawMessage(`{"reviewers":2}`),
	}
	snapshot := AnalysisSnapshot{
		Demands: []DemandView{{
			ID: "demand-1", Title: "Release", Description: "Ship it", Status: "active",
			Priority: "p1", ProjectID: "project-1", ProjectHint: "Delivery",
			NextAction: "Deploy",
			Sources: []SourceView{{
				Kind: "message", ExternalID: "message-42", Title: "Release update",
				URL: "https://example.test/messages/42", Excerpt: "release is ready", OccurredAt: occurredAt.Add(-30 * time.Minute),
			}},
			Progress:  []ProgressView{{Text: "Checks passed", Links: observation.Links, CreatedAt: occurredAt.Add(-15 * time.Minute)}},
			CreatedAt: occurredAt.Add(-time.Hour), UpdatedAt: occurredAt, CompletedAt: &occurredAt,
		}},
		Projects: []ProjectView{{
			ID: "project-1", Name: "Delivery", Description: "Releases",
			Color: "blue", Status: "active", CreatedAt: occurredAt.Add(-2 * time.Hour), UpdatedAt: occurredAt,
		}},
	}
	evidence := EvidenceRef{Observation: observationRef, Excerpt: "release is ready"}

	values := []any{
		Descriptor{ID: "chat", Role: RoleConnector, Name: "Chat", Version: "1.2.3", ProtocolVersion: ProtocolVersion},
		observationRef,
		observation,
		CollectRequest{WindowStart: occurredAt.Add(-time.Hour), WindowEnd: occurredAt, Limit: 200, Checkpoint: json.RawMessage(`{"cursor":"abc"}`), Settings: json.RawMessage(`{"rooms":["room-7"]}`)},
		Collection{Observations: []Observation{observation}, CandidateRefs: []ObservationRef{observationRef}, NextCheckpoint: json.RawMessage(`{"cursor":"def"}`), AckRefs: []ObservationRef{observationRef}, Seen: 3, Warnings: []Warning{{Code: "partial", Message: "one room unavailable", Ref: &observationRef}}},
		EnrichRequest{Observations: []Observation{observation}, Settings: json.RawMessage(`{"fetch_body":true}`)},
		EnrichmentResult{Resources: []Resource{resource}, Warnings: []Warning{{Code: "partial", Message: "body unavailable"}}},
		resource.Ref,
		resource,
		AnalysisBatch{WindowStart: occurredAt.Add(-time.Hour), WindowEnd: occurredAt, Observations: []Observation{observation}, Candidates: []Observation{observation}, Resources: []Resource{resource}, Snapshot: snapshot, Settings: json.RawMessage(`{"model":"local"}`)},
		snapshot,
		snapshot.Demands[0],
		snapshot.Demands[0].Sources[0],
		snapshot.Demands[0].Progress[0],
		snapshot.Projects[0],
		evidence,
		DemandProposal{Title: "Follow release", Description: "Monitor rollout", NextAction: "Check metrics", ProjectHint: "Delivery", Evidence: []EvidenceRef{evidence}},
		ProgressProposal{DemandID: "demand-1", Text: "Release ready", DedupeKey: "progress:42", Evidence: evidence, Links: observation.Links, Confidence: .97, Anchors: []string{"release"}},
		ReviewProposal{SuggestedDemandID: "demand-1", ProgressText: "Maybe ready", ProgressDedupeKey: "review:42", Evidence: evidence, Links: observation.Links, Confidence: .6, Rationale: "ambiguous wording"},
		Usage{InputTokens: 100, CachedInputTokens: 25, OutputTokens: 40},
		AnalyzerRoute{Route: "primary", ProfileID: "profile-1", ProfileFingerprint: "abcdef", Healthy: true, FallbackUsed: true, PrimaryError: "timeout"},
		AnalysisResult{
			Demands:  []DemandProposal{{Title: "Follow release", Evidence: []EvidenceRef{evidence}}},
			Progress: []ProgressProposal{{DemandID: "demand-1", Text: "Ready", Evidence: evidence}},
			Reviews:  []ReviewProposal{{ProgressText: "Maybe ready", Evidence: evidence}},
			Skipped:  []ObservationRef{observationRef}, MissingContext: []ObservationRef{observationRef},
			Usage: Usage{InputTokens: 100}, Route: AnalyzerRoute{Route: "primary", Healthy: true},
			Warnings: []Warning{{Code: "truncated", Message: "context shortened"}},
		},
		ExportRequest{Destination: "document:weekly", DesiredVersion: 7, Snapshot: ExportSnapshot{Demands: snapshot.Demands, Projects: snapshot.Projects, GeneratedAt: occurredAt}, Settings: json.RawMessage(`{"document":"weekly"}`)},
		ExportSnapshot{Demands: snapshot.Demands, Projects: snapshot.Projects, GeneratedAt: occurredAt},
		ExportReceipt{Revision: "rev-4", URL: "https://example.test/doc", Hash: "1234", ExportedAt: occurredAt, Warnings: []Warning{{Code: "partial", Message: "one section omitted"}}},
	}

	for _, value := range values {
		value := value
		t.Run(reflect.TypeOf(value).Name(), func(t *testing.T) {
			payload, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			decoded := reflect.New(reflect.TypeOf(value))
			if err := json.Unmarshal(payload, decoded.Interface()); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(value, decoded.Elem().Interface()) {
				t.Fatalf("round trip mismatch:\nwant: %#v\n got: %#v", value, decoded.Elem().Interface())
			}
		})
	}
}

func TestObservationRefComparableAndStableKey(t *testing.T) {
	t.Parallel()

	first := ObservationRef{ConnectorID: "a:b", ExternalID: "c"}
	second := ObservationRef{ConnectorID: "a", ExternalID: "b:c"}
	refs := map[ObservationRef]string{first: "first", second: "second"}
	if len(refs) != 2 {
		t.Fatalf("observation refs collided as map keys: %#v", refs)
	}
	if first.Key() == second.Key() {
		t.Fatalf("stable keys collided: %q", first.Key())
	}
	if first.Key() != (ObservationRef{ConnectorID: "a:b", ExternalID: "c"}).Key() {
		t.Fatal("equal references returned different stable keys")
	}
}

func TestProtocolJSONFieldNames(t *testing.T) {
	t.Parallel()

	ref := ObservationRef{ConnectorID: "chat", ExternalID: "message-42"}
	tests := []struct {
		name    string
		value   any
		present []string
		absent  []string
	}{
		{
			name: "collect request",
			value: CollectRequest{
				Limit: 20, Checkpoint: json.RawMessage(`{"cursor":1}`), Settings: json.RawMessage(`{"room":"dev"}`),
			},
			present: []string{"window_start", "window_end", "limit", "checkpoint", "settings"},
		},
		{
			name: "collection",
			value: Collection{
				Observations: []Observation{}, CandidateRefs: []ObservationRef{ref},
				NextCheckpoint: json.RawMessage(`{"cursor":2}`), AckRefs: []ObservationRef{ref},
			},
			present: []string{"candidate_refs", "next_checkpoint", "ack_refs", "seen"},
			absent:  []string{"checkpoint", "observations"},
		},
		{
			name:    "resource",
			value:   Resource{ObservationRefs: []ObservationRef{ref}},
			present: []string{"ref", "observation_refs", "kind", "accessible"},
		},
		{
			name:    "analysis batch",
			value:   AnalysisBatch{Observations: []Observation{}, Candidates: []Observation{{Ref: ref}}, Resources: []Resource{}},
			present: []string{"window_start", "window_end", "candidates", "snapshot"},
			absent:  []string{"observations", "resources"},
		},
		{
			name: "demand view",
			value: DemandView{
				Sources:     []SourceView{{Kind: "message", ExternalID: "message-42"}},
				Progress:    []ProgressView{{Text: "ready", Links: []Link{{URL: "https://example.test/change"}}}},
				CompletedAt: &time.Time{},
			},
			present: []string{"id", "title", "description", "status", "priority", "sources", "progress", "created_at", "updated_at", "completed_at"},
		},
		{
			name:    "source view",
			value:   SourceView{Kind: "message", ExternalID: "message-42", Title: "Release", URL: "https://example.test/messages/42", Excerpt: "ready"},
			present: []string{"kind", "external_id", "title", "url", "excerpt", "occurred_at"},
		},
		{
			name:    "progress view",
			value:   ProgressView{Text: "ready", Links: []Link{{URL: "https://example.test/change"}}},
			present: []string{"text", "links", "created_at"},
		},
		{
			name:    "export request",
			value:   ExportRequest{Destination: "document:weekly", DesiredVersion: 7},
			present: []string{"destination", "desired_version", "snapshot"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			payload, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(payload, &fields); err != nil {
				t.Fatalf("unmarshal object: %v", err)
			}
			for _, field := range test.present {
				if _, ok := fields[field]; !ok {
					t.Errorf("missing JSON field %q in %s", field, payload)
				}
			}
			for _, field := range test.absent {
				if _, ok := fields[field]; ok {
					t.Errorf("unexpected JSON field %q in %s", field, payload)
				}
			}
		})
	}
}

func TestProtocolFixedJSON(t *testing.T) {
	t.Parallel()

	ref := ObservationRef{ConnectorID: "chat", ExternalID: "message-42"}
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "zero collection", value: Collection{}, want: `{"seen":0}`},
		{name: "zero analysis result", value: AnalysisResult{}, want: `{"usage":{},"route":{"healthy":false,"fallback_used":false}}`},
		{name: "zero export receipt", value: ExportReceipt{}, want: `{"exported_at":"0001-01-01T00:00:00Z"}`},
		{name: "global warning", value: Warning{Code: "partial", Message: "some data unavailable"}, want: `{"code":"partial","message":"some data unavailable"}`},
		{name: "referenced warning", value: Warning{Code: "invalid", Message: "observation skipped", Ref: &ref}, want: `{"code":"invalid","message":"observation skipped","ref":{"connector_id":"chat","external_id":"message-42"}}`},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			payload, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(payload) != test.want {
				t.Fatalf("unexpected JSON:\nwant: %s\n got: %s", test.want, payload)
			}
		})
	}
}

func TestProtocolListFieldsOmitNilAndEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		nilValue   any
		emptyValue any
		fields     []string
	}{
		{name: "observation", nilValue: Observation{}, emptyValue: Observation{Links: []Link{}}, fields: []string{"links"}},
		{name: "collection", nilValue: Collection{}, emptyValue: Collection{Observations: []Observation{}, CandidateRefs: []ObservationRef{}, AckRefs: []ObservationRef{}, Warnings: []Warning{}}, fields: []string{"observations", "candidate_refs", "ack_refs", "warnings"}},
		{name: "enrich request", nilValue: EnrichRequest{}, emptyValue: EnrichRequest{Observations: []Observation{}}, fields: []string{"observations"}},
		{name: "enrichment result", nilValue: EnrichmentResult{}, emptyValue: EnrichmentResult{Resources: []Resource{}, Warnings: []Warning{}}, fields: []string{"resources", "warnings"}},
		{name: "resource", nilValue: Resource{}, emptyValue: Resource{ObservationRefs: []ObservationRef{}}, fields: []string{"observation_refs"}},
		{name: "analysis batch", nilValue: AnalysisBatch{}, emptyValue: AnalysisBatch{Observations: []Observation{}, Candidates: []Observation{}, Resources: []Resource{}}, fields: []string{"observations", "candidates", "resources"}},
		{name: "analysis snapshot", nilValue: AnalysisSnapshot{}, emptyValue: AnalysisSnapshot{Demands: []DemandView{}, Projects: []ProjectView{}}, fields: []string{"demands", "projects"}},
		{name: "demand view", nilValue: DemandView{}, emptyValue: DemandView{Sources: []SourceView{}, Progress: []ProgressView{}}, fields: []string{"sources", "progress"}},
		{name: "progress view", nilValue: ProgressView{}, emptyValue: ProgressView{Links: []Link{}}, fields: []string{"links"}},
		{name: "demand proposal", nilValue: DemandProposal{}, emptyValue: DemandProposal{Evidence: []EvidenceRef{}}, fields: []string{"evidence"}},
		{name: "progress proposal", nilValue: ProgressProposal{}, emptyValue: ProgressProposal{Links: []Link{}, Anchors: []string{}}, fields: []string{"links", "anchors"}},
		{name: "review proposal", nilValue: ReviewProposal{}, emptyValue: ReviewProposal{Links: []Link{}}, fields: []string{"links"}},
		{name: "analysis result", nilValue: AnalysisResult{}, emptyValue: AnalysisResult{Demands: []DemandProposal{}, Progress: []ProgressProposal{}, Reviews: []ReviewProposal{}, Skipped: []ObservationRef{}, MissingContext: []ObservationRef{}, Warnings: []Warning{}}, fields: []string{"demand_proposals", "progress_proposals", "review_proposals", "skipped", "missing_context", "warnings"}},
		{name: "export snapshot", nilValue: ExportSnapshot{}, emptyValue: ExportSnapshot{Demands: []DemandView{}, Projects: []ProjectView{}}, fields: []string{"demands", "projects"}},
		{name: "export receipt", nilValue: ExportReceipt{}, emptyValue: ExportReceipt{Warnings: []Warning{}}, fields: []string{"warnings"}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			nilJSON := marshalObject(t, test.nilValue)
			emptyJSON := marshalObject(t, test.emptyValue)
			if !reflect.DeepEqual(nilJSON, emptyJSON) {
				t.Fatalf("nil and empty lists differ:\nnil:   %#v\nempty: %#v", nilJSON, emptyJSON)
			}
			for _, field := range test.fields {
				if _, ok := emptyJSON[field]; ok {
					t.Errorf("empty list field %q was not omitted", field)
				}
			}
		})
	}
}

func TestRawMessageJSONStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  any
		want   map[string]string
		absent []string
	}{
		{name: "collect absent", value: CollectRequest{}, absent: []string{"checkpoint", "settings"}},
		{name: "collect null", value: CollectRequest{Checkpoint: json.RawMessage(`null`), Settings: json.RawMessage(`null`)}, want: map[string]string{"checkpoint": "null", "settings": "null"}},
		{name: "collect object", value: CollectRequest{Checkpoint: json.RawMessage(`{"cursor":2}`), Settings: json.RawMessage(`{"room":"dev"}`)}, want: map[string]string{"checkpoint": `{"cursor":2}`, "settings": `{"room":"dev"}`}},
		{name: "next checkpoint absent", value: Collection{}, absent: []string{"next_checkpoint"}},
		{name: "next checkpoint null", value: Collection{NextCheckpoint: json.RawMessage(`null`)}, want: map[string]string{"next_checkpoint": "null"}},
		{name: "next checkpoint object", value: Collection{NextCheckpoint: json.RawMessage(`{"cursor":3}`)}, want: map[string]string{"next_checkpoint": `{"cursor":3}`}},
		{name: "observation attributes absent", value: Observation{}, absent: []string{"attributes"}},
		{name: "observation attributes null", value: Observation{Attributes: json.RawMessage(`null`)}, want: map[string]string{"attributes": "null"}},
		{name: "observation attributes object", value: Observation{Attributes: json.RawMessage(`{"priority":1}`)}, want: map[string]string{"attributes": `{"priority":1}`}},
		{name: "enricher settings absent", value: EnrichRequest{}, absent: []string{"settings"}},
		{name: "enricher settings null", value: EnrichRequest{Settings: json.RawMessage(`null`)}, want: map[string]string{"settings": "null"}},
		{name: "enricher settings object", value: EnrichRequest{Settings: json.RawMessage(`{"depth":2}`)}, want: map[string]string{"settings": `{"depth":2}`}},
		{name: "resource attributes absent", value: Resource{}, absent: []string{"attributes"}},
		{name: "resource attributes null", value: Resource{Attributes: json.RawMessage(`null`)}, want: map[string]string{"attributes": "null"}},
		{name: "resource attributes object", value: Resource{Attributes: json.RawMessage(`{"reviewers":2}`)}, want: map[string]string{"attributes": `{"reviewers":2}`}},
		{name: "analyzer settings absent", value: AnalysisBatch{}, absent: []string{"settings"}},
		{name: "analyzer settings null", value: AnalysisBatch{Settings: json.RawMessage(`null`)}, want: map[string]string{"settings": "null"}},
		{name: "analyzer settings object", value: AnalysisBatch{Settings: json.RawMessage(`{"model":"local"}`)}, want: map[string]string{"settings": `{"model":"local"}`}},
		{name: "exporter settings absent", value: ExportRequest{}, absent: []string{"settings"}},
		{name: "exporter settings null", value: ExportRequest{Settings: json.RawMessage(`null`)}, want: map[string]string{"settings": "null"}},
		{name: "exporter settings object", value: ExportRequest{Settings: json.RawMessage(`{"document":"weekly"}`)}, want: map[string]string{"settings": `{"document":"weekly"}`}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fields := marshalObject(t, test.value)
			for field, want := range test.want {
				got, ok := fields[field]
				if !ok {
					t.Errorf("missing RawMessage field %q", field)
					continue
				}
				if string(got) != want {
					t.Errorf("field %q: want %s, got %s", field, want, got)
				}
			}
			for _, field := range test.absent {
				if _, ok := fields[field]; ok {
					t.Errorf("RawMessage field %q should be absent", field)
				}
			}
		})
	}
}

func marshalObject(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	return fields
}

type fakeComponent struct {
	descriptor Descriptor
}

func (f *fakeComponent) Descriptor() Descriptor { return f.descriptor }

func (f *fakeComponent) Collect(context.Context, CollectRequest) (Collection, error) {
	return Collection{}, nil
}

func (f *fakeComponent) Enrich(context.Context, EnrichRequest) (EnrichmentResult, error) {
	return EnrichmentResult{}, nil
}

func (f *fakeComponent) Analyze(context.Context, AnalysisBatch) (AnalysisResult, error) {
	return AnalysisResult{}, nil
}

func (f *fakeComponent) Export(context.Context, ExportRequest) (ExportReceipt, error) {
	return ExportReceipt{}, nil
}
