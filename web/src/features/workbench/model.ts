export type DemandStatus =
  | "pending"
  | "planned"
  | "active"
  | "waiting"
  | "done"
  | "dismissed";

export type DemandPriority = "p0" | "p1" | "p2" | "p3" | "none";
export type WorkbenchSection = "pending" | "demands" | "projects";

export interface DemandProgress {
  id: string;
  text: string;
  createdAt: string;
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

export interface DemandPatch {
  title?: string;
  summary?: string;
  status?: DemandStatus;
  priority?: DemandPriority;
  projectId?: string | null;
  nextStep?: string | null;
  waitingFor?: string | null;
}
