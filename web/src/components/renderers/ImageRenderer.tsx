import type { Item } from "@/lib/types";
import { Markdown } from "@/components/markdown/Markdown";

export function ImageRenderer({ item }: { item: Item }) {
  const { meta } = item;
  const url: string | undefined = meta?.url;
  const caption: string | undefined = meta?.caption;
  if (!url) return null;
  return (
    <div>
      <img
        src={url}
        alt={caption || item.title || ""}
        loading="lazy"
        className="w-full rounded-lg border border-[hsl(var(--border))]"
      />
      {caption && (
        <p className="mt-2 text-center text-xs text-[hsl(var(--muted-foreground))]">
          {caption}
        </p>
      )}
      {item.body && item.body.trim().length > 0 && (
        <div className="mt-3">
          <Markdown>{item.body}</Markdown>
        </div>
      )}
    </div>
  );
}
