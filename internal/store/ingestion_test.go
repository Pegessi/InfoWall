package store

import (
	"context"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/model"
)

func enableIngestion(t *testing.T, st *Store) model.FeishuIngestionState {
	t.Helper()
	state, err := st.ConfigureFeishuIngestion(context.Background(), model.FeishuIngestionState{
		Enabled: true, Timezone: "Asia/Shanghai", ActiveStart: "09:00", ActiveEnd: "23:00",
		IntervalMinutes: 30, OverlapMinutes: 5, ExcludedChatIDs: []string{},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return *state
}

func TestFeishuIngestionCommitIsTransactionalAndRetrySafe(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	enableIngestion(t, st)
	end := time.Now().UTC().Truncate(time.Second)
	run, err := st.StartFeishuIngestionRun(ctx, "manual", end.Add(-time.Hour), end, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	demand := &model.Demand{Title: "修复 prefill only 首次分配失败后的安全重试",
		Description: "首次 prefill 分配失败需要在可见输出前安全重放。", NextAction: "补齐失败注入测试",
		ProjectHint: "Server", Sources: []model.Source{{Kind: "feishu-im", ExternalID: "om_1",
			DedupeKey: "feishu-im:om_1:0", Excerpt: "prefill only 任务需要重试", MessageTime: end}}}
	completed, err := st.CompleteFeishuIngestion(ctx, model.FeishuIngestionCommit{RunID: run.ID,
		WindowEnd: end, MessagesSeen: 1, MessagesCandidate: 1,
		ProcessedMessageIDs: []string{"om_1"}, NewDemands: []*model.Demand{demand}})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Created != 1 || completed.Status != "success" {
		t.Fatalf("unexpected completed run: %+v", completed)
	}
	state, _ := st.GetFeishuIngestionState(ctx)
	if state.LastSuccessEnd == nil || !state.LastSuccessEnd.Equal(end) {
		t.Fatalf("watermark was not committed: %+v", state)
	}
	newIDs, err := st.FilterNewFeishuMessageIDs(ctx, []string{"om_1", "om_2"})
	if err != nil || newIDs["om_1"] || !newIDs["om_2"] {
		t.Fatalf("seen-message filter = %+v err=%v", newIDs, err)
	}
	run2, err := st.StartFeishuIngestionRun(ctx, "manual", end.Add(-time.Hour), end, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	completed, err = st.CompleteFeishuIngestion(ctx, model.FeishuIngestionCommit{RunID: run2.ID,
		WindowEnd: end, MessagesSeen: 1, ProcessedMessageIDs: []string{"om_1"}, NewDemands: []*model.Demand{demand}})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Created != 0 || completed.Skipped != 1 {
		t.Fatalf("repeat created duplicate: %+v", completed)
	}
	demands, _ := st.ListDemands(ctx, DemandListOptions{})
	if len(demands) != 1 || demands[0].Priority != model.DemandPriorityNone || demands[0].Status != model.DemandStatusPending || demands[0].ProjectID != nil {
		t.Fatalf("new automated demand invariants lost: %+v", demands)
	}
}

func TestFeishuIngestionResumePointIsOneTime(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	resume := time.Date(2026, 8, 8, 16, 25, 12, 0, time.UTC)
	state, err := st.ConfigureFeishuIngestion(ctx, model.FeishuIngestionState{Enabled: true,
		Timezone: "Asia/Shanghai", ActiveStart: "09:00", ActiveEnd: "23:00",
		IntervalMinutes: 30, OverlapMinutes: 5}, &resume)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastSuccessEnd == nil || !state.LastSuccessEnd.Equal(resume) || state.LastBackfillAt == nil {
		t.Fatalf("resume point was not saved: %+v", state)
	}
	if _, err := st.ConfigureFeishuIngestion(ctx, *state, &resume); err == nil {
		t.Fatal("second resume point should be rejected")
	}
	if _, err := st.ConfigureFeishuIngestion(ctx, *state, nil); err != nil {
		t.Fatalf("ordinary config update should preserve watermark: %v", err)
	}
}

func TestFeishuIngestionFailureDoesNotAdvanceWatermark(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	enableIngestion(t, st)
	end := time.Now().UTC()
	run, err := st.StartFeishuIngestionRun(ctx, "manual", end.Add(-time.Hour), end, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CompleteFeishuIngestion(ctx, model.FeishuIngestionCommit{RunID: run.ID,
		WindowEnd: end, NewDemands: []*model.Demand{{Title: "", Sources: []model.Source{{Kind: "feishu-im", DedupeKey: "feishu-im:bad:0"}}}}})
	if err == nil {
		t.Fatal("expected invalid demand to abort transaction")
	}
	state, _ := st.GetFeishuIngestionState(ctx)
	if state.LastSuccessEnd != nil {
		t.Fatalf("failed transaction advanced watermark: %+v", state)
	}
	demands, _ := st.ListDemands(ctx, DemandListOptions{})
	if len(demands) != 0 {
		t.Fatalf("failed transaction left demands: %+v", demands)
	}
}

func TestFeishuIngestionLeaseRenewalAndDisabledFailureState(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	state := enableIngestion(t, st)
	end := time.Now().UTC()
	run, err := st.StartFeishuIngestionRun(ctx, "manual", end.Add(-time.Hour), end, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RenewFeishuIngestionLease(ctx, run.ID, 20*time.Minute); err != nil {
		t.Fatal(err)
	}
	renewed, err := st.GetFeishuIngestionState(ctx)
	if err != nil || renewed.LeaseUntil == nil || renewed.LeaseUntil.Before(time.Now().UTC().Add(19*time.Minute)) {
		t.Fatalf("lease was not renewed: state=%+v err=%v", renewed, err)
	}
	state.Enabled = false
	if _, err := st.ConfigureFeishuIngestion(ctx, state, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.FailFeishuIngestionRun(ctx, run.ID, "cancelled while disabled", 1, 1, 123, 100, 7); err != nil {
		t.Fatal(err)
	}
	disabled, err := st.GetFeishuIngestionState(ctx)
	if err != nil || disabled.Status != "disabled" || disabled.CurrentRunID != "" {
		t.Fatalf("disabled failure state = %+v err=%v", disabled, err)
	}
	runs, err := st.ListFeishuIngestionRuns(ctx, 1)
	if err != nil || len(runs) != 1 || runs[0].InputTokens != 123 || runs[0].CachedInputTokens != 100 || runs[0].OutputTokens != 7 {
		t.Fatalf("failed run usage = %+v err=%v", runs, err)
	}
}

func TestAutomaticProgressConfidenceGateAndReviewResolution(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	enableIngestion(t, st)
	demand, err := st.CreateDemand(ctx, &model.Demand{Title: "Arnold 部署服务动态热更新", Sources: []model.Source{{Kind: "manual", DedupeKey: "manual:hot-update"}}})
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now().UTC()
	run, _ := st.StartFeishuIngestionRun(ctx, "manual", end.Add(-time.Hour), end, time.Minute)
	update := model.DemandProgressUpdate{DemandID: demand.ID, Text: "已完成首轮灰度", DedupeKey: "feishu-progress:om_2:" + demand.ID,
		Source:     model.Source{Kind: "feishu-im", ExternalID: "om_2", DedupeKey: "feishu-im:om_2:0", Excerpt: "首轮灰度完成"},
		Confidence: 0.7, Anchors: []string{"Arnold"}}
	completed, err := st.CompleteFeishuIngestion(ctx, model.FeishuIngestionCommit{RunID: run.ID, WindowEnd: end,
		MessagesCandidate: 1, ProcessedMessageIDs: []string{"om_2"}, ProgressUpdates: []model.DemandProgressUpdate{update}})
	if err != nil {
		t.Fatal(err)
	}
	if completed.ReviewCount != 1 || completed.Updated != 0 {
		t.Fatalf("low-confidence update bypassed review: %+v", completed)
	}
	reviews, err := st.ListDemandReviews(ctx, "pending")
	if err != nil || len(reviews) != 1 {
		t.Fatalf("reviews=%+v err=%v", reviews, err)
	}
	resolved, err := st.ResolveDemandReview(ctx, reviews[0].ID, "accept", demand.ID)
	if err != nil || resolved.Status != "accepted" {
		t.Fatalf("resolve=%+v err=%v", resolved, err)
	}
	updatedDemand, _ := st.GetDemand(ctx, demand.ID)
	if len(updatedDemand.Progress) != 1 || updatedDemand.Progress[0].DedupeKey != update.DedupeKey || len(updatedDemand.Sources) != 2 {
		t.Fatalf("review evidence/progress was not atomically appended: %+v", updatedDemand)
	}
}
