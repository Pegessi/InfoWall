import type { Item } from "@/lib/types";
import { CardHeader } from "./CardHeader";
import { getRenderer } from "@/components/renderers/registry";

interface ItemCardProps {
  item: Item;
  onPin: (id: string, pinned: boolean) => void;
  onDelete: (id: string) => void;
  showTypeLabel?: boolean;
}

export function ItemCard({
  item,
  onPin,
  onDelete,
  showTypeLabel = true,
}: ItemCardProps) {
  const Renderer = getRenderer(item.type);
  return (
    <article
      className={`group rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--card))] p-5 shadow-sm transition-shadow hover:shadow-md sm:p-6 animate-fade-up ${
        item.pinned ? "ring-1 ring-[hsl(var(--accent)/0.4)]" : ""
      }`}
    >
      <CardHeader
        item={item}
        onPin={onPin}
        onDelete={onDelete}
        showTypeLabel={showTypeLabel}
      />
      <div className="mt-4">
        <Renderer item={item} />
      </div>
    </article>
  );
}
