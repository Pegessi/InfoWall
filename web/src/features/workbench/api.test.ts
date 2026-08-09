import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchDemandReviews, fetchFeishuChatIntegration, fetchFeishuIngestionRuns, normalizeDemand, updateProject } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("normalizeDemand", () => {
  it("preserves project hints and structured source evidence", () => {
    const demand = normalizeDemand({
      id: "demand-1",
      title: "合入 xperf_evo 部署规划自动推导 Serving 并行拓扑（MR 262）",
      description: "部署规划需要在模型初始化前推导完整并行拓扑。",
      status: "pending",
      priority: "p1",
      project_hint: "xperf_evo",
      next_action: "完成 review 并合入 develop",
      created_at: "2026-08-09T09:00:00+08:00",
      updated_at: "2026-08-09T10:00:00+08:00",
      sources: [
        {
          id: "source-1",
          kind: "feishu-im",
          external_id: "om_123",
          chat_id: "oc_123",
          chat_name: "EVO Server",
          sender_id: "ou_123",
          sender_name: "需求提出人",
          message_time: "2026-08-08T16:20:00+08:00",
          excerpt: "这个 MR 是部署规划阶段自动推导 serving topology",
          url: "https://example.test/message/om_123",
          dedupe_key: "feishu-im:om_123:0",
          created_at: "2026-08-09T09:00:00+08:00",
        },
        {
          id: "source-2",
          kind: "codebase-mr",
          external_id: "seed/xperf_evo!262",
          message_time: "0001-01-01T00:00:00Z",
          excerpt: "open · infer serving topology during deployment planning",
          dedupe_key: "codebase-mr:seed/xperf_evo:262",
          created_at: "2026-08-09T10:00:00+08:00",
        },
      ],
    });

    expect(demand.projectHint).toBe("xperf_evo");
    expect(demand.nextStep).toBe("完成 review 并合入 develop");
    expect(demand.sources[0]).toEqual(expect.objectContaining({
      kind: "feishu-im",
      label: "这个 MR 是部署规划阶段自动推导 serving topology",
      externalId: "om_123",
      chatId: "oc_123",
      chatName: "EVO Server",
      senderId: "ou_123",
      senderName: "需求提出人",
      messageTime: "2026-08-08T16:20:00+08:00",
      excerpt: "这个 MR 是部署规划阶段自动推导 serving topology",
      dedupeKey: "feishu-im:om_123:0",
    }));
    expect(demand.sources[1]).toEqual(expect.objectContaining({
      kind: "codebase-mr",
      externalId: "seed/xperf_evo!262",
      messageTime: undefined,
      createdAt: "2026-08-09T10:00:00+08:00",
    }));
  });
});

describe("updateProject", () => {
  it("patches the existing project id with a new name", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: "project-large", name: "多模态 3.0", description: "", color: "#64748b" }),
    });
    vi.stubGlobal("fetch", fetchMock);

    const project = await updateProject("project-large", { name: "多模态 3.0" });

    expect(project.name).toBe("多模态 3.0");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(path).toBe("/api/projects/project-large");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(String(init.body))).toEqual({ name: "多模态 3.0" });
  });
});

describe("automatic ingestion normalization", () => {
  it("preserves state, run stats, token usage, and structured review evidence", async () => {
    const responses = [
      { enabled: true, timezone: "Asia/Shanghai", active_start: "09:00", active_end: "23:00", interval_minutes: 30, overlap_minutes: 5, excluded_chat_ids: ["oc_skip"], last_success_end: "2026-08-09T10:00:00Z", next_run_at: "2026-08-09T10:30:00Z", status: "idle" },
      { runs: [{ id: "run-1", trigger: "manual", status: "success", window_start: "2026-08-09T09:55:00Z", window_end: "2026-08-09T10:00:00Z", messages_seen: 4, messages_candidate: 1, created: 1, updated: 0, skipped: 3, review_count: 0, input_tokens: 1234, cached_input_tokens: 1000, output_tokens: 120, started_at: "2026-08-09T10:00:00Z" }] },
      { reviews: [{ id: "review-1", status: "pending", suggested_demand_id: "demand-1", progress_text: "完成灰度", progress_dedupe_key: "feishu-progress:om_1:demand-1", confidence: 0.7, created_at: "2026-08-09T10:00:00Z", source: { kind: "feishu-im", external_id: "om_1", sender_name: "请求人", excerpt: "完成灰度" } }] },
    ];
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async () => ({ ok: true, json: async () => responses.shift() })));
    const state = await fetchFeishuChatIntegration();
    const runs = await fetchFeishuIngestionRuns();
    const reviews = await fetchDemandReviews();
    expect(state).toEqual(expect.objectContaining({ enabled: true, activeStart: "09:00", intervalMinutes: 30, excludedChatIds: ["oc_skip"] }));
    expect(runs[0]).toEqual(expect.objectContaining({ messagesCandidate: 1, inputTokens: 1234, outputTokens: 120 }));
    expect(reviews[0]).toEqual(expect.objectContaining({ suggestedDemandId: "demand-1", progressText: "完成灰度" }));
    expect(reviews[0].source).toEqual(expect.objectContaining({ externalId: "om_1", senderName: "请求人", excerpt: "完成灰度" }));
  });
});
