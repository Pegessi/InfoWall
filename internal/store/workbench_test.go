package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/model"
)

func TestV1ToCurrentMigrationPreservesItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO items
		(id, type, title, tags, meta, body, raw, pinned, created_at)
		VALUES ('keep-me', 'note', 'preserved', '[]', '{}', 'body', 'raw', 1, ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	item, err := st.Get(context.Background(), "keep-me")
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "preserved" || item.Raw != "raw" || !item.Pinned {
		t.Fatalf("v1 item changed during migration: %+v", item)
	}
	if got := userVersion(t, path); got != schemaVersion {
		t.Fatalf("user_version = %d, want %d", got, schemaVersion)
	}
	for _, table := range []string{"demands", "projects", "demand_sources", "demand_progress", "demand_progress_links", "feishu_sync_state", "app_settings", "feishu_ingestion_state", "feishu_ingestion_runs", "feishu_ingestion_seen", "demand_reviews"} {
		var count int
		if err := st.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("migration did not create table %s", table)
		}
	}
}

func TestV4MigrationBackfillsNamedProgressLinksFromEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v4.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{schema, workbenchSchema, appSettingsSchema, feishuIngestionSchema} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO demands
		(id,title,description,status,priority,created_at,updated_at) VALUES ('d1','VLM serving','', 'active','p1',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	sources := []struct{ id, kind, external, url, excerpt, key string }{
		{"s1", "model-card", "mc30b", "https://example.test/model-card/30b", "30B VLM Model Card", "model-card:mc30b"},
		{"s2", "seed-jobrun", "2edd6e5c33e4baca:394541347", "https://example.test/jobrun/2edd6e5c33e4baca?trialId=394541347", "RUNNING · MIX Serving", "seed-jobrun:2edd6e5c33e4baca:394541347"},
		{"s3", "arena-eval", "ddfwxx6xox6a780f81", "https://example.test/arena/ddfwxx6xox6a780f81", "RUNNING · 长上下文对照", "arena-eval:ddfwxx6xox6a780f81"},
	}
	for _, source := range sources {
		if _, err := db.Exec(`INSERT INTO demand_sources
			(id,demand_id,kind,external_id,url,excerpt,dedupe_key,created_at) VALUES (?, 'd1', ?, ?, ?, ?, ?, ?)`,
			source.id, source.kind, source.external, source.url, source.excerpt, source.key, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO demand_progress
		(id,demand_id,text,dedupe_key,created_at) VALUES
		('p1','d1','已用 30B Model Card 拉起 MIX 服务：JobRun 2edd6e5c33e4baca / Trial 394541347','seed-jobrun:2edd6e5c33e4baca:394541347:ready',?),
		('p2','d1','已从源评测 fork Arena ddfwxx6xox6a780f81','arena-eval:ddfwxx6xox6a780f81:forked',?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 4`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	demand, err := st.GetDemand(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(demand.Progress) != 2 || len(demand.Progress[0].Links) != 2 || len(demand.Progress[1].Links) != 1 {
		t.Fatalf("historical progress links were not backfilled: %+v", demand.Progress)
	}
	if demand.Progress[0].Links[0].URL == "" || demand.Progress[1].Links[0].Title == "" {
		t.Fatalf("backfilled links are not directly usable: %+v", demand.Progress)
	}
}

func TestDemandProgressPersistsStructuredAndInlineLinks(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	demand, err := st.CreateDemand(ctx, &model.Demand{Title: "验证服务"})
	if err != nil {
		t.Fatal(err)
	}
	progress, err := st.AddDemandProgressWithSourceAndLinks(ctx, demand.ID,
		"JobRun 已启动，日志 https://logs.example.test/run/1。", nil,
		[]model.ProgressLink{{Kind: "seed-jobrun", Title: "MIX Serving JobRun", URL: "https://jobs.example.test/run/1", DedupeKey: "job:1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(progress.Links) != 2 {
		t.Fatalf("progress links=%+v, want structured plus inline URL", progress.Links)
	}
	loaded, err := st.GetDemand(ctx, demand.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Progress) != 1 || len(loaded.Progress[0].Links) != 2 {
		t.Fatalf("stored progress links were lost: %+v", loaded.Progress)
	}
	if _, err := st.AddDemandProgressWithSourceAndLinks(ctx, demand.ID, "bad link", nil,
		[]model.ProgressLink{{URL: "javascript:alert(1)"}}); err == nil {
		t.Fatal("unsafe progress link should be rejected")
	}
}

func TestAppSettingsLifecycle(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()

	if value, found, err := st.GetSetting(ctx, "default_view"); err != nil || found || value != "" {
		t.Fatalf("missing setting = (%q, %v, %v), want empty false nil", value, found, err)
	}
	if err := st.SetSetting(ctx, "default_view", "workbench"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "default_view", "infowall"); err != nil {
		t.Fatal(err)
	}
	value, found, err := st.GetSetting(ctx, "default_view")
	if err != nil {
		t.Fatal(err)
	}
	if !found || value != "infowall" {
		t.Fatalf("setting = (%q, %v), want infowall true", value, found)
	}
}

func TestDemandLifecycleFiltersAndSyncDirty(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if err := st.ConfigureFeishuSync(ctx, "doc", "https://example.test/doc", true); err != nil {
		t.Fatal(err)
	}
	cleanSyncState(t, st)

	project, err := st.CreateProject(ctx, &model.Project{Name: "LLMServer", Color: "#123456"})
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.GetFeishuSyncState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Dirty || state.Status != "pending" || state.DesiredVersion != state.SyncedVersion+1 {
		t.Fatalf("project mutation did not dirty sync state: %+v", state)
	}
	cleanSyncState(t, st)

	unknown := "A project that does not exist"
	demand, err := st.CreateDemand(ctx, &model.Demand{
		Title:       "Investigate periodic decode starvation",
		Description: "Align rank stacks with the exact throughput window",
		Priority:    model.DemandPriorityP0,
		ProjectID:   &unknown,
		Sources: []model.Source{{
			Kind: "feishu-chat", DedupeKey: "chat:oc_1:om_1", Excerpt: "please investigate",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if demand.ProjectID != nil || demand.ProjectHint != unknown {
		t.Fatalf("unknown project should become hint, got project=%v hint=%q", demand.ProjectID, demand.ProjectHint)
	}
	if demand.Status != model.DemandStatusPending || demand.Priority != model.DemandPriorityP0 {
		t.Fatalf("unexpected defaults: %+v", demand)
	}

	updated, err := st.UpdateDemand(ctx, demand.ID, DemandUpdate{
		Status:       ptr(model.DemandStatusDone),
		ProjectIDSet: true,
		ProjectID:    &project.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CompletedAt == nil || updated.ProjectID == nil || *updated.ProjectID != project.ID {
		t.Fatalf("done/project update not applied: %+v", updated)
	}
	if _, err := st.AddDemandProgress(ctx, demand.ID, "Real request returned HTTP 200"); err != nil {
		t.Fatal(err)
	}
	updated, err = st.GetDemand(ctx, demand.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Progress) != 1 || len(updated.Sources) != 1 {
		t.Fatalf("relations were not loaded: %+v", updated)
	}

	if _, err := st.UpdateDemand(ctx, demand.ID, DemandUpdate{Status: ptr(model.DemandStatusDismissed)}); err != nil {
		t.Fatal(err)
	}
	visible, err := st.ListDemands(ctx, DemandListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("dismissed demand returned by default: %+v", visible)
	}
	all, err := st.ListDemands(ctx, DemandListOptions{IncludeDismissed: true, Query: "decode starvation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != demand.ID {
		t.Fatalf("include_dismissed/search mismatch: %+v", all)
	}
}

func TestImportDemandsDeduplicatesAndUpdates(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	input := func(description string) []*model.Demand {
		return []*model.Demand{{
			Title: "整理飞书聊天需求", Description: description,
			Sources: []model.Source{{Kind: "feishu-chat", DedupeKey: "chat:1:message:9", Excerpt: description}},
		}}
	}
	first, err := st.ImportDemands(ctx, input("first"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 1 || first.Updated != 0 || first.Skipped != 0 {
		t.Fatalf("first import = %+v", first)
	}
	second, err := st.ImportDemands(ctx, input("first"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Skipped != 1 || second.Created != 0 || second.Updated != 0 {
		t.Fatalf("repeat import = %+v", second)
	}
	changedInput := input("new description")
	changedInput[0].Title = "must not overwrite title"
	changedInput[0].Priority = model.DemandPriorityP3
	otherProject := "must-not-reassign"
	changedInput[0].ProjectID = &otherProject
	changedInput[0].Sources = append(changedInput[0].Sources, model.Source{
		Kind: "feishu-chat", DedupeKey: "chat:1:message:10", Excerpt: "a later message",
	})
	changedInput[0].Progress = []model.Progress{{Text: "收到补充日志", CreatedAt: time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)}}
	third, err := st.ImportDemands(ctx, changedInput)
	if err != nil {
		t.Fatal(err)
	}
	thirdDemand := third.Results[0].Demand
	if third.Updated != 1 || thirdDemand.Description != "first" || thirdDemand.Title != "整理飞书聊天需求" ||
		thirdDemand.Priority != model.DemandPriorityNone || thirdDemand.ProjectID != nil ||
		len(thirdDemand.Sources) != 2 || len(thirdDemand.Progress) != 1 {
		t.Fatalf("changed import = %+v", third)
	}
	all, err := st.ListDemands(ctx, DemandListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("dedupe should leave one demand, got %d", len(all))
	}
}

func TestImportExistingDemandIDDoesNotFollowForeignSourceKey(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	first, err := st.CreateDemand(ctx, &model.Demand{
		Title:   "first",
		Sources: []model.Source{{Kind: "feishu-im", DedupeKey: "feishu-im:om_shared:0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateDemand(ctx, &model.Demand{Title: "second"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = st.ImportDemands(ctx, []*model.Demand{{
		ID:      second.ID,
		Sources: []model.Source{{Kind: "feishu-im", DedupeKey: "feishu-im:om_shared:0"}},
	}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("import error = %v, want conflict", err)
	}

	unchanged, err := st.GetDemand(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.Sources) != 1 {
		t.Fatalf("foreign demand was changed: %+v", unchanged.Sources)
	}
}

func TestFeishuSyncStateLifecycleAndVersionRace(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if err := st.ConfigureFeishuSync(ctx, "token", "url", true); err != nil {
		t.Fatal(err)
	}
	state, _ := st.GetFeishuSyncState(ctx)
	firstVersion := state.DesiredVersion
	if err := st.MarkFeishuSyncRunning(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProject(ctx, &model.Project{Name: "Changed while syncing"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteFeishuSync(ctx, firstVersion, "rev-1", "hash-1"); err != nil {
		t.Fatal(err)
	}
	state, _ = st.GetFeishuSyncState(ctx)
	if !state.Dirty || state.Status != "pending" || state.SyncedVersion != firstVersion {
		t.Fatalf("older completion incorrectly cleared newer mutation: %+v", state)
	}
	nextRetry := time.Now().UTC().Add(time.Minute)
	if err := st.FailFeishuSync(ctx, "temporary", nextRetry); err != nil {
		t.Fatal(err)
	}
	state, _ = st.GetFeishuSyncState(ctx)
	if state.RetryCount != 1 || state.LastError != "temporary" || state.NextRetryAt == nil {
		t.Fatalf("failure state mismatch: %+v", state)
	}
	if _, err := st.CreateDemand(ctx, &model.Demand{Title: "wake failed worker"}); err != nil {
		t.Fatal(err)
	}
	state, _ = st.GetFeishuSyncState(ctx)
	if state.RetryCount != 0 || state.NextRetryAt != nil || state.Status != "pending" {
		t.Fatalf("mutation did not wake failed sync: %+v", state)
	}
	if err := st.CompleteFeishuSync(ctx, state.DesiredVersion, "rev-2", "hash-2"); err != nil {
		t.Fatal(err)
	}
	state, _ = st.GetFeishuSyncState(ctx)
	if state.Dirty || state.Status != "idle" || state.LastHash != "hash-2" || state.LastSuccessAt == nil {
		t.Fatalf("complete state mismatch: %+v", state)
	}
	if err := st.DisableFeishuSync(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ = st.GetFeishuSyncState(ctx)
	if state.Enabled || state.Dirty || state.Status != "disabled" {
		t.Fatalf("disable state mismatch: %+v", state)
	}
}

func TestDemandMutationRollsBackWhenSyncStateCannotBeMarked(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if _, err := st.db.ExecContext(ctx, `DROP TABLE feishu_sync_state`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDemand(ctx, &model.Demand{Title: "must roll back"}); err == nil {
		t.Fatal("expected mutation to fail when sync state cannot be marked")
	}
	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM demands`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("business row committed without dirty marker: %d demands", count)
	}
}

func cleanSyncState(t *testing.T, st *Store) {
	t.Helper()
	state, err := st.GetFeishuSyncState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteFeishuSync(context.Background(), state.DesiredVersion, "", ""); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](value T) *T { return &value }
