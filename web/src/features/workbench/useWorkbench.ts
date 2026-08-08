import { useCallback, useEffect, useRef, useState } from "react";
import {
  appendDemandProgress,
  createDemand as apiCreateDemand,
  createProject as apiCreateProject,
  createWorkbenchEventSource,
  fetchDemand,
  fetchDemands,
  fetchFeishuIntegration,
  fetchProjects,
  syncFeishuIntegration,
  updateDemand as apiUpdateDemand,
  updateProject as apiUpdateProject,
} from "./api";
import type { Demand, DemandPatch, DemandStatus, FeishuDocIntegration, Project } from "./model";

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : "请求失败，请稍后重试";
}

export function useWorkbench() {
  const [demands, setDemands] = useState<Demand[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [integration, setIntegration] = useState<FeishuDocIntegration | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [mutating, setMutating] = useState(false);
  const refreshTimer = useRef<number | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    const [demandsResult, projectsResult, integrationResult] = await Promise.allSettled([
      fetchDemands(),
      fetchProjects(),
      fetchFeishuIntegration(),
    ]);
    if (demandsResult.status === "fulfilled") setDemands(demandsResult.value);
    if (projectsResult.status === "fulfilled") setProjects(projectsResult.value);
    if (integrationResult.status === "fulfilled") setIntegration(integrationResult.value);
    const failed = [demandsResult, projectsResult, integrationResult].find((result) => result.status === "rejected");
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
    ["demand.created", "demand.updated", "demand.deleted", "demand.progress", "project.created", "project.updated", "project.deleted", "feishu_sync.updated"]
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
    addProgress: (id: string, text: string) => mutate(() => appendDemandProgress(id, text)),
    syncFeishu: () => mutate(syncFeishuIntegration),
  };
}
