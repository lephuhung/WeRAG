"use client";

/* Consumption of a package's limits: "120 / 500 questions/month" with a
 * small bar, red once exhausted. Shared by the SuperAdmin usage page, the
 * workspace package page and the account menu. */
import { useT } from "@/lib/i18n";
import { formatTokens, type UsageLimitStatus } from "@/lib/api/usage";

export function LimitStatusList({
  items,
  compact = false,
  variant = "bar",
}: {
  items: UsageLimitStatus[];
  compact?: boolean;
  /* "ring": a small donut with the percentage inside and the figures in
   * two short lines beside it, limits side by side — fits a narrow table
   * column (SuperAdmin usage lists). */
  variant?: "bar" | "ring";
}) {
  const { t } = useT();
  if (!items.length) return <span className="caption text-muted-soft">{t("usg.unlimited")}</span>;
  if (variant === "ring") {
    return (
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
        {items.map((l, i) => {
          const fmt = l.metric === "tokens" ? formatTokens : (n: number) => n.toLocaleString();
          const unit = `${l.metric === "tokens" ? t("usg.metricTokens") : t("usg.metricQuestions")}/${
            l.period === "day" ? t("usg.periodDay") : t("usg.periodMonth")
          }`;
          return (
            <div key={i} className="flex items-center gap-2">
              {l.limit > 0 && <UsageRing used={l.used} limit={l.limit} exhausted={l.exhausted} />}
              <div className="leading-tight">
                <div className={`text-[13px] tabular-nums ${l.exhausted ? "text-error" : "text-ink"}`}>
                  {fmt(l.used)}
                  {l.limit > 0 ? ` / ${fmt(l.limit)}` : ""}
                </div>
                <div className="text-[11.5px] text-muted">{unit}</div>
              </div>
            </div>
          );
        })}
      </div>
    );
  }
  return (
    <div className="space-y-0.5">
      {items.map((l, i) => {
        const fmt = l.metric === "tokens" ? formatTokens : (n: number) => n.toLocaleString();
        const pct = l.limit > 0 ? Math.min(100, Math.round((l.used / l.limit) * 100)) : 0;
        return (
          <div key={i} className={`${compact ? "text-[12.5px]" : "caption"} flex items-center gap-2`}>
            <span className={l.exhausted ? "text-error" : "text-body"}>
              {fmt(l.used)}
              {l.limit > 0 ? ` / ${fmt(l.limit)}` : ""}{" "}
              {l.metric === "tokens" ? t("usg.metricTokens") : t("usg.metricQuestions")}/
              {l.period === "day" ? t("usg.periodDay") : t("usg.periodMonth")}
            </span>
            {l.limit > 0 && (
              <span className="h-1.5 w-16 overflow-hidden rounded-full bg-surface-strong">
                <span className={`block h-full ${l.exhausted ? "bg-error" : "bg-success"}`} style={{ width: `${pct}%` }} />
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}

/* Donut gauge: track + arc from 12 o'clock, percentage in the middle.
 * Any non-zero use shows at least a sliver so 96K of 8M is not "empty". */
function UsageRing({ used, limit, exhausted }: { used: number; limit: number; exhausted: boolean }) {
  const ratio = Math.min(1, used / limit);
  const pct = Math.round(ratio * 100);
  const r = 16;
  const c = 2 * Math.PI * r;
  const arc = used > 0 ? Math.max(ratio * c, 2.5) : 0;
  const tone = exhausted ? "text-error" : ratio >= 0.8 ? "text-warning" : "text-success";
  return (
    <svg viewBox="0 0 40 40" className="h-9 w-9 shrink-0" role="img" aria-label={`${pct}%`}>
      <circle cx="20" cy="20" r={r} fill="none" strokeWidth="3.5" className="stroke-hairline-strong" />
      {arc > 0 && (
        <circle
          cx="20"
          cy="20"
          r={r}
          fill="none"
          strokeWidth="3.5"
          strokeLinecap="round"
          stroke="currentColor"
          strokeDasharray={`${arc} ${c}`}
          transform="rotate(-90 20 20)"
          className={tone}
        />
      )}
      <text
        x="20"
        y="20"
        textAnchor="middle"
        dominantBaseline="central"
        className={`fill-ink font-medium tabular-nums ${pct >= 100 ? "text-[8.5px]" : "text-[10px]"}`}
      >
        {pct}%
      </text>
    </svg>
  );
}
