import { formatDistanceToNow, format } from "date-fns";

export function formatRelative(date: string | Date): string {
  const d = typeof date === "string" ? new Date(date) : date;
  return formatDistanceToNow(d, { addSuffix: true });
}

export function formatAbsolute(date: string | Date, fmt = "PPpp"): string {
  const d = typeof date === "string" ? new Date(date) : date;
  return format(d, fmt);
}
