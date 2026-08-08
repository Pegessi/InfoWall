package feishusync

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type fakeBackend struct {
	state      State
	snapshot   Snapshot
	running    int
	completed  int
	failed     int
	completion SyncResult
}

func (b *fakeBackend) State(context.Context) (State, error) { return b.state, nil }
func (b *fakeBackend) Snapshot(context.Context) (Snapshot, error) {
	return b.snapshot, nil
}
func (b *fakeBackend) MarkRunning(context.Context) error { b.running++; return nil }
func (b *fakeBackend) Complete(_ context.Context, _, revision int64, hash string) error {
	b.completed++
	b.completion = SyncResult{Revision: revision, Hash: hash}
	return nil
}
func (b *fakeBackend) Fail(context.Context, string, *time.Time) error { b.failed++; return nil }

func TestWorkerSkipsDuringDebounce(t *testing.T) {
	backend := &fakeBackend{state: State{Enabled: true, DocToken: "doc", Dirty: true, UpdatedAt: time.Now()}}
	worker := NewWorker(backend, Client{Runner: &scriptedRunner{}})
	if err := worker.sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if backend.running != 0 {
		t.Fatal("sync ran during debounce")
	}
}

func TestWorkerStopsAutomaticRetryAfterFiveFailures(t *testing.T) {
	backend := &fakeBackend{state: State{Enabled: true, DocToken: "doc", Dirty: true, RetryCount: 5, UpdatedAt: time.Now().Add(-time.Hour)}}
	worker := NewWorker(backend, Client{Runner: &scriptedRunner{}})
	if err := worker.sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if backend.running != 0 {
		t.Fatal("automatic sync ran after retry cap")
	}
}

func TestWorkerRecordsClientFailure(t *testing.T) {
	backend := &fakeBackend{state: State{Enabled: true, DocToken: "doc", Dirty: true, UpdatedAt: time.Now().Add(-time.Hour)}}
	runner := runnerFunc(func(context.Context, string, ...string) ([]byte, error) { return nil, fmt.Errorf("auth expired") })
	worker := NewWorker(backend, Client{Runner: runner})
	err := worker.sync(context.Background(), false)
	if err == nil || backend.failed != 1 || backend.running != 1 {
		t.Fatalf("err=%v failed=%d running=%d", err, backend.failed, backend.running)
	}
}

type runnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f runnerFunc) Run(ctx context.Context, input string, args ...string) ([]byte, error) {
	return f(ctx, input, args...)
}
