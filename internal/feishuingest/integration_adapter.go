package feishuingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/infowall/infowall/internal/integration"
	"github.com/infowall/infowall/internal/model"
)

const legacyResourceComponentID integration.ComponentID = "legacy"

// IntegrationConnector adapts a legacy Collector to the neutral integration
// protocol. Legacy-only message fields are carried in Observation.Attributes
// so an in-process round trip remains lossless.
type IntegrationConnector struct {
	collector  Collector
	descriptor integration.Descriptor
}

// NewIntegrationConnector wraps collector with a connector descriptor using
// id as its stable component identity.
func NewIntegrationConnector(id integration.ComponentID, collector Collector) (*IntegrationConnector, error) {
	if nilBridgeComponent(collector) {
		return nil, fmt.Errorf("integration connector %q: collector is nil", id)
	}
	descriptor, err := bridgeDescriptor(id, integration.RoleConnector)
	if err != nil {
		return nil, fmt.Errorf("integration connector: %w", err)
	}
	return &IntegrationConnector{collector: collector, descriptor: descriptor}, nil
}

// Descriptor describes the wrapped legacy collector.
func (a *IntegrationConnector) Descriptor() integration.Descriptor {
	return a.descriptor
}

// Collect invokes the legacy collector and maps its messages and candidate
// identities without changing their external IDs.
func (a *IntegrationConnector) Collect(ctx context.Context, request integration.CollectRequest) (integration.Collection, error) {
	excluded, err := excludedChatIDs(request.Settings)
	if err != nil {
		return integration.Collection{}, err
	}
	collected, err := a.collector.Collect(ctx, request.WindowStart, request.WindowEnd, excluded)
	if err != nil {
		return integration.Collection{}, err
	}

	result := integration.Collection{
		Observations: make([]integration.Observation, 0, len(collected.Messages)),
		Seen:         collected.Seen,
	}
	refsByExternalID := make(map[string]integration.ObservationRef, len(collected.Messages))
	for _, message := range collected.Messages {
		observation := observationFromLegacyMessage(message, a.descriptor.ID)
		result.Observations = append(result.Observations, observation)
		if _, exists := refsByExternalID[message.ID]; !exists {
			refsByExternalID[message.ID] = observation.Ref
		}
	}
	for _, candidate := range collected.Candidates {
		ref, exists := refsByExternalID[candidate.ID]
		if !exists {
			continue
		}
		result.CandidateRefs = append(result.CandidateRefs, ref)
		result.AckRefs = append(result.AckRefs, ref)
	}
	return result, nil
}

// IntegrationEnricher adapts a legacy Enricher to the neutral integration
// protocol.
type IntegrationEnricher struct {
	enricher   Enricher
	descriptor integration.Descriptor
}

// NewIntegrationEnricher wraps enricher with an enricher descriptor using id
// as its stable component identity.
func NewIntegrationEnricher(id integration.ComponentID, enricher Enricher) (*IntegrationEnricher, error) {
	if nilBridgeComponent(enricher) {
		return nil, fmt.Errorf("integration enricher %q: enricher is nil", id)
	}
	descriptor, err := bridgeDescriptor(id, integration.RoleEnricher)
	if err != nil {
		return nil, fmt.Errorf("integration enricher: %w", err)
	}
	return &IntegrationEnricher{enricher: enricher, descriptor: descriptor}, nil
}

// Descriptor describes the wrapped legacy enricher.
func (a *IntegrationEnricher) Descriptor() integration.Descriptor {
	return a.descriptor
}

// Enrich invokes the legacy enricher. The legacy interface has no warning or
// fatal-error channel, so this direction always returns a nil error.
func (a *IntegrationEnricher) Enrich(ctx context.Context, request integration.EnrichRequest) (integration.EnrichmentResult, error) {
	messages := messagesFromObservations(request.Observations)
	resources := a.enricher.Enrich(ctx, messages)
	result := integration.EnrichmentResult{Resources: make([]integration.Resource, 0, len(resources))}
	for _, resource := range resources {
		converted := resourceFromLegacy(resource, a.descriptor.ID)
		if len(converted.ObservationRefs) == 0 && resource.URL != "" {
			for index, message := range messages {
				if strings.Contains(message.Content, resource.URL) {
					converted.ObservationRefs = append(converted.ObservationRefs, request.Observations[index].Ref)
				}
			}
		}
		result.Resources = append(result.Resources, converted)
	}
	return result, nil
}

