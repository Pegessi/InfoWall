import type {
  Demand,
  DemandPatch,
  DemandPriority,
  DemandProgress,
  DemandSource,
  DemandStatus,
  FeishuDocIntegration,
  FeishuChatIntegration,
  FeishuIngestionRun,
  DemandReview,
  Project,
} from "./model";

type JsonRecord = Record<string, unknown>;

function getKey(): string | null {
  try {
    return localStorage.getItem("infowall-key");
  } catch {
    return null;
  }
}

function authHeaders(): Record<string, string> {
  const key = getKey();
  return key ? { Authorization: `Bearer ${key}` } : {};
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      ...authHeaders(),
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  if (!response.ok) {
    const message = await response.text().catch(() => "");
    throw new Error(message || `请求失败（${response.status}）`);
  }
  return (await response.json()) as T;
}

function stringValue(record: JsonRecord, camel: string, snake = camel): string | undefined {
  const value = record[camel] ?? record[snake];
  return typeof value === "string" && value !== "" ? value : undefined;
}

function meaningfulTimestamp(value?: string): string | undefined {
  return value?.startsWith("0001-01-01T00:00:00") ? undefined : value;
}

function normalizeProgress(value: unknown): DemandProgress[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry) => {
    if (!entry || typeof entry !== "object") return [];
    const row = entry as JsonRecord;
    return [{
      id: String(row.id ?? crypto.randomUUID()),
      text: String(row.text ?? row.body ?? ""),
      createdAt: stringValue(row, "createdAt", "created_at") ?? new Date().toISOString(),
    }];
  });
}

export function normalizeSources(value: unknown, fallback?: unknown): DemandSource[] {
  const list = Array.isArray(value) ? value : fallback && typeof fallback === "object" ? [fallback] : [];
  return list.flatMap((entry) => {
    if (!entry || typeof entry !== "object") return [];
    const row = entry as JsonRecord;
    const messageTime = meaningfulTimestamp(stringValue(row, "messageTime", "message_time"));
    return [{
      id: stringValue(row, "id"),
      kind: String(row.kind ?? row.type ?? "manual"),
      label: String(row.label ?? row.title ?? row.excerpt ?? row.chat_name ?? row.sender_name ?? "来源记录"),
      url: stringValue(row, "url"),
      createdAt: stringValue(row, "createdAt", "created_at"),
      externalId: stringValue(row, "externalId", "external_id"),
      chatId: stringValue(row, "chatId", "chat_id"),
      chatName: stringValue(row, "chatName", "chat_name"),
      senderId: stringValue(row, "senderId", "sender_id"),
      senderName: stringValue(row, "senderName", "sender_name"),
      messageTime,
      excerpt: stringValue(row, "excerpt"),
      dedupeKey: stringValue(row, "dedupeKey", "dedupe_key"),
    }];
  });
}

export function normalizeDemand(value: unknown): Demand {
  const row = (value ?? {}) as JsonRecord;
  const status = String(row.status ?? "pending") as DemandStatus;
  const priority = String(row.priority ?? "none").toLowerCase() as DemandPriority;
  return {
    id: String(row.id ?? ""),
    title: String(row.title ?? "未命名需求"),
    summary: String(row.description ?? row.summary ?? ""),
    status,
    priority,
    projectId: stringValue(row, "projectId", "project_id"),
    projectHint: stringValue(row, "projectHint", "project_hint"),
    nextStep: stringValue(row, "nextStep", "next_action") ?? stringValue(row, "next_step"),
    waitingFor: stringValue(row, "waitingFor", "blocked_reason") ?? stringValue(row, "waiting_for"),
    createdAt: stringValue(row, "createdAt", "created_at") ?? new Date().toISOString(),
    updatedAt: stringValue(row, "updatedAt", "updated_at") ?? new Date().toISOString(),
    progress: normalizeProgress(row.progress),
    sources: normalizeSources(row.sources, row.source),
  };
}

