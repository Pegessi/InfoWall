package feishusync

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type State struct {
	Enabled        bool
	DocToken       string
	DocURL         string
	Dirty          bool
	DesiredVersion int64
	SyncedVersion  int64
	Status         string
	RetryCount     int
	NextRetryAt    *time.Time
	UpdatedAt      time.Time
}

type Backend interface {
	State(ctx context.Context) (State, error)
	Snapshot(ctx context.Context) (Snapshot, error)
	MarkRunning(ctx context.Context) error
	Complete(ctx context.Context, desiredVersion, revision int64, hash string) error
	Fail(ctx context.Context, message string, nextRetry *time.Time) error
}

type Worker struct {
	Backend  Backend
	Client   Client
	Debounce time.Duration
	OnUpdate func()

	wake chan struct{}
	mu   sync.Mutex
}

func NewWorker(backend Backend, client Client) *Worker {
	return &Worker{
		Backend:  backend,
		Client:   client,
		Debounce: 2 * time.Second,
		wake:     make(chan struct{}, 1),
	}
}

func (w *Worker) Notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
		_ = w.sync(ctx, false)
	}
}

func (w *Worker) SyncNow(ctx context.Context) error {
	return w.sync(ctx, true)
}

func (w *Worker) sync(ctx context.Context, force bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Backend == nil {
		return fmt.Errorf("Feishu sync backend is not configured")
	}
	state, err := w.Backend.State(ctx)
	if err != nil {
		return err
	}
	if !state.Enabled || state.DocToken == "" {
		if force {
			return fmt.Errorf("Feishu document sync is not configured")
		}
		return nil
	}
	if !force {
		if !state.Dirty {
			return nil
		}
		if state.RetryCount >= 5 {
			return nil
		}
		now := time.Now()
		if state.NextRetryAt != nil && now.Before(*state.NextRetryAt) {
			return nil
		}
		debounce := w.Debounce
		if debounce <= 0 {
			debounce = 2 * time.Second
		}
		if !state.UpdatedAt.IsZero() && now.Before(state.UpdatedAt.Add(debounce)) {
			return nil
		}
	}

	if err := w.Backend.MarkRunning(ctx); err != nil {
		return err
	}
	w.updated()
	snapshot, err := w.Backend.Snapshot(ctx)
	if err != nil {
		return w.recordFailure(ctx, state, err)
	}
	rendered := Render(snapshot)
	result, err := w.Client.SyncManagedSection(ctx, state.DocToken, rendered)
	if err != nil {
		return w.recordFailure(ctx, state, err)
	}
	if err := w.Backend.Complete(ctx, state.DesiredVersion, result.Revision, result.Hash); err != nil {
		return err
	}
	w.updated()
	return nil
}

func (w *Worker) recordFailure(ctx context.Context, state State, cause error) error {
	retry := state.RetryCount + 1
	var next *time.Time
	if retry < 5 {
		delays := []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second, 2 * time.Minute, 5 * time.Minute}
		when := time.Now().Add(delays[retry-1])
		next = &when
	}
	if err := w.Backend.Fail(ctx, cause.Error(), next); err != nil {
		return fmt.Errorf("sync failed: %v; record failure: %w", cause, err)
	}
	w.updated()
	return cause
}

func (w *Worker) updated() {
	if w.OnUpdate != nil {
		w.OnUpdate()
	}
}
