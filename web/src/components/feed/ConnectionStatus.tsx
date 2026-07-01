import { RefreshCw } from "lucide-react";
import type { ConnectionState } from "@/hooks/useFeed";
import { cn } from "@/lib/utils";

interface ConnectionStatusProps {
  state: ConnectionState;
  onRetry: () => void;
}

interface StatusMeta {
  label: string;
  /** dot color class (foreground/background via currentColor) */
  dot: string;
  /** whether the dot should pulse */
  pulse: boolean;
}

const STATUS_META: Record<ConnectionState, StatusMeta> = {
  live: {
    label: "Live",
    dot: "bg-[hsl(var(--positive))]",
    pulse: false,
  },
  connecting: {
    label: "Connecting",
    dot: "bg-[hsl(var(--muted-foreground))]",
    pulse: true,
  },
  reconnecting: {
    label: "Reconnecting",
    dot: "bg-amber-500",
    pulse: true,
  },
  disconnected: {
    label: "Offline",
    dot: "bg-[hsl(var(--negative))]",
    pulse: false,
  },
};

/**
 * ConnectionStatus is a compact live-stream indicator: a colored dot + short
 * status word, with a manual Retry button shown when the stream is down. It is
 * deliberately small so it informs operations without dominating the wall.
 */
export function ConnectionStatus({ state, onRetry }: ConnectionStatusProps) {
  const meta = STATUS_META[state];
  // The browser auto-retries a dropped stream (readyState stays CONNECTING), so
  // a down server usually shows as "reconnecting" rather than ever reaching
  // CLOSED. Offer the manual Retry whenever the stream is not healthy and not in
  // its initial connect, so an immediate reconnect + list refresh is reachable.
  const showRetry = state === "reconnecting" || state === "disconnected";

  return (
    <div
      className="inline-flex items-center gap-1.5"
      role="status"
      aria-live="polite"
      aria-label={`Live updates: ${meta.label}`}
      title={`Live updates: ${meta.label}`}
    >
      <span className="relative flex h-2 w-2 shrink-0">
        {meta.pulse && (
          <span
            className={cn(
              "absolute inline-flex h-full w-full animate-ping rounded-full opacity-60",
              meta.dot
            )}
          />
        )}
        <span className={cn("relative inline-flex h-2 w-2 rounded-full", meta.dot)} />
      </span>
      <span className="text-xs tabular-nums text-[hsl(var(--muted-foreground))]">
        {meta.label}
      </span>
      {showRetry && (
        <button
          type="button"
          onClick={onRetry}
          aria-label="Retry live connection"
          title="Retry connection and refresh"
          className="ml-0.5 inline-flex h-6 items-center gap-1 rounded border border-[hsl(var(--border))] px-1.5 text-[11px] text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
        >
          <RefreshCw className="h-3 w-3" strokeWidth={2} />
          <span className="hidden sm:inline">Retry</span>
        </button>
      )}
    </div>
  );
}
