import { formatDistanceToNow, format, isSameDay, startOfDay } from "date-fns";

export function formatRelative(date: string | Date): string {
  const d = typeof date === "string" ? new Date(date) : date;
  return formatDistanceToNow(d, { addSuffix: true });
}

export function formatAbsolute(date: string | Date, fmt = "PPpp"): string {
  const d = typeof date === "string" ? new Date(date) : date;
  return format(d, fmt);
}

/** Returns true if the given date falls on today (local time). */
export function isToday(date: string | Date): boolean {
  const d = typeof date === "string" ? new Date(date) : date;
  return isSameDay(d, new Date());
}

/** Returns the start of today in local time — useful for date comparisons. */
export function startOfToday(): Date {
  return startOfDay(new Date());
}
