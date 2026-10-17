"use client";

/* The package name with the tokens left on it, as a pill that fills from
 * the left like rising water as the package is used up (the limit closest
 * to exhaustion sets the level); the water darkens toward the cap and turns
 * rose once the package refuses questions. */
import type { CSSProperties } from "react";
import { formatTokens, type UsageEntitlement } from "@/lib/api/usage";
import { useT } from "@/lib/i18n";
import { meterColors, usageMeter } from "@/lib/usage-meter";

export function UsagePlanPill({ entitlement: ent }: { entitlement: UsageEntitlement }) {
  const { t } = useT();
  const meter = usageMeter(ent);
  const detail = ent.expired
    ? t("acct.planExpired")
    : ent.mode === "seat" && !ent.has_seat
      ? t("acct.planNoSeat")
      : meter.tokens
        ? t("acct.planTokensLeft", { n: formatTokens(meter.tokens.remaining) })
        : "";
  const { fill, ink } = meterColors(meter.level, meter.blocked);
  const content = (
    <>
      <span className="truncate">{ent.plan_name}</span>
      {detail && <span className="shrink-0 tabular-nums opacity-90">· {detail}</span>}
    </>
  );
  /* Drawn twice so the text stays readable across the water's edge. */
  const water: CSSProperties = { background: fill, color: ink, clipPath: `inset(0 ${100 - meter.level}% 0 0)` };
  return (
    <span
      title={`${ent.plan_name} · ${meter.level}%`}
      className={`relative inline-flex h-6 min-w-0 max-w-[14rem] overflow-hidden rounded-full border text-[11.5px] font-medium ${
        meter.blocked ? "border-rose-300 dark:border-rose-900/60" : "border-blue-200 dark:border-blue-800"
      } bg-blue-50 text-blue-900 dark:bg-blue-950/40 dark:text-blue-100`}
    >
      <span className="flex min-w-0 items-center gap-1 px-2.5">{content}</span>
      <span aria-hidden className="absolute inset-0 flex min-w-0 items-center gap-1 px-2.5 motion-safe:transition-[clip-path,background-color] motion-safe:duration-700" style={water}>
        {content}
      </span>
    </span>
  );
}
