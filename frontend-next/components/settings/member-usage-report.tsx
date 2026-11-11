"use client";

/* Per-member usage of the organization for one month (Vietnam calendar):
 * questions, tokens, OCR and — for a SuperAdmin only — what it cost the
 * deployment, with the organization total and its package for context.
 * Tenant Admin of the organization or SuperAdmin; the server refuses
 * everyone else. CSV export is built in the browser from the same rows. */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import { IconRefresh } from "@/components/icons";
import { DataTable, type DataTableColumn } from "@/components/settings/data-table";
import { LimitStatusList } from "@/components/settings/usage-limits";
import { Notice } from "@/components/settings/notice";
import {
  getTenantMemberUsage,
  usageLimitLabel,
  type UsageMemberReport,
  type UsagePeriodTotals,
} from "@/lib/api/usage";
import { formatDate, vnParts } from "@/lib/datetime";
import { toCSV } from "@/lib/csv";

function monthKey(year: number, month: number): string {
  return `${year}-${String(month).padStart(2, "0")}`;
}

/** This month and the previous one in Vietnam time, as YYYY-MM. */
function vnMonths(): { current: string; previous: string } {
  const p = vnParts(new Date());
  if (!p) return { current: "", previous: "" };
  const prevYear = p.month === 1 ? p.year - 1 : p.year;
  const prevMonth = p.month === 1 ? 12 : p.month - 1;
  return { current: monthKey(p.year, p.month), previous: monthKey(prevYear, prevMonth) };
}

const num = (n: number | undefined) => (n ?? 0).toLocaleString("vi-VN");
const vnd = (micro: number | undefined) => `${Math.round((micro ?? 0) / 1e6).toLocaleString("vi-VN")} VNĐ`;

type Line =
  | { kind: "member"; key: string; name: string; email: string; member: boolean; role?: string; seat?: boolean; plan?: string; usage: UsagePeriodTotals }
  | { kind: "outside" | "unattributed" | "total"; key: string; usage: UsagePeriodTotals };

