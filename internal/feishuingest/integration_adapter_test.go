package feishuingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/integration"
	"github.com/infowall/infowall/internal/model"
)

func TestIntegrationBridgeDescriptorsAndValidation(t *testing.T) {
	tests := []struct {
		name string
		role integration.Role
		new  func(integration.ComponentID) (integration.Descriptor, error)
	}{
		{name: "connector", role: integration.RoleConnector, new: func(id integration.ComponentID) (integration.Descriptor, error) {
			adapter, err := NewIntegrationConnector(id, &adapterTestLegacyCollector{})
			if err != nil {
				return integration.Descriptor{}, err
			}
			return adapter.Descriptor(), nil
		}},
		{name: "enricher", role: integration.RoleEnricher, new: func(id integration.ComponentID) (integration.Descriptor, error) {
			adapter, err := NewIntegrationEnricher(id, adapterTestLegacyEnricher{})
			if err != nil {
				return integration.Descriptor{}, err
			}
			return adapter.Descriptor(), nil
		}},
		{name: "analyzer", role: integration.RoleAnalyzer, new: func(id integration.ComponentID) (integration.Descriptor, error) {
			adapter, err := NewIntegrationAnalyzer(id, &adapterTestLegacyAnalyzer{})
			if err != nil {
				return integration.Descriptor{}, err
			}
			return adapter.Descriptor(), nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor, err := test.new("component.test")
			if err != nil {
				t.Fatal(err)
			}
			want := integration.Descriptor{
				ID: "component.test", Role: test.role, Name: "component.test",
				ProtocolVersion: integration.ProtocolVersion,
			}
			if !reflect.DeepEqual(descriptor, want) {
				t.Fatalf("descriptor mismatch:\nwant: %#v\n got: %#v", want, descriptor)
			}
			if _, err := test.new("  "); err == nil || !strings.Contains(err.Error(), "component id is empty") {
				t.Fatalf("empty ID error = %v", err)
			}
		})
	}

	var nilCollector *adapterTestLegacyCollectorPointer
	if _, err := NewIntegrationConnector("nil", nilCollector); err == nil || !strings.Contains(err.Error(), "collector is nil") {
		t.Fatalf("typed-nil collector error = %v", err)
	}
	var nilConnector *adapterTestConnector
	if _, err := NewLegacyCollector([]integration.Connector{nilConnector}); err == nil || !strings.Contains(err.Error(), "component is nil") {
		t.Fatalf("typed-nil connector error = %v", err)
	}
	wrongRole := &adapterTestConnector{descriptor: adapterTestDescriptor("wrong", integration.RoleAnalyzer)}
	if _, err := NewLegacyCollector([]integration.Connector{wrongRole}); err == nil || !strings.Contains(err.Error(), `want "connector"`) {
		t.Fatalf("wrong role error = %v", err)
	}
	duplicate := &adapterTestConnector{descriptor: adapterTestDescriptor("duplicate", integration.RoleConnector)}
	if _, err := NewLegacyCollector([]integration.Connector{duplicate, duplicate}); err == nil || !strings.Contains(err.Error(), "duplicate component id") {
		t.Fatalf("duplicate ID error = %v", err)
	}
	legacyValidation := []struct {
		name string
		new  func(integration.Descriptor) error
	}{
		{name: "connector", new: func(descriptor integration.Descriptor) error {
			_, err := NewLegacyCollector([]integration.Connector{&adapterTestConnector{descriptor: descriptor}})
			return err
		}},
		{name: "enricher", new: func(descriptor integration.Descriptor) error {
			_, err := NewLegacyEnricher([]integration.Enricher{&adapterTestEnricher{descriptor: descriptor}})
			return err
		}},
		{name: "analyzer", new: func(descriptor integration.Descriptor) error {
			_, err := NewLegacyAnalyzer(&adapterTestAnalyzer{descriptor: descriptor})
			return err
		}},
	}
	for _, test := range legacyValidation {
		t.Run("legacy "+test.name+" invalid descriptor", func(t *testing.T) {
			if err := test.new(integration.Descriptor{}); err == nil || !strings.Contains(err.Error(), "component id is empty") {
				t.Fatalf("invalid descriptor error = %v", err)
			}
		})
	}
}

func TestLegacyMessageObservationRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, time.October, 6, 9, 8, 7, 0, time.FixedZone("test", 8*60*60))
	tests := []struct {
		name    string
		message Message
	}{
		{name: "Feishu message", message: Message{
			ID: "message-1", ChatID: "chat-1", ChatName: "Bridge", ChatType: "group", ThreadID: "thread-1",
			SenderID: "user-1", SenderName: "Ada", SenderType: "user", MessageType: "text",
			Content: "ship it", URL: "https://example.test/message/1", CreatedAt: createdAt, Deleted: true,
			Mentions: []Mention{{ID: "user-2", Key: "@_user_2", Name: "Grace"}},
			Links: []model.ProgressLink{{Kind: "change", ExternalID: "change-1", Title: "Ship",
				URL: "https://example.test/change/1", State: "merged", DedupeKey: "change:1"}},
		}},
		{name: "local agent turn", message: Message{
			ID: "turn-1", SenderName: "Codex", Content: "implemented", CreatedAt: createdAt,
			SourceKind: "codex-conversation", SessionID: "session-1", TurnID: "turn-raw", CWD: "/workspace",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := observationFromLegacyMessage(test.message, "bridge.connector")
			if observation.Ref.ExternalID != test.message.ID || observation.Ref.ConnectorID != "bridge.connector" {
				t.Fatalf("identity changed: %+v", observation.Ref)
			}
			want := test.message
			want.ConnectorID = "bridge.connector"
			if got := messageFromObservation(observation); !reflect.DeepEqual(got, want) {
				t.Fatalf("round trip mismatch:\nwant: %#v\n got: %#v", test.message, got)
			}
		})
	}
}

