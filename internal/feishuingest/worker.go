package feishuingest

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/infowall/infowall/internal/model"
)

type Backend interface {
	GetFeishuIngestionState(context.Context) (*model.FeishuIngestionState, error)
	SetFeishuIngestionNextRun(context.Context, time.Time) error
	StartFeishuIngestionRun(context.Context, string, time.Time, time.Time, time.Duration) (*model.FeishuIngestionRun, error)
	RenewFeishuIngestionLease(context.Context, string, time.Duration) error
	FailFeishuIngestionRun(context.Context, string, string, int, int, int64, int64, int64) error
	CompleteFeishuIngestion(context.Context, model.FeishuIngestionCommit) (*model.FeishuIngestionRun, error)
	FilterNewFeishuMessageIDs(context.Context, []string) (map[string]bool, error)
	IngestionSnapshot(context.Context) (Snapshot, error)
}

type Worker struct {
	Backend   Backend
	Collector Collector
	Analyzer  Analyzer
	Enricher  Enricher
	Now       func() time.Time
	OnUpdate  func()

	wake chan struct{}
	mu   sync.Mutex
}

func NewWorker(backend Backend, collector Collector, analyzer Analyzer) *Worker {
	return &Worker{Backend: backend, Collector: collector, Analyzer: analyzer,
		Now: time.Now, wake: make(chan struct{}, 1)}
}

func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	w.check(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.check(ctx)
		case <-w.wake:
			w.check(ctx)
		}
	}
}

func (w *Worker) check(ctx context.Context) {
	if !w.mu.TryLock() {
		return
	}
	defer w.mu.Unlock()
	state, err := w.Backend.GetFeishuIngestionState(ctx)
	if err != nil || !state.Enabled {
		return
	}
	now := w.Now()
	location, err := time.LoadLocation(state.Timezone)
	if err != nil {
		return
	}
	localNow := now.In(location)
	trigger := "scheduled"
	due := scheduledDue(*state, localNow)
	if state.Requested {
		trigger = "manual"
		due = true
	}
	next := NextScheduledRun(*state, localNow)
	_ = w.Backend.SetFeishuIngestionNextRun(ctx, next)
	if !due {
		w.updated()
		return
	}
	windows, backfill := ingestionWindows(*state, localNow)
	for index, window := range windows {
		var backfillAt *time.Time
		if backfill && index == len(windows)-1 {
			value := localNow.UTC()
			backfillAt = &value
		}
		if err := w.runWindow(ctx, trigger, window[0], window[1], backfillAt, state.ExcludedChatIDs); err != nil {
			w.updated()
			return
		}
		trigger = "recovery"
	}
	_ = w.Backend.SetFeishuIngestionNextRun(ctx, NextScheduledRun(*state, localNow.Add(time.Minute)))
	w.updated()
}

