// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Demand, DemandReview, Project } from "./model";
import { DemandCard, PendingCard, ProjectCard, ReviewCard } from "./WorkbenchPage";

const project: Project = {
  id: "project-xperf",
  name: "xperf_evo",
  description: "EVO serving runtime",
  color: "#60a5fa",
};

const demand: Demand = {
  id: "demand-262",
  title: "合入 xperf_evo 部署规划自动推导 Serving 并行拓扑（MR 262）",
  summary: "MR 262 在模型初始化前，根据配置与 world size 推导完整并行拓扑；当前仍为 open。",
  status: "pending",
  priority: "p1",
  projectHint: "xperf_evo",
  nextStep: "完成 review 并将能力合入 develop",
  createdAt: "2026-08-09T09:00:00+08:00",
  updatedAt: "2026-08-09T10:00:00+08:00",
  progress: [],
  sources: [
    {
      id: "source-chat",
      kind: "feishu-im",
      label: "请 review 这个部署规划自动推导 topology 的 MR",
      excerpt: "请 review 这个部署规划自动推导 topology 的 MR",
      externalId: "om_opaque_message_id",
      senderName: "需求提出人",
      messageTime: "2026-08-08T16:20:00+08:00",
      url: "https://example.test/message/source-chat",
    },
    {
      id: "source-mr",
      kind: "codebase-mr",
      label: "open · infer serving topology during deployment planning · codex/auto-parallel-topology → develop",
      excerpt: "open · infer serving topology during deployment planning · codex/auto-parallel-topology → develop",
      externalId: "seed/xperf_evo!262",
      url: "https://code.byted.org/seed/xperf_evo/merge_requests/262",
    },
  ],
};

afterEach(cleanup);

describe("PendingCard", () => {
  it("shows a complete preview before revealing the edit form", () => {
    render(<PendingCard demand={demand} projects={[project]} disabled={false} onConfirm={vi.fn()} onDismiss={vi.fn()} />);

    expect(screen.getByText(demand.title)).toBeTruthy();
    expect(screen.getByText(demand.summary)).toBeTruthy();
    expect(screen.getByText(demand.nextStep!)).toBeTruthy();
    expect(screen.getByText("建议项目：xperf_evo · 已匹配")).toBeTruthy();
    expect(screen.getByText("请 review 这个部署规划自动推导 topology 的 MR")).toBeTruthy();
    expect(screen.getByText("需求提出人的消息")).toBeTruthy();
    expect(screen.queryByText("om_opaque_message_id")).toBeNull();
    expect(screen.queryByLabelText("需求标题")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "展开全部证据（2）" }));
    expect(screen.getByText("infer serving topology during deployment planning · codex/auto-parallel-topology → develop")).toBeTruthy();
    expect(screen.getByText("xperf_evo · MR 262")).toBeTruthy();
    expect(screen.queryByText("seed/xperf_evo!262")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    expect(screen.getByLabelText("需求标题")).toHaveProperty("value", demand.title);
    expect(screen.getByLabelText("所属项目")).toHaveProperty("value", project.id);
    expect(screen.getByLabelText("下一步")).toHaveProperty("value", demand.nextStep);
  });

  it("writes the matched project only when confirmed and keeps ignore available", () => {
    const onConfirm = vi.fn();
    const onDismiss = vi.fn();
    render(<PendingCard demand={demand} projects={[project]} disabled={false} onConfirm={onConfirm} onDismiss={onDismiss} />);

    expect(onConfirm).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "确认需求" }));
    expect(onConfirm).toHaveBeenCalledWith(demand.id, "planned", expect.objectContaining({
      title: demand.title,
      summary: demand.summary,
      priority: demand.priority,
      projectId: project.id,
      nextStep: demand.nextStep,
    }));

    fireEvent.click(screen.getByRole("button", { name: "忽略" }));
    expect(onDismiss).toHaveBeenCalledWith(demand.id);
  });

  it("chooses a compact priority before confirming", () => {
    const onConfirm = vi.fn();
    render(<PendingCard demand={demand} projects={[project]} disabled={false} onConfirm={onConfirm} onDismiss={vi.fn()} />);

    const p0 = screen.getByRole("button", { name: "优先级 P0" });
    const p1 = screen.getByRole("button", { name: "优先级 P1" });
    const p2 = screen.getByRole("button", { name: "优先级 P2" });
    expect(p0.getAttribute("aria-pressed")).toBe("false");
    expect(p1.getAttribute("aria-pressed")).toBe("true");
    expect(p2.getAttribute("aria-pressed")).toBe("false");

    fireEvent.click(p0);
    expect(p0.getAttribute("aria-pressed")).toBe("true");
    expect(p1.getAttribute("aria-pressed")).toBe("false");
    expect(onConfirm).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "确认需求" }));
    expect(onConfirm).toHaveBeenCalledWith(demand.id, "planned", expect.objectContaining({ priority: "p0" }));
  });
});

