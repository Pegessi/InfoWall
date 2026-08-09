import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  AlertCircle,
  ArrowUpRight,
  Check,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Clock3,
  ExternalLink,
  FileText,
  FolderKanban,
  Inbox,
  ListTodo,
  LoaderCircle,
  MessageSquarePlus,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Sparkles,
  X,
} from "lucide-react";
import type {
  Demand,
  DemandPatch,
  DemandPriority,
  DemandProgressLink,
  DemandSource,
  DemandStatus,
  DemandReview,
  FeishuChatIntegration,
  FeishuDocIntegration,
  FeishuIngestionRun,
  Project,
  WorkbenchSection,
} from "./model";
import { useWorkbench } from "./useWorkbench";

export type { WorkbenchSection } from "./model";

interface WorkbenchPageProps {
  section: WorkbenchSection;
  onSectionChange?: (section: WorkbenchSection) => void;
}

const NAV: Array<{ id: WorkbenchSection; label: string; icon: typeof Inbox }> = [
  { id: "pending", label: "待确认", icon: Inbox },
  { id: "demands", label: "需求清单", icon: ListTodo },
  { id: "projects", label: "项目", icon: FolderKanban },
];

const STATUS: Record<DemandStatus, { label: string; tone: string }> = {
  pending: { label: "待确认", tone: "bg-amber-500/10 text-amber-700 dark:text-amber-300" },
  planned: { label: "已计划", tone: "bg-sky-500/10 text-sky-700 dark:text-sky-300" },
  active: { label: "进行中", tone: "bg-blue-500/10 text-blue-700 dark:text-blue-300" },
  waiting: { label: "等待中", tone: "bg-violet-500/10 text-violet-700 dark:text-violet-300" },
  done: { label: "已完成", tone: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300" },
  dismissed: { label: "已忽略", tone: "bg-zinc-500/10 text-zinc-600 dark:text-zinc-300" },
};

const PRIORITY: Record<DemandPriority, { label: string; tone: string; rank: number }> = {
  p0: { label: "P0", tone: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300", rank: 0 },
  p1: { label: "P1", tone: "border-orange-500/30 bg-orange-500/10 text-orange-700 dark:text-orange-300", rank: 1 },
  p2: { label: "P2", tone: "border-[hsl(var(--border))] bg-[hsl(var(--muted))]", rank: 2 },
  p3: { label: "P3", tone: "border-[hsl(var(--border))] text-[hsl(var(--muted-foreground))]", rank: 3 },
  none: { label: "未定级", tone: "border-[hsl(var(--border))] text-[hsl(var(--muted-foreground))]", rank: 4 },
};

const ALL_STATUSES: DemandStatus[] = ["pending", "planned", "active", "waiting", "done", "dismissed"];
const COMPACT_PRIORITY_CHOICES: Array<{
  value: "p0" | "p1" | "p2";
  selectedTone: string;
}> = [
  { value: "p0", selectedTone: "bg-red-500/15 text-red-700 dark:text-red-300" },
  { value: "p1", selectedTone: "bg-orange-500/15 text-orange-700 dark:text-orange-300" },
  { value: "p2", selectedTone: "bg-slate-500/15 text-slate-800 dark:text-slate-200" },
];
const MORE_PRIORITY_CHOICES: Array<"p3" | "none"> = ["p3", "none"];
const inputClass = "h-11 w-full rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--background))] px-3 text-sm outline-none focus:ring-2 focus:ring-[hsl(var(--ring))] sm:h-10";
const textareaClass = `${inputClass} h-auto min-h-24 py-2`;

function formatDate(value?: string): string {
  if (!value) return "尚未同步";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit",
  }).format(date);
}

function sortDemands(demands: Demand[]): Demand[] {
  return [...demands].sort((a, b) =>
    PRIORITY[a.priority].rank - PRIORITY[b.priority].rank ||
    Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
}

function ProjectMark({ project }: { project?: Project }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-[hsl(var(--muted-foreground))]">
      <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: project?.color ?? "#94a3b8" }} />
      <span className="truncate">{project?.name ?? "未分类"}</span>
    </span>
  );
}

export function CompactPriorityPicker({ value, disabled = false, onChange }: {
  value: DemandPriority;
  disabled?: boolean;
  onChange: (value: DemandPriority) => void;
}) {
  const moreSelected = value === "p3" || value === "none";
  const choose = (next: DemandPriority) => {
    if (!disabled && next !== value) onChange(next);
  };
  return (
    <div role="group" aria-label="设置优先级" className="inline-flex h-8 shrink-0 rounded-md border border-[hsl(var(--border))] bg-[hsl(var(--background))] p-0.5">
      {COMPACT_PRIORITY_CHOICES.map(({ value: choice, selectedTone }) => {
        const selected = value === choice;
        return (
          <button
            key={choice}
            type="button"
            aria-label={`优先级 ${PRIORITY[choice].label}`}
            aria-pressed={selected}
            disabled={disabled}
            onClick={() => choose(choice)}
            className={`min-w-9 rounded-[4px] px-2 text-[11px] font-semibold transition-colors disabled:opacity-50 ${selected ? selectedTone : "text-[hsl(var(--muted-foreground))] hover:bg-[hsl(var(--muted))]"}`}
          >
            {PRIORITY[choice].label}
          </button>
        );
      })}
      <details className="relative ml-0.5 border-l border-[hsl(var(--border))] pl-0.5">
        <summary
          role="button"
          aria-label={`更多优先级，当前${PRIORITY[value].label}`}
          aria-disabled={disabled}
          tabIndex={disabled ? -1 : 0}
          onClick={(event) => { if (disabled) event.preventDefault(); }}
          className={`flex h-full min-w-7 cursor-pointer list-none items-center justify-center gap-0.5 rounded-[4px] px-1 text-[10px] font-semibold transition-colors [&::-webkit-details-marker]:hidden ${moreSelected ? "bg-zinc-500/12 text-[hsl(var(--foreground))]" : "text-[hsl(var(--muted-foreground))] hover:bg-[hsl(var(--muted))]"} ${disabled ? "cursor-not-allowed opacity-50" : ""}`}
        >
          {moreSelected && <span>{PRIORITY[value].label}</span>}
          <ChevronDown className="h-3 w-3" />
        </summary>
        <div className="absolute bottom-full right-0 z-30 mb-1 w-24 rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--background))] p-1 shadow-xl shadow-black/20">
          {MORE_PRIORITY_CHOICES.map((choice) => (
            <button
              key={choice}
              type="button"
              aria-label={`设置优先级 ${PRIORITY[choice].label}`}
              aria-pressed={value === choice}
              disabled={disabled}
              onClick={(event) => {
                choose(choice);
                event.currentTarget.closest("details")?.removeAttribute("open");
              }}
              className="flex h-8 w-full items-center justify-between rounded-md px-2 text-xs hover:bg-[hsl(var(--muted))] disabled:opacity-50"
            >
              {PRIORITY[choice].label}
              {value === choice && <Check className="h-3.5 w-3.5" />}
            </button>
          ))}
        </div>
      </details>
    </div>
  );
}

function PageTitle({ kicker, title, description, action }: { kicker: string; title: string; description: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col gap-3 border-b border-[hsl(var(--border))] pb-4 sm:flex-row sm:items-end sm:justify-between sm:gap-4 sm:pb-5">
      <div>
        <div className="text-[11px] font-semibold uppercase tracking-[0.2em] text-[hsl(var(--accent))]">{kicker}</div>
        <h1 className="mt-1 text-2xl font-semibold tracking-tight sm:text-3xl">{title}</h1>
        <p className="mt-2 max-w-3xl text-sm leading-6 text-[hsl(var(--muted-foreground))]">{description}</p>
      </div>
      {action && <div className="w-full sm:w-auto">{action}</div>}
    </div>
  );
}

