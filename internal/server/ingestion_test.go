package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/feishuingest"
	"github.com/infowall/infowall/internal/model"
)

type emptyIngestionCollector struct{}

func (emptyIngestionCollector) Collect(context.Context, time.Time, time.Time, []string) (feishuingest.Collection, error) {
	return feishuingest.Collection{}, nil
}

type noCallAnalyzer struct{}

func (noCallAnalyzer) Analyze(context.Context, []feishuingest.AnalysisInput) (feishuingest.Result, error) {
	return feishuingest.Result{}, nil
}

func TestFeishuChatIntegrationAndDemandReviewAPI(t *testing.T) {
	srv, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "wall.db"),
		IngestionCollector: emptyIngestionCollector{}, IngestionAnalyzer: noCallAnalyzer{}})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	httpSrv := httptest.NewServer(srv.mux)
	defer httpSrv.Close()

	var state model.FeishuIngestionState
	requestJSON(t, http.MethodPatch, httpSrv.URL+"/api/integrations/feishu-chat", map[string]any{
		"enabled": true, "timezone": "Asia/Shanghai", "active_start": "09:00", "active_end": "23:00",
		"interval_minutes": 30, "overlap_minutes": 5, "excluded_chat_ids": []string{"oc_skip"},
	}, http.StatusOK, &state)
	if !state.Enabled || len(state.ExcludedChatIDs) != 1 || state.ExcludedChatIDs[0] != "oc_skip" {
		t.Fatalf("unexpected ingestion state: %+v", state)
	}
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/integrations/feishu-chat", nil, http.StatusOK, &state)

	demand, err := srv.store.CreateDemand(context.Background(), &model.Demand{Title: "修复动态热更新", Sources: []model.Source{{Kind: "manual", DedupeKey: "manual:hot"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Wait for a queued empty collection to release its short lease, if the PATCH
	// wake raced this setup, then create a review through the same transactional path.
	deadline := time.Now().Add(2 * time.Second)
	var run *model.FeishuIngestionRun
	for time.Now().Before(deadline) {
		run, err = srv.store.StartFeishuIngestionRun(context.Background(), "test", time.Now().Add(-time.Hour), time.Now(), time.Minute)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	_, err = srv.store.CompleteFeishuIngestion(context.Background(), model.FeishuIngestionCommit{RunID: run.ID,
		WindowEnd: time.Now(), Reviews: []model.DemandReview{{Kind: "progress", SuggestedDemandID: demand.ID,
			ProgressText: "灰度完成", ProgressDedupeKey: "feishu-progress:om_review:" + demand.ID,
			Source: model.Source{Kind: "feishu-im", ExternalID: "om_review", DedupeKey: "feishu-im:om_review:0"}, Confidence: 0.7}}})
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Reviews []model.DemandReview `json:"reviews"`
	}
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/demand-reviews?status=pending", nil, http.StatusOK, &list)
	if len(list.Reviews) != 1 {
		t.Fatalf("review list = %+v", list.Reviews)
	}
	var accepted model.DemandReview
	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/demand-reviews/"+list.Reviews[0].ID+"/accept",
		map[string]any{"demand_id": demand.ID}, http.StatusOK, &accepted)
	if accepted.Status != "accepted" {
		t.Fatalf("accepted review = %+v", accepted)
	}

	requestJSON(t, http.MethodPost, httpSrv.URL+"/api/integrations/feishu-chat/scan", nil, http.StatusAccepted, &state)
	var runs struct {
		Runs []model.FeishuIngestionRun `json:"runs"`
	}
	requestJSON(t, http.MethodGet, httpSrv.URL+"/api/integrations/feishu-chat/runs", nil, http.StatusOK, &runs)
	if len(runs.Runs) == 0 {
		t.Fatal("expected ingestion run history")
	}
}