func (w *Worker) runWindow(ctx context.Context, trigger string, start, end time.Time, backfillAt *time.Time, excluded []string) error {
	run, err := w.Backend.StartFeishuIngestionRun(ctx, trigger, start, end, 20*time.Minute)
	if err != nil {
		return err
	}
	w.updated()
	collection, err := w.Collector.Collect(ctx, start, end, excluded)
	if err != nil {
		_ = w.Backend.FailFeishuIngestionRun(ctx, run.ID, err.Error(), 0, 0, 0, 0, 0)
		return err
	}
	ids := make([]string, 0, len(collection.Candidates))
	for _, message := range collection.Candidates {
		ids = append(ids, message.ID)
	}
	newIDs, err := w.Backend.FilterNewFeishuMessageIDs(ctx, ids)
	if err != nil {
		_ = w.Backend.FailFeishuIngestionRun(ctx, run.ID, err.Error(), collection.Seen, 0, 0, 0, 0)
		return err
	}
	newCandidates := make([]Message, 0, len(collection.Candidates))
	for _, message := range collection.Candidates {
		if newIDs[message.ID] {
			newCandidates = append(newCandidates, message)
		}
	}
	commit := model.FeishuIngestionCommit{RunID: run.ID, WindowEnd: end.UTC(), BackfillAt: backfillAt,
		MessagesSeen: collection.Seen, MessagesCandidate: len(newCandidates),
		ProcessedMessageIDs: ids, Skipped: len(collection.Candidates) - len(newCandidates)}
	if len(newCandidates) == 0 {
		_, err = w.Backend.CompleteFeishuIngestion(ctx, commit)
		return err
	}
	snapshot, err := w.Backend.IngestionSnapshot(ctx)
	if err != nil {
		_ = w.Backend.FailFeishuIngestionRun(ctx, run.ID, err.Error(), collection.Seen, len(newCandidates), 0, 0, 0)
		return err
	}
	resources := []Resource{}
	if w.Enricher != nil {
		resources = w.Enricher.Enrich(ctx, collection.Messages)
	}
	batches := buildAnalysisBatches(start, end, collection.Messages, newCandidates, snapshot, resources)
	var result Result
	for _, request := range chunkAnalysisBatches(batches, 40) {
		if err := w.Backend.RenewFeishuIngestionLease(ctx, run.ID, 20*time.Minute); err != nil {
			_ = w.Backend.FailFeishuIngestionRun(ctx, run.ID, err.Error(), collection.Seen, len(newCandidates),
				result.InputTokens, result.CachedInputTokens, result.OutputTokens)
			return err
		}
		partial, analyzeErr := w.Analyzer.Analyze(ctx, request)
		mergeAnalysisResult(&result, partial)
		if analyzeErr != nil {
			_ = w.Backend.FailFeishuIngestionRun(ctx, run.ID, analyzeErr.Error(), collection.Seen, len(newCandidates),
				result.InputTokens, result.CachedInputTokens, result.OutputTokens)
			return analyzeErr
		}
	}
	commit.InputTokens = result.InputTokens
	commit.CachedInputTokens = result.CachedInputTokens
	commit.OutputTokens = result.OutputTokens
	commit.MissingContextCount = len(result.MissingContextIDs)
	commit.Skipped += len(result.SkippedMessageIDs)
	messages := make(map[string]Message, len(collection.Messages))
	for _, message := range collection.Messages {
		messages[message.ID] = message
	}
	for _, candidate := range result.NewDemands {
		demand := &model.Demand{Title: strings.TrimSpace(candidate.Title),
			Description: strings.TrimSpace(candidate.Description), Status: model.DemandStatusPending,
			Priority: model.DemandPriorityNone, ProjectHint: strings.TrimSpace(candidate.ProjectHint),
			NextAction: strings.TrimSpace(candidate.NextAction), Sources: []model.Source{}, Progress: []model.Progress{}}
		for _, source := range candidate.Sources {
			message := messages[source.ExternalID]
			demand.Sources = append(demand.Sources, canonicalSource(message, source.Excerpt))
			for _, resource := range resourcesForMessage(message, resources) {
				demand.Sources = append(demand.Sources, resourceSource(resource))
			}
		}
		demand.Sources = dedupeModelSources(demand.Sources)
		commit.NewDemands = append(commit.NewDemands, demand)
	}
	for _, update := range result.ProgressUpdates {
		commit.ProgressUpdates = append(commit.ProgressUpdates, model.DemandProgressUpdate{
			DemandID: update.DemandID, Text: update.Text, DedupeKey: update.DedupeKey,
			Source:     canonicalSource(messages[update.Source.ExternalID], update.Source.Excerpt),
			Confidence: update.Confidence, Anchors: update.Anchors})
	}
	for _, item := range result.Reviews {
		commit.Reviews = append(commit.Reviews, model.DemandReview{Kind: "progress",
			SuggestedDemandID: item.SuggestedDemandID, ProgressText: item.ProgressText,
			ProgressDedupeKey: item.ProgressDedupeKey,
			Source:            canonicalSource(messages[item.Source.ExternalID], item.Source.Excerpt),
			Confidence:        item.Confidence, Rationale: item.Rationale})
	}
	_, err = w.Backend.CompleteFeishuIngestion(ctx, commit)
	return err
}

func chunkAnalysisBatches(batches []AnalysisInput, candidateLimit int) [][]AnalysisInput {
	if candidateLimit <= 0 {
		candidateLimit = 40
	}
	normalized := make([]AnalysisInput, 0, len(batches))
	for _, batch := range batches {
		if len(batch.Candidates) <= candidateLimit {
			normalized = append(normalized, batch)
			continue
		}
		for offset := 0; offset < len(batch.Candidates); offset += candidateLimit {
			end := offset + candidateLimit
			if end > len(batch.Candidates) {
				end = len(batch.Candidates)
			}
			candidates := append([]Message(nil), batch.Candidates[offset:end]...)
			messages := boundedContext(batch.Messages, candidates, 120)
			resources := make([]Resource, 0)
			for _, message := range messages {
				resources = append(resources, resourcesForMessage(message, batch.Resources)...)
			}
			normalized = append(normalized, AnalysisInput{
				WindowStart: batch.WindowStart,
				WindowEnd:   batch.WindowEnd,
				Messages:    messages,
				Candidates:  candidates,
				Snapshot:    batch.Snapshot,
				Resources:   dedupeResources(resources),
			})
		}
	}

	result := make([][]AnalysisInput, 0)
	current := make([]AnalysisInput, 0)
	count := 0
	for _, batch := range normalized {
		batchCount := len(batch.Candidates)
		if len(current) > 0 && count+batchCount > candidateLimit {
			result = append(result, current)
			current = make([]AnalysisInput, 0)
			count = 0
		}
		current = append(current, batch)
		count += batchCount
	}
	if len(current) > 0 {
		result = append(result, current)
	}
	return result
}