function SyncBar({ integration, busy, onSync }: { integration: FeishuDocIntegration | null; busy: boolean; onSync: () => void }) {
  const syncing = busy || integration?.status === "syncing" || integration?.status === "running";
  const failed = integration?.status === "error";
  const configured = Boolean(integration?.documentUrl);
  return (
    <div className="mb-4 flex flex-col gap-3 rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--card))] px-3 py-3 sm:mb-6 sm:flex-row sm:items-center sm:justify-between sm:px-4">
      <div className="flex min-w-0 items-start gap-3">
        <div className={`mt-0.5 rounded-lg p-2 ${failed ? "bg-red-500/10 text-red-500" : "bg-blue-500/10 text-blue-500"}`}>
          <FileText className="h-4 w-4" />
        </div>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
            飞书需求文档
            <span className={`rounded-full px-2 py-0.5 text-[10px] ${failed ? "bg-red-500/10 text-red-600" : syncing ? "bg-blue-500/10 text-blue-600" : "bg-emerald-500/10 text-emerald-600"}`}>
              {failed ? "同步失败" : syncing ? "同步中" : integration?.lastSyncedAt ? "已同步" : configured ? "尚未同步" : "未配置"}
            </span>
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-3 text-xs text-[hsl(var(--muted-foreground))]">
            <span>最近同步：{formatDate(integration?.lastSyncedAt)}</span>
            {integration?.documentUrl && (
              <a className="inline-flex items-center gap-1 hover:text-[hsl(var(--accent))]" href={integration.documentUrl} target="_blank" rel="noreferrer">
                打开原文 <ExternalLink className="h-3 w-3" />
              </a>
            )}
            {integration?.lastError && <span className="text-red-500">{integration.lastError}</span>}
          </div>
        </div>
      </div>
      <button type="button" disabled={syncing || !configured} onClick={onSync} className="inline-flex h-10 w-full shrink-0 items-center justify-center gap-1.5 rounded-md border border-[hsl(var(--border))] px-3 text-xs font-medium hover:bg-[hsl(var(--muted))] disabled:opacity-50 sm:h-8 sm:w-auto">
        <RefreshCw className={`h-3.5 w-3.5 ${syncing ? "animate-spin" : ""}`} />{!configured ? "尚未配置" : failed ? "重试同步" : "立即同步"}
      </button>
    </div>
  );
}

function IngestionBar({ integration, latestRun, busy, onScan }: {
  integration: FeishuChatIntegration | null;
  latestRun?: FeishuIngestionRun;
  busy: boolean;
  onScan: () => void;
}) {
  const running = integration?.status === "running" || integration?.status === "pending";
  const failed = integration?.status === "error";
  const enabled = Boolean(integration?.enabled);
  const status = !enabled ? "未启用" : failed ? "采集失败" : running ? "采集中" : "自动采集";
  return (
    <div className="mb-4 flex flex-col gap-3 rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--card))] px-3 py-3 sm:mb-6 sm:flex-row sm:items-center sm:justify-between sm:px-4">
      <div className="flex min-w-0 items-start gap-3">
        <div className={`mt-0.5 rounded-lg p-2 ${failed ? "bg-red-500/10 text-red-500" : "bg-violet-500/10 text-violet-500"}`}>
          <Sparkles className={`h-4 w-4 ${running ? "animate-pulse" : ""}`} />
        </div>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
            飞书聊天增量采集
            <span className={`rounded-full px-2 py-0.5 text-[10px] ${failed ? "bg-red-500/10 text-red-600" : running ? "bg-violet-500/10 text-violet-600" : enabled ? "bg-emerald-500/10 text-emerald-600" : "bg-zinc-500/10 text-zinc-500"}`}>{status}</span>
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[hsl(var(--muted-foreground))]">
            {enabled && <span>{integration?.activeStart}–{integration?.activeEnd} · 每 {integration?.intervalMinutes} 分钟</span>}
            {enabled && <span title="群聊仅保留我发送、明确 @我，或我参与线程中的消息">范围：私聊 + 与我有关的群聊</span>}
            <span>最近成功：{formatDate(integration?.lastSuccessEnd)}</span>
            {enabled && <span>下次：{formatDate(integration?.nextRunAt)}</span>}
            {latestRun?.status === "success" && <span>上轮：新建 {latestRun.created} · 更新 {latestRun.updated} · 审核 {latestRun.reviewCount} · 跳过 {latestRun.skipped}</span>}
            {latestRun && latestRun.inputTokens > 0 && <span>Token：{latestRun.inputTokens.toLocaleString()} in / {latestRun.outputTokens.toLocaleString()} out</span>}
            {integration?.lastError && <span className="basis-full break-words text-red-500">{integration.lastError}</span>}
          </div>
        </div>
      </div>
      <button type="button" disabled={busy || running || !enabled} onClick={onScan} className="inline-flex h-10 w-full shrink-0 items-center justify-center gap-1.5 rounded-md border border-[hsl(var(--border))] px-3 text-xs font-medium hover:bg-[hsl(var(--muted))] disabled:opacity-50 sm:h-8 sm:w-auto">
        <RefreshCw className={`h-3.5 w-3.5 ${running ? "animate-spin" : ""}`} />{!enabled ? "尚未启用" : failed ? "立即重试" : running ? "正在采集" : "立即扫描"}
      </button>
    </div>
  );
}

function WorkbenchNav({ section, pendingCount, onChange }: { section: WorkbenchSection; pendingCount: number; onChange?: (value: WorkbenchSection) => void }) {
  const renderItems = (mobile: boolean) => NAV.map((item) => {
    const Icon = item.icon;
    const active = item.id === section;
    return (
      <button
        key={item.id}
        type="button"
        onClick={() => onChange?.(item.id)}
        aria-current={active ? "page" : undefined}
        className={mobile
          ? `relative flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-lg px-2 py-1 text-[11px] transition-colors ${active ? "bg-[hsl(var(--muted))] font-medium text-[hsl(var(--foreground))]" : "text-[hsl(var(--muted-foreground))]"}`
          : `flex items-center gap-2 rounded-lg px-3 py-2.5 text-sm transition-colors ${active ? "bg-[hsl(var(--foreground))] font-medium text-[hsl(var(--background))]" : "text-[hsl(var(--muted-foreground))] hover:bg-[hsl(var(--muted))] hover:text-[hsl(var(--foreground))]"}`}
      >
        <Icon className={mobile ? "h-5 w-5" : "h-4 w-4"} />
        <span className={mobile ? "leading-none" : "flex-1 text-left"}>{item.label}</span>
        {item.id === "pending" && pendingCount > 0 && (
          <span className={mobile
            ? "absolute right-[calc(50%-1.6rem)] top-0.5 min-w-4 rounded-full bg-amber-500 px-1 text-[9px] leading-4 text-white"
            : "rounded-full bg-amber-500/15 px-1.5 text-[10px] text-amber-600"}
          >
            {pendingCount}
          </span>
        )}
      </button>
    );
  });
  return (
    <>
      <aside data-testid="workbench-sidebar" className="hidden h-full w-56 shrink-0 border-r border-[hsl(var(--border))] p-4 pt-7 lg:block">
        <div className="mb-5 px-3"><div className="flex items-center gap-2 font-semibold"><Sparkles className="h-4 w-4 text-blue-500" />个人工作台</div><p className="mt-1 text-xs text-[hsl(var(--muted-foreground))]">需求是主体，项目是聚合视角</p></div>
        <nav className="space-y-1">{renderItems(false)}</nav>
      </aside>
      <nav aria-label="工作台页面" className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-3 gap-1 border-t border-[hsl(var(--border))] bg-[hsl(var(--background)/0.94)] px-2 pb-[max(0.25rem,env(safe-area-inset-bottom))] pt-1 shadow-[0_-8px_24px_rgba(0,0,0,0.08)] backdrop-blur lg:hidden">
        {renderItems(true)}
      </nav>
    </>
  );
}

