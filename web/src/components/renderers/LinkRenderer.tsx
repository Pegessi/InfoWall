import { Link2 } from "lucide-react";
import type { Item } from "@/lib/types";

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
}

export function LinkRenderer({ item }: { item: Item }) {
  const { meta } = item;
  const url: string | undefined = meta?.url;
  const thumbnail: string | undefined = meta?.thumbnail;
  const favicon: string | undefined = meta?.favicon;
  const description: string | undefined = meta?.description;
  const hostname = url ? hostOf(url) : "";

  return (
    <div>
      {thumbnail && (
        <a
          href={url}
          target="_blank"
          rel="noreferrer noopener"
          className="block mb-3 overflow-hidden rounded-lg border border-[hsl(var(--border))]"
        >
          <img
            src={thumbnail}
            alt={item.title}
            loading="lazy"
            className="aspect-[1200/630] w-full object-cover transition-transform hover:scale-[1.02]"
          />
        </a>
      )}
      <div className="flex gap-3 items-start">
        <div className="mt-0.5 flex-shrink-0">
          {favicon ? (
            <img
              src={favicon}
              alt=""
              className="h-5 w-5 rounded-sm"
              loading="lazy"
            />
          ) : (
            <Link2 className="h-5 w-5 text-[hsl(var(--muted-foreground))]" strokeWidth={1.75} />
          )}
        </div>
        <div className="flex-1 min-w-0">
          <a
            href={url}
            target="_blank"
            rel="noreferrer noopener"
            className="block truncate font-medium text-[hsl(var(--accent))] hover:underline"
            title={item.title}
          >
            {item.title}
          </a>
          <div className="mt-0.5 truncate text-xs text-[hsl(var(--muted-foreground))]">
            {hostname}
          </div>
          {description && (
            <p className="mt-1 text-sm text-foreground/90 line-clamp-2">
              {description}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
