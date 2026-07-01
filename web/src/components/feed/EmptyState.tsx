import { Activity, Check, Clipboard, FilePlus2, Terminal } from "lucide-react";
import { useMemo, useState } from "react";

type CommandId = "health" | "push";

interface CommandRowProps {
  id: CommandId;
  label: string;
  command: string;
  icon: typeof Activity;
  copied: boolean;
  onCopy: (id: CommandId, command: string) => void;
}

export function EmptyState() {
  const [copiedCommand, setCopiedCommand] = useState<CommandId | null>(null);
  const serverURL = useMemo(() => {
    if (typeof window === "undefined") return "http://localhost:8899";
    return window.location.origin;
  }, []);
  const healthCommand = `infowall health --server ${serverURL} --json`;
  const pushCommand = `printf '# First note\\n\\nReady for daily operation.\\n' | infowall push - --server ${serverURL}`;

  const copyCommand = (id: CommandId, command: string) => {
    const write = navigator.clipboard?.writeText(command);
    if (!write) return;
    void write
      .then(() => {
        setCopiedCommand(id);
        window.setTimeout(() => {
          setCopiedCommand((current) => (current === id ? null : current));
        }, 1400);
      })
      .catch(() => undefined);
  };

  return (
    <section className="flex min-h-[calc(100vh-7rem)] items-center justify-center py-8 text-[hsl(var(--foreground))]">
      <div className="w-full max-w-3xl rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] shadow-sm">
        <div className="border-b border-[hsl(var(--border))] px-4 py-4 sm:px-5">
          <div className="flex items-start gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-[hsl(var(--muted))] text-[hsl(var(--accent))]">
              <Terminal className="h-5 w-5" strokeWidth={1.75} />
            </div>
            <div className="min-w-0">
              <h2 className="text-base font-semibold">Ready for the first item</h2>
              <p className="mt-1 max-w-2xl text-sm text-[hsl(var(--muted-foreground))]">
                Verify the local service, then push a small markdown note. The
                wall will switch to the live feed as soon as the first item arrives.
              </p>
            </div>
          </div>
        </div>

        <div className="divide-y divide-[hsl(var(--border))]">
          <CommandRow
            id="health"
            label="Check service"
            command={healthCommand}
            icon={Activity}
            copied={copiedCommand === "health"}
            onCopy={copyCommand}
          />
          <CommandRow
            id="push"
            label="Push first note"
            command={pushCommand}
            icon={FilePlus2}
            copied={copiedCommand === "push"}
            onCopy={copyCommand}
          />
        </div>

        <div className="grid gap-2 border-t border-[hsl(var(--border))] px-4 py-3 text-xs text-[hsl(var(--muted-foreground))] sm:grid-cols-[7rem_1fr] sm:px-5">
          <span className="font-medium text-[hsl(var(--foreground))]">
            Server URL
          </span>
          <span>
            Browser requests use this page origin. CLI commands can also use{" "}
            <code className="rounded bg-[hsl(var(--muted))] px-1 py-0.5 font-mono text-[11px] text-[hsl(var(--foreground))]">
              INFOWALL_URL
            </code>{" "}
            or{" "}
            <code className="rounded bg-[hsl(var(--muted))] px-1 py-0.5 font-mono text-[11px] text-[hsl(var(--foreground))]">
              --server
            </code>
            .
          </span>
        </div>
      </div>
    </section>
  );
}

function CommandRow({
  id,
  label,
  command,
  icon: Icon,
  copied,
  onCopy,
}: CommandRowProps) {
  const CopyIcon = copied ? Check : Clipboard;

  return (
    <div className="grid gap-3 px-4 py-3 sm:grid-cols-[9rem_minmax(0,1fr)_auto] sm:items-center sm:px-5">
      <div className="flex min-w-0 items-center gap-2 text-sm font-medium">
        <Icon
          className="h-4 w-4 shrink-0 text-[hsl(var(--muted-foreground))]"
          strokeWidth={1.75}
        />
        <span className="truncate">{label}</span>
      </div>
      <pre className="min-w-0 overflow-x-auto rounded-md bg-[hsl(var(--muted))] px-3 py-2 text-left font-mono text-[12px] leading-5 text-[hsl(var(--foreground))]">
        <code>{command}</code>
      </pre>
      <button
        type="button"
        aria-label={`Copy ${label} command`}
        title={copied ? "Copied" : "Copy"}
        onClick={() => onCopy(id, command)}
        className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-[hsl(var(--border))] text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
      >
        <CopyIcon className="h-4 w-4" strokeWidth={1.75} />
      </button>
    </div>
  );
}
