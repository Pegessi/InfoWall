export type DemandStatus =
  | "pending"
  | "planned"
  | "active"
  | "waiting"
  | "done"
  | "dismissed";

export type DemandPriority = "p0" | "p1" | "p2" | "p3" | "none";
export type WorkbenchSection = "pending" | "demands" | "projects";

export interface DemandProgressLink {
  kind: string;
  externalId?: string;
  title: string;
  url: string;
  state?: string;
  dedupeKey?: string;
}

export interface DemandProgress {
  id: string;
  text: string;
  createdAt: string;
  links: DemandProgressLink[];
}

export interface DemandSource {
  id?: string;
  kind: string;
  label: string;
  url?: string;
  createdAt?: string;
  externalId?: string;
  chatId?: string;
  chatName?: string;
  senderId?: string;
  senderName?: string;
  messageTime?: string;
  excerpt?: string;
  dedupeKey?: string;
}

export interface Demand {
  id: string;
  title: string;
  summary: string;
  status: DemandStatus;
  priority: DemandPriority;
  projectId?: string;
  projectHint?: string;
  nextStep?: string;
  waitingFor?: string;
  createdAt: string;
  updatedAt: string;
  progress: DemandProgress[];
  sources: DemandSource[];
}

export interface Project {
  id: string;
  name: string;
  description: string;
  color: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface FeishuDocIntegration {
  status: "idle" | "syncing" | "success" | "error" | "never" | string;
  documentUrl?: string;
  lastSyncedAt?: string;
  lastError?: string;
}

export interface ActivityIntegration {
  enabled: boolean;
  timezone: string;
  activeStart: string;
  activeEnd: string;
  intervalMinutes: number;
  overlapMinutes: number;
  excludedChatIds: string[];
  lastSuccessEnd?: string;
  nextRunAt?: string;
  status: string;
  lastError?: string;
  analyzerRoute?: string;
  analyzerProfileId?: string;
  analyzerProfileFingerprint?: string;
  analyzerHealthy: boolean;
  fallbackActive: boolean;
  lastPrimaryError?: string;
  sourceWatermarks: Record<string, string>;
}

export type FeishuChatIntegration = ActivityIntegration;

export interface ActivityIngestionRun {
  id: string;
  status: string;
  trigger: string;
  windowStart: string;
  windowEnd: string;
  messagesSeen: number;
  messagesCandidate: number;
  created: number;
  updated: number;
  skipped: number;
  reviewCount: number;
  inputTokens: number;
  cachedInputTokens: number;
  outputTokens: number;
  feishuCandidates: number;
  codexCandidates: number;
  claudeCandidates: number;
  analyzerRoute?: string;
  analyzerProfileId?: string;
  analyzerProfileFingerprint?: string;
  analyzerHealthy: boolean;
  fallbackUsed: boolean;
  primaryError?: string;
  startedAt: string;
  finishedAt?: string;
  error?: string;
}

export type FeishuIngestionRun = ActivityIngestionRun;

export interface DemandReview {
  id: string;
  status: string;
  suggestedDemandId?: string;
  progressText: string;
  progressDedupeKey: string;
  source: DemandSource;
  links: DemandProgressLink[];
  confidence: number;
  rationale?: string;
  createdAt: string;
}

export interface DemandPatch {
  title?: string;
  summary?: string;
  status?: DemandStatus;
  priority?: DemandPriority;
  projectId?: string | null;
  nextStep?: string | null;
  waitingFor?: string | null;
}
