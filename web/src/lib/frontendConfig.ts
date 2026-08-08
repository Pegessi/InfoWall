import type { DefaultView } from "./appRoute";

interface FrontendConfigResponse {
  default_view?: unknown;
}

function getKey(): string | null {
  try {
    return localStorage.getItem("infowall-key");
  } catch {
    return null;
  }
}

function parseDefaultView(payload: FrontendConfigResponse): DefaultView {
  return payload.default_view === "infowall" ? "infowall" : "workbench";
}

export async function fetchFrontendDefaultView(): Promise<DefaultView> {
  const response = await fetch("/api/config");
  if (!response.ok) throw new Error(`读取默认首页失败（${response.status}）`);
  return parseDefaultView((await response.json()) as FrontendConfigResponse);
}

export async function saveFrontendDefaultView(
  defaultView: DefaultView,
): Promise<DefaultView> {
  const key = getKey();
  const response = await fetch("/api/config", {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
      ...(key ? { Authorization: `Bearer ${key}` } : {}),
    },
    body: JSON.stringify({ default_view: defaultView }),
  });
  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as {
      error?: unknown;
    } | null;
    throw new Error(
      typeof payload?.error === "string"
        ? payload.error
        : `保存默认首页失败（${response.status}）`,
    );
  }
  return parseDefaultView((await response.json()) as FrontendConfigResponse);
}