describe("DemandCard", () => {
  it("uses compact primary priorities and a menu for additional values", () => {
    const onOpen = vi.fn();
    const onUpdate = vi.fn();
    render(<DemandCard demand={{ ...demand, status: "active" }} project={project} onOpen={onOpen} onUpdate={onUpdate} />);

    const p0 = screen.getByRole("button", { name: "优先级 P0" });
    const p1 = screen.getByRole("button", { name: "优先级 P1" });
    const p2 = screen.getByRole("button", { name: "优先级 P2" });
    expect(p0.getAttribute("aria-pressed")).toBe("false");
    expect(p1.getAttribute("aria-pressed")).toBe("true");
    expect(p2.getAttribute("aria-pressed")).toBe("false");
    expect(screen.queryByRole("combobox", { name: "优先级" })).toBeNull();

    fireEvent.click(p0);
    expect(onUpdate).toHaveBeenCalledWith({ priority: "p0" });
    expect(onOpen).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "更多优先级，当前P1" }));
    fireEvent.click(screen.getByRole("button", { name: "设置优先级 P3" }));
    expect(onUpdate).toHaveBeenCalledWith({ priority: "p3" });
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("renders latest progress resources as direct links without opening the drawer", () => {
    const onOpen = vi.fn();
    render(<DemandCard demand={{ ...demand, status: "active", progress: [{
      id: "progress-1", text: "服务已拉起", createdAt: "2026-08-09T10:00:00+08:00",
      links: [{ kind: "seed-jobrun", title: "MIX Serving JobRun", url: "https://example.test/jobrun/1", state: "RUNNING" }],
    }] }} project={project} onOpen={onOpen} onUpdate={vi.fn()} />);

    const link = screen.getByRole("link", { name: /MIX Serving JobRun/ });
    expect(link.getAttribute("href")).toBe("https://example.test/jobrun/1");
    fireEvent.click(link);
    expect(onOpen).not.toHaveBeenCalled();
  });
});

describe("ReviewCard", () => {
  it("shows ambiguous progress evidence and requires a target before acceptance", () => {
    const review: DemandReview = {
      id: "review-1", status: "pending", suggestedDemandId: demand.id,
      progressText: "已完成首轮灰度，等待扩大实例范围。",
      progressDedupeKey: `feishu-progress:om_review:${demand.id}`,
      confidence: 0.72, rationale: "组件相同，但聊天同时提到两个部署需求。",
      createdAt: "2026-08-09T10:00:00+08:00",
      source: { kind: "feishu-im", label: "已完成首轮灰度", excerpt: "已完成首轮灰度", senderName: "需求提出人" },
      links: [{ kind: "feishu-doc", title: "灰度验收记录", url: "https://example.test/doc/gray" }],
    };
    const onAccept = vi.fn();
    const onDismiss = vi.fn();
    render(<ReviewCard review={review} demands={[demand]} disabled={false} onAccept={onAccept} onDismiss={onDismiss} />);

    expect(screen.getByText(`追加到：${demand.title}`)).toBeTruthy();
    expect(screen.getByText("已完成首轮灰度，等待扩大实例范围。")).toBeTruthy();
    expect(screen.getByLabelText("关联需求")).toHaveProperty("value", demand.id);
    expect(screen.getByRole("link", { name: /灰度验收记录/ }).getAttribute("href")).toBe("https://example.test/doc/gray");
    fireEvent.click(screen.getByRole("button", { name: "确认追加" }));
    expect(onAccept).toHaveBeenCalledWith(review.id, demand.id);
    fireEvent.click(screen.getByRole("button", { name: "忽略" }));
    expect(onDismiss).toHaveBeenCalledWith(review.id);
  });
});

describe("ProjectCard", () => {
  it("renames a project inline without opening it", async () => {
    const onOpen = vi.fn();
    const onRename = vi.fn().mockResolvedValue(true);
    render(<ProjectCard project={project} demands={[{ ...demand, status: "planned", projectId: project.id }]} disabled={false} onOpen={onOpen} onRename={onRename} />);

    fireEvent.click(screen.getByRole("button", { name: `重命名项目 ${project.name}` }));
    const input = screen.getByLabelText(`项目名称：${project.name}`);
    expect(input).toHaveProperty("value", project.name);
    fireEvent.change(input, { target: { value: "Xperf EVO Runtime" } });
    fireEvent.keyDown(input, { key: "Enter", code: "Enter" });

    await waitFor(() => expect(onRename).toHaveBeenCalledWith(project.id, "Xperf EVO Runtime"));
    expect(onOpen).not.toHaveBeenCalled();
    expect(screen.queryByLabelText(`项目名称：${project.name}`)).toBeNull();
  });
});
