package feishuingest

import (
	"context"
	"strings"
	"testing"
)

func TestCommandEnricherReadsMinimalCodebaseAndFeishuMetadata(t *testing.T) {
	byted := &fakeCommandRunner{output: []byte(`{"title":"infer serving topology during deployment planning","state":"open","source_branch":"codex/auto-parallel-topology","target_branch":"develop","description":"derive topology before model initialization"}`)}
	lark := &fakeCommandRunner{output: []byte(`{"title":"部署规划说明","content":"# 目标\n自动推导并行拓扑\n# 其他章节"}`)}
	enricher := CommandEnricher{BytedRunner: byted, LarkRunner: lark}
	messages := []Message{{Content: "请看 https://code.byted.org/seed/xperf_evo/merge_requests/262 和 https://bytedance.feishu.cn/docx/doc_token。"}}
	resources := enricher.Enrich(context.Background(), messages)
	if len(resources) != 2 {
		t.Fatalf("resources = %+v", resources)
	}
	var mr, doc *Resource
	for index := range resources {
		switch resources[index].Kind {
		case "codebase-mr":
			mr = &resources[index]
		case "feishu-doc":
			doc = &resources[index]
		}
	}
	if mr == nil || !mr.Accessible || mr.ExternalID != "seed/xperf_evo!262" || mr.State != "open" || !strings.Contains(mr.Excerpt, "target_branch: develop") {
		t.Fatalf("MR metadata = %+v", mr)
	}
	if doc == nil || !doc.Accessible || doc.Title != "部署规划说明" || !strings.Contains(doc.Excerpt, "自动推导并行拓扑") {
		t.Fatalf("doc metadata = %+v", doc)
	}
	if got := strings.Join(byted.args, " "); !strings.Contains(got, "--json codebase mr get") {
		t.Fatalf("unexpected bytedcli args: %s", got)
	}
	if got := strings.Join(lark.args, " "); !strings.Contains(got, "docs +fetch") || !strings.Contains(got, "--scope outline") {
		t.Fatalf("unexpected lark args: %s", got)
	}
}

func TestCommandEnricherMarksUnavailableResourceWithoutPersistingErrorText(t *testing.T) {
	byted := &fakeCommandRunner{err: context.DeadlineExceeded}
	resources := (CommandEnricher{BytedRunner: byted}).Enrich(context.Background(), []Message{{Content: "https://code.byted.org/seed/xperf_evo/merge_requests/262"}})
	if len(resources) != 1 || resources[0].Accessible || resources[0].Excerpt != "" {
		t.Fatalf("unavailable resource leaked failure detail: %+v", resources)
	}
}
