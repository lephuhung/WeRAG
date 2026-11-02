"use client";

/* Consumption of a package's limits: "120 / 500 questions/month" with a
 * small bar, red once exhausted. Shared by the SuperAdmin usage page, the
 * workspace package page and the account menu. */
import { useT } from "@/lib/i18n";
import { formatTokens, type UsageLimitStatus } from "@/lib/api/usage";

export function LimitStatusList({ items, compact = false }: { items: UsageLimitStatus[]; compact?: boolean }) {
  const { t } = useT();
  if (!items.length) return <span className="caption text-muted-soft">{t("usg.unlimited")}</span>;
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
