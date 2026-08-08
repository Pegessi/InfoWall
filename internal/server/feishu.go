package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/infowall/infowall/internal/feishusync"
	"github.com/infowall/infowall/internal/store"
)

type feishuBackend struct {
	store *store.Store
}

func (b *feishuBackend) State(ctx context.Context) (feishusync.State, error) {
	state, err := b.store.GetFeishuSyncState(ctx)
	if err != nil {
		return feishusync.State{}, err
	}
	return feishusync.State{
		Enabled:        state.Enabled,
		DocToken:       state.DocToken,
		DocURL:         state.DocURL,
		Dirty:          state.Dirty,
		DesiredVersion: state.DesiredVersion,
		SyncedVersion:  state.SyncedVersion,
		Status:         state.Status,
		RetryCount:     state.RetryCount,
		NextRetryAt:    state.NextRetryAt,
		UpdatedAt:      state.UpdatedAt,
	}, nil
}

func (b *feishuBackend) Snapshot(ctx context.Context) (feishusync.Snapshot, error) {
	demands, err := b.store.ListDemands(ctx, store.DemandListOptions{})
	if err != nil {
		return feishusync.Snapshot{}, err
	}
	projects, err := b.store.ListProjects(ctx)
	if err != nil {
		return feishusync.Snapshot{}, err
	}
	snapshot := feishusync.Snapshot{Generated: time.Now(), Projects: make([]feishusync.ProjectView, 0, len(projects)), Demands: make([]feishusync.DemandView, 0, len(demands))}
	for _, project := range projects {
		snapshot.Projects = append(snapshot.Projects, feishusync.ProjectView{ID: project.ID, Name: project.Name})
	}
	for _, demand := range demands {
		view := feishusync.DemandView{
			ID:            demand.ID,
			Title:         demand.Title,
			Description:   demand.Description,
			Status:        string(demand.Status),
			Priority:      string(demand.Priority),
			ProjectHint:   demand.ProjectHint,
			NextAction:    demand.NextAction,
			BlockedReason: demand.BlockedReason,
			UpdatedAt:     demand.UpdatedAt,
			CompletedAt:   demand.CompletedAt,
		}
		if demand.ProjectID != nil {
			view.ProjectID = *demand.ProjectID
		}
		if len(demand.Progress) > 0 {
			view.LatestProgress = demand.Progress[len(demand.Progress)-1].Text
		}
		for i := len(demand.Sources) - 1; i >= 0; i-- {
			if demand.Sources[i].URL != "" {
				view.SourceURL = demand.Sources[i].URL
				break
			}
		}
		snapshot.Demands = append(snapshot.Demands, view)
	}
	return snapshot, nil
}

func (b *feishuBackend) MarkRunning(ctx context.Context) error {
	return b.store.MarkFeishuSyncRunning(ctx)
}

func (b *feishuBackend) Complete(ctx context.Context, desiredVersion, revision int64, hash string) error {
	return b.store.CompleteFeishuSync(ctx, desiredVersion, strconv.FormatInt(revision, 10), hash)
}

func (b *feishuBackend) Fail(ctx context.Context, message string, nextRetry *time.Time) error {
	var retryAt time.Time
	if nextRetry != nil {
		retryAt = *nextRetry
	}
	return b.store.FailFeishuSync(ctx, message, retryAt)
}

func (s *Server) handleGetFeishuDoc(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.GetFeishuSyncState(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleSetupFeishuDoc(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Create bool   `json:"create"`
		DocURL string `json:"doc_url"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	request.DocURL = strings.TrimSpace(request.DocURL)
	if request.Create == (request.DocURL != "") {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("provide exactly one of create=true or doc_url"))
		return
	}

	backend := &feishuBackend{store: s.store}
	snapshot, err := backend.Snapshot(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rendered := feishusync.Render(snapshot)
	var docToken, docURL string
	var result feishusync.SyncResult
	if request.Create {
		document, createErr := s.feishuClient.Create(r.Context(), rendered)
		if createErr != nil {
			writeErr(w, http.StatusBadGateway, createErr)
			return
		}
		docToken, docURL = document.ID, document.URL
		result = feishusync.SyncResult{Revision: document.Revision, Hash: rendered.Hash}
	} else {
		bindResult, bindErr := s.feishuClient.Bind(r.Context(), request.DocURL, rendered)
		if bindErr != nil {
			writeErr(w, http.StatusBadGateway, bindErr)
			return
		}
		docToken, docURL = documentReference(request.DocURL), request.DocURL
		result = bindResult
	}
	if err := s.store.ConfigureFeishuSync(r.Context(), docToken, docURL, true); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	state, err := s.store.GetFeishuSyncState(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.CompleteFeishuSync(r.Context(), state.DesiredVersion, strconv.FormatInt(result.Revision, 10), result.Hash); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.broadcastFeishuSyncState()
	state, err = s.store.GetFeishuSyncState(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleDisableFeishuDoc(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DisableFeishuSync(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.broadcastFeishuSyncState()
	state, err := s.store.GetFeishuSyncState(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleSyncFeishuDoc(w http.ResponseWriter, r *http.Request) {
	if err := s.syncWorker.SyncNow(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	state, err := s.store.GetFeishuSyncState(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) broadcastFeishuSyncState() {
	state, err := s.store.GetFeishuSyncState(context.Background())
	if err == nil {
		s.broadcast("feishu_sync.updated", state)
	}
}

func documentReference(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "docx" || parts[i] == "doc" {
			return parts[i+1]
		}
	}
	// Keep wiki URLs intact: lark-cli resolves wiki token to the backing object.
	return value
}

var _ feishusync.Backend = (*feishuBackend)(nil)