// IntegrationAnalyzer adapts a legacy Analyzer to the neutral integration
// protocol.
type IntegrationAnalyzer struct {
	analyzer   Analyzer
	descriptor integration.Descriptor
}

// NewIntegrationAnalyzer wraps analyzer with an analyzer descriptor using id
// as its stable component identity.
func NewIntegrationAnalyzer(id integration.ComponentID, analyzer Analyzer) (*IntegrationAnalyzer, error) {
	if nilBridgeComponent(analyzer) {
		return nil, fmt.Errorf("integration analyzer %q: analyzer is nil", id)
	}
	descriptor, err := bridgeDescriptor(id, integration.RoleAnalyzer)
	if err != nil {
		return nil, fmt.Errorf("integration analyzer: %w", err)
	}
	return &IntegrationAnalyzer{analyzer: analyzer, descriptor: descriptor}, nil
}

// Descriptor describes the wrapped legacy analyzer.
func (a *IntegrationAnalyzer) Descriptor() integration.Descriptor {
	return a.descriptor
}

// Analyze maps one protocol batch to the legacy analyzer's one-element batch
// list and maps both its partial result and error back to the protocol.
func (a *IntegrationAnalyzer) Analyze(ctx context.Context, batch integration.AnalysisBatch) (integration.AnalysisResult, error) {
	candidateRefs, err := validateAnalysisCandidates(batch.Observations, batch.Candidates)
	if err != nil {
		return integration.AnalysisResult{}, err
	}
	legacyInput := analysisInputFromIntegration(batch)
	result, analyzeErr := a.analyzer.Analyze(ctx, []AnalysisInput{legacyInput})
	converted, convertErr := analysisResultFromLegacy(result, batch, candidateRefs)
	return converted, errors.Join(analyzeErr, convertErr)
}

// LegacyCollector adapts one or more neutral connectors to the Collector
// contract consumed by the existing ingestion worker.
type LegacyCollector struct {
	connectors []integration.Connector
}

type collectedConnectorResult struct {
	descriptor integration.Descriptor
	collection integration.Collection
}

// NewLegacyCollector validates and wraps connectors. Connector results are
// collected in slice order and merged without rewriting external IDs.
func NewLegacyCollector(connectors []integration.Connector) (*LegacyCollector, error) {
	seen := make(map[integration.ComponentID]int, len(connectors))
	for index, connector := range connectors {
		if err := validateBridgeComponent(fmt.Sprintf("connector[%d]", index), connector, integration.RoleConnector); err != nil {
			return nil, err
		}
		id := connector.Descriptor().ID
		if previous, exists := seen[id]; exists {
			return nil, fmt.Errorf("connector[%d]: duplicate component id %q (already used by connector[%d])", index, id, previous)
		}
		seen[id] = index
	}
	return &LegacyCollector{connectors: append([]integration.Connector(nil), connectors...)}, nil
}

// Collect invokes all connectors, filters excluded containers, and selects
// candidates only when their refs identify observations from the same
// connector result.
func (a *LegacyCollector) Collect(ctx context.Context, start, end time.Time, excluded []string) (Collection, error) {
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, id := range excluded {
		excludedSet[strings.TrimSpace(id)] = struct{}{}
	}

	collections := make([]collectedConnectorResult, 0, len(a.connectors))
	externalIDOwners := make(map[string]integration.ComponentID)
	for _, connector := range a.connectors {
		descriptor := connector.Descriptor()
		collected, err := connector.Collect(ctx, integration.CollectRequest{
			WindowStart: start,
			WindowEnd:   end,
			Limit:       MessageLimit,
		})
		if err != nil {
			return Collection{}, fmt.Errorf("connector %q collect: %w", descriptor.ID, err)
		}
		if err := validateCollectionScope(descriptor, collected); err != nil {
			return Collection{}, err
		}
		if err := validateUniqueExternalIDs(descriptor, collected, externalIDOwners); err != nil {
			return Collection{}, err
		}
		collections = append(collections, collectedConnectorResult{descriptor: descriptor, collection: collected})
	}

	var merged Collection
	for _, item := range collections {
		collected := item.collection
		merged.Seen += collected.Seen

		messagesByRef := make(map[integration.ObservationRef]Message, len(collected.Observations))
		for _, observation := range collected.Observations {
			if _, skip := excludedSet[observation.ContainerID]; skip {
				continue
			}
			message := messageFromObservation(observation)
			merged.Messages = append(merged.Messages, message)
			messagesByRef[observation.Ref] = message
		}
		for _, ref := range collected.CandidateRefs {
			if message, exists := messagesByRef[ref]; exists {
				merged.Candidates = append(merged.Candidates, message)
			}
		}
	}
	return merged, nil
}