func mergeAnalysisResult(destination *Result, source Result) {
	destination.NewDemands = append(destination.NewDemands, source.NewDemands...)
	destination.ProgressUpdates = append(destination.ProgressUpdates, source.ProgressUpdates...)
	destination.Reviews = append(destination.Reviews, source.Reviews...)
	destination.SkippedMessageIDs = append(destination.SkippedMessageIDs, source.SkippedMessageIDs...)
	destination.MissingContextIDs = append(destination.MissingContextIDs, source.MissingContextIDs...)
	destination.InputTokens += source.InputTokens
	destination.CachedInputTokens += source.CachedInputTokens
	destination.OutputTokens += source.OutputTokens
}

func buildAnalysisBatches(start, end time.Time, messages, candidates []Message, snapshot Snapshot, resources []Resource) []AnalysisInput {
	keyFor := func(message Message) string {
		if message.ThreadID != "" {
			return message.ChatID + ":thread:" + message.ThreadID
		}
		return message.ChatID + ":chat"
	}
	candidateGroups := make(map[string][]Message)
	for _, message := range candidates {
		candidateGroups[keyFor(message)] = append(candidateGroups[keyFor(message)], message)
	}
	messageGroups := make(map[string][]Message)
	for _, message := range messages {
		key := keyFor(message)
		if _, relevant := candidateGroups[key]; relevant {
			messageGroups[key] = append(messageGroups[key], message)
		}
	}
	keys := make([]string, 0, len(candidateGroups))
	for key := range candidateGroups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]AnalysisInput, 0, len(keys))
	for _, key := range keys {
		contextMessages := boundedContext(messageGroups[key], candidateGroups[key], 120)
		batchResources := make([]Resource, 0)
		for _, message := range contextMessages {
			for _, resource := range resourcesForMessage(message, resources) {
				batchResources = append(batchResources, resource)
			}
		}
		result = append(result, AnalysisInput{WindowStart: start, WindowEnd: end,
			Messages: contextMessages, Candidates: candidateGroups[key], Snapshot: snapshot,
			Resources: dedupeResources(batchResources)})
	}
	return result
}

func dedupeResources(resources []Resource) []Resource {
	seen := make(map[string]struct{}, len(resources))
	result := make([]Resource, 0, len(resources))
	for _, resource := range resources {
		if _, exists := seen[resource.DedupeKey]; exists {
			continue
		}
		seen[resource.DedupeKey] = struct{}{}
		result = append(result, resource)
	}
	return result
}

