// Every date and time the UI shows is Vietnam time (GMT+7), whatever zone
// the viewer's computer is set to. Format through these helpers instead of
// Date#toLocale*String / getHours / getDate, which follow the browser zone.

export const TIME_ZONE = "Asia/Ho_Chi_Minh";

export type VnParts = { year: number; month: number; day: number; hour: number; minute: number; second: number };

const partsFormat = new Intl.DateTimeFormat("en-GB", {
  timeZone: TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

function toDate(value: Date | string | number | null | undefined): Date | null {
  if (value === null || value === undefined || value === "") return null;
  const d = value instanceof Date ? value : new Date(value);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** Calendar fields of an instant in Vietnam time (month is 1–12). */
export function vnParts(value: Date | string | number | null | undefined): VnParts | null {
  const d = toDate(value);
  if (!d) return null;
  const out: Record<string, number> = {};
  for (const p of partsFormat.formatToParts(d)) {
    if (p.type !== "literal") out[p.type] = Number(p.value);
  }
  return {
    year: out.year,
    month: out.month,
    day: out.day,
    hour: out.hour % 24,
    minute: out.minute,
    second: out.second,
  };
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** "16/10/2026" */
export function formatDate(value: Date | string | number | null | undefined): string {
  const p = vnParts(value);
  return p ? `${pad(p.day)}/${pad(p.month)}/${p.year}` : "";
}

/** "14h05" in Vietnamese, "14:05" otherwise. */
export function formatTime(value: Date | string | number | null | undefined, locale: string = "vi"): string {
  const p = vnParts(value);
  return p ? `${pad(p.hour)}${locale === "vi" ? "h" : ":"}${pad(p.minute)}` : "";
}

/** "14h05 - 16/10/2026" */
export function formatDateTime(value: Date | string | number | null | undefined, locale: string = "vi"): string {
  const d = toDate(value);
  return d ? `${formatTime(d, locale)} - ${formatDate(d)}` : "";
}

/** Day key ("2026-10-16") of an instant in Vietnam time — for "same day"
 * comparisons and grouping. */
export function vnDayKey(value: Date | string | number | null | undefined): string {
  const p = vnParts(value);
  return p ? `${p.year}-${pad(p.month)}-${pad(p.day)}` : "";
}

/** Whole Vietnam calendar days from `value` to `now` (0 = same day). */
export function vnDaysAgo(value: Date | string | number, now: Date = new Date()): number {
  const a = vnDayKey(value);
  const b = vnDayKey(now);
  if (!a || !b) return 0;
  return Math.round((Date.parse(`${b}T00:00:00Z`) - Date.parse(`${a}T00:00:00Z`)) / 86_400_000);
}

/** Intl options with the Vietnam zone, for the rare format the helpers above
 * do not cover (e.g. a month name). */
export function vnIntl(options: Intl.DateTimeFormatOptions): Intl.DateTimeFormatOptions {
  return { ...options, timeZone: TIME_ZONE };
}
