import type { Item } from "./types";

const API_BASE = "";

function getKey(): string | null {
  try {
    return localStorage.getItem("infowall-key");
  } catch {
    return null;
  }
}

function authHeaders(): Record<string, string> {
  const key = getKey();
  return key ? { Authorization: `Bearer ${key}` } : {};
}

function buildEventUrl(path: string): string {
  const key = getKey();
  return key ? `${API_BASE}${path}?key=${encodeURIComponent(key)}` : `${API_BASE}${path}`;
}

export async function fetchItems(
  limit = 50,
  offset = 0,
  type?: string
): Promise<Item[]> {
  const params = new URLSearchParams();
  params.set("limit", String(limit));
  params.set("offset", String(offset));
  if (type) params.set("type", type);
  const res = await fetch(`${API_BASE}/api/items?${params.toString()}`, {
    headers: { ...authHeaders() },
  });
  if (!res.ok) throw new Error(`fetchItems failed: ${res.status}`);
  // The server responds with { items: [...] }; tolerate a bare array too.
  const data = (await res.json()) as Item[] | { items: Item[] };
  return Array.isArray(data) ? data : data.items ?? [];
}

export async function pushItem(markdown: string): Promise<Item> {
  const res = await fetch(`${API_BASE}/api/items`, {
    method: "POST",
    headers: {
      "Content-Type": "text/markdown",
      ...authHeaders(),
    },
    body: markdown,
  });
  if (!res.ok) throw new Error(`pushItem failed: ${res.status}`);
  return (await res.json()) as Item;
}

export async function pinItem(id: string, pinned: boolean): Promise<void> {
  const res = await fetch(`${API_BASE}/api/items/${encodeURIComponent(id)}/pin`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...authHeaders(),
    },
    body: JSON.stringify({ pinned }),
  });
  if (!res.ok) throw new Error(`pinItem failed: ${res.status}`);
}

export async function deleteItem(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/items/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: { ...authHeaders() },
  });
  if (!res.ok) throw new Error(`deleteItem failed: ${res.status}`);
}

export function createEventSource(): EventSource {
  return new EventSource(buildEventUrl("/events"));
}
