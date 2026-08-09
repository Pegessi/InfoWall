package feishuingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infowall/infowall/internal/model"
)

type fakeCollector struct {
	result Collection
	err    error
}

func (collector fakeCollector) Collect(context.Context, time.Time, time.Time, []string) (Collection, error) {
	return collector.result, collector.err
}

type fakeAnalyzer struct {
	calls  int
	result Result
	err    error
}

func (analyzer *fakeAnalyzer) Analyze(context.Context, AnalysisInput) (Result, error) {
	analyzer.calls++
	return analyzer.result, analyzer.err
}

type fakeBackend struct {
	state     model.FeishuIngestionState
	newIDs    map[string]bool
	completed []model.FeishuIngestionCommit
	failed    int
	snapshot  Snapshot
}

func (backend *fakeBackend) GetFeishuIngestionState(context.Context) (*model.FeishuIngestionState, error) {
	value := backend.state
	return &value, nil
}
func (backend *fakeBackend) SetFeishuIngestionNextRun(context.Context, time.Time) error { return nil }
func (backend *fakeBackend) StartFeishuIngestionRun(_ context.Context, trigger string, start, end time.Time, _ time.Duration) (*model.FeishuIngestionRun, error) {
	return &model.FeishuIngestionRun{ID: uuid.NewString(), Trigger: trigger, WindowStart: start, WindowEnd: end}, nil
}
func (backend *fakeBackend) FailFeishuIngestionRun(context.Context, string, string, int, int) error {
	backend.failed++
	return nil
}
func (backend *fakeBackend) CompleteFeishuIngestion(_ context.Context, commit model.FeishuIngestionCommit) (*model.FeishuIngestionRun, error) {
	backend.completed = append(backend.completed, commit)
	return &model.FeishuIngestionRun{ID: commit.RunID}, nil
}
func (backend *fakeBackend) FilterNewFeishuMessageIDs(context.Context, []string) (map[string]bool, error) {
	return backend.newIDs, nil
}
func (backend *fakeBackend) IngestionSnapshot(context.Context) (Snapshot, error) {
	return backend.snapshot, nil
}

func TestWorkerDoesNotStartCodexWithoutNewHumanMessages(t *testing.T) {
	message := Message{ID: "old", SenderType: "user", Content: "already processed"}
	backend := &fakeBackend{newIDs: map[string]bool{"old": false}}
	analyzer := &fakeAnalyzer{}
	worker := NewWorker(backend, fakeCollector{result: Collection{Messages: []Message{message}, Candidates: []Message{message}, Seen: 1}}, analyzer)
	if err := worker.runWindow(context.Background(), "manual", time.Now().Add(-time.Hour), time.Now(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if analyzer.calls != 0 || len(backend.completed) != 1 || backend.completed[0].Skipped != 1 {
		t.Fatalf("unexpected empty-batch flow: calls=%d commits=%+v", analyzer.calls, backend.completed)
	}
}

func TestWorkerFailureDoesNotCompleteWindow(t *testing.T) {
	message := Message{ID: "new", SenderType: "user", Content: "new demand"}
	backend := &fakeBackend{newIDs: map[string]bool{"new": true}}
	analyzer := &fakeAnalyzer{err: errors.New("malformed JSON")}
	worker := NewWorker(backend, fakeCollector{result: Collection{Messages: []Message{message}, Candidates: []Message{message}, Seen: 1}}, analyzer)
	if err := worker.runWindow(context.Background(), "manual", time.Now().Add(-time.Hour), time.Now(), nil, nil); err == nil {
		t.Fatal("expected analyzer failure")
	}
	if backend.failed != 1 || len(backend.completed) != 0 {
		t.Fatalf("failed analysis advanced completion: failed=%d commits=%d", backend.failed, len(backend.completed))
	}
}

func TestIngestionWindowsBackfillRecoveryAndSchedule(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 8, 9, 9, 0, 0, 0, location)
	state := model.FeishuIngestionState{ActiveStart: "09:00", ActiveEnd: "23:00", IntervalMinutes: 30, OverlapMinutes: 5}
	windows, backfill := ingestionWindows(state, now)
	if !backfill || len(windows) != 7 || windows[0][0] != now.UTC().Add(-7*24*time.Hour) || windows[len(windows)-1][1] != now.UTC() {
		t.Fatalf("first-run recovery windows = %+v backfill=%v", windows, backfill)
	}
	last := now.Add(-30 * time.Minute).UTC()
	backfillAt := now.Add(-time.Hour)
	state.LastSuccessEnd = &last
	state.LastBackfillAt = &backfillAt
	windows, backfill = ingestionWindows(state, now)
	if backfill || len(windows) != 1 || windows[0][0] != last.Add(-5*time.Minute) {
		t.Fatalf("overlap window = %+v backfill=%v", windows, backfill)
	}
	if !scheduledDue(state, now) {
		t.Fatal("09:00 slot should be due when watermark is 08:30")
	}
	if next := NextScheduledRun(state, now); next != now.Add(30*time.Minute).UTC() {
		t.Fatalf("next slot = %s", next)
	}
}

func TestAnalysisBatchesAreSeparatedByConversationAndContextIsBounded(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	messages := make([]Message, 0, 140)
	for index := 0; index < 130; index++ {
		messages = append(messages, Message{ID: uuid.NewString(), ChatID: "chat-a", ThreadID: "thread-a", CreatedAt: start.Add(time.Duration(index) * time.Second)})
	}
	candidateA := messages[64]
	candidateB := Message{ID: "candidate-b", ChatID: "chat-b", CreatedAt: start.Add(time.Minute)}
	messages = append(messages, candidateB)
	batches := buildAnalysisBatches(start, time.Now(), messages, []Message{candidateA, candidateB}, Snapshot{}, nil)
	if len(batches) != 2 {
		t.Fatalf("batch count = %d", len(batches))
	}
	for _, batch := range batches {
		if len(batch.Messages) > 120 {
			t.Fatalf("context batch is not bounded: %d", len(batch.Messages))
		}
		if len(batch.Candidates) != 1 {
			t.Fatalf("conversation candidates mixed: %+v", batch.Candidates)
		}
	}
}
