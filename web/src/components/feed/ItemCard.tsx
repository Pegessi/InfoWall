import { ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useState } from "react";
import type { Item } from "@/lib/types";
import { cn } from "@/lib/utils";
import { CardHeader } from "./CardHeader";
import { getRenderer } from "@/components/renderers/registry";

interface ItemCardProps {
  item: Item;
  onPin: (id: string, pinned: boolean) => void;
  onDelete: (id: string) => void;
  showTypeLabel?: boolean;
  /** If true, the card starts collapsed (title only); otherwise it starts expanded. */
  defaultCollapsed?: boolean;
}

export function ItemCard({
  item,
  onPin,
  onDelete,
  showTypeLabel = true,
  defaultCollapsed = false,
}: ItemCardProps) {
  const [collapsed, setCollapsed] = useState(defaultCollapsed);
  const Renderer = getRenderer(item.type);

  // An item is collapsible only if it started collapsed AND is not currently pinned.
  // Pinning always forces expanded (pin is an explicit "keep visible" signal).
  const isCollapsible = defaultCollapsed && !item.pinned;
  const isCollapsed = isCollapsible && collapsed;

  // Auto-expand when the item becomes pinned (in-session pin).
  useEffect(() => {
    if (item.pinned && collapsed) {
      setCollapsed(false);
    }
  }, [item.pinned, collapsed]);

  const toggleCollapse = () => {
    if (!isCollapsible) return;
    setCollapsed((c) => !c);
  };

  return (
    <article
      className={cn(
        "group rounded-xl border border-[hsl(var(--border))] bg-[hsl(var(--card))] shadow-sm transition-shadow animate-fade-up",
        item.pinned ? "ring-1 ring-[hsl(var(--accent)/0.4)]" : "",
        isCollapsed
          ? "p-3 hover:shadow-md cursor-pointer"
          : "p-5 hover:shadow-md sm:p-6"
      )}
      onClick={isCollapsible ? toggleCollapse : undefined}
      role={isCollapsible ? "button" : undefined}
      aria-expanded={isCollapsible ? !isCollapsed : undefined}
      tabIndex={isCollapsible ? 0 : undefined}
      onKeyDown={
        isCollapsible
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                toggleCollapse();
              }
            }
          : undefined
      }
    >
      <div className="flex items-start gap-2">
        <div className="flex-1 min-w-0">
          <CardHeader
            item={item}
            onPin={onPin}
            onDelete={onDelete}
            showTypeLabel={showTypeLabel}
            compact={isCollapsed}
          />
        </div>
        {isCollapsible && (
          <div className="flex-shrink-0 pt-0.5 text-[hsl(var(--muted-foreground))]">
            {isCollapsed ? (
              <ChevronRight className="h-4 w-4" strokeWidth={2} />
            ) : (
              <ChevronDown className="h-4 w-4" strokeWidth={2} />
            )}
          </div>
        )}
      </div>
      {!isCollapsed && (
        <div
          className="mt-4"
          onClick={(e) => isCollapsible && e.stopPropagation()}
        >
          <Renderer item={item} />
        </div>
      )}
    </article>
  );
}