function ErrorBanner({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div role="alert" className="mb-5 flex flex-col items-start justify-between gap-3 rounded-lg border border-red-500/25 bg-red-500/[0.07] px-4 py-3 text-sm text-red-700 dark:text-red-300 sm:flex-row sm:items-center">
      <span className="flex items-center gap-2"><AlertCircle className="h-4 w-4 shrink-0" />{message}</span>
      <button type="button" onClick={onRetry} className="shrink-0 font-medium underline underline-offset-2">重试</button>
    </div>
  );
}

function sourceKindLabel(kind: string): string {
  switch (kind) {
    case "feishu-im": return "飞书消息";
    case "codebase-mr": return "Codebase MR";
    case "feishu-doc": return "飞书文档";
    case "feishu-wiki": return "飞书 Wiki";
    case "feishu-minutes": return "飞书妙记";
    case "trial": return "Trial";
    case "seed-jobrun": return "JobRun";
    case "arena-eval": return "Arena 评测";
    case "model-card": return "Model Card";
    case "insight": return "Insight";
    case "agent": return "Agent";
    default: return kind || "来源记录";
  }
}

function safeHTTPURL(value: string): string | undefined {
  try {
    const parsed = new URL(value);
    return parsed.protocol === "http:" || parsed.protocol === "https:" ? parsed.toString() : undefined;
  } catch {
    return undefined;
  }
}

export function ProgressLinks({ links = [], compact = false }: { links?: DemandProgressLink[]; compact?: boolean }) {
  const visible = links.flatMap((link) => {
    const url = safeHTTPURL(link.url);
    return url ? [{ ...link, url }] : [];
  });
  if (!visible.length) return null;
  return (
    <div aria-label="相关资源" className={`flex min-w-0 flex-wrap ${compact ? "mt-1.5 gap-1" : "mt-2 gap-1.5"}`}>
      {visible.map((link, index) => (
        <a
          key={link.dedupeKey ?? `${link.url}:${index}`}
          href={link.url}
          target="_blank"
          rel="noreferrer noopener"
          title={link.externalId ? `${link.title} (${link.externalId})` : link.title}
          onClick={(event) => event.stopPropagation()}
          className={`inline-flex min-w-0 max-w-full items-center gap-1 rounded-md border border-blue-500/20 bg-blue-500/[0.07] text-blue-600 hover:border-blue-500/40 hover:bg-blue-500/10 hover:underline dark:text-blue-300 ${compact ? "px-1.5 py-0.5 text-[10px]" : "px-2 py-1 text-xs"}`}
        >
          <span className="truncate">{link.title}</span>
          {link.state && <span className="shrink-0 text-[9px] uppercase text-[hsl(var(--muted-foreground))]">{link.state}</span>}
          <ArrowUpRight className="h-3 w-3 shrink-0" />
        </a>
      ))}
    </div>
  );
}

function manualProgressLinks(value: string): { links: DemandProgressLink[]; invalid: string[] } {
  const links: DemandProgressLink[] = [];
  const invalid: string[] = [];
  const seen = new Set<string>();
  for (const raw of value.split(/\r?\n/).map((part) => part.trim()).filter(Boolean)) {
    const url = safeHTTPURL(raw);
    if (!url) {
      invalid.push(raw);
      continue;
    }
    if (seen.has(url)) continue;
    seen.add(url);
    links.push({ kind: "link", title: new URL(url).hostname, url });
  }
  return { links, invalid };
}

function sourceDisplayName(source: DemandSource): string | undefined {
  if (source.kind === "feishu-im") {
    return source.chatName || (source.senderName ? `${source.senderName}的消息` : undefined);
  }
  if (source.kind === "codebase-mr" && source.externalId) {
    const match = source.externalId.match(/^(.+?)(?:!|:)(\d+)$/);
    if (match) {
      const repositoryParts = match[1].split("/");
      const repository = repositoryParts[repositoryParts.length - 1];
      return `${repository} · MR ${match[2]}`;
    }
    return "Merge Request";
  }
  return undefined;
}

function SourceEvidence({ source }: { source: DemandSource }) {
  const rawEvidence = source.excerpt ?? source.label;
  const resourceParts = source.kind === "feishu-im"
    ? []
    : rawEvidence.split(/\s*·\s*/).filter(Boolean);
  const resourceStatus = resourceParts.length > 1 ? resourceParts[0] : undefined;
  const evidence = resourceStatus ? resourceParts.slice(1).join(" · ") : rawEvidence;
  const displayName = sourceDisplayName(source);
  const metadata = [source.senderName, source.chatName, formatDate(source.messageTime ?? source.createdAt)]
    .filter((value) => value && value !== "尚未同步");
  return (
    <article className="min-w-0 rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--background)/0.55)] p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-[10px] text-[hsl(var(--muted-foreground))]">
        <span className="rounded-full bg-[hsl(var(--muted))] px-2 py-0.5 font-medium text-[hsl(var(--foreground))]">{sourceKindLabel(source.kind)}</span>
        {resourceStatus && <span className="rounded-full border border-[hsl(var(--border))] px-2 py-0.5">{resourceStatus}</span>}
        {displayName && <span title={source.externalId} className="min-w-0 break-words">{displayName}</span>}
      </div>
      <p className="mt-2 break-words text-sm leading-6 text-[hsl(var(--foreground))]">{evidence || "暂无证据摘录"}</p>
      {(metadata.length > 0 || source.url) && (
        <div className="mt-2 flex min-w-0 flex-col gap-2 text-xs text-[hsl(var(--muted-foreground))] sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
          {metadata.length > 0 && <span className="min-w-0 break-words">{metadata.join(" · ")}</span>}
          {source.url && (
            <a href={source.url} target="_blank" rel="noreferrer" className="inline-flex min-h-8 shrink-0 items-center gap-1 self-start text-blue-500 hover:underline">
              查看原始证据 <ArrowUpRight className="h-3.5 w-3.5" />
            </a>
          )}
        </div>
      )}
    </article>
  );
}

