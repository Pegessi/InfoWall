export type WorkbenchSection = "pending" | "demands" | "projects";

export type AppRoute =
  | { mode: "wall" }
  | { mode: "workbench"; section: WorkbenchSection };

export type AppMode = AppRoute["mode"];

export type DefaultView = "infowall" | "workbench";

export const DEFAULT_ROUTE: AppRoute = {
  mode: "workbench",
  section: "demands",
};

export function defaultRouteForView(defaultView: DefaultView): AppRoute {
  return defaultView === "infowall"
    ? { mode: "wall" }
    : { mode: "workbench", section: "demands" };
}

const WORKBENCH_SECTIONS = new Set<WorkbenchSection>([
  "pending",
  "demands",
  "projects",
]);

function isWorkbenchSection(value: string): value is WorkbenchSection {
  return WORKBENCH_SECTIONS.has(value as WorkbenchSection);
}

export function parseAppRoute(
  hash: string,
  defaultView: DefaultView = "workbench",
): AppRoute {
  const path = hash.replace(/^#\/?/, "").replace(/\/+$/, "");

  if (path === "wall") return { mode: "wall" };

  // Canonicalize links from the prototype navigation.
  if (path === "workbench/today" || path === "workbench/items") {
    return { mode: "workbench", section: "demands" };
  }
  if (path === "workbench/inbox") {
    return { mode: "workbench", section: "pending" };
  }

  const [mode, rawSection, ...rest] = path.split("/");
  const section = rawSection ?? "";
  if (
    mode === "workbench" &&
    rest.length === 0 &&
    isWorkbenchSection(section)
  ) {
    return { mode: "workbench", section };
  }

  return defaultRouteForView(defaultView);
}

export function appRouteToHash(route: AppRoute): string {
  return route.mode === "wall" ? "#wall" : `#workbench/${route.section}`;
}

export function navigateToAppRoute(route: AppRoute): void {
  const nextHash = appRouteToHash(route);
  if (window.location.hash !== nextHash) window.location.hash = nextHash;
}

export function canonicalizeAppRoute(route: AppRoute): void {
  const nextHash = appRouteToHash(route);
  if (window.location.hash === nextHash) return;

  window.history.replaceState(
    window.history.state,
    "",
    `${window.location.pathname}${window.location.search}${nextHash}`,
  );
}
