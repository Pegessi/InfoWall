package server

import (
	"context"
	"fmt"
	"reflect"

	"github.com/infowall/infowall/internal/conversationingest"
	"github.com/infowall/infowall/internal/feishuingest"
	"github.com/infowall/infowall/internal/feishusync"
	"github.com/infowall/infowall/internal/integration"
)

const (
	defaultConnectorID integration.ComponentID = "feishu.im"
	defaultEnricherID  integration.ComponentID = "resources.command"
	defaultAnalyzerID  integration.ComponentID = "analysis.claude-codex"
)

// resolvedIntegrations contains the legacy worker adapters built from a
// validated component composition. Explicit exporters are deliberately only
// retained in components: the first integration protocol version has no
// generic export scheduler or legacy Feishu document-control adapter.
type resolvedIntegrations struct {
	components   integration.Components
	feishuClient *feishusync.Client
	collector    feishuingest.Collector
	analyzer     feishuingest.Analyzer
	enricher     feishuingest.Enricher
	legacyLocal  bool
}

// resolveIntegrations validates and builds the integration graph before the
// SQLite store is opened. Component constructors used here are side-effect
// free: command lookup and execution remain deferred until an operation runs.
func resolveIntegrations(cfg Config) (resolvedIntegrations, error) {
	if cfg.Integrations != nil {
		components := cloneComponents(*cfg.Integrations)
		if err := components.Validate(); err != nil {
			return resolvedIntegrations{}, fmt.Errorf("validate integrations: %w", err)
		}
		return bridgeComponents(components, nil, false)
	}

	syncRunner, ingestionRunner, err := legacyLarkRunners(cfg.LarkRunner)
	if err != nil {
		return resolvedIntegrations{}, err
	}
	client := &feishusync.Client{Runner: syncRunner}
	collector := cfg.IngestionCollector
	if collector == nil {
		collector = feishuingest.LarkCollector{Runner: ingestionRunner}
	}
	analyzer := cfg.IngestionAnalyzer
	if analyzer == nil {
		analyzer = feishuingest.FallbackAnalyzer{
			Primary: feishuingest.Claude0821Analyzer{
				Path: cfg.ClaudePath, PresetsPath: cfg.ClaudePresetsPath,
				Preset: cfg.ClaudePreset, Timeout: cfg.ClaudeTimeout,
			},
			Fallback: feishuingest.CodexAnalyzer{
				Path: cfg.CodexPath, CWD: cfg.CodexCWD, Timeout: cfg.CodexTimeout,
			},
		}
	}

	connectorComponent, err := feishuingest.NewIntegrationConnector(defaultConnectorID, collector)
	if err != nil {
		return resolvedIntegrations{}, err
	}
	enricherComponent, err := feishuingest.NewIntegrationEnricher(defaultEnricherID, feishuingest.CommandEnricher{LarkRunner: ingestionRunner})
	if err != nil {
		return resolvedIntegrations{}, err
	}
	analyzerComponent, err := feishuingest.NewIntegrationAnalyzer(defaultAnalyzerID, analyzer)
	if err != nil {
		return resolvedIntegrations{}, err
	}
	exporterComponent, err := feishusync.NewIntegrationExporter(*client, integration.Descriptor{})
	if err != nil {
		return resolvedIntegrations{}, err
	}
	components := integration.Components{
		Connectors: []integration.Connector{connectorComponent},
		Enrichers:  []integration.Enricher{enricherComponent},
		Analyzer:   analyzerComponent,
		Exporters:  []integration.Exporter{exporterComponent},
	}
	if err := components.Validate(); err != nil {
		return resolvedIntegrations{}, fmt.Errorf("validate default integrations: %w", err)
	}
	return bridgeComponents(components, client, true)
}

// legacyLarkRunners resolves the two historical timeout policies without
// performing command discovery. A typed-nil interface must be rejected here:
// it otherwise survives interface nil checks and panics only when invoked.
func legacyLarkRunners(configured feishusync.CommandRunner) (feishusync.CommandRunner, feishusync.CommandRunner, error) {
	if configured != nil && nilInterface(configured) {
		return nil, nil, fmt.Errorf("legacy LarkRunner: command runner is nil")
	}
	if configured != nil {
		return configured, configured, nil
	}
	return feishusync.ExecRunner{}, feishusync.ExecRunner{Timeout: defaultIngestionLarkTimeout}, nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func cloneComponents(source integration.Components) integration.Components {
	return integration.Components{
		Connectors: append([]integration.Connector(nil), source.Connectors...),
		Enrichers:  append([]integration.Enricher(nil), source.Enrichers...),
		Analyzer:   source.Analyzer,
		Exporters:  append([]integration.Exporter(nil), source.Exporters...),
	}
}

func bridgeComponents(components integration.Components, client *feishusync.Client, legacyLocal bool) (resolvedIntegrations, error) {
	resolved := resolvedIntegrations{components: components, feishuClient: client, legacyLocal: legacyLocal}
	if len(components.Connectors) == 0 {
		return resolved, nil
	}

	collector, err := feishuingest.NewLegacyCollector(components.Connectors)
	if err != nil {
		return resolvedIntegrations{}, fmt.Errorf("build integration collector: %w", err)
	}
	analyzer, err := feishuingest.NewLegacyAnalyzer(components.Analyzer)
	if err != nil {
		return resolvedIntegrations{}, fmt.Errorf("build integration analyzer: %w", err)
	}
	resolved.collector = collector
	resolved.analyzer = analyzer
	enricher, err := feishuingest.NewLegacyEnricher(components.Enrichers)
	if err != nil {
		return resolvedIntegrations{}, fmt.Errorf("build integration enricher: %w", err)
	}
	resolved.enricher = enricher
	return resolved, nil
}

func (s *Server) wireIntegrations(resolved resolvedIntegrations) {
	s.feishuClient = resolved.feishuClient
	if s.feishuClient != nil {
		s.syncWorker = feishusync.NewWorker(&feishuBackend{store: s.store}, *s.feishuClient)
		s.syncWorker.OnUpdate = s.broadcastFeishuSyncState
	}
	if resolved.collector == nil {
		return
	}
	s.ingestWorker = feishuingest.NewWorker(&ingestionBackend{store: s.store}, resolved.collector, resolved.analyzer)
	s.ingestWorker.Enricher = resolved.enricher
	if resolved.legacyLocal {
		s.ingestWorker.LocalCollector = localConversationCollector{store: s.store, stateDir: conversationingest.DefaultStateDir()}
	}
	s.ingestWorker.OnUpdate = s.broadcastFeishuIngestionState
}

func (s *Server) startIntegrationWorkers(ctx context.Context) {
	if s.cfg.DisableBackgroundWorkers || (s.syncWorker == nil && s.ingestWorker == nil) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workerContext, cancel := context.WithCancel(ctx)
	s.syncCancel = cancel
	if s.syncWorker != nil {
		s.syncWG.Add(1)
		go func() {
			defer s.syncWG.Done()
			s.syncWorker.Run(workerContext)
		}()
	}
	if s.ingestWorker != nil {
		s.syncWG.Add(1)
		go func() {
			defer s.syncWG.Done()
			s.ingestWorker.Run(workerContext)
		}()
	}
}