export function DemandCard({ demand, project, onOpen, onUpdate }: { demand: Demand; project?: Project; onOpen: () => void; onUpdate: (patch: DemandPatch) => void }) {
  const latest = demand.progress.at(-1);
  return (
    <article onClick={onOpen} role="button" tabIndex={0} onKeyDown={(event) => { if (event.key === "Enter") onOpen(); }}
      className="group cursor-pointer rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--card))] p-3.5 shadow-sm shadow-black/[0.02] transition hover:border-[hsl(var(--ring))] hover:shadow-md sm:p-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${PRIORITY[demand.priority].tone}`}>{PRIORITY[demand.priority].label}</span>
            <span className={`rounded-full px-2 py-0.5 text-[10px] ${STATUS[demand.status].tone}`}>{STATUS[demand.status].label}</span>
            <ProjectMark project={project} />
          </div>
          <h2 className="font-semibold leading-5">{demand.title}</h2>
          <p className="mt-1.5 line-clamp-2 text-sm leading-5 text-[hsl(var(--muted-foreground))]">{demand.summary || "暂无摘要"}</p>
        </div>
        <ChevronRight className="h-4 w-4 shrink-0 text-[hsl(var(--muted-foreground))] transition group-hover:translate-x-0.5" />
      </div>
      {demand.nextStep && <div className="mt-3 rounded-lg bg-[hsl(var(--muted)/0.7)] px-3 py-2 text-xs leading-5"><span className="font-medium">下一步：</span>{demand.nextStep}</div>}
      {latest && <div className="mt-3 min-w-0 border-l-2 border-blue-500/30 pl-3 text-xs leading-5 text-[hsl(var(--muted-foreground))]"><span className="font-medium text-[hsl(var(--foreground))]">最近进展：</span>{latest.text}<ProgressLinks links={latest.links} compact /></div>}
      <div className="mt-3 flex flex-col gap-2 border-t border-[hsl(var(--border))] pt-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
        <span className="text-[10px] text-[hsl(var(--muted-foreground))]">更新于 {formatDate(demand.updatedAt)}</span>
        <div className="flex w-full items-center justify-end gap-2 sm:w-auto" onClick={(event) => event.stopPropagation()}>
          <CompactPriorityPicker value={demand.priority} onChange={(priority) => onUpdate({ priority })} />
          <select aria-label="状态" value={demand.status} onChange={(event) => onUpdate({ status: event.target.value as DemandStatus })} className="h-8 min-w-24 flex-1 rounded-md border border-[hsl(var(--border))] bg-[hsl(var(--background))] px-2 text-xs sm:min-w-0 sm:flex-none sm:text-[10px]">
            {ALL_STATUSES.map((value) => <option key={value} value={value}>{STATUS[value].label}</option>)}
          </select>
        </div>
      </div>
    </article>
  );
}

function normalizedProjectName(value?: string): string {
  return value?.trim().toLocaleLowerCase() ?? "";
}

