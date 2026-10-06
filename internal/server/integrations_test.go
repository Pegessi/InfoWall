package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/feishuingest"
	"github.com/infowall/infowall/internal/integration"
	"github.com/infowall/infowall/internal/model"
)

type recordingLegacyCollector struct {
	calls int
}

func (c *recordingLegacyCollector) Collect(context.Context, time.Time, time.Time, []string) (feishuingest.Collection, error) {
	c.calls++
	return feishuingest.Collection{}, nil
}

type recordingLegacyAnalyzer struct {
	calls int
}

func (a *recordingLegacyAnalyzer) Analyze(context.Context, []feishuingest.AnalysisInput) (feishuingest.Result, error) {
	a.calls++
	return feishuingest.Result{}, nil
}

func TestNilIntegrationsPreserveLegacyCompositionAndInjection(t *testing.T) {
	collector := &recordingLegacyCollector{}
	analyzer := &recordingLegacyAnalyzer{}
	runner := &fakeLarkRunner{}
	srv, err := New(context.Background(), Config{
		DBPath:                   filepath.Join(t.TempDir(), "wall.db"),
		DisableBackgroundWorkers: true,
		LarkRunner:               runner,
		IngestionCollector:       collector,
		IngestionAnalyzer:        analyzer,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if srv.feishuClient == nil || srv.feishuClient.Runner != runner || srv.syncWorker == nil {
		t.Fatalf("legacy Feishu control was not wired from the injected runner: client=%#v worker=%#v", srv.feishuClient, srv.syncWorker)
	}
	if srv.ingestWorker == nil || srv.ingestWorker.LocalCollector == nil {
		t.Fatalf("legacy ingestion worker = %#v", srv.ingestWorker)
	}
	if got := componentIDs(srv.integrations); strings.Join(got, ",") != "feishu.im,resources.command,analysis.claude-codex,feishu.doc" {
		t.Fatalf("default component IDs = %v", got)
	}
	if _, err := srv.ingestWorker.Collector.Collect(context.Background(), time.Time{}, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ingestWorker.Analyzer.Analyze(context.Background(), []feishuingest.AnalysisInput{{}}); err != nil {
		t.Fatal(err)
	}
	if collector.calls != 1 || analyzer.calls != 1 {
		t.Fatalf("legacy injections not used: collector=%d analyzer=%d", collector.calls, analyzer.calls)
	}
}

func TestDefaultCompositionDoesNotDiscoverCommandsDuringConstruction(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	srv, err := New(context.Background(), Config{
		DBPath:                   filepath.Join(t.TempDir(), "wall.db"),
		DisableBackgroundWorkers: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if srv.feishuClient == nil || srv.syncWorker == nil || srv.ingestWorker == nil {
		t.Fatalf("default composition was not built: client=%#v sync=%#v ingest=%#v", srv.feishuClient, srv.syncWorker, srv.ingestWorker)
	}
}

func TestLegacyTypedNilLarkRunnerFailsBeforeDatabaseOpen(t *testing.T) {
	var runner *fakeLarkRunner
	badPath := filepath.Join(t.TempDir(), "missing", "wall.db")
	_, err := New(context.Background(), Config{DBPath: badPath, LarkRunner: runner})
	if err == nil || !strings.Contains(err.Error(), "legacy LarkRunner") || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("New error = %v, want clear typed-nil LarkRunner error", err)
	}
	if strings.Contains(err.Error(), "open database") {
		t.Fatalf("database opened before typed-nil runner validation: %v", err)
	}
}

func TestExplicitEmptyIntegrationsRunCoreWithoutCommandDiscovery(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	components := integration.Components{}
	srv, err := New(context.Background(), Config{
		DBPath:       filepath.Join(t.TempDir(), "wall.db"),
		Integrations: &components,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if srv.feishuClient != nil || srv.syncWorker != nil || srv.ingestWorker != nil || srv.syncCancel != nil {
		t.Fatalf("explicit empty composition created adapters: client=%#v sync=%#v ingest=%#v cancel=%v",
			srv.feishuClient, srv.syncWorker, srv.ingestWorker, srv.syncCancel != nil)
	}
	var health map[string]any
	requestMuxJSON(t, http.MethodGet, "/api/health", nil, http.StatusOK, &health, srv.mux)
	if backgroundWorkersCapability(t, srv) {
		t.Fatal("pure Core composition advertised background workers")
	}
	var docBefore, docAfter map[string]any
	requestMuxJSON(t, http.MethodGet, "/api/integrations/feishu-doc", nil, http.StatusOK, &docBefore, srv.mux)
	assertUnavailable(t, srv, http.MethodPost, "/api/integrations/feishu-doc", map[string]any{"create": true})
	assertUnavailable(t, srv, http.MethodPost, "/api/integrations/feishu-doc/sync", nil)
	requestMuxJSON(t, http.MethodGet, "/api/integrations/feishu-doc", nil, http.StatusOK, &docAfter, srv.mux)
	if string(mustJSON(t, docAfter)) != string(mustJSON(t, docBefore)) {
		t.Fatalf("unavailable document writes mutated state: before=%v after=%v", docBefore, docAfter)
	}

	var ingestionBefore, ingestionAfter model.FeishuIngestionState
	requestMuxJSON(t, http.MethodGet, "/api/integrations/activity", nil, http.StatusOK, &ingestionBefore, srv.mux)
	var runs struct {
		Runs []model.FeishuIngestionRun `json:"runs"`
	}
	requestMuxJSON(t, http.MethodGet, "/api/integrations/activity/runs", nil, http.StatusOK, &runs, srv.mux)
	assertUnavailable(t, srv, http.MethodPatch, "/api/integrations/activity", map[string]any{"enabled": true})
	assertUnavailable(t, srv, http.MethodPost, "/api/integrations/activity/scan", nil)
	hook := model.ConversationHookEvent{
		ID: "unavailable-hook", Source: "codex", EventName: "UserPromptSubmit",
		SessionID: "session-1", Prompt: "must not persist", OccurredAt: time.Now().UTC(),
	}
	statsBefore, err := srv.store.StorageStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertUnavailable(t, srv, http.MethodPost, "/api/integrations/conversations/events", hook)
	statsAfter, err := srv.store.StorageStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if statsAfter.HookEvents != statsBefore.HookEvents {
		t.Fatalf("unavailable conversation event was persisted: before=%d after=%d", statsBefore.HookEvents, statsAfter.HookEvents)
	}
	requestMuxJSON(t, http.MethodGet, "/api/integrations/activity", nil, http.StatusOK, &ingestionAfter, srv.mux)
	if ingestionAfter.Enabled != ingestionBefore.Enabled || ingestionAfter.Requested != ingestionBefore.Requested {
		t.Fatalf("unavailable ingestion writes mutated state: before=%+v after=%+v", ingestionBefore, ingestionAfter)
	}
	// Disabling remains a store-only operation and therefore stays available.
	requestMuxJSON(t, http.MethodPatch, "/api/integrations/activity", map[string]any{"enabled": false}, http.StatusOK, &ingestionAfter, srv.mux)
	requestMuxJSON(t, http.MethodDelete, "/api/integrations/feishu-doc", nil, http.StatusOK, &docAfter, srv.mux)
}

func TestUnavailableIngestionPatchAllowsOnlyPureDisable(t *testing.T) {
	components := integration.Components{}
	srv, err := New(context.Background(), Config{
		DBPath:       filepath.Join(t.TempDir(), "wall.db"),
		Integrations: &components,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// Seed a legacy enabled state directly so an omitted enabled field cannot
	// preserve true and bypass the unavailable-adapter guard.
	state, err := srv.store.GetFeishuIngestionState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state.Enabled = true
	if _, err := srv.store.ConfigureFeishuIngestion(context.Background(), *state, nil); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		body map[string]any
	}{
		{name: "enabled omitted", body: map[string]any{}},
		{name: "enable", body: map[string]any{"enabled": true}},
		{name: "timezone", body: map[string]any{"timezone": ""}},
		{name: "active start", body: map[string]any{"active_start": ""}},
		{name: "active end", body: map[string]any{"active_end": ""}},
		{name: "interval explicit zero", body: map[string]any{"interval_minutes": 0}},
		{name: "overlap explicit zero", body: map[string]any{"overlap_minutes": 0}},
		{name: "excluded chats explicit empty", body: map[string]any{"excluded_chat_ids": []string{}}},
		{name: "resume explicit empty", body: map[string]any{"resume_from": ""}},
		{name: "disable plus config", body: map[string]any{"enabled": false, "timezone": "Asia/Shanghai"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before, err := srv.store.GetFeishuIngestionState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertUnavailable(t, srv, http.MethodPatch, "/api/integrations/activity", test.body)
			after, err := srv.store.GetFeishuIngestionState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if string(mustJSON(t, after)) != string(mustJSON(t, before)) {
				t.Fatalf("unavailable patch mutated state: before=%+v after=%+v", before, after)
			}
		})
	}

	var disabled model.FeishuIngestionState
	requestMuxJSON(t, http.MethodPatch, "/api/integrations/activity", map[string]any{"enabled": false}, http.StatusOK, &disabled, srv.mux)
	if disabled.Enabled || disabled.Status != "disabled" {
		t.Fatalf("pure disable result = %+v", disabled)
	}
}

func TestExplicitIntegrationsValidateBeforeOpeningDatabase(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "missing", "wall.db")
	tests := []struct {
		name       string
		components integration.Components
		want       string
	}{
		{
			name: "invalid descriptor",
			components: integration.Components{Analyzer: &testAnalyzer{descriptor: integration.Descriptor{
				ID: "broken", Role: integration.RoleAnalyzer, ProtocolVersion: integration.ProtocolVersion + 1,
			}}},
			want: "validate integrations",
		},
		{
			name:       "connector without analyzer",
			components: integration.Components{Connectors: []integration.Connector{newTestConnector("only.connector")}},
			want:       "analyzer is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(context.Background(), Config{DBPath: badPath, Integrations: &test.components})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New error = %v, want %q", err, test.want)
			}
			if strings.Contains(err.Error(), "open database") {
				t.Fatalf("database opened before component validation: %v", err)
			}
		})
	}
}

func TestExplicitComponentsWireOnlySuppliedIngestionPipeline(t *testing.T) {
	connector := newTestConnector("custom.connector")
	enricher := newTestEnricher("custom.enricher")
	analyzer := newTestAnalyzer("custom.analyzer")
	components := integration.Components{
		Connectors: []integration.Connector{connector},
		Enrichers:  []integration.Enricher{enricher},
		Analyzer:   analyzer,
	}
	srv, err := New(context.Background(), Config{
		DBPath:                   filepath.Join(t.TempDir(), "wall.db"),
		Integrations:             &components,
		DisableBackgroundWorkers: true,
		// These legacy fields must be ignored in explicit mode.
		IngestionCollector: &recordingLegacyCollector{},
		IngestionAnalyzer:  &recordingLegacyAnalyzer{},
		LarkRunner:         &fakeLarkRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if srv.ingestWorker == nil || srv.ingestWorker.LocalCollector != nil {
		t.Fatalf("explicit worker/local collector = %#v/%#v", srv.ingestWorker, srv.ingestWorker.LocalCollector)
	}
	if srv.feishuClient != nil || srv.syncWorker != nil || srv.syncCancel != nil {
		t.Fatalf("explicit ingestion unexpectedly mixed legacy Feishu control or started workers")
	}
	hookStatsBefore, err := srv.store.StorageStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertUnavailable(t, srv, http.MethodPost, "/api/integrations/conversations/events", model.ConversationHookEvent{
		ID: "custom-pipeline-hook", Source: "codex", EventName: "UserPromptSubmit",
		SessionID: "custom-session", Prompt: "must not persist", OccurredAt: time.Now().UTC(),
	})
	hookStatsAfter, err := srv.store.StorageStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hookStatsAfter.HookEvents != hookStatsBefore.HookEvents {
		t.Fatalf("custom connector pipeline persisted an unconsumable local hook: before=%d after=%d",
			hookStatsBefore.HookEvents, hookStatsAfter.HookEvents)
	}
	collection, err := srv.ingestWorker.Collector.Collect(context.Background(), time.Time{}, time.Now(), nil)
	if err != nil || len(collection.Candidates) != 1 {
		t.Fatalf("legacy connector bridge returned %+v, %v", collection, err)
	}
	if resources := srv.ingestWorker.Enricher.Enrich(context.Background(), collection.Messages); len(resources) != 1 {
		t.Fatalf("legacy enricher bridge returned %+v", resources)
	}
	if _, err := srv.ingestWorker.Analyzer.Analyze(context.Background(), []feishuingest.AnalysisInput{{
		Messages: collection.Messages, Candidates: collection.Candidates,
	}}); err != nil {
		t.Fatal(err)
	}
	if connector.calls != 1 || enricher.calls != 1 || analyzer.calls != 1 {
		t.Fatalf("explicit calls connector=%d enricher=%d analyzer=%d", connector.calls, enricher.calls, analyzer.calls)
	}

	active, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "active.db"), Integrations: &components})
	if err != nil {
		t.Fatal(err)
	}
	if active.syncCancel == nil {
		active.Close()
		t.Fatal("existing integration worker was not started")
	}
	if !backgroundWorkersCapability(t, active) {
		active.Close()
		t.Fatal("connector composition did not advertise its background worker")
	}
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitExporterIsValidatedButDoesNotEnableLegacyControl(t *testing.T) {
	exporter := &testExporter{descriptor: testDescriptor("custom.exporter", integration.RoleExporter)}
	components := integration.Components{Exporters: []integration.Exporter{exporter}}
	srv, err := New(context.Background(), Config{
		DBPath:       filepath.Join(t.TempDir(), "wall.db"),
		Integrations: &components,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if len(srv.integrations.Exporters) != 1 || srv.integrations.Exporters[0] != exporter {
		t.Fatalf("export composition was not retained: %#v", srv.integrations.Exporters)
	}
	if srv.feishuClient != nil || srv.syncWorker != nil {
		t.Fatal("explicit exporter enabled unsupported legacy export scheduling")
	}
	if backgroundWorkersCapability(t, srv) {
		t.Fatal("exporter-only composition advertised background workers")
	}
	assertUnavailable(t, srv, http.MethodPost, "/api/integrations/feishu-doc", map[string]any{"create": true})
}

func TestLegacyCompositionAdvertisesBackgroundWorkers(t *testing.T) {
	srv, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "wall.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if !backgroundWorkersCapability(t, srv) {
		t.Fatal("legacy composition did not advertise its background workers")
	}
}

type testConnector struct {
	descriptor integration.Descriptor
	calls      int
}

func newTestConnector(id integration.ComponentID) *testConnector {
	return &testConnector{descriptor: testDescriptor(id, integration.RoleConnector)}
}

func (c *testConnector) Descriptor() integration.Descriptor { return c.descriptor }

func (c *testConnector) Collect(context.Context, integration.CollectRequest) (integration.Collection, error) {
	c.calls++
	ref := integration.ObservationRef{ConnectorID: c.descriptor.ID, ExternalID: "observation-1"}
	observation := integration.Observation{Ref: ref, Kind: "message", Content: "hello", OccurredAt: time.Now()}
	return integration.Collection{Observations: []integration.Observation{observation}, CandidateRefs: []integration.ObservationRef{ref}, Seen: 1}, nil
}

type testEnricher struct {
	descriptor integration.Descriptor
	calls      int
}

func newTestEnricher(id integration.ComponentID) *testEnricher {
	return &testEnricher{descriptor: testDescriptor(id, integration.RoleEnricher)}
}

func (e *testEnricher) Descriptor() integration.Descriptor { return e.descriptor }

func (e *testEnricher) Enrich(context.Context, integration.EnrichRequest) (integration.EnrichmentResult, error) {
	e.calls++
	return integration.EnrichmentResult{Resources: []integration.Resource{{
		Ref: integration.ResourceRef{EnricherID: e.descriptor.ID, ExternalID: "resource-1"}, Kind: "test", Accessible: true,
	}}}, nil
}

type testAnalyzer struct {
	descriptor integration.Descriptor
	calls      int
}

func newTestAnalyzer(id integration.ComponentID) *testAnalyzer {
	return &testAnalyzer{descriptor: testDescriptor(id, integration.RoleAnalyzer)}
}

func (a *testAnalyzer) Descriptor() integration.Descriptor { return a.descriptor }

func (a *testAnalyzer) Analyze(context.Context, integration.AnalysisBatch) (integration.AnalysisResult, error) {
	a.calls++
	return integration.AnalysisResult{}, nil
}

type testExporter struct {
	descriptor integration.Descriptor
}

func (e *testExporter) Descriptor() integration.Descriptor { return e.descriptor }

func (e *testExporter) Export(context.Context, integration.ExportRequest) (integration.ExportReceipt, error) {
	return integration.ExportReceipt{}, errors.New("not called")
}

func testDescriptor(id integration.ComponentID, role integration.Role) integration.Descriptor {
	return integration.Descriptor{ID: id, Role: role, Name: string(id), ProtocolVersion: integration.ProtocolVersion}
}

func componentIDs(components integration.Components) []string {
	result := make([]string, 0, len(components.Connectors)+len(components.Enrichers)+len(components.Exporters)+1)
	for _, component := range components.Connectors {
		result = append(result, string(component.Descriptor().ID))
	}
	for _, component := range components.Enrichers {
		result = append(result, string(component.Descriptor().ID))
	}
	if components.Analyzer != nil {
		result = append(result, string(components.Analyzer.Descriptor().ID))
	}
	for _, component := range components.Exporters {
		result = append(result, string(component.Descriptor().ID))
	}
	return result
}

func assertUnavailable(t *testing.T, srv *Server, method, path string, body any) {
	t.Helper()
	var response map[string]any
	requestMuxJSON(t, method, path, body, http.StatusConflict, &response, srv.mux)
	if response["error"] == "" || response["error_code"] != "integration_unavailable" {
		t.Fatalf("unavailable response = %#v", response)
	}
}

func backgroundWorkersCapability(t *testing.T, srv *Server) bool {
	t.Helper()
	var response struct {
		BackgroundWorkers bool `json:"background_workers"`
	}
	requestMuxJSON(t, http.MethodGet, "/api/capabilities", nil, http.StatusOK, &response, srv.mux)
	return response.BackgroundWorkers
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
