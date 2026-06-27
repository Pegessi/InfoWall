import { useCallback, useEffect, useRef, useState } from "react";
import type { Item } from "@/lib/types";
import {
  fetchItems as apiFetchItems,
  pinItem as apiPinItem,
  deleteItem as apiDeleteItem,
  createEventSource,
} from "@/lib/api";

const PAGE_SIZE = 50;

export function useFeed() {
  const [items, setItems] = useState<Item[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    apiFetchItems(PAGE_SIZE, 0)
      .then((data) => {
        if (cancelled) return;
        setItems(data);
        setHasMore(data.length === PAGE_SIZE);
        setError(null);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : "Failed to load items");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    const es = createEventSource();
    esRef.current = es;

    es.addEventListener("item.new", (ev) => {
      try {
        const item = JSON.parse((ev as MessageEvent).data) as Item;
        setItems((prev) => {
          if (prev.some((p) => p.id === item.id)) return prev;
          return [item, ...prev];
        });
      } catch {
        // ignore
      }
    });

    es.addEventListener("item.pin", (ev) => {
      try {
        const { id, pinned } = JSON.parse((ev as MessageEvent).data) as {
          id: string;
          pinned: boolean;
        };
        setItems((prev) =>
          prev.map((it) => (it.id === id ? { ...it, pinned } : it))
        );
      } catch {
        // ignore
      }
    });

    es.addEventListener("item.delete", (ev) => {
      try {
        const { id } = JSON.parse((ev as MessageEvent).data) as { id: string };
        setItems((prev) => prev.filter((it) => it.id !== id));
      } catch {
        // ignore
      }
    });

    es.onerror = () => {
      // Let browser auto-reconnect; surface a non-fatal error if no items.
    };

    return () => {
      cancelled = true;
      es.close();
      esRef.current = null;
    };
  }, []);

  const loadMore = useCallback(async () => {
    setLoadingMore(true);
    try {
      const offset = items.length;
      const data = await apiFetchItems(PAGE_SIZE, offset);
      setItems((prev) => [...prev, ...data]);
      setHasMore(data.length === PAGE_SIZE);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to load more items");
    } finally {
      setLoadingMore(false);
    }
  }, [items.length]);

  const pinItem = useCallback(async (id: string, pinned: boolean) => {
    setItems((prev) =>
      prev.map((it) => (it.id === id ? { ...it, pinned } : it))
    );
    try {
      await apiPinItem(id, pinned);
    } catch (e: unknown) {
      setItems((prev) =>
        prev.map((it) => (it.id === id ? { ...it, pinned: !pinned } : it))
      );
      setError(e instanceof Error ? e.message : "Failed to pin item");
    }
  }, []);

  const deleteItem = useCallback(async (id: string) => {
    const previous = items;
    setItems((prev) => prev.filter((it) => it.id !== id));
    try {
      await apiDeleteItem(id);
    } catch (e: unknown) {
      setItems(previous);
      setError(e instanceof Error ? e.message : "Failed to delete item");
    }
  }, [items]);

  return {
    items,
    loading,
    error,
    hasMore,
    loadingMore,
    loadMore,
    pinItem,
    deleteItem,
  };
}