// LegacyEnricher adapts neutral enrichers to the legacy Enricher contract.
// Because that contract cannot return errors, a failing component is logged,
// its partial result is discarded, and remaining components continue unless
// the context has been canceled.
type LegacyEnricher struct {
	enrichers []integration.Enricher
}

// NewLegacyEnricher validates and wraps enrichers.
func NewLegacyEnricher(enrichers []integration.Enricher) (*LegacyEnricher, error) {
	seen := make(map[integration.ComponentID]int, len(enrichers))
	for index, enricher := range enrichers {
		if err := validateBridgeComponent(fmt.Sprintf("enricher[%d]", index), enricher, integration.RoleEnricher); err != nil {
			return nil, err
		}
		id := enricher.Descriptor().ID
		if previous, exists := seen[id]; exists {
			return nil, fmt.Errorf("enricher[%d]: duplicate component id %q (already used by enricher[%d])", index, id, previous)
		}
		seen[id] = index
	}
	return &LegacyEnricher{enrichers: append([]integration.Enricher(nil), enrichers...)}, nil
}

// Enrich invokes all configured enrichers and concatenates successful
// resources. Protocol warnings have no legacy representation and are ignored.
func (a *LegacyEnricher) Enrich(ctx context.Context, messages []Message) []Resource {
	observations := observationsFromLegacyMessages(messages)
	var resources []Resource
	for _, enricher := range a.enrichers {
		if ctx.Err() != nil {
			break
		}
		descriptor := enricher.Descriptor()
		result, err := enricher.Enrich(ctx, integration.EnrichRequest{Observations: observations})
		if err != nil {
			log.Printf("feishuingest: enricher %q failed; result discarded", descriptor.ID)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		for _, resource := range result.Resources {
			resources = append(resources, resourceToLegacy(resource))
		}
	}
	return resources
}

// LegacyAnalyzer adapts a neutral single-batch analyzer to the legacy
// Analyzer contract. Each legacy batch is analyzed separately, then merged
// with the same route, health, fallback, and usage rules as the worker.
type LegacyAnalyzer struct {
	analyzer integration.Analyzer
}

// NewLegacyAnalyzer validates and wraps analyzer.
func NewLegacyAnalyzer(analyzer integration.Analyzer) (*LegacyAnalyzer, error) {
	if err := validateBridgeComponent("analyzer", analyzer, integration.RoleAnalyzer); err != nil {
		return nil, err
	}
	return &LegacyAnalyzer{analyzer: analyzer}, nil
}

// Analyze invokes the protocol analyzer once per legacy batch. A partial
// protocol result is merged before its accompanying error is returned.
func (a *LegacyAnalyzer) Analyze(ctx context.Context, batches []AnalysisInput) (Result, error) {
	var merged Result
	for _, batch := range batches {
		protocolBatch := analysisBatchFromLegacy(batch)
		result, err := a.analyzer.Analyze(ctx, protocolBatch)
		mergeAnalysisResult(&merged, analysisResultToLegacy(result))
		if err != nil {
			return merged, fmt.Errorf("analyzer %q analyze: %w", a.analyzer.Descriptor().ID, err)
		}
	}
	return merged, nil
}

func bridgeDescriptor(id integration.ComponentID, role integration.Role) (integration.Descriptor, error) {
	descriptor := integration.Descriptor{
		ID:              id,
		Role:            role,
		Name:            string(id),
		ProtocolVersion: integration.ProtocolVersion,
	}
	if err := descriptor.Validate(); err != nil {
		return integration.Descriptor{}, err
	}
	return descriptor, nil
}

type describedComponent interface {
	Descriptor() integration.Descriptor
}

func validateBridgeComponent(label string, component describedComponent, role integration.Role) error {
	if nilBridgeComponent(component) {
		return fmt.Errorf("%s: component is nil", label)
	}
	descriptor := component.Descriptor()
	if err := descriptor.Validate(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if descriptor.Role != role {
		return fmt.Errorf("%s: component %q has role %q; want %q", label, descriptor.ID, descriptor.Role, role)
	}
	return nil
}

func validateCollectionScope(descriptor integration.Descriptor, collection integration.Collection) error {
	validate := func(label string, index int, ref integration.ObservationRef) error {
		if ref.ConnectorID != descriptor.ID {
			return fmt.Errorf("connector %q returned %s[%d] with connector_id %q", descriptor.ID, label, index, ref.ConnectorID)
		}
		return nil
	}
	observationRefs := make(map[integration.ObservationRef]struct{}, len(collection.Observations))
	for index, observation := range collection.Observations {
		if err := validate("observations", index, observation.Ref); err != nil {
			return err
		}
		if _, duplicate := observationRefs[observation.Ref]; duplicate {
			return fmt.Errorf("connector %q returned duplicate observations[%d] ref %q", descriptor.ID, index, observation.Ref.Key())
		}
		observationRefs[observation.Ref] = struct{}{}
	}
	for index, ref := range collection.CandidateRefs {
		if err := validate("candidate_refs", index, ref); err != nil {
			return err
		}
		if _, exists := observationRefs[ref]; !exists {
			return fmt.Errorf("connector %q returned candidate_refs[%d] not present in observations", descriptor.ID, index)
		}
	}
	for index, ref := range collection.AckRefs {
		if err := validate("ack_refs", index, ref); err != nil {
			return err
		}
		if _, exists := observationRefs[ref]; !exists {
			return fmt.Errorf("connector %q returned ack_refs[%d] not present in observations", descriptor.ID, index)
		}
	}
	return nil
}

func validateUniqueExternalIDs(descriptor integration.Descriptor, collection integration.Collection, owners map[string]integration.ComponentID) error {
	for _, observation := range collection.Observations {
		if owner, exists := owners[observation.Ref.ExternalID]; exists && owner != descriptor.ID {
			return fmt.Errorf("connectors %q and %q returned duplicate external_id %q", owner, descriptor.ID, observation.Ref.ExternalID)
		}
		owners[observation.Ref.ExternalID] = descriptor.ID
	}
	return nil
}

func nilBridgeComponent(component any) bool {
	if component == nil {
		return true
	}
	value := reflect.ValueOf(component)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type legacyMessageAttributesEnvelope struct {
	Message *legacyMessageAttributes `json:"infowall_legacy_message,omitempty"`
}

type legacyMessageAttributes struct {
	ChatType    string    `json:"chat_type,omitempty"`
	SenderType  string    `json:"sender_type,omitempty"`
	MessageType string    `json:"message_type,omitempty"`
	Mentions    []Mention `json:"mentions,omitempty"`
	SourceKind  string    `json:"source_kind,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	TurnID      string    `json:"turn_id,omitempty"`
	CWD         string    `json:"cwd,omitempty"`
}

func observationFromLegacyMessage(message Message, connectorID integration.ComponentID) integration.Observation {
	attributes, _ := json.Marshal(legacyMessageAttributesEnvelope{Message: &legacyMessageAttributes{
		ChatType: message.ChatType, SenderType: message.SenderType, MessageType: message.MessageType,
		Mentions: append([]Mention(nil), message.Mentions...), SourceKind: message.SourceKind,
		SessionID: message.SessionID, TurnID: message.TurnID, CWD: message.CWD,
	}})
	return integration.Observation{
		Ref:           integration.ObservationRef{ConnectorID: connectorID, ExternalID: message.ID},
		Kind:          normalizedSourceKind(message),
		ContainerID:   message.ChatID,
		ContainerName: message.ChatName,
		ThreadID:      message.ThreadID,
		ActorID:       message.SenderID,
		ActorName:     message.SenderName,
		Content:       message.Content,
		URL:           message.URL,
		OccurredAt:    message.CreatedAt,
		Deleted:       message.Deleted,
		Links:         linksFromLegacy(message.Links),
		Attributes:    attributes,
	}
}

func observationsFromLegacyMessages(messages []Message) []integration.Observation {
	result := make([]integration.Observation, 0, len(messages))
	for _, message := range messages {
		connectorID := integration.ComponentID(strings.TrimSpace(message.ConnectorID))
		if connectorID == "" {
			connectorID = integration.ComponentID(strings.TrimSpace(message.SourceKind))
		}
		if connectorID == "" {
			connectorID = integration.ComponentID(normalizedSourceKind(message))
		}
		result = append(result, observationFromLegacyMessage(message, connectorID))
	}
	return result
}

func messageFromObservation(observation integration.Observation) Message {
	message := Message{
		ID: observation.Ref.ExternalID, ChatID: observation.ContainerID, ChatName: observation.ContainerName,
		ThreadID: observation.ThreadID, SenderID: observation.ActorID, SenderName: observation.ActorName,
		MessageType: observation.Kind, Content: observation.Content, URL: observation.URL,
		CreatedAt: observation.OccurredAt, Deleted: observation.Deleted,
		SourceKind: observation.Kind, ConnectorID: string(observation.Ref.ConnectorID),
		Links: linksToLegacy(observation.Links),
	}
	var attributes legacyMessageAttributesEnvelope
	if json.Unmarshal(observation.Attributes, &attributes) == nil && attributes.Message != nil {
		legacy := attributes.Message
		message.ChatType = legacy.ChatType
		message.SenderType = legacy.SenderType
		message.MessageType = legacy.MessageType
		message.Mentions = append([]Mention(nil), legacy.Mentions...)
		message.SourceKind = legacy.SourceKind
		message.SessionID = legacy.SessionID
		message.TurnID = legacy.TurnID
		message.CWD = legacy.CWD
	}
	return message
}

func messagesFromObservations(observations []integration.Observation) []Message {
	result := make([]Message, 0, len(observations))
	for _, observation := range observations {
		result = append(result, messageFromObservation(observation))
	}
	return result
}

func linksFromLegacy(links []model.ProgressLink) []integration.Link {
	if links == nil {
		return nil
	}
	result := make([]integration.Link, 0, len(links))
	for _, link := range links {
		result = append(result, integration.Link{
			Kind: link.Kind, ExternalID: link.ExternalID, Title: link.Title, URL: link.URL,
			State: link.State, DedupeKey: link.DedupeKey,
		})
	}
	return result
}

func linksToLegacy(links []integration.Link) []model.ProgressLink {
	if links == nil {
		return nil
	}
	result := make([]model.ProgressLink, 0, len(links))
	for _, link := range links {
		result = append(result, model.ProgressLink{
			Kind: link.Kind, ExternalID: link.ExternalID, Title: link.Title, URL: link.URL,
			State: link.State, DedupeKey: link.DedupeKey,
		})
	}
	return result
}

func resourceFromLegacy(resource Resource, enricherID integration.ComponentID) integration.Resource {
	converted := integration.Resource{
		Ref:  integration.ResourceRef{EnricherID: enricherID, ExternalID: resource.ExternalID},
		Kind: resource.Kind, URL: resource.URL, Title: resource.Title, State: resource.State,
		Excerpt: resource.Excerpt, DedupeKey: resource.DedupeKey, Accessible: resource.Accessible,
	}
	for _, ref := range resource.ObservationRefs {
		converted.ObservationRefs = append(converted.ObservationRefs, integration.ObservationRef{
			ConnectorID: integration.ComponentID(ref.ConnectorID), ExternalID: ref.ExternalID,
		})
	}
	return converted
}

func resourceToLegacy(resource integration.Resource) Resource {
	// The legacy Resource contract has no representation for protocol
	// Attributes. ObservationRefs are retained separately because the worker
	// needs them for identity-safe resource association.
	converted := Resource{
		Kind: resource.Kind, ExternalID: resource.Ref.ExternalID, URL: resource.URL, Title: resource.Title,
		State: resource.State, Excerpt: resource.Excerpt, DedupeKey: resource.DedupeKey,
		Accessible: resource.Accessible,
	}
	for _, ref := range resource.ObservationRefs {
		converted.ObservationRefs = append(converted.ObservationRefs, ResourceObservationRef{
			ConnectorID: string(ref.ConnectorID), ExternalID: ref.ExternalID,
		})
	}
	return converted
}

func analysisBatchFromLegacy(input AnalysisInput) integration.AnalysisBatch {
	observations := observationsFromLegacyMessages(input.Messages)
	observationsByRef := make(map[integration.ObservationRef]integration.Observation, len(observations))
	for _, observation := range observations {
		observationsByRef[observation.Ref] = observation
	}
	candidates := make([]integration.Observation, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		candidateRef := observationsFromLegacyMessages([]Message{candidate})[0].Ref
		if observation, exists := observationsByRef[candidateRef]; exists {
			candidates = append(candidates, observation)
		}
	}
	resources := make([]integration.Resource, 0, len(input.Resources))
	for _, resource := range input.Resources {
		resources = append(resources, resourceFromLegacy(resource, legacyResourceComponentID))
	}
	return integration.AnalysisBatch{
		WindowStart: input.WindowStart, WindowEnd: input.WindowEnd, Observations: observations,
		Candidates: candidates, Resources: resources, Snapshot: snapshotFromLegacy(input.Snapshot),
	}
}

func analysisInputFromIntegration(batch integration.AnalysisBatch) AnalysisInput {
	messages := messagesFromObservations(batch.Observations)
	messagesByRef := make(map[integration.ObservationRef]Message, len(batch.Observations))
	for index, observation := range batch.Observations {
		messagesByRef[observation.Ref] = messages[index]
	}
	candidates := make([]Message, 0, len(batch.Candidates))
	for _, candidate := range batch.Candidates {
		if message, exists := messagesByRef[candidate.Ref]; exists {
			candidates = append(candidates, message)
		}
	}
	resources := make([]Resource, 0, len(batch.Resources))
	for _, resource := range batch.Resources {
		resources = append(resources, resourceToLegacy(resource))
	}
	return AnalysisInput{
		WindowStart: batch.WindowStart, WindowEnd: batch.WindowEnd, Messages: messages,
		Candidates: candidates, Resources: resources, Snapshot: snapshotToLegacy(batch.Snapshot),
	}
}

func snapshotFromLegacy(snapshot Snapshot) integration.AnalysisSnapshot {
	result := integration.AnalysisSnapshot{
		Demands:  make([]integration.DemandView, 0, len(snapshot.Demands)),
		Projects: make([]integration.ProjectView, 0, len(snapshot.Projects)),
	}
	for _, demand := range snapshot.Demands {
		if demand == nil {
			continue
		}
		view := integration.DemandView{
			ID: demand.ID, Title: demand.Title, Description: demand.Description, Status: string(demand.Status),
			Priority: string(demand.Priority), ProjectHint: demand.ProjectHint, NextAction: demand.NextAction,
			BlockedReason: demand.BlockedReason, CreatedAt: demand.CreatedAt, UpdatedAt: demand.UpdatedAt,
			CompletedAt: cloneTime(demand.CompletedAt), Sources: make([]integration.SourceView, 0, len(demand.Sources)),
			Progress: make([]integration.ProgressView, 0, len(demand.Progress)),
		}
		if demand.ProjectID != nil {
			view.ProjectID = *demand.ProjectID
		}
		for _, source := range demand.Sources {
			view.Sources = append(view.Sources, integration.SourceView{
				Kind: source.Kind, ExternalID: source.ExternalID, URL: source.URL, Excerpt: source.Excerpt,
				OccurredAt: source.MessageTime,
			})
		}
		for _, progress := range demand.Progress {
			view.Progress = append(view.Progress, integration.ProgressView{
				Text: progress.Text, Links: linksFromLegacy(progress.Links), CreatedAt: progress.CreatedAt,
			})
		}
		result.Demands = append(result.Demands, view)
	}
	for _, project := range snapshot.Projects {
		if project == nil {
			continue
		}
		result.Projects = append(result.Projects, integration.ProjectView{
			ID: project.ID, Name: project.Name, Description: project.Description, Color: project.Color,
			Status: string(project.Status), CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
		})
	}
	return result
}

func snapshotToLegacy(snapshot integration.AnalysisSnapshot) Snapshot {
	result := Snapshot{
		Demands:  make([]*model.Demand, 0, len(snapshot.Demands)),
		Projects: make([]*model.Project, 0, len(snapshot.Projects)),
	}
	for _, view := range snapshot.Demands {
		demand := &model.Demand{
			ID: view.ID, Title: view.Title, Description: view.Description, Status: model.DemandStatus(view.Status),
			Priority: model.DemandPriority(view.Priority), ProjectHint: view.ProjectHint, NextAction: view.NextAction,
			BlockedReason: view.BlockedReason, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
			CompletedAt: cloneTime(view.CompletedAt), Sources: make([]model.Source, 0, len(view.Sources)),
			Progress: make([]model.Progress, 0, len(view.Progress)),
		}
		if view.ProjectID != "" {
			projectID := view.ProjectID
			demand.ProjectID = &projectID
		}
		for _, source := range view.Sources {
			demand.Sources = append(demand.Sources, model.Source{
				Kind: source.Kind, ExternalID: source.ExternalID, URL: source.URL, Excerpt: source.Excerpt,
				MessageTime: source.OccurredAt,
			})
		}
		for _, progress := range view.Progress {
			demand.Progress = append(demand.Progress, model.Progress{
				Text: progress.Text, Links: linksToLegacy(progress.Links), CreatedAt: progress.CreatedAt,
			})
		}
		result.Demands = append(result.Demands, demand)
	}
	for _, view := range snapshot.Projects {
		result.Projects = append(result.Projects, &model.Project{
			ID: view.ID, Name: view.Name, Description: view.Description, Color: view.Color,
			Status: model.ProjectStatus(view.Status), CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
		})
	}
	return result
}

func analysisResultFromLegacy(result Result, batch integration.AnalysisBatch, candidateRefs map[string]integration.ObservationRef) (integration.AnalysisResult, error) {
	observationsByRef := make(map[integration.ObservationRef]integration.Observation, len(batch.Observations))
	for _, observation := range batch.Observations {
		observationsByRef[observation.Ref] = observation
	}
	converted := integration.AnalysisResult{
		Usage: integration.Usage{
			InputTokens: result.InputTokens, CachedInputTokens: result.CachedInputTokens, OutputTokens: result.OutputTokens,
		},
		Route: integration.AnalyzerRoute{
			Route: result.AnalyzerRoute, ProfileID: result.AnalyzerProfileID,
			ProfileFingerprint: result.AnalyzerProfileFingerprint, Healthy: result.AnalyzerHealthy,
			FallbackUsed: result.FallbackUsed, PrimaryError: result.PrimaryError,
		},
	}
	for demandIndex, demand := range result.NewDemands {
		proposal := integration.DemandProposal{
			Title: demand.Title, Description: demand.Description, NextAction: demand.NextAction,
			ProjectHint: demand.ProjectHint, Evidence: make([]integration.EvidenceRef, 0, len(demand.Sources)),
		}
		for evidenceIndex, evidence := range demand.Sources {
			ref, err := candidateRef(candidateRefs, evidence.ExternalID, fmt.Sprintf("new_demands[%d].sources[%d]", demandIndex, evidenceIndex))
			if err != nil {
				return integration.AnalysisResult{}, err
			}
			proposal.Evidence = append(proposal.Evidence, integration.EvidenceRef{
				Observation: ref, Excerpt: evidence.Excerpt,
			})
		}
		converted.Demands = append(converted.Demands, proposal)
	}
	for index, progress := range result.ProgressUpdates {
		ref, err := candidateRef(candidateRefs, progress.Source.ExternalID, fmt.Sprintf("progress_updates[%d].source", index))
		if err != nil {
			return integration.AnalysisResult{}, err
		}
		converted.Progress = append(converted.Progress, integration.ProgressProposal{
			DemandID: progress.DemandID, Text: progress.Text, DedupeKey: progress.DedupeKey,
			Evidence: integration.EvidenceRef{Observation: ref, Excerpt: progress.Source.Excerpt},
			Links:    linksForObservation(observationsByRef, ref), Confidence: progress.Confidence, Anchors: append([]string(nil), progress.Anchors...),
		})
	}
	for index, review := range result.Reviews {
		ref, err := candidateRef(candidateRefs, review.Source.ExternalID, fmt.Sprintf("reviews[%d].source", index))
		if err != nil {
			return integration.AnalysisResult{}, err
		}
		converted.Reviews = append(converted.Reviews, integration.ReviewProposal{
			SuggestedDemandID: review.SuggestedDemandID, ProgressText: review.ProgressText,
			ProgressDedupeKey: review.ProgressDedupeKey,
			Evidence:          integration.EvidenceRef{Observation: ref, Excerpt: review.Source.Excerpt},
			Links:             linksForObservation(observationsByRef, ref), Confidence: review.Confidence, Rationale: review.Rationale,
		})
	}
	for index, externalID := range result.SkippedMessageIDs {
		ref, err := candidateRef(candidateRefs, externalID, fmt.Sprintf("skipped_message_ids[%d]", index))
		if err != nil {
			return integration.AnalysisResult{}, err
		}
		converted.Skipped = append(converted.Skipped, ref)
	}
	for index, externalID := range result.MissingContextIDs {
		ref, err := candidateRef(candidateRefs, externalID, fmt.Sprintf("missing_context_message_ids[%d]", index))
		if err != nil {
			return integration.AnalysisResult{}, err
		}
		converted.MissingContext = append(converted.MissingContext, ref)
	}
	return converted, nil
}

func analysisResultToLegacy(result integration.AnalysisResult) Result {
	converted := Result{
		InputTokens: result.Usage.InputTokens, CachedInputTokens: result.Usage.CachedInputTokens,
		OutputTokens: result.Usage.OutputTokens, AnalyzerRoute: result.Route.Route,
		AnalyzerProfileID: result.Route.ProfileID, AnalyzerProfileFingerprint: result.Route.ProfileFingerprint,
		AnalyzerHealthy: result.Route.Healthy, FallbackUsed: result.Route.FallbackUsed,
		PrimaryError: result.Route.PrimaryError,
	}
	for _, demand := range result.Demands {
		proposal := NewDemand{
			Title: demand.Title, Description: demand.Description, NextAction: demand.NextAction,
			ProjectHint: demand.ProjectHint, Sources: make([]EvidenceRef, 0, len(demand.Evidence)),
		}
		for _, evidence := range demand.Evidence {
			proposal.Sources = append(proposal.Sources, EvidenceRef{
				ExternalID: evidence.Observation.ExternalID, Excerpt: evidence.Excerpt,
			})
		}
		converted.NewDemands = append(converted.NewDemands, proposal)
	}
	for _, progress := range result.Progress {
		converted.ProgressUpdates = append(converted.ProgressUpdates, ProgressUpdate{
			DemandID: progress.DemandID, Text: progress.Text, DedupeKey: progress.DedupeKey,
			Source:     EvidenceRef{ExternalID: progress.Evidence.Observation.ExternalID, Excerpt: progress.Evidence.Excerpt},
			Confidence: progress.Confidence, Anchors: append([]string(nil), progress.Anchors...),
		})
	}
	for _, review := range result.Reviews {
		converted.Reviews = append(converted.Reviews, Review{
			SuggestedDemandID: review.SuggestedDemandID, ProgressText: review.ProgressText,
			ProgressDedupeKey: review.ProgressDedupeKey,
			Source:            EvidenceRef{ExternalID: review.Evidence.Observation.ExternalID, Excerpt: review.Evidence.Excerpt},
			Confidence:        review.Confidence, Rationale: review.Rationale,
		})
	}
	for _, ref := range result.Skipped {
		converted.SkippedMessageIDs = append(converted.SkippedMessageIDs, ref.ExternalID)
	}
	for _, ref := range result.MissingContext {
		converted.MissingContextIDs = append(converted.MissingContextIDs, ref.ExternalID)
	}
	return converted
}

func validateAnalysisCandidates(observations, candidates []integration.Observation) (map[string]integration.ObservationRef, error) {
	observationRefs := make(map[integration.ObservationRef]struct{}, len(observations))
	for _, observation := range observations {
		observationRefs[observation.Ref] = struct{}{}
	}
	refs := make(map[string]integration.ObservationRef, len(candidates))
	seenCandidates := make(map[integration.ObservationRef]struct{}, len(candidates))
	for index, candidate := range candidates {
		if _, exists := observationRefs[candidate.Ref]; !exists {
			return nil, fmt.Errorf("candidates[%d] ref %q is not present in observations", index, candidate.Ref.Key())
		}
		if _, duplicate := seenCandidates[candidate.Ref]; duplicate {
			return nil, fmt.Errorf("candidates[%d] duplicates ref %q", index, candidate.Ref.Key())
		}
		seenCandidates[candidate.Ref] = struct{}{}
		if existing, exists := refs[candidate.Ref.ExternalID]; exists && existing != candidate.Ref {
			return nil, fmt.Errorf("candidates[%d] external_id %q is ambiguous between connector %q and connector %q",
				index, candidate.Ref.ExternalID, existing.ConnectorID, candidate.Ref.ConnectorID)
		}
		refs[candidate.Ref.ExternalID] = candidate.Ref
	}
	return refs, nil
}

func candidateRef(refs map[string]integration.ObservationRef, externalID, field string) (integration.ObservationRef, error) {
	if ref, exists := refs[externalID]; exists {
		return ref, nil
	}
	return integration.ObservationRef{}, fmt.Errorf("%s references non-candidate external_id %q", field, externalID)
}

func linksForObservation(observations map[integration.ObservationRef]integration.Observation, ref integration.ObservationRef) []integration.Link {
	if observation, exists := observations[ref]; exists {
		return append([]integration.Link(nil), observation.Links...)
	}
	return nil
}

func excludedChatIDs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var settings struct {
		ExcludedChatIDs []string `json:"excluded_chat_ids"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("decode legacy collector settings: %w", err)
	}
	return settings.ExcludedChatIDs, nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

var (
	_ integration.Connector = (*IntegrationConnector)(nil)
	_ integration.Enricher  = (*IntegrationEnricher)(nil)
	_ integration.Analyzer  = (*IntegrationAnalyzer)(nil)
	_ Collector             = (*LegacyCollector)(nil)
	_ Enricher              = (*LegacyEnricher)(nil)
	_ Analyzer              = (*LegacyAnalyzer)(nil)
)