function normalizeProject(value: unknown): Project {
  const row = (value ?? {}) as JsonRecord;
  return {
    id: String(row.id ?? ""),
    name: String(row.name ?? "未命名项目"),
    description: String(row.description ?? ""),
    color: String(row.color ?? "#64748b"),
    createdAt: stringValue(row, "createdAt", "created_at"),
    updatedAt: stringValue(row, "updatedAt", "updated_at"),
  };
}

function patchBody(patch: DemandPatch): JsonRecord {
  return {
    ...(patch.title !== undefined ? { title: patch.title } : {}),
    ...(patch.summary !== undefined ? { description: patch.summary } : {}),
    ...(patch.status !== undefined ? { status: patch.status } : {}),
    ...(patch.priority !== undefined ? { priority: patch.priority } : {}),
    ...(patch.projectId !== undefined ? { project_id: patch.projectId } : {}),
    ...(patch.nextStep !== undefined ? { next_action: patch.nextStep } : {}),
    ...(patch.waitingFor !== undefined ? { blocked_reason: patch.waitingFor } : {}),
  };
}

export async function fetchDemands(): Promise<Demand[]> {
  const data = await request<{ demands?: unknown[] } | unknown[]>("/api/demands?include_dismissed=true");
  const rows = Array.isArray(data) ? data : data.demands ?? [];
  return rows.map(normalizeDemand);
}

export async function fetchDemand(id: string): Promise<Demand> {
  return normalizeDemand(await request<unknown>(`/api/demands/${encodeURIComponent(id)}`));
}

export async function updateDemand(id: string, patch: DemandPatch): Promise<Demand> {
  return normalizeDemand(await request<unknown>(`/api/demands/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(patchBody(patch)),
  }));
}

export async function createDemand(patch: DemandPatch & { title: string }): Promise<Demand> {
  const data = await request<unknown>("/api/demands", {
    method: "POST",
    body: JSON.stringify(patchBody(patch)),
  });
  const row = data && typeof data === "object" && "demand" in data
    ? (data as { demand: unknown }).demand
    : data;
  return normalizeDemand(row);
}

export async function appendDemandProgress(id: string, text: string): Promise<void> {
  await request<unknown>(`/api/demands/${encodeURIComponent(id)}/progress`, {
    method: "POST",
    body: JSON.stringify({ text }),
  });
}

export async function fetchProjects(): Promise<Project[]> {
  const data = await request<{ projects?: unknown[] } | unknown[]>("/api/projects");
  const rows = Array.isArray(data) ? data : data.projects ?? [];
  return rows.map(normalizeProject);
}

export async function createProject(input: {
  name: string;
  description?: string;
  color?: string;
}): Promise<Project> {
  const data = await request<unknown>("/api/projects", {
    method: "POST",
    body: JSON.stringify({
      name: input.name,
      description: input.description ?? "",
      color: input.color ?? "#60a5fa",
      status: "active",
    }),
  });
  const row = data && typeof data === "object" && "project" in data
    ? (data as { project: unknown }).project
    : data;
  return normalizeProject(row);
}

export async function updateProject(
  id: string,
  patch: Partial<Pick<Project, "name" | "description" | "color">>,
): Promise<Project> {
  return normalizeProject(await request<unknown>(`/api/projects/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  }));
}

export async function fetchFeishuIntegration(): Promise<FeishuDocIntegration> {
  const raw = await request<JsonRecord>("/api/integrations/feishu-doc");
  return {
    status: String(raw.status ?? "never"),
    documentUrl: stringValue(raw, "documentUrl", "doc_url") ?? stringValue(raw, "document_url") ?? stringValue(raw, "url"),
    lastSyncedAt: stringValue(raw, "lastSyncedAt", "last_success_at") ?? stringValue(raw, "last_synced_at"),
    lastError: stringValue(raw, "lastError", "last_error") ?? stringValue(raw, "error"),
  };
}

