// Message timestamps in the chat: the time under each bubble and the day
// divider shown where the conversation crosses into another calendar day.
// Days and times are Vietnam time (lib/datetime).

import { formatDate, formatDateTime, formatTime, vnDayKey, vnDaysAgo } from "./datetime.ts";

export type DayLabel = { kind: "today" | "yesterday" } | { kind: "date"; text: string };

function valid(iso: string | undefined): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  // Go's zero time ("0001-01-01T00:00:00Z") means the row has no timestamp.
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() < 2000) return null;
  return iso;
}

/** "14h05" ("14:05" in English) — the time a message was sent or answered. */
export function messageTime(iso: string | undefined, locale: string): string {
  const v = valid(iso);
  return v ? formatTime(v, locale) : "";
}

/** Time and date for the hover title: "14h05 - 16/10/2026". */
export function messageDateTime(iso: string | undefined, locale: string): string {
  const v = valid(iso);
  return v ? formatDateTime(v, locale) : "";
}

/** Label of the day divider: today, yesterday, or the date ("16/10/2026"). */
export function dayLabel(iso: string | undefined, now: Date = new Date()): DayLabel | null {
  const v = valid(iso);
  if (!v) return null;
  const days = vnDaysAgo(v, now);
  if (days === 0) return { kind: "today" };
  if (days === 1) return { kind: "yesterday" };
  return { kind: "date", text: formatDate(v) };
}

/** Indexes of the messages that start a new day — a divider goes above
 * each. Messages without a timestamp never start a day. */
export function dayStarts(times: (string | undefined)[]): Set<number> {
  const starts = new Set<number>();
  let prev = "";
  times.forEach((iso, i) => {
    const v = valid(iso);
    if (!v) return;
    const key = vnDayKey(v);
    if (key !== prev) starts.add(i);
    prev = key;
  });
  return starts;
}
