import type { ComponentType } from "react";
import type { Item } from "@/lib/types";
import { NoteRenderer } from "./NoteRenderer";

export type RendererProps = { item: Item };
export type Renderer = ComponentType<RendererProps>;

const registry = new Map<string, Renderer>();

export function register(type: string, component: Renderer) {
  registry.set(type, component);
}

export function getRenderer(type: string): Renderer {
  return registry.get(type) ?? NoteRenderer;
}

// Register built-in renderers. We import them inline to avoid circular deps
// while keeping register() the single source of truth.
import { PaperRenderer } from "./PaperRenderer";
import { LinkRenderer } from "./LinkRenderer";
import { ImageRenderer } from "./ImageRenderer";
import { StockChartRenderer } from "./StockChartRenderer";

register("note", NoteRenderer);
register("paper", PaperRenderer);
register("link", LinkRenderer);
register("image", ImageRenderer);
register("stock-chart", StockChartRenderer);
