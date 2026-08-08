import { useEffect, useRef, useState } from "react";
import { Header } from "@/components/layout/Header";
import { FeedList } from "@/components/feed/FeedList";
import {
  WorkbenchPage,
  type WorkbenchSection,
} from "@/features/workbench/WorkbenchPage";
import { useTheme } from "@/hooks/useTheme";
import {
  appRouteToHash,
  canonicalizeAppRoute,
  navigateToAppRoute,
  parseAppRoute,
  type AppMode,
  type AppRoute,
  type DefaultView,
} from "@/lib/appRoute";
import { saveFrontendDefaultView } from "@/lib/frontendConfig";

interface AppProps {
  defaultView?: DefaultView;
}

export default function App({ defaultView: initialDefaultView = "workbench" }: AppProps) {
  // Initialize theme (applies class + persists)
  useTheme();
  const [defaultView, setDefaultView] = useState<DefaultView>(initialDefaultView);
  const [defaultViewSaving, setDefaultViewSaving] = useState(false);
  const [defaultViewError, setDefaultViewError] = useState<string | null>(null);
  const [route, setRoute] = useState<AppRoute>(() =>
    parseAppRoute(window.location.hash, defaultView),
  );
  const lastWorkbenchSection = useRef<WorkbenchSection>(
    route.mode === "workbench" ? route.section : "demands",
  );

  useEffect(() => {
    canonicalizeAppRoute(route);

    const syncRouteFromHash = () => {
      const nextRoute = parseAppRoute(window.location.hash, defaultView);
      if (nextRoute.mode === "workbench") {
        lastWorkbenchSection.current = nextRoute.section;
      }
      setRoute(nextRoute);

      // Invalid hashes still land somewhere useful and become shareable.
      if (window.location.hash !== appRouteToHash(nextRoute)) {
        canonicalizeAppRoute(nextRoute);
      }
    };

    window.addEventListener("hashchange", syncRouteFromHash);
    return () => window.removeEventListener("hashchange", syncRouteFromHash);
    // defaultView is immutable after bootstrap; hash changes drive later routes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [defaultView]);

  const navigateMode = (mode: AppMode) => {
    if (mode === "wall") {
      navigateToAppRoute({ mode: "wall" });
      return;
    }

    navigateToAppRoute({
      mode: "workbench",
      section: lastWorkbenchSection.current,
    });
  };

  const navigateWorkbenchSection = (section: WorkbenchSection) => {
    lastWorkbenchSection.current = section;
    navigateToAppRoute({ mode: "workbench", section });
  };

  const updateDefaultView = async (next: DefaultView): Promise<boolean> => {
    setDefaultViewSaving(true);
    setDefaultViewError(null);
    try {
      setDefaultView(await saveFrontendDefaultView(next));
      return true;
    } catch (error) {
      setDefaultViewError(error instanceof Error ? error.message : "保存失败");
      return false;
    } finally {
      setDefaultViewSaving(false);
    }
  };

  return (
    <div className={route.mode === "workbench" ? "flex h-dvh min-h-0 flex-col overflow-hidden" : "min-h-dvh"}>
      <Header
        mode={route.mode}
        defaultView={defaultView}
        defaultViewSaving={defaultViewSaving}
        defaultViewError={defaultViewError}
        onNavigate={navigateMode}
        onDefaultViewChange={updateDefaultView}
      />
      {route.mode === "wall" ? (
        <main className="min-h-[calc(100vh-3rem)] w-full px-2 py-2 sm:px-3 lg:px-4">
          <FeedList />
        </main>
      ) : (
        <main className="min-h-0 w-full flex-1 overflow-hidden bg-[hsl(var(--muted)/0.35)]">
          <WorkbenchPage
            section={route.section}
            onSectionChange={navigateWorkbenchSection}
          />
        </main>
      )}
    </div>
  );
}
