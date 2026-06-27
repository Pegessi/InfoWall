import { Pin, PinOff, Trash2 } from "lucide-react";
import type { Item } from "@/lib/types";
import { formatRelative, formatAbsolute } from "@/lib/time";
import { ICON_MAP } from "./iconMap";

interface CardHeaderProps {
  item: Item;
  onPin: (id: string, pinned: boolean) => void;
  onDelete: (id: string) => void;
}

function hostOf(url?: string): string | null {
  if (!url) return null;
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return null;
  }
}

export function CardHeader({ item, onPin, onDelete }: CardHeaderProps) {
  const Icon = ICON_MAP[item.type] ?? ICON_MAP.note;
  const absTime = formatAbsolute(item.created_at);
  const relTime = formatRelative(item.created_at);
  const hostname = hostOf(item.meta?.url);

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
        <div className="flex items-center gap-2">
          <span className="text-sm text-[hsl(var(--muted-foreground))] capitalize">
            {item.type === "stock-chart" ? "chart" : item.type}
          </span>
        </div>
        {item.meta?.url ? (
          <a
            href={item.meta.url}
            target="_blank"
            rel="noreferrer noopener"
            className="mt-0.5 block truncate font-semibold text-base text-[hsl(var(--accent))] hover:underline"
            title={item.title}
          >
            {item.title}
          </a>
        ) : (
          <h2 className="mt-0.5 font-semibold text-base leading-snug" title={item.title}>
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
      <div className="hidden group-hover:flex gap-1 pt-0.5">
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
        <button
          type="button"
          onClick={() => onDelete(item.id)}
          title="Delete"
          aria-label="Delete item"
          className="rounded p-1.5 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-[hsl(var(--negative))]"
        >
          <Trash2 className="h-4 w-4" strokeWidth={1.75} />
        </button>
      </div>
    </div>
  );
}