export function PendingCard({ demand, projects, disabled, onConfirm, onDismiss }: {
  demand: Demand; projects: Project[]; disabled: boolean;
  onConfirm: (id: string, status: "planned" | "active", patch: DemandPatch) => void;
  onDismiss: (id: string) => void;
}) {
  const suggestedProject = projects.find((project) =>
    normalizedProjectName(project.name) === normalizedProjectName(demand.projectHint));
  const [title, setTitle] = useState(demand.title);
  const [summary, setSummary] = useState(demand.summary);
  const [nextStep, setNextStep] = useState(demand.nextStep ?? "");
  const [priority, setPriority] = useState<DemandPriority>(demand.priority);
  const [projectId, setProjectId] = useState(demand.projectId ?? suggestedProject?.id ?? "");
  const [target, setTarget] = useState<"planned" | "active">("planned");
  const [editing, setEditing] = useState(false);
  const [expandedSources, setExpandedSources] = useState(false);
  const visibleSources = expandedSources ? demand.sources : demand.sources.slice(0, 1);

  useEffect(() => {
    if (editing) return;
    setTitle(demand.title);
    setSummary(demand.summary);
    setNextStep(demand.nextStep ?? "");
    setPriority(demand.priority);
    setProjectId(demand.projectId ?? suggestedProject?.id ?? "");
  }, [demand, editing, suggestedProject?.id]);

  return (
    <article className="min-w-0 overflow-hidden rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--card))] shadow-sm shadow-black/[0.02]">
      <div className="min-w-0 p-4 sm:p-5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${PRIORITY[priority].tone}`}>{PRIORITY[priority].label}</span>
          <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-700 dark:text-amber-300">AI 候选</span>
          <span className="rounded-full border border-[hsl(var(--border))] px-2 py-0.5 text-[10px] text-[hsl(var(--muted-foreground))]">
            建议项目：{demand.projectHint || "未分类"}{suggestedProject ? " · 已匹配" : ""}
          </span>
          <span className="text-[10px] text-[hsl(var(--muted-foreground))]">{demand.sources.length} 条来源</span>
        </div>
        <h2 className="mt-3 break-words text-lg font-semibold leading-7 sm:text-xl">{demand.title}</h2>
        <p className="mt-2 break-words text-sm leading-6 text-[hsl(var(--muted-foreground))]">{demand.summary || "暂无摘要"}</p>
        <div className="mt-3 rounded-lg bg-blue-500/[0.07] px-3 py-2.5 text-sm leading-6">
          <span className="font-medium text-blue-700 dark:text-blue-300">下一步：</span>
          <span className="break-words">{demand.nextStep || "待补充具体下一步"}</span>
        </div>

        <section className="mt-4 min-w-0">
          <div className="mb-2 flex items-center justify-between gap-3">
            <h3 className="text-xs font-medium text-[hsl(var(--muted-foreground))]">关键证据</h3>
            {demand.sources.length > 1 && (
              <button type="button" onClick={() => setExpandedSources((value) => !value)} className="inline-flex min-h-8 shrink-0 items-center gap-1 text-xs text-blue-500">
                {expandedSources ? "收起证据" : `展开全部证据（${demand.sources.length}）`}
                {expandedSources ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
              </button>
            )}
          </div>
          <div className="space-y-2">
            {visibleSources.length ? visibleSources.map((source, index) => <SourceEvidence key={source.id ?? source.dedupeKey ?? index} source={source} />) : <div className="rounded-lg border border-dashed border-[hsl(var(--border))] px-3 py-4 text-sm text-[hsl(var(--muted-foreground))]">暂无来源记录</div>}
          </div>
        </section>

        {editing && (
          <div className="mt-5 space-y-3 rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--background)/0.55)] p-3 sm:p-4">
            <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_12rem_9rem]">
              <label className="text-xs text-[hsl(var(--muted-foreground))]">需求标题<input className={`${inputClass} mt-1 font-medium text-[hsl(var(--foreground))]`} value={title} onChange={(e) => setTitle(e.target.value)} /></label>
              <label className="text-xs text-[hsl(var(--muted-foreground))]">所属项目<select className={`${inputClass} mt-1`} value={projectId} onChange={(e) => setProjectId(e.target.value)}><option value="">未分类</option>{projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label>
              <label className="text-xs text-[hsl(var(--muted-foreground))]">优先级<select className={`${inputClass} mt-1`} value={priority} onChange={(e) => setPriority(e.target.value as DemandPriority)}>{Object.entries(PRIORITY).map(([v, m]) => <option key={v} value={v}>{m.label}</option>)}</select></label>
            </div>
            <label className="block text-xs text-[hsl(var(--muted-foreground))]">摘要<textarea className={`${textareaClass} mt-1 min-h-24`} value={summary} onChange={(e) => setSummary(e.target.value)} /></label>
            <label className="block text-xs text-[hsl(var(--muted-foreground))]">下一步<input className={`${inputClass} mt-1`} value={nextStep} onChange={(e) => setNextStep(e.target.value)} placeholder="一个具体、可执行动作" /></label>
          </div>
        )}
      </div>
      <div className="border-t border-[hsl(var(--border))] bg-[hsl(var(--muted)/0.28)] p-3 sm:flex sm:items-center sm:justify-between sm:gap-3 sm:px-5">
        <div className="flex shrink-0 items-center gap-2">
          <span className="text-[11px] font-medium text-[hsl(var(--muted-foreground))]">优先级</span>
          <CompactPriorityPicker value={priority} disabled={disabled} onChange={setPriority} />
          <span className="hidden text-xs text-[hsl(var(--muted-foreground))] xl:block">确认后写入</span>
        </div>
        <div className="mt-3 grid min-w-0 w-full grid-cols-2 gap-2 sm:mt-0 sm:flex sm:w-auto sm:flex-wrap sm:justify-end">
          <button disabled={disabled} type="button" onClick={() => onDismiss(demand.id)} className="inline-flex h-10 items-center justify-center gap-1 rounded-md border border-[hsl(var(--border))] px-3 text-sm hover:bg-[hsl(var(--muted))] sm:h-9"><X className="h-4 w-4" />忽略</button>
          <button disabled={disabled} type="button" onClick={() => setEditing((value) => !value)} className="inline-flex h-10 items-center justify-center gap-1 rounded-md border border-[hsl(var(--border))] px-3 text-sm hover:bg-[hsl(var(--muted))] sm:h-9"><Pencil className="h-4 w-4" />{editing ? "完成编辑" : "编辑"}</button>
          <select aria-label="确认后的状态" value={target} onChange={(e) => setTarget(e.target.value as "planned" | "active")} className="col-span-2 h-10 min-w-0 rounded-md border border-[hsl(var(--border))] bg-[hsl(var(--background))] px-2 text-sm sm:col-span-1 sm:h-9"><option value="planned">确认到：已计划</option><option value="active">确认到：进行中</option></select>
          <button disabled={disabled || !title.trim()} type="button" onClick={() => onConfirm(demand.id, target, { title: title.trim(), summary: summary.trim(), priority, projectId: projectId || null, nextStep: nextStep.trim() || null })} className="col-span-2 inline-flex h-10 items-center justify-center gap-1 rounded-md bg-[hsl(var(--foreground))] px-3 text-sm font-medium text-[hsl(var(--background))] disabled:opacity-50 sm:col-span-1 sm:h-9"><Check className="h-4 w-4" />确认需求</button>
        </div>
      </div>
    </article>
  );
}

export function ReviewCard({ review, demands, disabled, onAccept, onDismiss }: {
  review: DemandReview;
  demands: Demand[];
  disabled: boolean;
  onAccept: (id: string, demandId?: string) => void;
  onDismiss: (id: string) => void;
}) {
  const [demandId, setDemandId] = useState(review.suggestedDemandId ?? "");
  const suggested = demands.find((demand) => demand.id === review.suggestedDemandId);
  return (
    <article className="min-w-0 overflow-hidden rounded-xl border border-violet-500/25 bg-[hsl(var(--card))] shadow-sm shadow-black/[0.02]">
      <div className="p-4 sm:p-5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="rounded-full bg-violet-500/10 px-2 py-0.5 text-[10px] font-medium text-violet-700 dark:text-violet-300">进展待审核</span>
          <span className="text-[10px] text-[hsl(var(--muted-foreground))]">关联置信度 {Math.round(review.confidence * 100)}%</span>
        </div>
        <h2 className="mt-3 break-words text-lg font-semibold leading-7">{suggested ? `追加到：${suggested.title}` : "请选择要追加进展的需求"}</h2>
        <div className="mt-3 rounded-lg bg-violet-500/[0.07] px-3 py-2.5 text-sm leading-6"><span className="font-medium text-violet-700 dark:text-violet-300">候选进展：</span>{review.progressText}</div>
        <ProgressLinks links={review.links} />
        {review.rationale && <p className="mt-2 text-xs leading-5 text-[hsl(var(--muted-foreground))]">为什么需要确认：{review.rationale}</p>}
        <div className="mt-4"><SourceEvidence source={review.source} /></div>
      </div>
      <div className="border-t border-[hsl(var(--border))] bg-[hsl(var(--muted)/0.28)] p-3 sm:flex sm:items-center sm:justify-between sm:gap-3 sm:px-5">
        <select aria-label="关联需求" value={demandId} onChange={(event) => setDemandId(event.target.value)} className={`${inputClass} min-w-0 sm:max-w-xl`}>
          <option value="">选择需求</option>
          {sortDemands(demands.filter((demand) => demand.status !== "dismissed" && demand.status !== "done")).map((demand) => <option key={demand.id} value={demand.id}>{demand.title}</option>)}
        </select>
        <div className="mt-3 grid shrink-0 grid-cols-2 gap-2 sm:mt-0 sm:flex">
          <button disabled={disabled} type="button" onClick={() => onDismiss(review.id)} className="inline-flex h-10 items-center justify-center gap-1.5 rounded-md border border-[hsl(var(--border))] px-3 text-sm hover:bg-[hsl(var(--muted))] disabled:opacity-50 sm:h-9"><X className="h-4 w-4" />忽略</button>
          <button disabled={disabled || !demandId} type="button" onClick={() => onAccept(review.id, demandId)} className="inline-flex h-10 items-center justify-center gap-1.5 rounded-md bg-[hsl(var(--foreground))] px-3 text-sm font-medium text-[hsl(var(--background))] disabled:opacity-50 sm:h-9"><Check className="h-4 w-4" />确认追加</button>
        </div>
      </div>
    </article>
  );
}

function DetailDrawer({ id, projects, loadDemand, onClose, onSave, onProgress }: {
  id: string | null; projects: Project[]; loadDemand: (id: string) => Promise<Demand>;
  onClose: () => void; onSave: (id: string, patch: DemandPatch) => Promise<boolean>;
  onProgress: (id: string, text: string, links?: DemandProgressLink[]) => Promise<boolean>;
}) {
  const [demand, setDemand] = useState<Demand | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [progressText, setProgressText] = useState("");
  const [progressURLs, setProgressURLs] = useState("");
  useEffect(() => {
    if (!id) return;
    let alive = true;
    setDemand(null); setLoadError(null);
    loadDemand(id).then((value) => { if (alive) setDemand(value); }).catch((error: unknown) => { if (alive) setLoadError(error instanceof Error ? error.message : "详情加载失败"); });
    return () => { alive = false; };
  }, [id, loadDemand]);
  useEffect(() => {
    if (!id) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = previousOverflow; };
  }, [id]);
  if (!id) return null;
  const parsedProgressLinks = manualProgressLinks(progressURLs);
  const patch = async (next: DemandPatch) => {
    if (await onSave(id, next)) setDemand(await loadDemand(id));
  };
  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-black/35" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <aside aria-label="需求详情" className="h-[100dvh] w-full max-w-2xl overflow-y-auto border-l border-[hsl(var(--border))] bg-[hsl(var(--background))] pb-[env(safe-area-inset-bottom)] shadow-2xl">
        <div className="sticky top-0 z-10 flex items-center justify-between border-b border-[hsl(var(--border))] bg-[hsl(var(--background)/0.92)] px-4 py-3 pt-[max(0.75rem,env(safe-area-inset-top))] backdrop-blur sm:px-5 sm:py-4"><div><div className="text-xs text-[hsl(var(--muted-foreground))]">需求详情</div><div className="font-semibold">完整记录与进展</div></div><button type="button" onClick={onClose} aria-label="关闭需求详情" className="inline-flex h-10 w-10 items-center justify-center rounded-md hover:bg-[hsl(var(--muted))]"><X className="h-5 w-5" /></button></div>
        <div className="p-4 sm:p-6">
          {!demand && !loadError && <div className="flex justify-center py-20"><LoaderCircle className="h-6 w-6 animate-spin text-blue-500" /></div>}
          {loadError && <ErrorBanner message={loadError} onRetry={() => loadDemand(id).then(setDemand).catch(() => undefined)} />}
          {demand && <>
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="sm:col-span-2 text-xs text-[hsl(var(--muted-foreground))]">标题<input className={`${inputClass} mt-1`} defaultValue={demand.title} onBlur={(e) => { if (e.target.value !== demand.title) void patch({ title: e.target.value }); }} /></label>
              <label className="sm:col-span-2 text-xs text-[hsl(var(--muted-foreground))]">摘要<textarea className={`${textareaClass} mt-1`} defaultValue={demand.summary} onBlur={(e) => { if (e.target.value !== demand.summary) void patch({ summary: e.target.value }); }} /></label>
              <label className="text-xs text-[hsl(var(--muted-foreground))]">状态<select className={`${inputClass} mt-1`} value={demand.status} onChange={(e) => void patch({ status: e.target.value as DemandStatus })}>{ALL_STATUSES.map((v) => <option key={v} value={v}>{STATUS[v].label}</option>)}</select></label>
              <label className="text-xs text-[hsl(var(--muted-foreground))]">优先级<select className={`${inputClass} mt-1`} value={demand.priority} onChange={(e) => void patch({ priority: e.target.value as DemandPriority })}>{Object.entries(PRIORITY).map(([v, m]) => <option key={v} value={v}>{m.label}</option>)}</select></label>
              <label className="text-xs text-[hsl(var(--muted-foreground))]">项目<select className={`${inputClass} mt-1`} value={demand.projectId ?? ""} onChange={(e) => void patch({ projectId: e.target.value || null })}><option value="">未分类</option>{projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select>{demand.projectHint && !demand.projectId && <span className="mt-1 block break-words text-[10px]">建议项目：{demand.projectHint}</span>}</label>
              <label className="sm:col-span-2 text-xs text-[hsl(var(--muted-foreground))]">下一步<input className={`${inputClass} mt-1`} defaultValue={demand.nextStep ?? ""} onBlur={(e) => void patch({ nextStep: e.target.value || null })} /></label>
              {demand.status === "waiting" && <label className="sm:col-span-2 text-xs text-[hsl(var(--muted-foreground))]">等待什么<input className={`${inputClass} mt-1`} defaultValue={demand.waitingFor ?? ""} onBlur={(e) => void patch({ waitingFor: e.target.value || null })} /></label>}
            </div>
            <section className="mt-8"><h3 className="font-semibold">来源证据</h3><div className="mt-3 space-y-2">{demand.sources.length ? demand.sources.map((source, index) => <SourceEvidence key={source.id ?? source.dedupeKey ?? index} source={source} />) : <div className="text-sm text-[hsl(var(--muted-foreground))]">暂无来源记录</div>}</div></section>
            <section className="mt-8">
              <h3 className="font-semibold">进展时间线</h3>
              <div className="mt-3 grid min-w-0 gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
                <textarea value={progressText} onChange={(e) => setProgressText(e.target.value)} placeholder="记录刚刚发生的关键进展…" className={`${textareaClass} min-h-20 min-w-0`} />
                <button type="button" disabled={!progressText.trim() || parsedProgressLinks.invalid.length > 0} onClick={async () => { if (await onProgress(id, progressText.trim(), parsedProgressLinks.links)) { setProgressText(""); setProgressURLs(""); setDemand(await loadDemand(id)); } }} className="inline-flex h-10 w-full items-center justify-center gap-1 rounded-md bg-[hsl(var(--foreground))] px-3 text-sm text-[hsl(var(--background))] disabled:opacity-50 sm:w-auto sm:self-end"><MessageSquarePlus className="h-4 w-4" />追加</button>
                <label className="min-w-0 text-xs text-[hsl(var(--muted-foreground))] sm:col-span-1">相关链接（可选，一行一个）<textarea value={progressURLs} onChange={(event) => setProgressURLs(event.target.value)} placeholder="https://…" className={`${textareaClass} mt-1 min-h-16 min-w-0 font-mono text-xs`} /></label>
                {parsedProgressLinks.invalid.length > 0 && <div role="alert" className="break-all text-xs text-red-500 sm:col-span-2">链接必须是完整的 http(s) 地址：{parsedProgressLinks.invalid.join("，")}</div>}
              </div>
              <div className="mt-5 min-w-0 space-y-4 border-l border-[hsl(var(--border))] pl-4">{[...demand.progress].reverse().map((entry) => <div key={entry.id} className="relative min-w-0 text-sm before:absolute before:-left-[1.22rem] before:top-1.5 before:h-2 before:w-2 before:rounded-full before:bg-blue-500"><div className="break-words">{entry.text}</div><ProgressLinks links={entry.links} /><div className="mt-1 text-xs text-[hsl(var(--muted-foreground))]">{formatDate(entry.createdAt)}</div></div>)}{!demand.progress.length && <div className="text-sm text-[hsl(var(--muted-foreground))]">还没有进展记录</div>}</div>
            </section>
          </>}
        </div>
      </aside>
    </div>
  );
}

