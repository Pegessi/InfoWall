package feishusync

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/infowall/infowall/internal/integration"
)

func TestIntegrationExporterDescriptor(t *testing.T) {
	runner := &scriptedRunner{}
	exporter, err := NewIntegrationExporter(Client{Runner: runner}, integration.Descriptor{Version: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := exporter.Descriptor()
	if descriptor.ID != DefaultExporterID || descriptor.Name != "Feishu document" || descriptor.Version != "1.2.3" {
		t.Fatalf("unexpected descriptor: %+v", descriptor)
	}
	if descriptor.Role != integration.RoleExporter || descriptor.ProtocolVersion != integration.ProtocolVersion {
		t.Fatalf("invalid exporter contract: %+v", descriptor)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("constructor invoked client: %#v", runner.calls)
	}

	custom, err := NewIntegrationExporter(Client{}, integration.Descriptor{
		ID: "feishu.weekly", Name: "Weekly Feishu document", Version: "2.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := custom.Descriptor(); got.ID != "feishu.weekly" || got.Name != "Weekly Feishu document" || got.Version != "2.0.0" {
		t.Fatalf("custom descriptor was not preserved: %+v", got)
	}

	_, err = NewIntegrationExporter(Client{}, integration.Descriptor{
		ID: DefaultExporterID, Role: integration.RoleConnector, ProtocolVersion: integration.ProtocolVersion,
	})
	if err == nil || !strings.Contains(err.Error(), "want \"exporter\"") {
		t.Fatalf("wrong-role error = %v", err)
	}
}

func TestSnapshotFromExportSnapshot(t *testing.T) {
	generated := time.Date(2026, time.October, 6, 8, 9, 10, 0, time.UTC)
	updated := generated.AddDate(0, 0, -60)
	completed := generated.Add(-2 * time.Hour)
	old := generated.Add(-4 * time.Hour)
	middle := generated.Add(-3 * time.Hour)
	latest := generated.Add(-2 * time.Hour)
	got := snapshotFromExportSnapshot(integration.ExportSnapshot{
		GeneratedAt: generated,
		Projects: []integration.ProjectView{{
			ID: "project-1", Name: "Bridge", Description: "not rendered by the legacy view",
		}},
		Demands: []integration.DemandView{{
			ID: "demand-1", Title: "Export", Description: "Ship the bridge", Status: "done", Priority: "p1",
			ProjectID: "project-1", ProjectHint: "Bridge", NextAction: "Test it", BlockedReason: "",
			Sources: []integration.SourceView{
				{URL: "https://example.test/latest-source-first", OccurredAt: latest},
				{URL: "https://example.test/old-source", OccurredAt: old},
				{URL: "   ", OccurredAt: generated},
				{URL: "https://example.test/middle-source", OccurredAt: middle},
				{URL: "https://example.test/latest-source", OccurredAt: latest},
			},
			Progress: []integration.ProgressView{
				{Text: "latest first", Links: []integration.Link{{Title: "first", URL: "https://example.test/first"}}, CreatedAt: latest},
				{Text: "started", Links: []integration.Link{{Title: "old", URL: "https://example.test/old"}}, CreatedAt: old},
				{Text: "middle", CreatedAt: middle},
				{Text: "adapter complete", Links: []integration.Link{{Title: "change", URL: "https://example.test/change", State: "merged"}}, CreatedAt: latest},
			},
			UpdatedAt: updated, CompletedAt: &completed,
		}},
	})
	want := Snapshot{
		Generated: generated,
		Projects:  []ProjectView{{ID: "project-1", Name: "Bridge"}},
		Demands: []DemandView{{
			ID: "demand-1", Title: "Export", Description: "Ship the bridge", Status: "done", Priority: "p1",
			ProjectID: "project-1", ProjectHint: "Bridge", NextAction: "Test it", LatestProgress: "adapter complete", UpdatedAt: updated,
			ProgressLinks: []ProgressLinkView{{Title: "change", URL: "https://example.test/change", State: "merged"}},
			SourceURL:     "https://example.test/latest-source", CompletedAt: &completed,
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot mismatch:\nwant: %#v\n got: %#v", want, got)
	}
	rendered := Render(got).ManagedXML
	for _, fragment := range []string{
		`<h2>最近 30 天完成</h2>`,
		`Export`,
		`href="https://example.test/change"`,
		`change（merged）`,
		`href="https://example.test/latest-source"`,
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("legacy render lost %q: %s", fragment, rendered)
		}
	}
}

func TestIntegrationExporterCreate(t *testing.T) {
	request := integration.ExportRequest{Snapshot: exportTestSnapshot(), Settings: json.RawMessage(`{"create":true}`)}
	rendered := Render(snapshotFromExportSnapshot(request.Snapshot))
	runner := &scriptedRunner{responses: [][]byte{
		envelope("docx123", "https://example.test/docx/docx123", "", 7),
		envelope("docx123", "https://example.test/docx/docx123", `<fragment><h1 id="managed">InfoWall 自动同步</h1></fragment>`, 8),
		envelope("docx123", "https://example.test/docx/docx123", `<fragment><h1 id="managed">InfoWall 自动同步</h1><p id="marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 9),
	}}
	exporter := newTestIntegrationExporter(t, runner)
	exportedAt := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	exporter.now = func() time.Time { return exportedAt }

	receipt, err := exporter.Export(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Revision != "9" || receipt.Hash != rendered.Hash || receipt.URL != "https://example.test/docx/docx123" || !receipt.ExportedAt.Equal(exportedAt) {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if len(runner.calls) != 3 || !strings.Contains(strings.Join(runner.calls[0], " "), "docs +create") {
		t.Fatalf("unexpected create calls: %#v", runner.calls)
	}
	if !strings.Contains(runner.inputs[0], "adapter complete") {
		t.Fatalf("mapped latest progress missing from render: %s", runner.inputs[0])
	}
}

func TestIntegrationExporterUsesOneTimestampForZeroGeneratedAt(t *testing.T) {
	request := integration.ExportRequest{Snapshot: exportTestSnapshot(), Settings: json.RawMessage(`{"create":true}`)}
	request.Snapshot.GeneratedAt = time.Time{}
	now := time.Date(2026, time.October, 7, 1, 2, 3, 0, time.UTC)
	expectedSnapshot := snapshotFromExportSnapshot(request.Snapshot)
	expectedSnapshot.Generated = now
	rendered := Render(expectedSnapshot)
	runner := &scriptedRunner{responses: [][]byte{
		envelope("docx123", "https://example.test/docx/docx123", "", 7),
		envelope("docx123", "https://example.test/docx/docx123", `<fragment><h1 id="managed">InfoWall 自动同步</h1></fragment>`, 8),
		envelope("docx123", "https://example.test/docx/docx123", `<fragment><h1 id="managed">InfoWall 自动同步</h1><p id="marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 9),
	}}
	exporter := newTestIntegrationExporter(t, runner)
	nowCalls := 0
	exporter.now = func() time.Time {
		nowCalls++
		return now
	}

	receipt, err := exporter.Export(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if nowCalls != 1 {
		t.Fatalf("now called %d times; want 1", nowCalls)
	}
	if !receipt.ExportedAt.Equal(now) || receipt.Hash != rendered.Hash {
		t.Fatalf("timestamp/hash mismatch: receipt=%+v rendered=%+v", receipt, rendered)
	}
	if !strings.Contains(runner.inputs[0], rendered.ManagedXML) {
		t.Fatal("created document did not use the snapshot rendered with exported_at")
	}
}

func TestIntegrationExporterBindsUnmanagedDocument(t *testing.T) {
	request := integration.ExportRequest{Snapshot: exportTestSnapshot(), Settings: json.RawMessage(`{"doc_ref":" https://example.test/docx/existing "}`)}
	rendered := Render(snapshotFromExportSnapshot(request.Snapshot))
	runner := &scriptedRunner{responses: [][]byte{
		envelope("existing", "https://example.test/docx/existing", `<fragment><h1 id="existing">Existing</h1></fragment>`, 4),
		envelope("existing", "https://example.test/docx/existing", "", 5),
		envelope("existing", "https://example.test/docx/existing", `<fragment><h1 id="managed">InfoWall 自动同步</h1></fragment>`, 5),
		envelope("existing", "https://example.test/docx/existing", `<fragment><h1 id="managed">InfoWall 自动同步</h1></fragment>`, 5),
		envelope("existing", "https://example.test/docx/existing", `<fragment><h1 id="managed">InfoWall 自动同步</h1><p id="marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 6),
	}}
	exporter := newTestIntegrationExporter(t, runner)
	receipt, err := exporter.Export(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Revision != "6" || receipt.Hash != rendered.Hash || receipt.URL != "https://example.test/docx/existing" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if got := strings.Join(runner.calls[1], " "); !strings.Contains(got, "block_insert_after") {
		t.Fatalf("document was not bound: %s", got)
	}
}

func TestIntegrationExporterSyncsManagedDocument(t *testing.T) {
	request := integration.ExportRequest{Snapshot: exportTestSnapshot(), Settings: json.RawMessage(`{"doc_ref":"doc-token"}`)}
	rendered := Render(snapshotFromExportSnapshot(request.Snapshot))
	runner := &scriptedRunner{responses: [][]byte{
		envelope("doc-token", "https://example.test/docx/doc-token", `<fragment><h1 id="old-heading">InfoWall 自动同步</h1></fragment>`, 10),
		envelope("doc-token", "https://example.test/docx/doc-token", `<fragment><h1 id="old-heading">InfoWall 自动同步</h1></fragment>`, 10),
		envelope("doc-token", "https://example.test/docx/doc-token", "", 11),
		envelope("doc-token", "https://example.test/docx/doc-token", `<fragment><h1 id="new-heading">InfoWall 自动同步</h1></fragment>`, 11),
		envelope("doc-token", "https://example.test/docx/doc-token", `<fragment><h1 id="new-heading">InfoWall 自动同步</h1><p id="marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 11),
		envelope("doc-token", "https://example.test/docx/doc-token", `<fragment><h1 id="verified-heading">InfoWall 自动同步</h1></fragment>`, 12),
		envelope("doc-token", "https://example.test/docx/doc-token", `<fragment><h1 id="verified-heading">InfoWall 自动同步</h1><p id="verified-marker">INFOWALL_SYNC_END:`+rendered.Hash+`</p></fragment>`, 12),
	}}
	exporter := newTestIntegrationExporter(t, runner)
	receipt, err := exporter.Export(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Revision != "12" || receipt.Hash != rendered.Hash || receipt.URL != "" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if got := strings.Join(runner.calls[2], " "); !strings.Contains(got, "block_replace") {
		t.Fatalf("managed document was not synced: %s", got)
	}
}

func TestIntegrationExporterDoesNotExposeUnsafeReceiptURLs(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "http", value: "http://example.test/doc", want: "http://example.test/doc"},
		{name: "https", value: "https://example.test/doc", want: "https://example.test/doc"},
		{name: "mixed case https", value: "HTTPS://example.test/doc", want: "HTTPS://example.test/doc"},
		{name: "token", value: "doc-token"},
		{name: "ftp", value: "ftp://example.test/doc"},
		{name: "userinfo", value: "https://secret@example.test/doc"},
		{name: "missing host", value: "https:///doc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := safeReceiptURL(test.value); got != test.want {
				t.Fatalf("safeReceiptURL(%q) = %q; want %q", test.value, got, test.want)
			}
		})
	}
}

func TestIntegrationExporterErrorDoesNotEchoDocumentReference(t *testing.T) {
	const secretRef = "private-document-token"
	runner := runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("upstream rejected %s", secretRef)
	})
	exporter := newTestIntegrationExporter(t, runner)
	_, err := exporter.Export(context.Background(), integration.ExportRequest{Settings: json.RawMessage(`{"doc_ref":"` + secretRef + `"}`)})
	if err == nil {
		t.Fatal("expected export error")
	}
	if strings.Contains(err.Error(), secretRef) {
		t.Fatalf("error leaked document reference: %v", err)
	}
	if !strings.Contains(err.Error(), "upstream rejected [redacted]") {
		t.Fatalf("error lost useful redacted context: %v", err)
	}
}

func TestIntegrationExporterRejectsInvalidSettingsBeforeClientCall(t *testing.T) {
	tests := []struct {
		name     string
		settings string
	}{
		{name: "missing", settings: ``},
		{name: "empty", settings: `{}`},
		{name: "create false", settings: `{"create":false}`},
		{name: "blank ref", settings: `{"doc_ref":"  "}`},
		{name: "both", settings: `{"create":true,"doc_ref":"doc"}`},
		{name: "unknown", settings: `{"create":true,"folder":"root"}`},
		{name: "uppercase create", settings: `{"CREATE":true}`},
		{name: "mixed case doc ref", settings: `{"Doc_Ref":"doc"}`},
		{name: "duplicate create", settings: `{"create":false,"create":true}`},
		{name: "duplicate doc ref", settings: `{"doc_ref":"first","doc_ref":"second"}`},
		{name: "null create", settings: `{"create":null,"doc_ref":"doc"}`},
		{name: "null doc ref", settings: `{"doc_ref":null}`},
		{name: "wrong create type", settings: `{"create":"true"}`},
		{name: "wrong doc ref type", settings: `{"doc_ref":42}`},
		{name: "trailing value", settings: `{"create":true} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedRunner{}
			exporter := newTestIntegrationExporter(t, runner)
			_, err := exporter.Export(context.Background(), integration.ExportRequest{Settings: json.RawMessage(test.settings)})
			if err == nil {
				t.Fatal("expected settings error")
			}
			if len(runner.calls) != 0 {
				t.Fatalf("invalid settings invoked client: %#v", runner.calls)
			}
		})
	}
}

func newTestIntegrationExporter(t *testing.T, runner CommandRunner) *IntegrationExporter {
	t.Helper()
	exporter, err := NewIntegrationExporter(Client{Runner: runner}, integration.Descriptor{})
	if err != nil {
		t.Fatal(err)
	}
	return exporter
}

func exportTestSnapshot() integration.ExportSnapshot {
	generated := time.Date(2026, time.October, 6, 10, 0, 0, 0, time.UTC)
	return integration.ExportSnapshot{
		GeneratedAt: generated,
		Projects:    []integration.ProjectView{{ID: "project-1", Name: "Bridge"}},
		Demands: []integration.DemandView{{
			ID: "demand-1", Title: "Export", Status: "active", Priority: "p1", ProjectID: "project-1",
			Progress: []integration.ProgressView{{Text: "started"}, {Text: "adapter complete"}}, UpdatedAt: generated.Add(-time.Minute),
		}},
	}
}
