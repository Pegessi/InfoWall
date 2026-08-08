import { Check, House, Moon, Sun } from "lucide-react";
import { useTheme } from "@/hooks/useTheme";
import type { AppMode, DefaultView } from "@/lib/appRoute";

interface HeaderProps {
  mode: AppMode;
  defaultView: DefaultView;
  defaultViewSaving: boolean;
  defaultViewError: string | null;
  onNavigate: (mode: AppMode) => void;
  onDefaultViewChange: (defaultView: DefaultView) => Promise<boolean>;
}

const MODE_ITEMS: Array<{ mode: AppMode; label: string }> = [
  { mode: "workbench", label: "工作台" },
  { mode: "wall", label: "信息墙" },
];

const DEFAULT_VIEW_ITEMS: Array<{
  value: DefaultView;
  label: string;
  description: string;
}> = [
  {
    value: "workbench",
    label: "工作台",
    description: "打开根地址时进入需求清单",
  },
  {
    value: "infowall",
    label: "信息墙",
    description: "打开根地址时进入信息流",
  },
];

export function Header({
  mode,
  defaultView,
  defaultViewSaving,
  defaultViewError,
  onNavigate,
  onDefaultViewChange,
}: HeaderProps) {
  const { theme, toggleTheme } = useTheme();

  return (
    <header className="sticky top-0 z-20 shrink-0 border-b border-[hsl(var(--border))] bg-[hsl(var(--background)/0.86)] pt-[env(safe-area-inset-top)] backdrop-blur">
      <div className="flex h-12 w-full items-center justify-between gap-2 px-2 sm:px-3 lg:px-4">
        <div className="flex min-w-0 items-center gap-2 sm:gap-4">
          <div className="flex shrink-0 items-center gap-2" aria-label="InfoWall">
            <span
              aria-hidden="true"
              className="h-2 w-2 rounded-full bg-[hsl(var(--accent))]"
            />
            <span className="text-base font-semibold tracking-tight sm:text-lg">
              InfoWall
            </span>
          </div>

          <nav
            aria-label="顶层视图"
            className="flex items-center rounded-lg bg-[hsl(var(--muted))] p-0.5"
          >
            {MODE_ITEMS.map((item) => {
              const active = item.mode === mode;
              return (
                <button
                  key={item.mode}
                  type="button"
                  onClick={() => onNavigate(item.mode)}
                  aria-current={active ? "page" : undefined}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors sm:px-3 sm:text-sm ${
                    active
                      ? "bg-[hsl(var(--background))] text-[hsl(var(--foreground))] shadow-sm"
                      : "text-[hsl(var(--muted-foreground))] hover:text-[hsl(var(--foreground))]"
                  }`}
                >
                  {item.label}
                </button>
              );
            })}
          </nav>
        </div>

        <div className="flex shrink-0 items-center gap-0.5">
          <details className="group relative">
            <summary
              aria-label="设置默认首页"
              title="设置默认首页"
              className="flex h-9 cursor-pointer list-none items-center gap-1.5 rounded-md px-2 text-xs font-medium text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground [&::-webkit-details-marker]:hidden"
            >
              <House className="h-4 w-4" strokeWidth={1.75} />
              <span className="hidden lg:inline">默认首页</span>
            </summary>
            <div className="absolute right-0 top-11 z-50 w-72 max-w-[calc(100vw-1rem)] rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--background))] p-2 shadow-2xl shadow-black/20">
              <div className="px-2 pb-2 pt-1">
                <div className="text-sm font-semibold">默认首页</div>
                <p className="mt-1 text-xs leading-5 text-[hsl(var(--muted-foreground))]">
                  打开不带 # 的地址时进入哪个页面。具体页面链接不受影响。
                </p>
              </div>
              <div className="space-y-1">
                {DEFAULT_VIEW_ITEMS.map((item) => {
                  const selected = item.value === defaultView;
                  return (
                    <button
                      key={item.value}
                      type="button"
                      aria-pressed={selected}
                      disabled={defaultViewSaving}
                      onClick={async (event) => {
                        const details = event.currentTarget.closest("details");
                        if (await onDefaultViewChange(item.value)) {
                          details?.removeAttribute("open");
                        }
                      }}
                      className={`flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors disabled:cursor-wait disabled:opacity-60 ${selected ? "bg-[hsl(var(--muted))]" : "hover:bg-[hsl(var(--muted)/0.65)]"}`}
                    >
                      <span className={`inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full border ${selected ? "border-[hsl(var(--foreground))] bg-[hsl(var(--foreground))] text-[hsl(var(--background))]" : "border-[hsl(var(--border))]"}`}>
                        {selected && <Check className="h-3 w-3" strokeWidth={2.5} />}
                      </span>
                      <span className="min-w-0">
                        <span className="block text-sm font-medium">{item.label}</span>
                        <span className="mt-0.5 block text-xs text-[hsl(var(--muted-foreground))]">{item.description}</span>
                      </span>
                    </button>
                  );
                })}
              </div>
              {defaultViewError && (
                <div role="alert" className="mt-2 rounded-md bg-red-500/10 px-2 py-1.5 text-xs text-red-600 dark:text-red-300">
                  {defaultViewError}
                </div>
              )}
              <div className="mt-2 border-t border-[hsl(var(--border))] px-2 pt-2 text-[10px] leading-4 text-[hsl(var(--muted-foreground))]">
                保存到本地服务，所有设备共用；启动参数仅作为首次默认值。
              </div>
            </div>
          </details>

          <button
            type="button"
            onClick={toggleTheme}
            aria-label={theme === "dark" ? "切换到浅色主题" : "切换到深色主题"}
            title={theme === "dark" ? "切换到浅色主题" : "切换到深色主题"}
            className="inline-flex h-9 w-9 items-center justify-center rounded-md text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
          >
            {theme === "dark" ? (
              <Sun className="h-5 w-5" strokeWidth={1.75} />
            ) : (
              <Moon className="h-5 w-5" strokeWidth={1.75} />
            )}
          </button>
        </div>
      </div>
    </header>
  );
}
