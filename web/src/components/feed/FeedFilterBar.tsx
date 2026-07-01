import { Pin, Search, X } from "lucide-react";
import { formatTopicLabel } from "@/lib/feedLayout";
import { isFilterActive, type FeedFilter, type PinnedFilter } from "@/lib/feedFilter";
import { cn } from "@/lib/utils";

interface FeedFilterBarProps {
  filter: FeedFilter;
  availableTypes: string[];
  /** number of items matching the active filter */
  resultCount: number;
  /** total number of loaded items */
  totalCount: number;
  /** true when older history is available but not loaded yet */
  hasMore: boolean;
  onQueryChange: (query: string) => void;
  onTypeChange: (type: string) => void;
  onPinnedChange: (pinned: PinnedFilter) => void;
  onReset: () => void;
}

export function FeedFilterBar({
  filter,
  availableTypes,
  resultCount,
  totalCount,
  hasMore,
  onQueryChange,
  onTypeChange,
  onPinnedChange,
  onReset,
}: FeedFilterBarProps) {
  const active = isFilterActive(filter);

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* Search */}
      <div className="relative min-w-0 flex-1 basis-48">
        <Search
          className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-[hsl(var(--muted-foreground))]"
          strokeWidth={1.75}
        />
        <input
          type="search"
          inputMode="search"
          value={filter.query}
          onChange={(event) => onQueryChange(event.target.value)}
          placeholder="Search feed…"
          aria-label="Search feed"
          className="h-9 w-full rounded-md border border-[hsl(var(--border))] bg-[hsl(var(--card))] pl-8 pr-8 text-sm text-[hsl(var(--foreground))] shadow-sm outline-none transition-colors placeholder:text-[hsl(var(--muted-foreground))] focus:ring-2 focus:ring-[hsl(var(--ring))]"
        />
        {filter.query !== "" && (
          <button
            type="button"
            aria-label="Clear search text"
            title="Clear search"
            onClick={() => onQueryChange("")}
            className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" strokeWidth={2} />
          </button>
        )}
      </div>

      {/* Type filter */}
      <select
        aria-label="Filter by type"
        value={filter.type}
        onChange={(event) => onTypeChange(event.target.value)}
        className="h-9 max-w-[40vw] rounded-md border border-[hsl(var(--border))] bg-[hsl(var(--card))] px-2.5 text-sm text-[hsl(var(--foreground))] shadow-sm outline-none transition-colors hover:bg-[hsl(var(--muted))] focus:ring-2 focus:ring-[hsl(var(--ring))]"
      >
        <option value="">All types</option>
        {availableTypes.map((type) => (
          <option key={type} value={type}>
            {formatTopicLabel(type)}
          </option>
        ))}
      </select>

      {/* Pinned toggle */}
      <button
        type="button"
        aria-pressed={filter.pinned === "pinned"}
        aria-label="Show pinned items only"
        title={filter.pinned === "pinned" ? "Showing pinned only" : "Show pinned only"}
        onClick={() =>
          onPinnedChange(filter.pinned === "pinned" ? "all" : "pinned")
        }
        className={cn(
          "inline-flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-[hsl(var(--border))] px-2.5 text-sm shadow-sm transition-colors",
          filter.pinned === "pinned"
            ? "bg-[hsl(var(--accent))] text-[hsl(var(--accent-foreground))]"
            : "bg-[hsl(var(--card))] text-[hsl(var(--muted-foreground))] hover:bg-[hsl(var(--muted))] hover:text-foreground"
        )}
      >
        <Pin
          className="h-4 w-4"
          strokeWidth={1.75}
          fill={filter.pinned === "pinned" ? "currentColor" : "none"}
        />
        <span className="hidden sm:inline">Pinned</span>
      </button>

      {/* Result count. Items are filtered server-side across the full history;
          this shows how many are loaded so far, with a + when more pages exist. */}
      <span
        aria-live="polite"
        className="shrink-0 whitespace-nowrap text-xs tabular-nums text-[hsl(var(--muted-foreground))]"
      >
        {active
          ? `${resultCount}${hasMore ? "+" : ""} ${resultCount === 1 ? "match" : "matches"}`
          : hasMore
            ? `${totalCount}+ loaded`
            : `${totalCount} ${totalCount === 1 ? "item" : "items"}`}
      </span>

      {/* Reset */}
      {active && (
        <button
          type="button"
          onClick={onReset}
          aria-label="Clear all filters"
          title="Clear filters"
          className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-[hsl(var(--border))] bg-[hsl(var(--card))] px-2.5 text-sm text-[hsl(var(--muted-foreground))] shadow-sm transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
        >
          <X className="h-3.5 w-3.5" strokeWidth={2} />
          <span className="hidden sm:inline">Clear</span>
        </button>
      )}
    </div>
  );
}