function CreateDialog({ kind, projects, busy, error, onClose, onCreateDemand, onCreateProject }: {
  kind: "demand" | "project" | null;
  projects: Project[];
  busy: boolean;
  error: string | null;
  onClose: () => void;
  onCreateDemand: (patch: DemandPatch & { title: string }) => Promise<boolean>;
  onCreateProject: (input: { name: string; description?: string; color?: string }) => Promise<boolean>;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [status, setStatus] = useState<DemandStatus>("planned");
  const [priority, setPriority] = useState<DemandPriority>("none");
  const [projectId, setProjectId] = useState("");
  const [nextStep, setNextStep] = useState("");
  const [color, setColor] = useState("#60a5fa");

  useEffect(() => {
    setTitle(""); setDescription(""); setStatus("planned"); setPriority("none");
    setProjectId(""); setNextStep(""); setColor("#60a5fa");
  }, [kind]);
  useEffect(() => {
    if (!kind) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = previousOverflow; };
  }, [kind]);

  if (!kind) return null;
  const isDemand = kind === "demand";
  const submit = async () => {
    if (!title.trim()) return;
    const succeeded = isDemand
      ? await onCreateDemand({
          title: title.trim(), summary: description.trim(), status, priority,
          projectId: projectId || null, nextStep: nextStep.trim() || null,
        })
      : await onCreateProject({ name: title.trim(), description: description.trim(), color });
    if (succeeded) onClose();
  };
  return (
    <div className="fixed inset-0 z-[60] flex items-end justify-center bg-black/40 p-0 sm:items-center sm:p-5" onMouseDown={(e) => { if (e.target === e.currentTarget && !busy) onClose(); }}>
      <section role="dialog" aria-modal="true" aria-label={isDemand ? "新建需求" : "新建项目"} className="max-h-[calc(100dvh-env(safe-area-inset-top))] w-full overflow-y-auto rounded-t-2xl border border-[hsl(var(--border))] bg-[hsl(var(--background))] pb-[env(safe-area-inset-bottom)] shadow-2xl sm:max-h-[92vh] sm:max-w-xl sm:rounded-2xl">
        <div className="sticky top-0 z-10 flex items-center justify-between border-b border-[hsl(var(--border))] bg-[hsl(var(--background)/0.94)] px-4 py-3 backdrop-blur sm:px-5 sm:py-4">
          <div><div className="text-xs text-[hsl(var(--muted-foreground))]">{isDemand ? "DEMAND" : "PROJECT"}</div><h2 className="text-lg font-semibold">{isDemand ? "新建需求" : "新建项目"}</h2></div>
          <button type="button" disabled={busy} onClick={onClose} aria-label="关闭新建窗口" className="inline-flex h-10 w-10 items-center justify-center rounded-md hover:bg-[hsl(var(--muted))] disabled:opacity-50"><X className="h-5 w-5" /></button>
        </div>
        <form className="space-y-4 p-4 sm:p-5" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          {error && <div role="alert" className="flex items-start gap-2 rounded-lg border border-red-500/25 bg-red-500/[0.07] px-3 py-2 text-sm text-red-600 dark:text-red-300"><AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />{error}</div>}
          <label className="block text-xs text-[hsl(var(--muted-foreground))]">{isDemand ? "需求标题" : "项目名称"} <span className="text-red-500">*</span><input autoFocus required className={`${inputClass} mt-1 text-[hsl(var(--foreground))]`} value={title} onChange={(e) => setTitle(e.target.value)} placeholder={isDemand ? "一句话描述要推进的事情" : "例如：LLMServer 稳定性"} /></label>
          <label className="block text-xs text-[hsl(var(--muted-foreground))]">{isDemand ? "需求描述" : "项目描述"}<textarea className={`${textareaClass} mt-1`} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="补充目标、边界或背景" /></label>
          {isDemand ? <>
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="text-xs text-[hsl(var(--muted-foreground))]">初始状态<select className={`${inputClass} mt-1`} value={status} onChange={(e) => setStatus(e.target.value as DemandStatus)}>{ALL_STATUSES.map((value) => <option key={value} value={value}>{STATUS[value].label}</option>)}</select></label>
              <label className="text-xs text-[hsl(var(--muted-foreground))]">优先级<select className={`${inputClass} mt-1`} value={priority} onChange={(e) => setPriority(e.target.value as DemandPriority)}>{Object.entries(PRIORITY).map(([value, meta]) => <option key={value} value={value}>{meta.label}</option>)}</select></label>
              <label className="sm:col-span-2 text-xs text-[hsl(var(--muted-foreground))]">所属项目<select className={`${inputClass} mt-1`} value={projectId} onChange={(e) => setProjectId(e.target.value)}><option value="">未分类</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
            </div>
            <label className="block text-xs text-[hsl(var(--muted-foreground))]">下一步<input className={`${inputClass} mt-1`} value={nextStep} onChange={(e) => setNextStep(e.target.value)} placeholder="最具体的下一个动作" /></label>
          </> : <label className="flex items-center gap-3 text-xs text-[hsl(var(--muted-foreground))]">项目颜色<input type="color" value={color} onChange={(e) => setColor(e.target.value)} className="h-10 w-16 cursor-pointer rounded border border-[hsl(var(--border))] bg-transparent p-1" /><span className="font-mono">{color}</span></label>}
          <div className="flex flex-col-reverse gap-2 border-t border-[hsl(var(--border))] pt-4 sm:flex-row sm:justify-end">
            <button type="button" disabled={busy} onClick={onClose} className="h-10 rounded-md border border-[hsl(var(--border))] px-4 text-sm hover:bg-[hsl(var(--muted))] disabled:opacity-50">取消</button>
            <button type="submit" disabled={busy || !title.trim()} className="inline-flex h-10 items-center justify-center gap-2 rounded-md bg-[hsl(var(--foreground))] px-4 text-sm font-medium text-[hsl(var(--background))] disabled:opacity-50">{busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}{isDemand ? "创建需求" : "创建项目"}</button>
          </div>
        </form>
      </section>
    </div>
  );
}

