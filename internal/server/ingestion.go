package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/infowall/infowall/internal/feishuingest"
	"github.com/infowall/infowall/internal/model"
	"github.com/infowall/infowall/internal/store"
)

type ingestionBackend struct {
	store *store.Store
}

func (b *ingestionBackend) GetFeishuIngestionState(ctx context.Context) (*model.FeishuIngestionState, error) {
	return b.store.GetFeishuIngestionState(ctx)
}

func (b *ingestionBackend) SetFeishuIngestionNextRun(ctx context.Context, next time.Time) error {
	return b.store.SetFeishuIngestionNextRun(ctx, next)
}

func (b *ingestionBackend) StartFeishuIngestionRun(ctx context.Context, trigger string, start, end time.Time, lease time.Duration) (*model.FeishuIngestionRun, error) {
	return b.store.StartFeishuIngestionRun(ctx, trigger, start, end, lease)
}

func (b *ingestionBackend) RenewFeishuIngestionLease(ctx context.Context, runID string, lease time.Duration) error {
	return b.store.RenewFeishuIngestionLease(ctx, runID, lease)
}

func (b *ingestionBackend) FailFeishuIngestionRun(ctx context.Context, runID, message string, seen, candidates int, inputTokens, cachedInputTokens, outputTokens int64) error {
	return b.store.FailFeishuIngestionRun(ctx, runID, message, seen, candidates, inputTokens, cachedInputTokens, outputTokens)
}

func (b *ingestionBackend) CompleteFeishuIngestion(ctx context.Context, commit model.FeishuIngestionCommit) (*model.FeishuIngestionRun, error) {
	return b.store.CompleteFeishuIngestion(ctx, commit)
}

func (b *ingestionBackend) FilterNewFeishuMessageIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	return b.store.FilterNewFeishuMessageIDs(ctx, ids)
}

func (b *ingestionBackend) IngestionSnapshot(ctx context.Context) (feishuingest.Snapshot, error) {
	demands, err := b.store.ListDemands(ctx, store.DemandListOptions{IncludeDismissed: true})
	if err != nil {
		return feishuingest.Snapshot{}, err
	}
	projects, err := b.store.ListProjects(ctx)
	if err != nil {
		return feishuingest.Snapshot{}, err
	}
	// Keep the model input bounded: full evidence history is not needed for
	// matching, while the most recent progress is useful as a semantic anchor.
	for _, demand := range demands {
		if len(demand.Sources) > 3 {
			demand.Sources = demand.Sources[len(demand.Sources)-3:]
		}
		if len(demand.Progress) > 3 {
			demand.Progress = demand.Progress[len(demand.Progress)-3:]
		}
	}
	return feishuingest.Snapshot{Demands: demands, Projects: projects}, nil
}

func (s *Server) handleGetFeishuChat(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.GetFeishuIngestionState(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

type feishuChatPatch struct {
	Enabled         *bool     `json:"enabled"`
	Timezone        *string   `json:"timezone"`
	ActiveStart     *string   `json:"active_start"`
	ActiveEnd       *string   `json:"active_end"`
	IntervalMinutes *int      `json:"interval_minutes"`
	OverlapMinutes  *int      `json:"overlap_minutes"`
	ExcludedChatIDs *[]string `json:"excluded_chat_ids"`
	ResumeFrom      *string   `json:"resume_from"`
}

func (s *Server) handlePatchFeishuChat(w http.ResponseWriter, r *http.Request) {
	var patch feishuChatPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	state, err := s.store.GetFeishuIngestionState(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if patch.Enabled != nil {
		state.Enabled = *patch.Enabled
	}
	if patch.Timezone != nil {
		state.Timezone = strings.TrimSpace(*patch.Timezone)
	}
	if patch.ActiveStart != nil {
		state.ActiveStart = strings.TrimSpace(*patch.ActiveStart)
	}
	if patch.ActiveEnd != nil {
		state.ActiveEnd = strings.TrimSpace(*patch.ActiveEnd)
	}
	if patch.IntervalMinutes != nil {
		state.IntervalMinutes = *patch.IntervalMinutes
	}
	if patch.OverlapMinutes != nil {
		state.OverlapMinutes = *patch.OverlapMinutes
	}
	if patch.ExcludedChatIDs != nil {
		state.ExcludedChatIDs = *patch.ExcludedChatIDs
	}
	var resumeFrom *time.Time
	if patch.ResumeFrom != nil {
		resumeAt, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(*patch.ResumeFrom))
		if parseErr != nil {
			writeErr(w, http.StatusBadRequest, errors.New("resume_from must be an RFC3339 timestamp"))
			return
		}
		resumeFrom = &resumeAt
	}
	updated, err := s.store.ConfigureFeishuIngestion(r.Context(), *state, resumeFrom)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if updated.Enabled {
		s.ingestWorker.Wake()
	}
	s.broadcast("feishu_ingestion.updated", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleScanFeishuChat(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.RequestFeishuIngestion(r.Context())
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	s.ingestWorker.Wake()
	s.broadcast("feishu_ingestion.updated", state)
	writeJSON(w, http.StatusAccepted, state)
}

func (s *Server) handleListFeishuChatRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.store.ListFeishuIngestionRuns(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleListDemandReviews(w http.ResponseWriter, r *http.Request) {
	reviews, err := s.store.ListDemandReviews(r.Context(), strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": reviews})
}

func (s *Server) handleAcceptDemandReview(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DemandID string `json:"demand_id"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &request); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	review, err := s.store.ResolveDemandReview(r.Context(), r.PathValue("id"), "accept", request.DemandID)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	s.broadcast("demand_review.accepted", review)
	s.broadcast("demand.progress", map[string]any{"demand_id": review.SuggestedDemandID})
	writeJSON(w, http.StatusOK, review)
}

func (s *Server) handleDismissDemandReview(w http.ResponseWriter, r *http.Request) {
	review, err := s.store.ResolveDemandReview(r.Context(), r.PathValue("id"), "dismiss", "")
	if err != nil {
		writeReviewError(w, err)
		return
	}
	s.broadcast("demand_review.dismissed", review)
	writeJSON(w, http.StatusOK, review)
}

func writeReviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, err)
	default:
		writeErr(w, http.StatusBadRequest, err)
	}
}

func (s *Server) broadcastFeishuIngestionState() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if state, err := s.store.GetFeishuIngestionState(ctx); err == nil {
		s.broadcast("feishu_ingestion.updated", state)
	}
	if reviews, err := s.store.ListDemandReviews(ctx, "pending"); err == nil {
		s.broadcast("demand_review.updated", map[string]any{"count": len(reviews)})
	}
}
