package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/feed"
	"github.com/infowall/infowall/internal/model"
)

func TestWorkbenchDemandAndProjectAPI(t *testing.T) {
	srv, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "wall.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	httpSrv := httptest.NewServer(srv.mux)
	defer httpSrv.Close()
	events, cancel := srv.hub.Subscribe()
	defer cancel()

	var project model.Project
	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/projects", map[string]any{
		"name": "LLMServer", "description": "Serving work", "color": "#60a5fa",
	}, http.StatusCreated, &project)
	if project.ID == "" || project.Status != model.ProjectStatusActive {
		t.Fatalf("unexpected project: %+v", project)
	}
	assertEvent(t, events, "project.created")
	var demand model.Demand
	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/demands", map[string]any{
		"title":       "Investigate decode starvation",
		"description": "Align stacks and throughput",
		"priority":    "p0",
		"project_id":  project.ID,
		"sources": []map[string]any{{
			"kind": "feishu-chat", "dedupe_key": "chat:c1:m1", "excerpt": "please check",
		}},
	}, http.StatusCreated, &demand)
	if demand.ID == "" || demand.ProjectID == nil || *demand.ProjectID != project.ID {
		t.Fatalf("unexpected demand: %+v", demand)
	}
	assertEvent(t, events, "demand.created")

	var list struct {
		Demands []model.Demand `json:"demands"`
	}
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/demands?q=decode&project_id="+project.ID,
		nil, http.StatusOK, &list)
	if len(list.Demands) != 1 || list.Demands[0].ID != demand.ID {
		t.Fatalf("filtered list = %+v", list.Demands)
	}

	var progress model.Progress
	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/demands/"+demand.ID+"/progress",
		map[string]any{"text": "A/B is running", "source": map[string]any{
			"kind": "feishu-im", "dedupe_key": "chat:c1:m2", "excerpt": "latest update",
		}}, http.StatusCreated, &progress)
	if progress.Text != "A/B is running" || progress.DemandID != demand.ID {
		t.Fatalf("unexpected progress: %+v", progress)
	}
	assertEvent(t, events, "demand.progress")
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/demands/"+demand.ID, nil, http.StatusOK, &demand)
	if len(demand.Sources) != 2 {
		t.Fatalf("progress source was not saved atomically: %+v", demand.Sources)
	}

	requestJSON(t, http.MethodPatch, httpSrv.URL+"/api/demands/"+demand.ID,
		map[string]any{"status": "dismissed"}, http.StatusOK, &demand)
	assertEvent(t, events, "demand.updated")
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/demands", nil, http.StatusOK, &list)
	if len(list.Demands) != 0 {
		t.Fatalf("default list includes dismissed demand: %+v", list.Demands)
	}
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/demands?include_dismissed=true", nil, http.StatusOK, &list)
	if len(list.Demands) != 1 {
		t.Fatalf("include dismissed returned %d", len(list.Demands))
	}

	requestJSON(t, http.MethodPatch, httpSrv.URL+"/api/projects/"+project.ID,
		map[string]any{"status": "archived"}, http.StatusOK, &project)
	if project.Status != model.ProjectStatusArchived {
		t.Fatalf("project was not archived: %+v", project)
	}
	assertEvent(t, events, "project.updated")
}

func TestDemandImportAPIReportsCreatedUpdatedSkipped(t *testing.T) {
	srv, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "wall.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	httpSrv := httptest.NewServer(srv.mux)
	defer httpSrv.Close()

	importBody := func(description string) map[string]any {
		return map[string]any{"demands": []map[string]any{{
			"title": "整理近期需求", "description": description,
			"project_id": "unknown-project",
			"sources":    []map[string]any{{"kind": "feishu-chat", "dedupe_key": "chat:x:y", "excerpt": description}},
		}}}
	}
	type importResponse struct {
		Created int `json:"created"`
		Updated int `json:"updated"`
		Skipped int `json:"skipped"`
		Results []struct {
			Action string       `json:"action"`
			Demand model.Demand `json:"demand"`
		} `json:"results"`
	}
	var first importResponse
	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/demands/import", importBody("first"), http.StatusOK, &first)
	if first.Created != 1 || len(first.Results) != 1 || first.Results[0].Demand.ProjectID != nil ||
		first.Results[0].Demand.ProjectHint != "unknown-project" {
		t.Fatalf("first import = %+v", first)
	}
	var repeated importResponse
	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/demands/import", importBody("first"), http.StatusOK, &repeated)
	if repeated.Skipped != 1 {
		t.Fatalf("repeated import = %+v", repeated)
	}
	changedBody := importBody("changed")
	changedDemand := changedBody["demands"].([]map[string]any)[0]
	changedDemand["priority"] = "p0"
	changedDemand["project_id"] = "another-project"
	changedDemand["sources"] = append(changedDemand["sources"].([]map[string]any),
		map[string]any{"kind": "feishu-chat", "dedupe_key": "chat:x:z", "excerpt": "later"})
	changedDemand["progress"] = []map[string]any{{"text": "received logs", "created_at": "2026-08-08T12:00:00Z"}}
	var changed importResponse
	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/demands/import", changedBody, http.StatusOK, &changed)
	if changed.Updated != 1 || changed.Results[0].Demand.Description != "first" ||
		changed.Results[0].Demand.Priority != model.DemandPriorityNone ||
		len(changed.Results[0].Demand.Sources) != 2 || len(changed.Results[0].Demand.Progress) != 1 {
		t.Fatalf("changed import = %+v", changed)
	}
}

func TestWorkbenchAPIValidationAndNotFound(t *testing.T) {
	srv, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "wall.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	httpSrv := httptest.NewServer(srv.mux)
	defer httpSrv.Close()

	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/demands", map[string]any{"title": ""}, http.StatusBadRequest, nil)
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/demands?status=unknown", nil, http.StatusBadRequest, nil)
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/demands/missing", nil, http.StatusNotFound, nil)
	requestJSON(t, http.MethodPatch, httpSrv.URL+"/api/projects/missing", map[string]any{"status": "archived"}, http.StatusNotFound, nil)
}

func requestJSON(t *testing.T, method, endpoint string, body any, wantStatus int, destination any) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request, err := http.NewRequest(method, endpoint, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		var errorPayload any
		_ = json.NewDecoder(response.Body).Decode(&errorPayload)
		t.Fatalf("%s %s status = %d, want %d: %#v", method, endpoint, response.StatusCode, wantStatus, errorPayload)
	}
	if destination != nil {
		if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
			t.Fatal(err)
		}
	}
}

func assertEvent(t *testing.T, events <-chan feed.Event, name string) {
	t.Helper()
	select {
	case event := <-events:
		if event.Name != name {
			t.Fatalf("event = %q, want %q", event.Name, name)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for event %q", name)
	}
}