export function MemberUsageReport({ tenantId }: { tenantId: number }) {
  const { t } = useT();
  const months = useMemo(vnMonths, []);
  const [month, setMonth] = useState(months.current);
  const [report, setReport] = useState<UsageMemberReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  // a slower answer for a month the admin already left must not overwrite
  // the one now on screen
  const seq = useRef(0);

  const load = useCallback(async () => {
    if (!tenantId || !month) return;
    const mine = ++seq.current;
    setLoading(true);
    try {
      const r = await getTenantMemberUsage(tenantId, month);
      if (mine !== seq.current) return;
      setReport(r);
      setError("");
    } catch (e) {
      if (mine !== seq.current) return;
      setReport(null);
      setError(e instanceof Error ? e.message : t("mur.loadFailed"));
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, [tenantId, month, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const seatMode = report?.subscription?.mode === "seat";
  const showCost = !!report?.cost_visible;
  const byMessages = report?.source === "messages";

  const lines: Line[] = useMemo(() => {
    if (!report) return [];
    const out: Line[] = report.members.map(
      (m): Line =>
        m.outside
          ? { kind: "outside", key: "~outside", usage: m.usage }
          : {
              kind: "member",
              key: m.user_id,
              name: m.username || m.email || m.user_id,
              email: m.username ? m.email : "",
              member: m.member,
              role: m.role,
              seat: m.has_seat,
              plan: m.personal_plan,
              usage: m.usage,
            },
    );
    if (report.unattributed.questions > 0 || report.unattributed.tokens > 0 || report.unattributed.ocr_pages) {
      out.push({ kind: "unattributed", key: "~unattributed", usage: report.unattributed });
    }
    out.push({ kind: "total", key: "~total", usage: report.total });
    return out;
  }, [report]);

  const columns: DataTableColumn[] = [
    { key: "member", header: t("mur.colMember") },
    { key: "questions", header: t("mur.colQuestions"), align: "right" },
    { key: "tokens", header: t("mur.colTokens"), align: "right" },
    { key: "prompt", header: t("mur.colPrompt"), align: "right" },
    { key: "completion", header: t("mur.colCompletion"), align: "right" },
    { key: "ocr", header: t("mur.colOCR"), align: "right" },
    ...(showCost ? [{ key: "cost", header: t("mur.colCost"), align: "right" as const }] : []),
    { key: "plan", header: seatMode ? t("mur.colSeatPlan") : t("mur.colPlan") },
  ];

  const roleLabel = (role?: string) =>
    role === "admin" || role === "owner" ? t("mur.roleAdmin") : role ? t("mur.roleMember") : "";

  const planCell = (l: Extract<Line, { kind: "member" }>) => {
    const parts: string[] = [];
    if (seatMode && l.member) parts.push(l.seat ? t("mur.hasSeat") : t("mur.noSeat"));
    if (l.plan) parts.push(l.plan);
    return parts.join(" · ") || "—";
  };

  const exportCSV = () => {
    if (!report) return;
    const header = [
      t("mur.colMember"),
      t("mur.colEmail"),
      t("mur.colRole"),
      t("mur.colQuestions"),
      t("mur.colTokens"),
      t("mur.colPrompt"),
      t("mur.colCompletion"),
      t("mur.colOCRPages"),
      t("mur.colOCRTokens"),
      ...(showCost ? [t("mur.colCostVND")] : []),
      seatMode ? t("mur.colSeatPlan") : t("mur.colPlan"),
    ];
    const rows = lines.map((l) => {
      const u = l.usage;
      const label =
        l.kind === "member"
          ? l.name
          : l.kind === "outside"
            ? t("mur.outside")
            : l.kind === "unattributed"
              ? t("mur.unattributed")
              : t("mur.total");
      return [
        label,
        l.kind === "member" ? l.email : "",
        l.kind === "member" ? (l.member ? roleLabel(l.role) : t("mur.outside")) : "",
        u.questions,
        u.tokens,
        u.prompt_tokens,
        u.completion_tokens,
        u.ocr_pages ?? 0,
        u.ocr_tokens ?? 0,
        ...(showCost ? [Math.round((u.cost_micro_vnd ?? 0) / 1e6)] : []),
        l.kind === "member" ? planCell(l).replace(/^—$/, "") : "",
      ];
    });
    const csv = toCSV([header, ...rows]);
    const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `su-dung-thanh-vien-${report.tenant_id}-${report.month}.csv`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  const sub = report?.subscription ?? null;
  const limitNames = {
    questions: t("usg.metricQuestions"),
    tokens: t("usg.metricTokens"),
    day: t("usg.periodDay"),
    month: t("usg.periodMonth"),
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-0 flex-1">
          <h4 className="text-[14px] font-medium text-ink">{t("mur.title")}</h4>
          <p className="caption text-muted">{t("mur.desc")}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            className={`btn btn-sm ${month === months.current ? "btn-primary" : "btn-outline"}`}
            onClick={() => setMonth(months.current)}
          >
            {t("mur.thisMonth")}
          </button>
          <button
            type="button"
            className={`btn btn-sm ${month === months.previous ? "btn-primary" : "btn-outline"}`}
            onClick={() => setMonth(months.previous)}
          >
            {t("mur.lastMonth")}
          </button>
          <input
            type="month"
            aria-label={t("mur.pickMonth")}
            className="input h-9 w-40 py-0"
            value={month}
            max={months.current}
            onChange={(e) => e.target.value && setMonth(e.target.value)}
          />
          <button type="button" className="btn btn-outline btn-sm" disabled={loading} onClick={() => void load()}>
            <IconRefresh className="h-3.5 w-3.5" /> {t("common.refresh")}
          </button>
          <button type="button" className="btn btn-outline btn-sm" disabled={!report || loading} onClick={exportCSV}>
            {t("mur.exportCSV")}
          </button>
        </div>
      </div>

      {error && (
        <Notice
          tone="error"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      {report && (
        <div className="caption flex flex-wrap gap-x-6 gap-y-1 text-body">
          <span>
            {t("mur.monthLabel")}: <span className="text-ink">{report.month.split("-").reverse().join("/")}</span>
          </span>
          {sub ? (
            <span className="flex flex-wrap items-center gap-2">
              {t("mur.quota", { plan: sub.plan?.name ?? sub.plan_id })}:{" "}
              {seatMode ? (
                <span>
                  {sub.limit_status.length
                    ? sub.limit_status.map((l) => usageLimitLabel(l, limitNames)).join(", ")
                    : t("usg.unlimited")}{" "}
                  {t("mur.perSeat")}
                </span>
              ) : (
                <LimitStatusList items={sub.limit_status} compact />
              )}
            </span>
          ) : (
            <span>{t("mur.noPackage")}</span>
          )}
        </div>
      )}
      {sub && !seatMode && report && report.month !== months.current && (
        <p className="caption text-muted-soft">{t("mur.quotaCurrentNote")}</p>
      )}
      {byMessages && <Notice tone="info">{t("mur.fromMessages")}</Notice>}
      {report?.partial && report.counted_since && (
        <Notice tone="info">{t("mur.partial", { date: formatDate(`${report.counted_since}T00:00:00+07:00`) })}</Notice>
      )}

      <DataTable<Line>
        columns={columns}
        rows={lines}
        rowKey={(l) => l.key}
        loading={loading && !report}
        emptyLabel={t("mur.empty")}
        minWidth="min-w-[760px]"
        caption={t("mur.title")}
        renderRow={(l) => {
          const u = l.usage;
          const strong = l.kind === "unattributed" || l.kind === "total";
          const cell = `px-5 py-2.5 text-right tabular-nums text-[13.5px] ${strong ? "font-medium text-ink" : "text-body"}`;
          return (
            <>
              <td className="px-5 py-2.5 text-[13.5px]">
                {l.kind === "member" ? (
                  <div className="min-w-0">
                    <div className="truncate text-ink">
                      {l.name}
                      {!l.member && <span className="badge-pill ml-2 text-[11px] text-muted">{t("mur.outside")}</span>}
                      {l.member && (l.role === "admin" || l.role === "owner") && (
                        <span className="badge-pill ml-2 text-[11px] text-muted">{t("mur.roleAdmin")}</span>
                      )}
                    </div>
                    {l.email && <div className="caption truncate text-muted">{l.email}</div>}
                  </div>
                ) : l.kind === "outside" ? (
                  <div>
                    <div className="font-medium text-ink">{t("mur.outside")}</div>
                    <div className="caption text-muted">{t("mur.outsideHint")}</div>
                  </div>
                ) : l.kind === "unattributed" ? (
                  <div>
                    <div className="font-medium text-ink">{t("mur.unattributed")}</div>
                    <div className="caption text-muted">{t("mur.unattributedHint")}</div>
                  </div>
                ) : (
                  <span className="font-medium text-ink">{t("mur.total")}</span>
                )}
              </td>
              <td className={cell}>{num(u.questions)}</td>
              <td className={cell}>{num(u.tokens)}</td>
              <td className={cell}>{num(u.prompt_tokens)}</td>
              <td className={cell}>{num(u.completion_tokens)}</td>
              <td className={cell}>
                {u.ocr_pages || u.ocr_tokens ? t("mur.ocrCell", { pages: num(u.ocr_pages), tokens: num(u.ocr_tokens) }) : "—"}
              </td>
              {showCost && <td className={cell}>{vnd(u.cost_micro_vnd)}</td>}
              <td className="px-5 py-2.5 text-[13px] text-body">{l.kind === "member" ? planCell(l) : ""}</td>
            </>
          );
        }}
      />
    </div>
  );
}
