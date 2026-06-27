import { MessageSquare } from "lucide-react";

export function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-20 text-center text-[hsl(var(--muted-foreground))]">
      <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-[hsl(var(--muted))]">
        <MessageSquare className="h-6 w-6" strokeWidth={1.5} />
      </div>
      <p className="mb-3 text-sm">
        Nothing here yet. Push your first item:
      </p>
      <pre className="max-w-full overflow-x-auto rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--muted))] px-4 py-2.5 font-mono text-xs text-foreground">
        <code>echo "# Hello from terminal" | curl -X POST --data-binary @- http://localhost:8899/api/items</code>
      </pre>
    </div>
  );
}
