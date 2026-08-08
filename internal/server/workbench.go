package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/infowall/infowall/internal/model"
	"github.com/infowall/infowall/internal/store"
)

func (s *Server) handleListDemands(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	status := model.DemandStatus(strings.TrimSpace(query.Get("status")))
	if status != "" && !status.Valid() {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid demand status %q", status))
		return
	}
	demands, err := s.store.ListDemands(r.Context(), store.DemandListOptions{
		Status:           status,
		ProjectID:        strings.TrimSpace(query.Get("project_id")),
		Query:            strings.TrimSpace(query.Get("q")),
		IncludeDismissed: boolQuery(query.Get("include_dismissed")),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"demands": demands})
}

func (s *Server) handleCreateDemand(w http.ResponseWriter, r *http.Request) {
	var demand model.Demand
	if err := decodeJSON(r, &demand); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	created, err := s.store.CreateDemand(r.Context(), &demand)
	if err != nil {
		writeWorkbenchMutationError(w, err)
		return
	}
	s.broadcast("demand.created", created)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleImportDemands(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Demands []*model.Demand `json:"demands"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if request.Demands == nil {
		writeErr(w, http.StatusBadRequest, errors.New("demands array is required"))
		return
	}
	result, err := s.store.ImportDemands(r.Context(), request.Demands)
	if err != nil {
		writeWorkbenchMutationError(w, err)
		return
	}
	for _, imported := range result.Results {
		switch imported.Action {
		case "created":
			s.broadcast("demand.created", imported.Demand)
		case "updated":
			s.broadcast("demand.updated", imported.Demand)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetDemand(w http.ResponseWriter, r *http.Request) {
	demand, err := s.store.GetDemand(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWorkbenchReadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, demand)
}

type demandPatchRequest struct {
	Title         *string               `json:"title"`
	Description   *string               `json:"description"`
	Status        *model.DemandStatus   `json:"status"`
	Priority      *model.DemandPriority `json:"priority"`
	ProjectID     json.RawMessage       `json:"project_id"`
	ProjectHint   *string               `json:"project_hint"`
	NextAction    *string               `json:"next_action"`
	BlockedReason *string               `json:"blocked_reason"`
}

func (s *Server) handlePatchDemand(w http.ResponseWriter, r *http.Request) {
	var request demandPatchRequest
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	projectIDSet, projectID, err := parseNullableString(request.ProjectID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id: %w", err))
		return
	}
	updated, err := s.store.UpdateDemand(r.Context(), r.PathValue("id"), store.DemandUpdate{
		Title:         request.Title,
		Description:   request.Description,
		Status:        request.Status,
		Priority:      request.Priority,
		ProjectIDSet:  projectIDSet,
		ProjectID:     projectID,
		ProjectHint:   request.ProjectHint,
		NextAction:    request.NextAction,
		BlockedReason: request.BlockedReason,
	})
	if err != nil {
		writeWorkbenchMutationError(w, err)
		return
	}
	s.broadcast("demand.updated", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleAddDemandProgress(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Text   string        `json:"text"`
		Source *model.Source `json:"source"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	progress, err := s.store.AddDemandProgressWithSource(r.Context(), r.PathValue("id"), request.Text, request.Source)
	if err != nil {
		writeWorkbenchMutationError(w, err)
		return
	}
	if demand, getErr := s.store.GetDemand(r.Context(), r.PathValue("id")); getErr == nil {
		s.broadcast("demand.progress", demand)
	} else {
		s.broadcast("demand.progress", progress)
	}
	writeJSON(w, http.StatusCreated, progress)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var project model.Project
	if err := decodeJSON(r, &project); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	created, err := s.store.CreateProject(r.Context(), &project)
	if err != nil {
		writeWorkbenchMutationError(w, err)
		return
	}
	s.broadcast("project.created", created)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWorkbenchReadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handlePatchProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name        *string              `json:"name"`
		Description *string              `json:"description"`
		Color       *string              `json:"color"`
		Status      *model.ProjectStatus `json:"status"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	updated, err := s.store.UpdateProject(r.Context(), r.PathValue("id"), store.ProjectUpdate{
		Name: request.Name, Description: request.Description, Color: request.Color, Status: request.Status,
	})
	if err != nil {
		writeWorkbenchMutationError(w, err)
		return
	}
	s.broadcast("project.updated", updated)
	writeJSON(w, http.StatusOK, updated)
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("invalid JSON body: multiple values")
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func parseNullableString(raw json.RawMessage) (bool, *string, error) {
	if len(raw) == 0 {
		return false, nil, nil
	}
	if string(raw) == "null" {
		return true, nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return true, nil, errors.New("must be a string or null")
	}
	return true, &value, nil
}

func boolQuery(value string) bool {
	return value == "1" || strings.EqualFold(strings.TrimSpace(value), "true")
}

func writeWorkbenchReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

func writeWorkbenchMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, err)
	default:
		writeErr(w, http.StatusBadRequest, err)
	}
}