func boundedContext(messages, candidates []Message, limit int) []Message {
	if len(messages) <= limit {
		return messages
	}
	candidateIDs := make(map[string]struct{}, len(candidates))
	selected := make(map[string]Message, limit)
	for _, candidate := range candidates {
		candidateIDs[candidate.ID] = struct{}{}
		selected[candidate.ID] = candidate
	}
	type ranked struct {
		message  Message
		distance time.Duration
	}
	rankedMessages := make([]ranked, 0, len(messages))
	for _, message := range messages {
		if _, candidate := candidateIDs[message.ID]; candidate {
			continue
		}
		distance := time.Duration(1<<63 - 1)
		for _, candidate := range candidates {
			delta := message.CreatedAt.Sub(candidate.CreatedAt)
			if delta < 0 {
				delta = -delta
			}
			if delta < distance {
				distance = delta
			}
		}
		rankedMessages = append(rankedMessages, ranked{message: message, distance: distance})
	}
	sort.Slice(rankedMessages, func(i, j int) bool { return rankedMessages[i].distance < rankedMessages[j].distance })
	for _, item := range rankedMessages {
		if len(selected) >= limit {
			break
		}
		selected[item.message.ID] = item.message
	}
	result := make([]Message, 0, len(selected))
	for _, message := range selected {
		result = append(result, message)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result
}

func canonicalSource(message Message, excerpt string) model.Source {
	content := strings.TrimSpace(excerpt)
	if content == "" || !strings.Contains(message.Content, content) {
		content = message.Content
	}
	content = truncateRunes(content, 240)
	return model.Source{Kind: "feishu-im", ExternalID: message.ID, ChatID: message.ChatID,
		ChatName: message.ChatName, SenderID: message.SenderID, SenderName: message.SenderName,
		MessageTime: message.CreatedAt, URL: message.URL, Excerpt: content,
		DedupeKey: "feishu-im:" + message.ID + ":0", CreatedAt: time.Now().UTC()}
}

func resourceSource(resource Resource) model.Source {
	parts := make([]string, 0, 3)
	for _, value := range []string{resource.State, resource.Title, resource.Excerpt} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	excerpt := strings.Join(parts, " · ")
	return model.Source{Kind: resource.Kind, ExternalID: resource.ExternalID, URL: resource.URL,
		Excerpt: truncateRunes(excerpt, 700), DedupeKey: resource.DedupeKey, CreatedAt: time.Now().UTC()}
}

func dedupeModelSources(sources []model.Source) []model.Source {
	seen := make(map[string]struct{}, len(sources))
	result := make([]model.Source, 0, len(sources))
	for _, source := range sources {
		key := source.DedupeKey
		if key == "" {
			key = source.Kind + ":" + source.ExternalID
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, source)
	}
	return result
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func (w *Worker) updated() {
	if w.OnUpdate != nil {
		w.OnUpdate()
	}
}

func scheduledDue(state model.FeishuIngestionState, now time.Time) bool {
	if state.NextRunAt != nil && now.UTC().Before(state.NextRunAt.UTC()) {
		return false
	}
	start, end, err := activeBounds(state, now)
	if err != nil || now.Before(start) || now.After(end.Add(time.Minute)) {
		return false
	}
	minutes := now.Hour()*60 + now.Minute()
	startMinutes := start.Hour()*60 + start.Minute()
	elapsed := minutes - startMinutes
	slot := start.Add(time.Duration(elapsed/state.IntervalMinutes*state.IntervalMinutes) * time.Minute)
	return state.LastSuccessEnd == nil || state.LastSuccessEnd.Before(slot.UTC())
}

func NextScheduledRun(state model.FeishuIngestionState, now time.Time) time.Time {
	start, end, err := activeBounds(state, now)
	if err != nil {
		return time.Time{}
	}
	if now.Before(start) {
		return start.UTC()
	}
	if !now.Before(end) {
		return start.AddDate(0, 0, 1).UTC()
	}
	elapsed := int(now.Sub(start) / time.Minute)
	nextSlot := (elapsed/state.IntervalMinutes + 1) * state.IntervalMinutes
	next := start.Add(time.Duration(nextSlot) * time.Minute)
	if next.After(end) {
		next = end
	}
	return next.UTC()
}

func activeBounds(state model.FeishuIngestionState, now time.Time) (time.Time, time.Time, error) {
	startClock, err := time.Parse("15:04", state.ActiveStart)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endClock, err := time.Parse("15:04", state.ActiveEnd)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), startClock.Hour(), startClock.Minute(), 0, 0, now.Location())
	end := time.Date(now.Year(), now.Month(), now.Day(), endClock.Hour(), endClock.Minute(), 0, 0, now.Location())
	return start, end, nil
}

func ingestionWindows(state model.FeishuIngestionState, now time.Time) ([][2]time.Time, bool) {
	end := now.UTC()
	backfill := state.LastBackfillAt == nil || !sameLocalDay(*state.LastBackfillAt, now)
	var start time.Time
	switch {
	case state.LastSuccessEnd == nil:
		start = end.Add(-7 * 24 * time.Hour)
		backfill = true
	case end.Sub(state.LastSuccessEnd.UTC()) > 48*time.Hour:
		start = state.LastSuccessEnd.UTC().Add(-time.Duration(state.OverlapMinutes) * time.Minute)
		if earliest := end.Add(-7 * 24 * time.Hour); start.Before(earliest) {
			start = earliest
		}
		backfill = true
	case backfill && now.Hour() >= 9:
		start = end.Add(-48 * time.Hour)
	default:
		start = state.LastSuccessEnd.UTC().Add(-time.Duration(state.OverlapMinutes) * time.Minute)
	}
	if !start.Before(end) {
		start = end.Add(-time.Duration(max(1, state.OverlapMinutes)) * time.Minute)
	}
	return splitWindows(start, end, 24*time.Hour), backfill
}

func splitWindows(start, end time.Time, maximum time.Duration) [][2]time.Time {
	result := make([][2]time.Time, 0)
	for start.Before(end) {
		windowEnd := start.Add(maximum)
		if windowEnd.After(end) {
			windowEnd = end
		}
		result = append(result, [2]time.Time{start, windowEnd})
		start = windowEnd
	}
	return result
}

func sameLocalDay(value time.Time, local time.Time) bool {
	value = value.In(local.Location())
	return value.Year() == local.Year() && value.YearDay() == local.YearDay()
}
