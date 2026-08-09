import { useCallback, useEffect, useRef, useState } from "react";
import {
  appendDemandProgress,
  acceptDemandReview,
  createDemand as apiCreateDemand,
  createProject as apiCreateProject,
  createWorkbenchEventSource,
  fetchDemand,
  fetchDemands,
  fetchFeishuIntegration,
  fetchFeishuChatIntegration,
  fetchFeishuIngestionRuns,
  fetchDemandReviews,
  fetchProjects,
  syncFeishuIntegration,
  scanFeishuNow,
  dismissDemandReview,
  updateDemand as apiUpdateDemand,
  updateProject as apiUpdateProject,
} from "./api";
import type { Demand, DemandPatch, DemandProgressLink, DemandReview, DemandStatus, FeishuChatIntegration, FeishuDocIntegration, FeishuIngestionRun, Project } from "./model";

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : "请求失败，请稍后重试";
}

export function useWorkbench() {
  const [demands, setDemands] = useState<Demand[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [integration, setIntegration] = useState<FeishuDocIntegration | null>(null);
  const [chatIntegration, setChatIntegration] = useState<FeishuChatIntegration | null>(null);
  const [ingestionRuns, setIngestionRuns] = useState<FeishuIngestionRun[]>([]);
  const [reviews, setReviews] = useState<DemandReview[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [mutating, setMutating] = useState(false);
  const refreshTimer = useRef<number | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    const [demandsResult, projectsResult, integrationResult, chatResult, runsResult, reviewsResult] = await Promise.allSettled([
      fetchDemands(),
      fetchProjects(),
      fetchFeishuIntegration(),
      fetchFeishuChatIntegration(),
      fetchFeishuIngestionRuns(),
      fetchDemandReviews(),
    ]);
    if (demandsResult.status === "fulfilled") setDemands(demandsResult.value);
    if (projectsResult.status === "fulfilled") setProjects(projectsResult.value);
    if (integrationResult.status === "fulfilled") setIntegration(integrationResult.value);
    if (chatResult.status === "fulfilled") setChatIntegration(chatResult.value);
    if (runsResult.status === "fulfilled") setIngestionRuns(runsResult.value);
    if (reviewsResult.status === "fulfilled") setReviews(reviewsResult.value);
    const failed = [demandsResult, projectsResult, integrationResult, chatResult, runsResult, reviewsResult].find((result) => result.status === "rejected");
    setError(failed?.status === "rejected" ? messageOf(failed.reason) : null);
    setLoading(false);
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  useEffect(() => {
    const events = createWorkbenchEventSource();
    const scheduleRefresh = () => {
      if (refreshTimer.current !== null) window.clearTimeout(refreshTimer.current);
      refreshTimer.current = window.setTimeout(() => void refresh(), 180);
    };
    ["demand.created", "demand.updated", "demand.deleted", "demand.progress", "project.created", "project.updated", "project.deleted", "feishu_sync.updated", "feishu_ingestion.updated", "demand_review.updated", "demand_review.accepted", "demand_review.dismissed"]
      .forEach((name) => events.addEventListener(name, scheduleRefresh));
    return () => {
      events.close();
      if (refreshTimer.current !== null) window.clearTimeout(refreshTimer.current);
    };
  }, [refresh]);

  const mutate = useCallback(async (operation: () => Promise<unknown>) => {
    setMutating(true);
    setError(null);
    try {
      await operation();
      await refresh();
      return true;
    } catch (caught) {
      setError(messageOf(caught));
      return false;
    } finally {
      setMutating(false);
    }
  }, [refresh]);

  return {
    demands,
    projects,
    integration,
    chatIntegration,
    ingestionRuns,
    reviews,
    loading,
    error,
    mutating,
    refresh,
    loadDemand: fetchDemand,
    createDemand: (patch: DemandPatch & { title: string }) => mutate(() => apiCreateDemand(patch)),
    createProject: (input: { name: string; description?: string; color?: string }) => mutate(() => apiCreateProject(input)),
    updateProject: (id: string, patch: Partial<Pick<Project, "name" | "description" | "color">>) =>
      mutate(() => apiUpdateProject(id, patch)),
    updateDemand: (id: string, patch: DemandPatch) => mutate(() => apiUpdateDemand(id, patch)),
    confirmDemand: (id: string, status: Extract<DemandStatus, "planned" | "active">, patch: DemandPatch) =>
      mutate(() => apiUpdateDemand(id, { ...patch, status })),
    dismissDemand: (id: string) => mutate(() => apiUpdateDemand(id, { status: "dismissed" })),
    addProgress: (id: string, text: string, links?: DemandProgressLink[]) => mutate(() => appendDemandProgress(id, text, links)),
    syncFeishu: () => mutate(syncFeishuIntegration),
    scanFeishu: () => mutate(scanFeishuNow),
    acceptReview: (id: string, demandId?: string) => mutate(() => acceptDemandReview(id, demandId)),
    dismissReview: (id: string) => mutate(() => dismissDemandReview(id)),
  };
}
