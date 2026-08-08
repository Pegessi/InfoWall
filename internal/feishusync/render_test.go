package feishusync

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestRenderSectionsAndEscaping(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	rendered := Render(Snapshot{
		Generated: now,
		Projects:  []ProjectView{{ID: "p1", Name: "同步 & 工作台"}},
		Demands: []DemandView{
			{ID: "d1", Title: "确认 <需求>", Status: "pending", Priority: "p0", UpdatedAt: now},
			{ID: "d2", Title: "实现同步", Status: "active", Priority: "p1", ProjectID: "p1", LatestProgress: "完成 50%", UpdatedAt: now, SourceURL: "https://example.com/?a=1&b=2"},
			{ID: "d3", Title: "已完成", Status: "done", Priority: "p2", UpdatedAt: now},
		},
	})
	for _, want := range []string{ManagedHeading, "待确认需求", "按项目", "未分类需求", "最近 30 天完成", "同步 &amp; 工作台", "确认 &lt;需求&gt;", EndMarkerPrefix + rendered.Hash} {
		if !strings.Contains(rendered.ManagedXML, want) {
			t.Fatalf("rendered XML missing %q:\n%s", want, rendered.ManagedXML)
		}
	}
	if strings.Contains(rendered.ManagedXML, "https://example.com/?a=1&b=2") {
		t.Fatal("URL attribute was not escaped")
	}
	var parsed any
	if err := xml.Unmarshal([]byte("<root>"+rendered.ManagedXML+"</root>"), &parsed); err != nil {
		t.Fatalf("rendered XML is not well formed: %v", err)
	}
}

func TestInitialDocumentHasStableBoundary(t *testing.T) {
	rendered := Render(Snapshot{Generated: time.Now()})
	doc := InitialDocument(rendered)
	if strings.Count(doc, "<title>") != 1 || !strings.Contains(doc, "<h1>同步说明</h1>") {
		t.Fatalf("unexpected initial document: %s", doc)
	}
}
