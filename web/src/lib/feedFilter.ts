import type { Item } from "@/lib/types";

/** Pinned-state quick filter. */
export type PinnedFilter = "all" | "pinned";

/** The active feed filter. `type` of "" means "all types". */
export interface FeedFilter {
  query: string;
  type: string;
  pinned: PinnedFilter;
}

export const EMPTY_FEED_FILTER: FeedFilter = {
  query: "",
  type: "",
  pinned: "all",
};

/** True when the filter would not narrow the feed at all. */
export function isFilterActive(filter: FeedFilter): boolean {
  return (
    filter.query.trim() !== "" || filter.type !== "" || filter.pinned !== "all"
  );
}

/**
 * availableTypes returns the distinct item types present in the feed, sorted
 * alphabetically. Derived from the live items so unknown/new types appear
 * automatically — nothing is hardcoded. Empty/missing types fall back to "note".
 */
export function availableTypes(items: Item[]): string[] {
  const seen = new Set<string>();
  for (const item of items) {
    seen.add(item.type || "note");
  }
  return Array.from(seen).sort((a, b) => a.localeCompare(b));
}

/**
 * itemHaystack builds the lowercased searchable text for an item, defensively
 * tolerating missing fields. It scans title, body, type, tags, and the string
 * values found in meta (e.g. url, summary, description), recursing one level
 * into nested meta objects/arrays. Non-string meta values are ignored.
 */
function itemHaystack(item: Item): string {
  const parts: string[] = [];
  if (item.title) parts.push(item.title);
  if (item.body) parts.push(item.body);
  if (item.type) parts.push(item.type);
  if (Array.isArray(item.tags)) {
    for (const tag of item.tags) {
      if (typeof tag === "string") parts.push(tag);
    }
  }
  collectMetaStrings(item.meta, parts, 0);
  return parts.join("\n").toLowerCase();
}

function collectMetaStrings(value: unknown, out: string[], depth: number): void {
  if (value == null || depth > 2) return;
  if (typeof value === "string") {
    out.push(value);
    return;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return;
  }
  if (Array.isArray(value)) {
    for (const entry of value) collectMetaStrings(entry, out, depth + 1);
    return;
  }
  if (typeof value === "object") {
    for (const entry of Object.values(value as Record<string, unknown>)) {
      collectMetaStrings(entry, out, depth + 1);
    }
  }
}

/**
 * itemMatches reports whether an item satisfies the filter. Search is
 * case-insensitive; multiple whitespace-separated terms must all be present
 * (AND), making the query forgiving and incremental.
 */
export function itemMatches(item: Item, filter: FeedFilter): boolean {
  if (filter.pinned === "pinned" && !item.pinned) return false;
  if (filter.type !== "" && (item.type || "note") !== filter.type) return false;

  const query = filter.query.trim().toLowerCase();
  if (query === "") return true;

  const haystack = itemHaystack(item);
  return query.split(/\s+/).every((term) => haystack.includes(term));
}

/** filterItems returns the items matching the filter, preserving input order. */
export function filterItems(items: Item[], filter: FeedFilter): Item[] {
  if (!isFilterActive(filter)) return items;
  return items.filter((item) => itemMatches(item, filter));
}
