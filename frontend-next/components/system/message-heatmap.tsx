"use client";

import { useMemo } from "react";
import { useT } from "@/lib/i18n";
import type { MessageDayActivity } from "@/lib/api/system";
import { vnDayKey } from "@/lib/datetime";

const CELL = 12;
const GAP = 3;
const WEEKDAY_ROWS = 7; // Mon..Sun

/* Calendar days are Vietnam days. A day is carried as UTC midnight of its
 * date so stepping and weekday maths never touch the viewer's zone. */
function dayOf(key: string): Date {
  return new Date(`${key}T00:00:00Z`);
}

function toISODate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

/* GitHub-style activity heatmap: one column per week (Mon→Sun rows),
 * color intensity by message count. `days` is the trailing window. */
export function MessageHeatmap({ days, data }: { days: number; data: MessageDayActivity[] }) {
  const { t } = useT();

  const { weeks, months, max, total } = useMemo(() => {
    const byDate = new Map<string, MessageDayActivity>();
    let max = 0;
    let total = 0;
    for (const d of data) {
      byDate.set(d.date, d);
      if (d.count > max) max = d.count;
      total += d.count;
    }

    // End on today (Vietnam); start `days-1` back, then rewind to the Monday of that week.
    const end = dayOf(vnDayKey(new Date()));
    const start = new Date(end);
    start.setUTCDate(start.getUTCDate() - (days - 1));
    // Align to Monday (getUTCDay: 0=Sun..6=Sat)
    const dow = (start.getUTCDay() + 6) % 7;
    start.setUTCDate(start.getUTCDate() - dow);

    const monthLabel = new Intl.DateTimeFormat(undefined, { month: "short", timeZone: "UTC" });
    const weeks: { date: string; count: number; future: boolean }[][] = [];
    const months: { index: number; label: string }[] = [];
    let prevMonth = -1;
    for (
      let cursor = new Date(start), w = 0;
      cursor <= end;
      w += 1
    ) {
      const week: { date: string; count: number; future: boolean }[] = [];
      for (let r = 0; r < WEEKDAY_ROWS; r += 1) {
        const iso = toISODate(cursor);
        week.push({
          date: iso,
          count: byDate.get(iso)?.count ?? 0,
          future: cursor > end,
        });
        cursor.setUTCDate(cursor.getUTCDate() + 1);
      }
      if (cursor.getUTCMonth() !== prevMonth && week.some((c) => !c.future)) {
        months.push({ index: w, label: monthLabel.format(cursor) });
        prevMonth = cursor.getUTCMonth();
      }
      weeks.push(week);
    }
    return { weeks, months, max, total };
  }, [data, days]);

  const levelFor = (count: number): number => {
    if (count <= 0 || max <= 0) return 0;
    const ratio = count / max;
    if (ratio <= 0.25) return 1;
    if (ratio <= 0.5) return 2;
    if (ratio <= 0.75) return 3;
    return 4;
  };

  const LEVEL_COLORS = [
    "var(--color-surface-strong)",
    "color-mix(in srgb, var(--color-success) 30%, transparent)",
    "color-mix(in srgb, var(--color-success) 55%, transparent)",
    "color-mix(in srgb, var(--color-success) 80%, transparent)",
    "var(--color-success)",
  ];

  const monthLabel = (i: number) => months.find((m) => m.index === i)?.label ?? "";

  return (
    <div className="overflow-x-auto">
      <div
        className="mb-1 grid text-[10px] text-muted-soft"
        style={{
          gridTemplateColumns: `repeat(${weeks.length}, ${CELL}px)`,
          columnGap: GAP,
          marginLeft: 28,
        }}
      >
        {weeks.map((_, i) => (
          <span key={i} className="whitespace-nowrap">
            {monthLabel(i)}
          </span>
        ))}
      </div>
      <div className="flex">
        <div
          className="mr-2 grid shrink-0 text-[10px] leading-none text-muted-soft"
          style={{ gridTemplateRows: `repeat(${WEEKDAY_ROWS}, ${CELL}px)`, rowGap: GAP }}
        >
          <span>Mon</span>
          <span />
          <span>Wed</span>
          <span />
          <span>Fri</span>
          <span />
          <span />
        </div>
        <div
          className="grid"
          style={{
            gridTemplateRows: `repeat(${WEEKDAY_ROWS}, ${CELL}px)`,
            gridAutoFlow: "column",
            gap: GAP,
          }}
        >
          {weeks.flatMap((week, w) =>
            week.map((cell, r) => (
              <div
                key={`${w}-${r}`}
                title={
                  cell.future
                    ? undefined
                    : `${cell.date.split("-").reverse().join("/")}: ${cell.count} ${t("stats.messages").toLowerCase()}`
                }
                className="rounded-[2px]"
                style={{
                  width: CELL,
                  height: CELL,
                  backgroundColor: cell.future
                    ? "transparent"
                    : LEVEL_COLORS[levelFor(cell.count)],
                }}
              />
            )),
          )}
        </div>
      </div>
      <div className="mt-3 flex items-center justify-between text-[11px] text-muted-soft">
        <span>
          {total.toLocaleString()} {t("stats.messages").toLowerCase()} ·{" "}
          {t("stats.lastDays").replace("{days}", String(days))}
        </span>
        <span className="flex items-center gap-1">
          {t("stats.less")}
          {LEVEL_COLORS.map((c, i) => (
            <span
              key={i}
              className="inline-block rounded-[2px]"
              style={{ width: CELL, height: CELL, backgroundColor: c }}
            />
          ))}
          {t("stats.more")}
        </span>
      </div>
    </div>
  );
}
