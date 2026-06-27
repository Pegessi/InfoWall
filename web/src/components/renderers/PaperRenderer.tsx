import { ExternalLink } from "lucide-react";
import type { Item } from "@/lib/types";
import { Markdown } from "@/components/markdown/Markdown";

export function PaperRenderer({ item }: { item: Item }) {
  const { meta } = item;
  const authors: string[] = Array.isArray(meta?.authors) ? meta.authors : [];
  const venue: string | undefined = meta?.venue;
  const published: string | undefined = meta?.published;
  const pdfUrl: string | undefined = meta?.pdf;
  const url: string | undefined = meta?.url;
  const thumbnail: string | undefined = meta?.thumbnail;

  return (
    <div>
      {thumbnail && (
        <img
          src={thumbnail}
          alt=""
          loading="lazy"
          className="float-right ml-4 mb-2 h-32 w-24 rounded-lg border border-[hsl(var(--border))] object-cover"
        />
      )}
      <h3 className="text-xl font-semibold leading-tight">{item.title}</h3>
      {authors.length > 0 && (
        <div className="mt-1 text-sm text-[hsl(var(--muted-foreground))]">
          {authors.join(", ")}
        </div>
      )}
      <div className="mt-2 flex flex-wrap gap-2">
        {published && (
          <span className="rounded-md bg-[hsl(var(--muted))] px-2 py-0.5 text-xs">
            {published}
          </span>
        )}
        {venue && (
          <span className="text-xs text-[hsl(var(--muted-foreground))] self-center">
            {venue}
          </span>
        )}
        {item.tags?.map((tag) => (
          <span
            key={tag}
            className="rounded-md bg-[hsl(var(--muted))] px-2 py-0.5 text-xs"
          >
            #{tag}
          </span>
        ))}
      </div>
      {(url || pdfUrl) && (
        <div className="mt-2 flex gap-3">
          {url && (
            <a
              href={url}
              target="_blank"
              rel="noreferrer noopener"
              className="inline-flex items-center gap-1 text-xs text-[hsl(var(--accent))] hover:underline"
            >
              <ExternalLink className="h-3 w-3" />
              Link
            </a>
          )}
          {pdfUrl && (
            <a
              href={pdfUrl}
              target="_blank"
              rel="noreferrer noopener"
              className="inline-flex items-center gap-1 text-xs text-[hsl(var(--accent))] hover:underline"
            >
              <ExternalLink className="h-3 w-3" />
              PDF
            </a>
          )}
        </div>
      )}
      {item.body && (
        <div className="mt-3 clear-both">
          <Markdown>{item.body}</Markdown>
        </div>
      )}
    </div>
  );
}
