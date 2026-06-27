import { Loader2 } from "lucide-react";
import { useFeed } from "@/hooks/useFeed";
import { ItemCard } from "./ItemCard";
import { EmptyState } from "./EmptyState";

export function FeedList() {
  const { items, loading, error, hasMore, loadingMore, loadMore, pinItem, deleteItem } =
    useFeed();

  return (
    <div className="space-y-6">
      {loading && items.length === 0 && (
        <>
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="rounded-xl bg-[hsl(var(--muted))] h-32 animate-pulse"
            />
          ))}
        </>
      )}

      {!loading && items.length === 0 && !error && <EmptyState />}

      {error && items.length === 0 && (
        <div className="rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] p-6 text-center text-sm text-[hsl(var(--negative))]">
          {error}
        </div>
      )}

      {items.map((item) => (
        <ItemCard
          key={item.id}
          item={item}
          onPin={pinItem}
          onDelete={deleteItem}
        />
      ))}

      {hasMore && (
        <div className="pt-2">
          <button
            type="button"
            onClick={() => void loadMore()}
            disabled={loadingMore}
            className="mx-auto block rounded-md border border-[hsl(var(--border))] px-4 py-2 text-sm transition-colors hover:bg-[hsl(var(--muted))] disabled:opacity-60"
          >
            {loadingMore ? (
              <span className="inline-flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                Loading…
              </span>
            ) : (
              "Load more"
            )}
          </button>
        </div>
      )}
    </div>
  );
}