export function ProjectCard({ project, demands, disabled, onOpen, onRename }: {
  project?: Project;
  demands: Demand[];
  disabled: boolean;
  onOpen: () => void;
  onRename: (id: string, name: string) => Promise<boolean>;
}) {
  const [editing, setEditing] = useState(false);
  const [draftName, setDraftName] = useState(project?.name ?? "");
  const displayName = project?.name ?? "未分类";
  const active = demands.filter((demand) => demand.status !== "done").length;

  useEffect(() => {
    if (!editing) setDraftName(project?.name ?? "");
  }, [editing, project?.name]);

  const cancelRename = () => {
    setDraftName(project?.name ?? "");
    setEditing(false);
  };
  const saveRename = async () => {
    const name = draftName.trim();
    if (!project || !name || name === project.name) return;
    if (await onRename(project.id, name)) setEditing(false);
  };

  return (
    <article className="min-w-0 rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--card))] p-4 transition hover:border-[hsl(var(--ring))] hover:shadow-md sm:p-5">
      {project && editing ? (
        <div>
          <form className="grid w-full min-w-0 grid-cols-[auto_minmax(0,1fr)_2.25rem_2.25rem] items-center gap-2" onSubmit={(event) => { event.preventDefault(); void saveRename(); }}>
            <span className="h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: project.color }} />
            <input
              autoFocus
              aria-label={`项目名称：${project.name}`}
              value={draftName}
              onChange={(event) => setDraftName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.preventDefault();
                  cancelRename();
                } else if (event.key === "Enter") {
                  event.preventDefault();
                  void saveRename();
                }
              }}
              className="h-9 w-full min-w-0 rounded-md border border-[hsl(var(--ring))] bg-[hsl(var(--background))] px-2.5 text-base font-semibold outline-none ring-2 ring-[hsl(var(--ring)/0.18)]"
            />
            <button type="submit" aria-label="保存项目名称" disabled={disabled || !draftName.trim() || draftName.trim() === project.name} className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[hsl(var(--foreground))] text-[hsl(var(--background))] disabled:opacity-40"><Check className="h-4 w-4" /></button>
            <button type="button" aria-label="取消重命名" disabled={disabled} onClick={cancelRename} className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-[hsl(var(--border))] hover:bg-[hsl(var(--muted))] disabled:opacity-40"><X className="h-4 w-4" /></button>
          </form>
          <p className="mt-2 text-sm leading-5 text-[hsl(var(--muted-foreground))]">{project.description}</p>
        </div>
      ) : (
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <span className="h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: project?.color ?? "#94a3b8" }} />
              <h2 className="truncate text-lg font-semibold">{displayName}</h2>
            </div>
            <p className="mt-2 text-sm leading-5 text-[hsl(var(--muted-foreground))]">{project?.description || "尚未归入具体项目的需求。"}</p>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {project && (
              <button type="button" aria-label={`重命名项目 ${project.name}`} disabled={disabled} onClick={() => setEditing(true)} className="inline-flex h-8 items-center justify-center gap-1 rounded-md px-2 text-xs text-[hsl(var(--muted-foreground))] hover:bg-[hsl(var(--muted))] hover:text-[hsl(var(--foreground))] disabled:opacity-40"><Pencil className="h-3.5 w-3.5" /><span className="hidden sm:inline">重命名</span></button>
            )}
            <button type="button" aria-label={`打开项目 ${displayName}`} onClick={onOpen} className="inline-flex h-8 w-8 items-center justify-center rounded-md text-[hsl(var(--muted-foreground))] hover:bg-[hsl(var(--muted))] hover:text-[hsl(var(--foreground))]"><ChevronRight className="h-5 w-5" /></button>
          </div>
        </div>
      )}
      <button type="button" aria-label={`查看项目 ${displayName} 的需求`} onClick={onOpen} className="mt-4 block w-full border-t border-[hsl(var(--border))] pt-4 text-left sm:mt-5">
        <span className="flex items-end justify-between">
          <span className="text-xs text-[hsl(var(--muted-foreground))]">共 {demands.length} 条需求</span>
          <span className="text-sm font-medium">{active} 条待推进</span>
        </span>
        {demands.slice(0, 3).map((demand) => <span key={demand.id} className="mt-2 block truncate rounded-md bg-[hsl(var(--muted)/0.6)] px-3 py-2 text-xs">{demand.title}</span>)}
      </button>
    </article>
  );
}