export async function syncFeishuIntegration(): Promise<void> {
  await request<unknown>("/api/integrations/feishu-doc/sync", { method: "POST" });
}

export async function fetchFeishuChatIntegration(): Promise<FeishuChatIntegration> {
  const raw = await request<JsonRecord>("/api/integrations/feishu-chat");
  return {
    enabled: Boolean(raw.enabled),
    timezone: String(raw.timezone ?? "Asia/Shanghai"),
    activeStart: String(raw.active_start ?? "09:00"),
    activeEnd: String(raw.active_end ?? "23:00"),
    intervalMinutes: Number(raw.interval_minutes ?? 30),
    overlapMinutes: Number(raw.overlap_minutes ?? 5),
    excludedChatIds: Array.isArray(raw.excluded_chat_ids) ? raw.excluded_chat_ids.map(String) : [],
    lastSuccessEnd: meaningfulTimestamp(stringValue(raw, "lastSuccessEnd", "last_success_end")),
    nextRunAt: meaningfulTimestamp(stringValue(raw, "nextRunAt", "next_run_at")),
    status: String(raw.status ?? "disabled"),
    lastError: stringValue(raw, "lastError", "last_error"),
  };
}

export async function fetchFeishuIngestionRuns(): Promise<FeishuIngestionRun[]> {
  const raw = await request<{ runs?: JsonRecord[] }>("/api/integrations/feishu-chat/runs?limit=5");
  return (raw.runs ?? []).map((row) => ({
    id: String(row.id ?? ""), status: String(row.status ?? ""), trigger: String(row.trigger ?? ""),
    windowStart: String(row.window_start ?? ""), windowEnd: String(row.window_end ?? ""),
    messagesSeen: Number(row.messages_seen ?? 0), messagesCandidate: Number(row.messages_candidate ?? 0),
    created: Number(row.created ?? 0), updated: Number(row.updated ?? 0), skipped: Number(row.skipped ?? 0),
    reviewCount: Number(row.review_count ?? 0), inputTokens: Number(row.input_tokens ?? 0),
    cachedInputTokens: Number(row.cached_input_tokens ?? 0), outputTokens: Number(row.output_tokens ?? 0),
    startedAt: String(row.started_at ?? ""), finishedAt: meaningfulTimestamp(stringValue(row, "finishedAt", "finished_at")),
    error: stringValue(row, "error"),
  }));
}

export async function scanFeishuNow(): Promise<void> {
  await request<unknown>("/api/integrations/feishu-chat/scan", { method: "POST" });
}

export async function fetchDemandReviews(): Promise<DemandReview[]> {
  const raw = await request<{ reviews?: JsonRecord[] }>("/api/demand-reviews?status=pending");
  return (raw.reviews ?? []).map((row) => ({
    id: String(row.id ?? ""), status: String(row.status ?? "pending"),
    suggestedDemandId: stringValue(row, "suggestedDemandId", "suggested_demand_id"),
    progressText: String(row.progress_text ?? ""), progressDedupeKey: String(row.progress_dedupe_key ?? ""),
    source: normalizeSources([row.source])[0] ?? { kind: "feishu-im", label: "来源记录" },
    confidence: Number(row.confidence ?? 0), rationale: stringValue(row, "rationale"),
    createdAt: String(row.created_at ?? ""),
  }));
}

export async function acceptDemandReview(id: string, demandId?: string): Promise<void> {
  await request<unknown>(`/api/demand-reviews/${encodeURIComponent(id)}/accept`, {
    method: "POST", body: JSON.stringify(demandId ? { demand_id: demandId } : {}),
  });
}

export async function dismissDemandReview(id: string): Promise<void> {
  await request<unknown>(`/api/demand-reviews/${encodeURIComponent(id)}/dismiss`, { method: "POST" });
}

export function createWorkbenchEventSource(): EventSource {
  const key = getKey();
  return new EventSource(key ? `/events?key=${encodeURIComponent(key)}` : "/events");
}
