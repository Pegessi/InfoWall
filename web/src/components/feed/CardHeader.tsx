import { Check, Pin, PinOff, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { Item } from "@/lib/types";
import { formatRelative, formatAbsolute } from "@/lib/time";
import { cn } from "@/lib/utils";
import { ICON_MAP } from "./iconMap";

interface CardHeaderProps {
  item: Item;
  onPin: (id: string, pinned: boolean) => void;
  onDelete: (id: string) => void;
  showTypeLabel?: boolean;
}

function hostOf(url?: string): string | null {
  if (!url) return null;
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return null;
  }
}

export function CardHeader({
  item,
  onPin,
  onDelete,
  showTypeLabel = true,
}: CardHeaderProps) {
  const Icon = ICON_MAP[item.type] ?? ICON_MAP.note;
  const absTime = formatAbsolute(item.created_at);
  const relTime = formatRelative(item.created_at);
  const hostname = hostOf(item.meta?.url);

  // Two-step delete guard: the first click arms a confirm state; only a second
  // confirm actually deletes. A short timeout silently disarms so a forgotten
  // armed card does not stay primed for deletion.
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const resetTimer = useRef<number | null>(null);
  const confirmButtonRef = useRef<HTMLButtonElement | null>(null);

  const clearResetTimer = () => {
    if (resetTimer.current !== null) {
      window.clearTimeout(resetTimer.current);
      resetTimer.current = null;
    }
  };

  const armDelete = () => {
    setConfirmingDelete(true);
    clearResetTimer();
    resetTimer.current = window.setTimeout(() => {
      setConfirmingDelete(false);
      resetTimer.current = null;
    }, 3000);
  };

  const cancelDelete = () => {
    clearResetTimer();
    setConfirmingDelete(false);
  };

  const confirmDelete = () => {
    clearResetTimer();
    setConfirmingDelete(false);
    onDelete(item.id);
  };

  // Move focus to the confirm button when armed, and clean up the timer.
  useEffect(() => {
    if (confirmingDelete) confirmButtonRef.current?.focus();
  }, [confirmingDelete]);
  useEffect(() => () => clearResetTimer(), []);

  return (
    <div className="flex items-start gap-3">
      {/* Left: type icon */}
      <div className="flex flex-col items-center gap-1 pt-0.5">
        <Icon
          className="h-3.5 w-3.5 text-[hsl(var(--muted-foreground))]"
          strokeWidth={2}
        />
        {item.pinned && (
          <Pin className="h-3 w-3 text-[hsl(var(--accent))]" strokeWidth={2.5} fill="currentColor" />
        )}
      </div>

      {/* Middle: title + meta */}
      <div className="flex-1 min-w-0">
        {showTypeLabel && (
          <div className="flex items-center gap-2">
            <span className="text-sm text-[hsl(var(--muted-foreground))] capitalize">
              {item.type === "stock-chart" ? "chart" : item.type}
            </span>
          </div>
        )}
        {item.meta?.url ? (
          <a
            href={item.meta.url}
            target="_blank"
            rel="noreferrer noopener"
            className="block truncate font-semibold text-base text-[hsl(var(--accent))] hover:underline"
            title={item.title}
          >
            {item.title}
          </a>
        ) : (
          <h2 className="font-semibold text-base leading-snug" title={item.title}>
            {item.title}
          </h2>
        )}
        <div className="mt-1 flex flex-wrap items-center gap-1 text-xs text-[hsl(var(--muted-foreground))]">
          <time dateTime={item.created_at} title={absTime} className="whitespace-nowrap">
            {relTime}
          </time>
          {hostname && (
            <>
              <span className="opacity-50">·</span>
              <span className="whitespace-nowrap">{hostname}</span>
            </>
          )}
          {item.tags?.map((tag) => (
            <span
              key={tag}
              className="ml-0.5 whitespace-nowrap rounded-md bg-[hsl(var(--muted))] px-2 py-0.5 text-[11px]"
            >
              #{tag}
            </span>
          ))}
        </div>
      </div>

      {/* Right: actions */}
      <div
        className={cn(
          "gap-1 pt-0.5",
          confirmingDelete ? "flex" : "hidden group-hover:flex"
        )}
      >
        <button
          type="button"
          onClick={() => onPin(item.id, !item.pinned)}
          title={item.pinned ? "Unpin" : "Pin"}
          aria-label={item.pinned ? "Unpin item" : "Pin item"}
          className="rounded p-1.5 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
        >
          {item.pinned ? (
            <PinOff className="h-4 w-4" strokeWidth={1.75} />
          ) : (
            <Pin className="h-4 w-4" strokeWidth={1.75} />
          )}
        </button>

        {confirmingDelete ? (
          <div
            className="flex items-center gap-1"
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault();
                cancelDelete();
              }
            }}
          >
            <button
              type="button"
              ref={confirmButtonRef}
              onClick={confirmDelete}
              title="Confirm delete"
              aria-label={`Confirm delete: ${item.title || "item"}`}
              className="inline-flex items-center gap-1 rounded bg-[hsl(var(--negative))] px-2 py-1 text-xs font-medium text-white transition-colors hover:opacity-90"
            >
              <Check className="h-3.5 w-3.5" strokeWidth={2.25} />
              <span>Delete</span>
            </button>
            <button
              type="button"
              onClick={cancelDelete}
              title="Cancel"
              aria-label="Cancel delete"
              className="rounded p-1.5 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
            >
              <X className="h-4 w-4" strokeWidth={1.75} />
            </button>
          </div>
        ) : (
          <button
            type="button"
            onClick={armDelete}
            title="Delete"
            aria-label="Delete item"
            className="rounded p-1.5 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-[hsl(var(--negative))]"
          >
            <Trash2 className="h-4 w-4" strokeWidth={1.75} />
          </button>
        )}
      </div>
    </div>
  );
}