func TestIntegrationConnectorMapsCollectionAndCandidateRefs(t *testing.T) {
	ctxKey := adapterTestContextKey{}
	ctx := context.WithValue(context.Background(), ctxKey, "present")
	start := time.Date(2026, time.October, 6, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	messages := []Message{{ID: "one", ChatID: "keep"}, {ID: "two", ChatID: "exclude"}}
	collector := &adapterTestLegacyCollector{collection: Collection{
		Messages: messages, Candidates: []Message{messages[1], {ID: "not-observed"}}, Seen: 7,
	}}
	adapter, err := NewIntegrationConnector("legacy.chat", collector)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.Collect(ctx, integration.CollectRequest{
		WindowStart: start, WindowEnd: end, Settings: []byte(`{"excluded_chat_ids":["exclude"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if collector.ctx.Value(ctxKey) != "present" || !collector.start.Equal(start) || !collector.end.Equal(end) || !reflect.DeepEqual(collector.excluded, []string{"exclude"}) {
		t.Fatalf("collector request not preserved: %+v", collector)
	}
	wantRef := integration.ObservationRef{ConnectorID: "legacy.chat", ExternalID: "two"}
	if len(got.Observations) != 2 || got.Seen != 7 || !reflect.DeepEqual(got.CandidateRefs, []integration.ObservationRef{wantRef}) || !reflect.DeepEqual(got.AckRefs, []integration.ObservationRef{wantRef}) {
		t.Fatalf("unexpected collection: %+v", got)
	}
}

func TestLegacyCollectorMergesConnectorsFiltersAndPropagatesErrors(t *testing.T) {
	ctxKey := adapterTestContextKey{}
	ctx := context.WithValue(context.Background(), ctxKey, "present")
	one := &adapterTestConnector{descriptor: adapterTestDescriptor("one", integration.RoleConnector), collection: integration.Collection{
		Observations: []integration.Observation{
			{Ref: integration.ObservationRef{ConnectorID: "one", ExternalID: "same-id"}, Kind: "source-one", ContainerID: "keep", Content: "first"},
			{Ref: integration.ObservationRef{ConnectorID: "one", ExternalID: "excluded"}, Kind: "source-one", ContainerID: "skip", Content: "hidden"},
		},
		CandidateRefs: []integration.ObservationRef{
			{ConnectorID: "one", ExternalID: "same-id"},
			{ConnectorID: "one", ExternalID: "excluded"},
		},
		Seen: 3,
	}}
	two := &adapterTestConnector{descriptor: adapterTestDescriptor("two", integration.RoleConnector), collection: integration.Collection{
		Observations:  []integration.Observation{{Ref: integration.ObservationRef{ConnectorID: "two", ExternalID: "second-id"}, Kind: "source-two", ContainerID: "keep", Content: "second"}},
		CandidateRefs: []integration.ObservationRef{{ConnectorID: "two", ExternalID: "second-id"}}, Seen: 5,
	}}
	collector, err := NewLegacyCollector([]integration.Connector{one, two})
	if err != nil {
		t.Fatal(err)
	}
	got, err := collector.Collect(ctx, time.Time{}, time.Unix(1, 0), []string{"skip"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Seen != 8 || len(got.Messages) != 2 || len(got.Candidates) != 2 {
		t.Fatalf("unexpected merged collection: %+v", got)
	}
	if got.Messages[0].ID != "same-id" || got.Messages[1].ID != "second-id" {
		t.Fatalf("external IDs were rewritten: %+v", got.Messages)
	}
	if got.Messages[0].ConnectorID != "one" || got.Messages[1].ConnectorID != "two" ||
		got.Messages[0].SourceKind != "source-one" || got.Messages[1].SourceKind != "source-two" {
		t.Fatalf("connector/source identity not retained: %+v", got.Messages)
	}
	if one.request.Limit != MessageLimit || one.ctx.Value(ctxKey) != "present" || two.ctx.Value(ctxKey) != "present" {
		t.Fatalf("request/context not propagated: one=%+v two=%+v", one.request, two.request)
	}

	sentinel := errors.New("collect failed")
	failing := &adapterTestConnector{descriptor: adapterTestDescriptor("failure", integration.RoleConnector), err: sentinel}
	after := &adapterTestConnector{descriptor: adapterTestDescriptor("after", integration.RoleConnector)}
	collector, err = NewLegacyCollector([]integration.Connector{one, failing, after})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = collector.Collect(ctx, time.Time{}, time.Time{}, nil); !errors.Is(err, sentinel) {
		t.Fatalf("collect error = %v", err)
	}
	if after.calls != 0 {
		t.Fatalf("connector after failure was called %d times", after.calls)
	}
}

func TestLegacyCollectorRejectsForeignScopedRefs(t *testing.T) {
	own := integration.ObservationRef{ConnectorID: "one", ExternalID: "message-1"}
	foreign := integration.ObservationRef{ConnectorID: "foreign", ExternalID: "message-1"}
	tests := []struct {
		name       string
		collection integration.Collection
		wantField  string
	}{
		{name: "observation", collection: integration.Collection{Observations: []integration.Observation{{Ref: foreign}}}, wantField: "observations[0]"},
		{name: "candidate", collection: integration.Collection{Observations: []integration.Observation{{Ref: own}}, CandidateRefs: []integration.ObservationRef{foreign}}, wantField: "candidate_refs[0]"},
		{name: "ack", collection: integration.Collection{Observations: []integration.Observation{{Ref: own}}, AckRefs: []integration.ObservationRef{foreign}}, wantField: "ack_refs[0]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connector := &adapterTestConnector{descriptor: adapterTestDescriptor("one", integration.RoleConnector), collection: test.collection}
			adapter, err := NewLegacyCollector([]integration.Connector{connector})
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.Collect(context.Background(), time.Time{}, time.Time{}, nil)
			if err == nil || !strings.Contains(err.Error(), test.wantField) || !strings.Contains(err.Error(), `connector_id "foreign"`) {
				t.Fatalf("foreign scope error = %v", err)
			}
		})
	}
}

func TestLegacyCollectorRejectsInvalidCollectionReferences(t *testing.T) {
	ref := integration.ObservationRef{ConnectorID: "one", ExternalID: "message-1"}
	missing := integration.ObservationRef{ConnectorID: "one", ExternalID: "missing"}
	tests := []struct {
		name       string
		collection integration.Collection
		want       string
	}{
		{
			name:       "duplicate observation ref",
			collection: integration.Collection{Observations: []integration.Observation{{Ref: ref}, {Ref: ref}}},
			want:       "duplicate observations[1]",
		},
		{
			name:       "candidate absent from observations",
			collection: integration.Collection{Observations: []integration.Observation{{Ref: ref}}, CandidateRefs: []integration.ObservationRef{missing}},
			want:       "candidate_refs[0] not present in observations",
		},
		{
			name:       "ack absent from observations",
			collection: integration.Collection{Observations: []integration.Observation{{Ref: ref}}, AckRefs: []integration.ObservationRef{missing}},
			want:       "ack_refs[0] not present in observations",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connector := &adapterTestConnector{descriptor: adapterTestDescriptor("one", integration.RoleConnector), collection: test.collection}
			adapter, err := NewLegacyCollector([]integration.Connector{connector})
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.Collect(context.Background(), time.Time{}, time.Time{}, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid collection error = %v", err)
			}
		})
	}
}

func TestLegacyCollectorRejectsCrossConnectorExternalIDCollision(t *testing.T) {
	one := &adapterTestConnector{descriptor: adapterTestDescriptor("one", integration.RoleConnector), collection: integration.Collection{
		Observations: []integration.Observation{{Ref: integration.ObservationRef{ConnectorID: "one", ExternalID: "shared"}}},
	}}
	two := &adapterTestConnector{descriptor: adapterTestDescriptor("two", integration.RoleConnector), collection: integration.Collection{
		Observations: []integration.Observation{{Ref: integration.ObservationRef{ConnectorID: "two", ExternalID: "shared"}}},
	}}
	after := &adapterTestConnector{descriptor: adapterTestDescriptor("after", integration.RoleConnector)}
	adapter, err := NewLegacyCollector([]integration.Connector{one, two, after})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Collect(context.Background(), time.Time{}, time.Time{}, nil)
	if err == nil || !strings.Contains(err.Error(), `connectors "one" and "two"`) || !strings.Contains(err.Error(), `external_id "shared"`) {
		t.Fatalf("duplicate external ID error = %v", err)
	}
	if after.calls != 0 {
		t.Fatalf("connector after collision was called %d times", after.calls)
	}
}

func TestLegacyEnricherSkipsFatalErrorsAndContinues(t *testing.T) {
	var logs bytes.Buffer
	originalOutput, originalFlags, originalPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
		log.SetPrefix(originalPrefix)
	}()

	const secret = "secret-token-must-not-leak"
	sentinel := errors.New(secret)
	failed := &adapterTestEnricher{
		descriptor: adapterTestDescriptor("failed", integration.RoleEnricher),
		result:     integration.EnrichmentResult{Resources: []integration.Resource{{Kind: "partial"}}}, err: sentinel,
	}
	success := &adapterTestEnricher{
		descriptor: adapterTestDescriptor("success", integration.RoleEnricher),
		result: integration.EnrichmentResult{
			Resources: []integration.Resource{{
				Ref:  integration.ResourceRef{EnricherID: "success", ExternalID: "resource-1"},
				Kind: "doc", URL: "https://example.test/doc", Title: "Design", State: "ready",
				Excerpt: "details", DedupeKey: "doc:1", Accessible: true,
			}},
			Warnings: []integration.Warning{{Code: "stale", Message: "cached"}},
		},
	}
	adapter, err := NewLegacyEnricher([]integration.Enricher{failed, success})
	if err != nil {
		t.Fatal(err)
	}
	got := adapter.Enrich(context.Background(), []Message{{ID: "message-1", Content: "see doc"}})
	want := []Resource{{
		Kind: "doc", ExternalID: "resource-1", URL: "https://example.test/doc", Title: "Design",
		State: "ready", Excerpt: "details", DedupeKey: "doc:1", Accessible: true,
	}}
	if !reflect.DeepEqual(got, want) || failed.calls != 1 || success.calls != 1 {
		t.Fatalf("unexpected resources/calls: got=%#v failed=%d success=%d", got, failed.calls, success.calls)
	}
	if output := logs.String(); strings.Contains(output, secret) || !strings.Contains(output, `enricher "failed" failed; result discarded`) {
		t.Fatalf("unsafe or unexpected enricher log: %q", output)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	contextFailure := &adapterTestEnricher{descriptor: adapterTestDescriptor("context", integration.RoleEnricher), err: context.Canceled}
	unused := &adapterTestEnricher{descriptor: adapterTestDescriptor("unused", integration.RoleEnricher)}
	adapter, err = NewLegacyEnricher([]integration.Enricher{contextFailure, unused})
	if err != nil {
		t.Fatal(err)
	}
	if got = adapter.Enrich(canceled, nil); len(got) != 0 || contextFailure.calls != 0 || unused.calls != 0 {
		t.Fatalf("canceled enrichment invoked components: resources=%#v first=%d second=%d", got, contextFailure.calls, unused.calls)
	}
}

func TestIntegrationEnricherMapsResourcesAndObservationRefs(t *testing.T) {
	ctxKey := adapterTestContextKey{}
	ctx := context.WithValue(context.Background(), ctxKey, "present")
	legacy := &adapterTestCapturingLegacyEnricher{resources: []Resource{{
		Kind: "doc", ExternalID: "doc-1", URL: "https://example.test/doc/1", Title: "Spec",
		State: "ready", Excerpt: "body", DedupeKey: "doc:1", Accessible: true,
		ObservationRefs: []ResourceObservationRef{{ConnectorID: "chat", ExternalID: "message-1"}},
	}}}
	adapter, err := NewIntegrationEnricher("legacy.docs", legacy)
	if err != nil {
		t.Fatal(err)
	}
	observation := integration.Observation{
		Ref:     integration.ObservationRef{ConnectorID: "chat", ExternalID: "message-1"},
		Content: "the URL is deliberately absent",
	}
	got, err := adapter.Enrich(ctx, integration.EnrichRequest{Observations: []integration.Observation{observation}})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ctx.Value(ctxKey) != "present" || len(legacy.messages) != 1 || legacy.messages[0].ID != "message-1" {
		t.Fatalf("context/messages not propagated: ctx=%v messages=%#v", legacy.ctx, legacy.messages)
	}
	want := integration.Resource{
		Ref:             integration.ResourceRef{EnricherID: "legacy.docs", ExternalID: "doc-1"},
		ObservationRefs: []integration.ObservationRef{observation.Ref}, Kind: "doc",
		URL: "https://example.test/doc/1", Title: "Spec", State: "ready", Excerpt: "body",
		DedupeKey: "doc:1", Accessible: true,
	}
	if len(got.Resources) != 1 || !reflect.DeepEqual(got.Resources[0], want) {
		t.Fatalf("resource mismatch:\nwant: %#v\n got: %#v", want, got.Resources)
	}
	if roundTrip := resourceToLegacy(got.Resources[0]); !reflect.DeepEqual(roundTrip, legacy.resources[0]) {
		t.Fatalf("resource round trip mismatch:\nwant: %#v\n got: %#v", legacy.resources[0], roundTrip)
	}
}

func TestIntegrationEnricherDoesNotAssociateEmptyURLWithoutSidecar(t *testing.T) {
	legacy := &adapterTestCapturingLegacyEnricher{resources: []Resource{{
		Kind: "opaque", ExternalID: "resource-1", URL: "",
	}}}
	adapter, err := NewIntegrationEnricher("legacy.opaque", legacy)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.Enrich(context.Background(), integration.EnrichRequest{Observations: []integration.Observation{{
		Ref: integration.ObservationRef{ConnectorID: "chat", ExternalID: "message-1"}, Content: "any content",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Resources) != 1 {
		t.Fatalf("resources = %#v", got.Resources)
	}
	if got.Resources[0].ObservationRefs != nil {
		t.Fatalf("empty URL resource was associated with every observation: %#v", got.Resources[0].ObservationRefs)
	}
}

func TestResourceSidecarsAreNotSerialized(t *testing.T) {
	payload, err := json.Marshal(struct {
		Message  Message  `json:"message"`
		Resource Resource `json:"resource"`
	}{
		Message:  Message{ID: "message-1", ConnectorID: "connector-1"},
		Resource: Resource{ObservationRefs: []ResourceObservationRef{{ConnectorID: "connector-1", ExternalID: "message-1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "connector-1") || strings.Contains(string(payload), "observation") {
		t.Fatalf("sidecar leaked into JSON: %s", payload)
	}
}

func TestResourcesForMessagePrefersExactObservationRefs(t *testing.T) {
	message := Message{ID: "message-1", ConnectorID: "chat-a", SourceKind: "message", Content: "no linked URL here"}
	resources := []Resource{
		{ExternalID: "exact", URL: "https://example.test/exact", ObservationRefs: []ResourceObservationRef{{ConnectorID: "chat-a", ExternalID: "message-1"}}},
		{ExternalID: "foreign", URL: "https://example.test/foreign", ObservationRefs: []ResourceObservationRef{{ConnectorID: "chat-b", ExternalID: "message-1"}}},
		{ExternalID: "legacy", URL: "https://example.test/legacy"},
	}
	message.Content += " https://example.test/foreign https://example.test/legacy"
	got := resourcesForMessage(message, resources)
	if len(got) != 2 || got[0].ExternalID != "exact" || got[1].ExternalID != "legacy" {
		t.Fatalf("resource association mismatch: %#v", got)
	}
	builtin := Message{ID: "turn-1", SourceKind: "codex-conversation"}
	got = resourcesForMessage(builtin, []Resource{{ExternalID: "turn-resource", ObservationRefs: []ResourceObservationRef{{ConnectorID: "codex-conversation", ExternalID: "turn-1"}}}})
	if len(got) != 1 || got[0].ExternalID != "turn-resource" {
		t.Fatalf("normalized source fallback mismatch: %#v", got)
	}
}

func TestAnalysisBatchAndSnapshotRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, time.October, 6, 2, 3, 4, 0, time.UTC)
	projectID := "project-1"
	completedAt := createdAt.Add(3 * time.Hour)
	message := Message{ID: "message-1", ChatID: "chat-1", Content: "ready", CreatedAt: createdAt, SourceKind: "connector-1"}
	message.ConnectorID = "connector-1"
	input := AnalysisInput{
		WindowStart: createdAt.Add(-time.Hour), WindowEnd: createdAt, Messages: []Message{message}, Candidates: []Message{message},
		Snapshot: Snapshot{
			Demands: []*model.Demand{{
				ID: "demand-1", Title: "Bridge", Description: "Implement", Status: model.DemandStatusDone,
				Priority: model.DemandPriorityP1, ProjectID: &projectID, ProjectHint: "Adapters", NextAction: "Merge",
				BlockedReason: "none", Sources: []model.Source{{Kind: "message", ExternalID: "message-1",
					URL: "https://example.test/message/1", Excerpt: "ready", MessageTime: createdAt}},
				Progress:  []model.Progress{{Text: "implemented", Links: []model.ProgressLink{{Kind: "change", URL: "https://example.test/change"}}, CreatedAt: createdAt.Add(time.Hour)}},
				CreatedAt: createdAt.Add(-time.Hour), UpdatedAt: createdAt.Add(2 * time.Hour), CompletedAt: &completedAt,
			}},
			Projects: []*model.Project{{ID: projectID, Name: "Integrations", Description: "Adapters", Color: "blue", Status: model.ProjectStatusActive, CreatedAt: createdAt, UpdatedAt: createdAt.Add(time.Hour)}},
		},
		Resources: []Resource{{Kind: "doc", ExternalID: "doc-1", URL: "https://example.test/doc", Title: "Spec", State: "ready", Excerpt: "body", DedupeKey: "doc:1", Accessible: true}},
	}
	batch := analysisBatchFromLegacy(input)
	if len(batch.Candidates) != 1 || batch.Candidates[0].Ref != batch.Observations[0].Ref {
		t.Fatalf("candidate did not select corresponding observation: %+v", batch)
	}
	if len(batch.Snapshot.Demands) != 1 || len(batch.Snapshot.Demands[0].Progress) != 1 || batch.Snapshot.Demands[0].Progress[0].Text != "implemented" {
		t.Fatalf("snapshot DTO incomplete: %+v", batch.Snapshot)
	}
	roundTrip := analysisInputFromIntegration(batch)
	if !reflect.DeepEqual(roundTrip, input) {
		t.Fatalf("analysis input mismatch:\nwant: %#v\n got: %#v", input, roundTrip)
	}
}

func TestIntegrationAnalyzerMapsProposalsUsageAndRoute(t *testing.T) {
	observation := integration.Observation{
		Ref:     integration.ObservationRef{ConnectorID: "chat", ExternalID: "message-1"},
		Content: "ready", Links: []integration.Link{{Kind: "change", URL: "https://example.test/change"}},
	}
	contextCollision := observation
	contextCollision.Ref.ConnectorID = "other-chat"
	legacyResult := Result{
		NewDemands:        []NewDemand{{Title: "Ship", Description: "Release", NextAction: "Deploy", ProjectHint: "Delivery", Sources: []EvidenceRef{{ExternalID: "message-1", Excerpt: "ready"}}}},
		ProgressUpdates:   []ProgressUpdate{{DemandID: "demand-1", Text: "ready", DedupeKey: "keep-this-key", Source: EvidenceRef{ExternalID: "message-1", Excerpt: "ready"}, Confidence: .9, Anchors: []string{"release"}}},
		Reviews:           []Review{{SuggestedDemandID: "demand-1", ProgressText: "maybe", ProgressDedupeKey: "review-key", Source: EvidenceRef{ExternalID: "message-1", Excerpt: "ready"}, Confidence: .5, Rationale: "ambiguous"}},
		SkippedMessageIDs: []string{"message-1"}, MissingContextIDs: []string{"message-1"},
		InputTokens: 10, CachedInputTokens: 3, OutputTokens: 4, AnalyzerRoute: "primary",
		AnalyzerProfileID: "profile", AnalyzerProfileFingerprint: "fingerprint", AnalyzerHealthy: true,
		FallbackUsed: true, PrimaryError: "timeout",
	}
	legacy := &adapterTestLegacyAnalyzer{result: legacyResult}
	adapter, err := NewIntegrationAnalyzer("legacy.analyzer", legacy)
	if err != nil {
		t.Fatal(err)
	}
	ctxKey := adapterTestContextKey{}
	ctx := context.WithValue(context.Background(), ctxKey, "present")
	got, err := adapter.Analyze(ctx, integration.AnalysisBatch{Observations: []integration.Observation{contextCollision, observation}, Candidates: []integration.Observation{observation}})
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.batches) != 1 || len(legacy.batches[0].Candidates) != 1 || legacy.batches[0].Candidates[0].ID != "message-1" {
		t.Fatalf("legacy input mismatch: %#v", legacy.batches)
	}
	if legacy.ctx.Value(ctxKey) != "present" {
		t.Fatalf("analyzer context not propagated: %v", legacy.ctx)
	}
	if got.Progress[0].DedupeKey != "keep-this-key" || got.Progress[0].Evidence.Observation != observation.Ref || !reflect.DeepEqual(got.Progress[0].Links, observation.Links) {
		t.Fatalf("progress proposal mismatch: %+v", got.Progress[0])
	}
	if got.Demands[0].Evidence[0].Observation != observation.Ref || got.Skipped[0] != observation.Ref || got.MissingContext[0] != observation.Ref {
		t.Fatalf("candidate refs did not win over context ExternalID collision: %+v", got)
	}
	if got.Usage.InputTokens != 10 || got.Usage.CachedInputTokens != 3 || got.Usage.OutputTokens != 4 || got.Route.Route != "primary" || !got.Route.FallbackUsed || got.Route.PrimaryError != "timeout" {
		t.Fatalf("usage/route mismatch: %+v %+v", got.Usage, got.Route)
	}
	if roundTrip := analysisResultToLegacy(got); !reflect.DeepEqual(roundTrip, legacyResult) {
		t.Fatalf("result round trip mismatch:\nwant: %#v\n got: %#v", legacyResult, roundTrip)
	}
}

func TestIntegrationAnalyzerRejectsAmbiguousCandidateExternalIDBeforeLegacyCall(t *testing.T) {
	legacy := &adapterTestLegacyAnalyzer{}
	adapter, err := NewIntegrationAnalyzer("legacy.analyzer", legacy)
	if err != nil {
		t.Fatal(err)
	}
	candidates := []integration.Observation{
		{Ref: integration.ObservationRef{ConnectorID: "one", ExternalID: "shared"}},
		{Ref: integration.ObservationRef{ConnectorID: "two", ExternalID: "shared"}},
	}
	_, err = adapter.Analyze(context.Background(), integration.AnalysisBatch{Observations: candidates, Candidates: candidates})
	if err == nil || !strings.Contains(err.Error(), `external_id "shared" is ambiguous`) {
		t.Fatalf("ambiguous candidate error = %v", err)
	}
	if len(legacy.batches) != 0 {
		t.Fatalf("legacy analyzer called with ambiguous candidates: %#v", legacy.batches)
	}
}

func TestIntegrationAnalyzerRejectsCandidateNotPresentInObservations(t *testing.T) {
	legacy := &adapterTestLegacyAnalyzer{}
	adapter, err := NewIntegrationAnalyzer("legacy.analyzer", legacy)
	if err != nil {
		t.Fatal(err)
	}
	candidate := integration.Observation{Ref: integration.ObservationRef{ConnectorID: "chat", ExternalID: "orphan"}}
	_, err = adapter.Analyze(context.Background(), integration.AnalysisBatch{Candidates: []integration.Observation{candidate}})
	if err == nil || !strings.Contains(err.Error(), "candidates[0]") || !strings.Contains(err.Error(), "not present in observations") {
		t.Fatalf("orphan candidate error = %v", err)
	}
	if len(legacy.batches) != 0 {
		t.Fatalf("legacy analyzer called with orphan candidate: %#v", legacy.batches)
	}
}

func TestIntegrationAnalyzerRejectsDuplicateCandidateRef(t *testing.T) {
	legacy := &adapterTestLegacyAnalyzer{}
	adapter, err := NewIntegrationAnalyzer("legacy.analyzer", legacy)
	if err != nil {
		t.Fatal(err)
	}
	candidate := integration.Observation{Ref: integration.ObservationRef{ConnectorID: "chat", ExternalID: "duplicate"}}
	_, err = adapter.Analyze(context.Background(), integration.AnalysisBatch{
		Observations: []integration.Observation{candidate}, Candidates: []integration.Observation{candidate, candidate},
	})
	if err == nil || !strings.Contains(err.Error(), "candidates[1] duplicates ref") {
		t.Fatalf("duplicate candidate error = %v", err)
	}
	if len(legacy.batches) != 0 {
		t.Fatalf("legacy analyzer called with duplicate candidate: %#v", legacy.batches)
	}
}

func TestIntegrationAnalyzerRejectsNonCandidateResultReferences(t *testing.T) {
	candidate := integration.Observation{Ref: integration.ObservationRef{ConnectorID: "chat", ExternalID: "candidate"}}
	contextOnly := integration.Observation{Ref: integration.ObservationRef{ConnectorID: "chat", ExternalID: "context"}}
	tests := []struct {
		name   string
		result Result
		want   string
	}{
		{name: "proposal evidence context observation", result: Result{NewDemands: []NewDemand{{Sources: []EvidenceRef{{ExternalID: "context"}}}}}, want: "new_demands[0].sources[0]"},
		{name: "proposal evidence unknown observation", result: Result{ProgressUpdates: []ProgressUpdate{{Source: EvidenceRef{ExternalID: "unknown"}}}}, want: "progress_updates[0].source"},
		{name: "skipped context observation", result: Result{SkippedMessageIDs: []string{"context"}}, want: "skipped_message_ids[0]"},
		{name: "missing unknown observation", result: Result{MissingContextIDs: []string{"unknown"}}, want: "missing_context_message_ids[0]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			legacy := &adapterTestLegacyAnalyzer{result: test.result}
			adapter, err := NewIntegrationAnalyzer("legacy.analyzer", legacy)
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.Analyze(context.Background(), integration.AnalysisBatch{
				Observations: []integration.Observation{candidate, contextOnly}, Candidates: []integration.Observation{candidate},
			})
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "non-candidate external_id") {
				t.Fatalf("non-candidate result error = %v", err)
			}
		})
	}
}

func TestLegacyAnalyzerAnalyzesEachBatchAndUsesWorkerMergeSemantics(t *testing.T) {
	sentinel := errors.New("second batch failed")
	protocolAnalyzer := &adapterTestAnalyzer{
		descriptor: adapterTestDescriptor("neutral.analyzer", integration.RoleAnalyzer),
		results: []integration.AnalysisResult{
			{
				Demands: []integration.DemandProposal{{Title: "one"}}, Skipped: []integration.ObservationRef{{ExternalID: "one"}},
				Usage: integration.Usage{InputTokens: 10, CachedInputTokens: 2, OutputTokens: 3},
				Route: integration.AnalyzerRoute{Route: "primary", ProfileID: "profile-1", ProfileFingerprint: "fp-1", Healthy: true, PrimaryError: "first"},
			},
			{
				Progress: []integration.ProgressProposal{{DemandID: "demand-1", Text: "two", DedupeKey: "two-key"}}, MissingContext: []integration.ObservationRef{{ExternalID: "two"}},
				Usage: integration.Usage{InputTokens: 20, CachedInputTokens: 4, OutputTokens: 6},
				Route: integration.AnalyzerRoute{ProfileID: "profile-2", ProfileFingerprint: "fp-2", Healthy: false, FallbackUsed: true, PrimaryError: "second"},
			},
		},
		errors: []error{nil, sentinel},
	}
	adapter, err := NewLegacyAnalyzer(protocolAnalyzer)
	if err != nil {
		t.Fatal(err)
	}
	batches := []AnalysisInput{
		{Messages: []Message{{ID: "one", SourceKind: "chat"}}, Candidates: []Message{{ID: "one", SourceKind: "chat"}}},
		{Messages: []Message{{ID: "two", SourceKind: "chat"}}, Candidates: []Message{{ID: "two", SourceKind: "chat"}}},
	}
	got, err := adapter.Analyze(context.Background(), batches)
	if !errors.Is(err, sentinel) {
		t.Fatalf("analyze error = %v", err)
	}
	if len(protocolAnalyzer.batches) != 2 || len(protocolAnalyzer.batches[0].Candidates) != 1 || protocolAnalyzer.batches[0].Candidates[0].Ref.ExternalID != "one" {
		t.Fatalf("protocol batches mismatch: %#v", protocolAnalyzer.batches)
	}
	if len(got.NewDemands) != 1 || len(got.ProgressUpdates) != 1 || !reflect.DeepEqual(got.SkippedMessageIDs, []string{"one"}) || !reflect.DeepEqual(got.MissingContextIDs, []string{"two"}) {
		t.Fatalf("partial results not merged: %+v", got)
	}
	if got.InputTokens != 30 || got.CachedInputTokens != 6 || got.OutputTokens != 9 {
		t.Fatalf("usage not summed: %+v", got)
	}
	if got.AnalyzerRoute != "primary" || got.AnalyzerProfileID != "profile-2" || got.AnalyzerProfileFingerprint != "fp-2" || got.AnalyzerHealthy || !got.FallbackUsed || got.PrimaryError != "second" {
		t.Fatalf("route merge mismatch: %+v", got)
	}
}

type adapterTestContextKey struct{}

type adapterTestLegacyCollector struct {
	collection Collection
	err        error
	ctx        context.Context
	start      time.Time
	end        time.Time
	excluded   []string
}

func (c *adapterTestLegacyCollector) Collect(ctx context.Context, start, end time.Time, excluded []string) (Collection, error) {
	c.ctx, c.start, c.end = ctx, start, end
	c.excluded = append([]string(nil), excluded...)
	return c.collection, c.err
}

type adapterTestLegacyCollectorPointer struct{}

func (*adapterTestLegacyCollectorPointer) Collect(context.Context, time.Time, time.Time, []string) (Collection, error) {
	return Collection{}, nil
}

type adapterTestLegacyEnricher struct{}

func (adapterTestLegacyEnricher) Enrich(context.Context, []Message) []Resource { return nil }

type adapterTestCapturingLegacyEnricher struct {
	resources []Resource
	ctx       context.Context
	messages  []Message
}

func (e *adapterTestCapturingLegacyEnricher) Enrich(ctx context.Context, messages []Message) []Resource {
	e.ctx = ctx
	e.messages = append([]Message(nil), messages...)
	return append([]Resource(nil), e.resources...)
}

type adapterTestLegacyAnalyzer struct {
	result  Result
	err     error
	ctx     context.Context
	batches []AnalysisInput
}

func (a *adapterTestLegacyAnalyzer) Analyze(ctx context.Context, batches []AnalysisInput) (Result, error) {
	a.ctx = ctx
	a.batches = append(a.batches, batches...)
	return a.result, a.err
}

type adapterTestConnector struct {
	descriptor integration.Descriptor
	collection integration.Collection
	err        error
	calls      int
	ctx        context.Context
	request    integration.CollectRequest
}

func (c *adapterTestConnector) Descriptor() integration.Descriptor { return c.descriptor }

func (c *adapterTestConnector) Collect(ctx context.Context, request integration.CollectRequest) (integration.Collection, error) {
	c.calls++
	c.ctx, c.request = ctx, request
	return c.collection, c.err
}

type adapterTestEnricher struct {
	descriptor integration.Descriptor
	result     integration.EnrichmentResult
	err        error
	calls      int
}

func (e *adapterTestEnricher) Descriptor() integration.Descriptor { return e.descriptor }

func (e *adapterTestEnricher) Enrich(context.Context, integration.EnrichRequest) (integration.EnrichmentResult, error) {
	e.calls++
	return e.result, e.err
}

type adapterTestAnalyzer struct {
	descriptor integration.Descriptor
	results    []integration.AnalysisResult
	errors     []error
	batches    []integration.AnalysisBatch
}

func (a *adapterTestAnalyzer) Descriptor() integration.Descriptor { return a.descriptor }

func (a *adapterTestAnalyzer) Analyze(_ context.Context, batch integration.AnalysisBatch) (integration.AnalysisResult, error) {
	index := len(a.batches)
	a.batches = append(a.batches, batch)
	var result integration.AnalysisResult
	if index < len(a.results) {
		result = a.results[index]
	}
	var err error
	if index < len(a.errors) {
		err = a.errors[index]
	}
	return result, err
}

func adapterTestDescriptor(id integration.ComponentID, role integration.Role) integration.Descriptor {
	return integration.Descriptor{ID: id, Role: role, Name: string(id), ProtocolVersion: integration.ProtocolVersion}
}
