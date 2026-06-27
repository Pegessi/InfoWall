import type { Item } from "@/lib/types";
import { Markdown } from "@/components/markdown/Markdown";

export function NoteRenderer({ item }: { item: Item }) {
  return <Markdown>{item.body}</Markdown>;
}