export function WorkbenchPage({ section, onSectionChange }: WorkbenchPageProps) {
  const workbench = useWorkbench();
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | DemandStatus>("all");
  const [projectFilter, setProjectFilter] = useState("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [createKind, setCreateKind] = useState<"demand" | "project" | null>(null);
  const projectsById = useMemo(() => new Map(workbench.projects.map((project) => [project.id, project])), [workbench.projects]);
  const pending = sortDemands(workbench.demands.filter((demand) => demand.status === "pending"));
  const visible = useMemo(() => sortDemands(workbench.demands.filter((demand) => {
    if (statusFilter === "all") {
      if (demand.status === "pending" || demand.status === "dismissed") return false;
    } else if (demand.status !== statusFilter) return false;
    if (projectFilter !== "all" && (projectFilter === "unassigned" ? Boolean(demand.projectId) : demand.projectId !== projectFilter)) return false;
    const text = `${demand.title} ${demand.summary} ${demand.nextStep ?? ""}`.toLowerCase();
    return text.includes(query.trim().toLowerCase());
  })), [projectFilter, query, statusFilter, workbench.demands]);

  const openProject = (id: string) => { setProjectFilter(id); onSectionChange?.("demands"); };
  return (
    <div data-testid="workbench-shell" className="h-full min-h-0 overflow-hidden bg-[linear-gradient(145deg,hsl(var(--background))_0%,hsl(var(--muted)/0.55)_100%)] lg:flex">
      <WorkbenchNav section={section} pendingCount={pending.length + workbench.reviews.length} onChange={onSectionChange} />
      <div data-testid="workbench-scroll-region" className="h-full min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain pb-20 lg:[scrollbar-gutter:stable] lg:pb-0"><div className="mx-auto max-w-[1500px] px-3 py-4 sm:px-6 sm:py-6 lg:px-10 lg:py-8">
        <SyncBar integration={workbench.integration} busy={workbench.mutating} onSync={() => void workbench.syncFeishu()} />
        <IngestionBar integration={workbench.chatIntegration} latestRun={workbench.ingestionRuns[0]} busy={workbench.mutating} onScan={() => void workbench.scanFeishu()} />
        {workbench.error && <ErrorBanner message={workbench.error} onRetry={() => void workbench.refresh()} />}
        {workbench.loading && workbench.demands.length === 0 ? <div className="flex items-center justify-center gap-2 py-28 text-sm text-[hsl(var(--muted-foreground))]"><LoaderCircle className="h-5 w-5 animate-spin" />正在加载工作台…</div> : <>
          {section === "pending" && <>
            <PageTitle kicker="AI INBOX" title="待确认" description="先用标题、摘要、下一步和证据判断候选是否成立；需要修正时再展开编辑。" />
            <div className="mt-6 space-y-3">
              {workbench.reviews.map((review) => <ReviewCard key={review.id} review={review} demands={workbench.demands} disabled={workbench.mutating} onAccept={(id, demandId) => void workbench.acceptReview(id, demandId)} onDismiss={(id) => void workbench.dismissReview(id)} />)}
              {pending.map((demand) => <PendingCard key={demand.id} demand={demand} projects={workbench.projects} disabled={workbench.mutating} onConfirm={(id, status, patch) => void workbench.confirmDemand(id, status, patch)} onDismiss={(id) => void workbench.dismissDemand(id)} />)}
              {pending.length === 0 && workbench.reviews.length === 0 && <div className="rounded-xl border border-dashed border-[hsl(var(--border))] py-16 text-center"><Check className="mx-auto h-7 w-7 text-emerald-500" /><div className="mt-3 font-medium">待确认已清空</div><div className="mt-1 text-sm text-[hsl(var(--muted-foreground))]">新的飞书提取结果和歧义进展会先进入这里。</div></div>}
            </div>
          </>}
          {section === "demands" && <>
            <PageTitle kicker="DEMANDS" title="需求清单" description="所有正式需求采用统一粒度；状态、优先级和项目只是筛选及组织方式。" action={<div className="flex w-full items-center justify-between gap-3 sm:w-auto sm:justify-end"><span className="text-sm text-[hsl(var(--muted-foreground))]">{visible.length} 条结果</span><button type="button" onClick={() => setCreateKind("demand")} className="inline-flex h-10 items-center gap-1.5 rounded-md bg-[hsl(var(--foreground))] px-3 text-sm font-medium text-[hsl(var(--background))] sm:h-9"><Plus className="h-4 w-4" />新建需求</button></div>} />
            <div className="mt-4 grid grid-cols-2 gap-2 md:mt-5 md:grid-cols-[minmax(15rem,1fr)_11rem_14rem] md:gap-3">
              <label className="relative col-span-2 md:col-span-1"><Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[hsl(var(--muted-foreground))]" /><input type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索标题、摘要、标签或下一步" className={`${inputClass} pl-9`} /></label>
              <select aria-label="状态筛选" value={statusFilter} onChange={(e) => setStatusFilter(e.target.value as "all" | DemandStatus)} className={inputClass}><option value="all">默认状态</option>{ALL_STATUSES.map((v) => <option key={v} value={v}>{STATUS[v].label}</option>)}</select>
              <select aria-label="项目筛选" value={projectFilter} onChange={(e) => setProjectFilter(e.target.value)} className={inputClass}><option value="all">全部项目</option><option value="unassigned">未分类</option>{workbench.projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
            </div>
            <div className="mt-4 grid gap-3 xl:grid-cols-2">{visible.map((demand) => <DemandCard key={demand.id} demand={demand} project={demand.projectId ? projectsById.get(demand.projectId) : undefined} onOpen={() => setSelectedId(demand.id)} onUpdate={(patch) => void workbench.updateDemand(demand.id, patch)} />)}{visible.length === 0 && <div className="xl:col-span-2 rounded-xl border border-dashed border-[hsl(var(--border))] py-16 text-center text-sm text-[hsl(var(--muted-foreground))]">没有匹配的需求，试试清除筛选条件。</div>}</div>
          </>}
          {section === "projects" && <>
            <PageTitle kicker="PROJECTS" title="按项目查看" description="项目负责聚合同一方向的需求；未归类需求也会保留，不会因为缺少项目而消失。" action={<button type="button" onClick={() => setCreateKind("project")} className="inline-flex h-10 w-full items-center justify-center gap-1.5 rounded-md bg-[hsl(var(--foreground))] px-3 text-sm font-medium text-[hsl(var(--background))] sm:h-9 sm:w-auto"><Plus className="h-4 w-4" />新建项目</button>} />
            <div className="mt-5 grid gap-3 sm:mt-6 sm:gap-4 lg:grid-cols-2">
              {[...workbench.projects.map((project) => ({ project, id: project.id })), { project: undefined, id: "unassigned" }].map(({ project, id }) => {
                const grouped = sortDemands(workbench.demands.filter((demand) =>
                  demand.status !== "pending" && demand.status !== "dismissed" &&
                  (project ? demand.projectId === project.id : !demand.projectId)));
                return <ProjectCard key={id} project={project} demands={grouped} disabled={workbench.mutating} onOpen={() => openProject(id)} onRename={(projectId, name) => workbench.updateProject(projectId, { name })} />;
              })}
            </div>
          </>}
        </>}
      </div></div>
      <DetailDrawer id={selectedId} projects={workbench.projects} loadDemand={workbench.loadDemand} onClose={() => setSelectedId(null)} onSave={workbench.updateDemand} onProgress={workbench.addProgress} />
      <CreateDialog kind={createKind} projects={workbench.projects} busy={workbench.mutating} error={workbench.error} onClose={() => setCreateKind(null)} onCreateDemand={workbench.createDemand} onCreateProject={workbench.createProject} />
    </div>
  );
}
