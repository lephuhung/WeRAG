/* SuperAdmin backup status: the latest status file of every backup job
 * (deploy/backup), read live by the backend's monitor — the page the
 * backup_alert notification links to. Read-only. */
"use client";

import { useEffect, useState } from "react";
import { ApiError } from "@/lib/api-client";
import { getBackupStatus, type BackupJobReport, type BackupReport } from "@/lib/api/system";
import { formatDateTime } from "@/lib/datetime";
import { useT, type LocaleKey } from "@/lib/i18n";
import { IconRefresh } from "@/components/icons";
import { ConsolePage } from "@/components/settings/console-page";
import { DataTable } from "@/components/settings/data-table";
import { DeniedView } from "@/components/settings/system-section";
import { Notice } from "@/components/settings/notice";

function formatBytes(bytes: number): string {
  if (!bytes || bytes < 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

/** "15 min" / "26 h" / "8 d" — how old the last run may be. */
function maxAgeLabel(seconds: number, t: (k: LocaleKey, v?: Record<string, string | number>) => string): string {
  if (seconds >= 2 * 86400 && seconds % 86400 === 0) return t("backups.days", { n: seconds / 86400 });
  if (seconds >= 3600) return t("backups.hours", { n: Math.round(seconds / 3600) });
  return t("backups.minutes", { n: Math.round(seconds / 60) });
}

const STATE_BADGE: Record<string, string> = {
  ok: "badge-success",
  unknown: "badge-neutral",
  stale: "badge-warning",
  failed: "badge-error",
  missing: "badge-error",
  unreadable: "badge-error",
};

export default function AdminBackupsPage() {
  const { t, locale } = useT();
  const [report, setReport] = useState<BackupReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [denied, setDenied] = useState(false);
  const [error, setError] = useState("");

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setReport(await getBackupStatus());
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) setDenied(true);
      else setError(e instanceof Error ? e.message : t("sys.loadFailed"));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (denied) return <DeniedView />;

  const jobs = report?.jobs ?? [];
  const noteOf = (r: BackupJobReport) => {
    const s = r.status;
    const parts = [s?.error, s?.note, r.read_error].filter((x): x is string => Boolean(x && x.trim()));
    if (s?.objects != null) parts.push(t("backups.objectsCount", { n: s.objects.toLocaleString(locale === "vi" ? "vi-VN" : "en-US") }));
    return parts.join(" · ");
  };

  return (
    <ConsolePage>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <p className="caption max-w-[760px] text-muted">
          {loading
            ? t("adm.loading")
            : report?.enabled
              ? report.problems > 0
                ? t("backups.problems", { n: report.problems, at: formatDateTime(report.checked_at, locale) })
                : t("backups.allOk", { at: formatDateTime(report.checked_at, locale) })
              : t("backups.disabled")}
        </p>
        <button className="btn btn-outline btn-sm" onClick={() => void load()} disabled={loading}>
          <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} /> {t("common.refresh")}
        </button>
      </div>

      {error && (
        <Notice
          tone="error"
          className="mb-4"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      {(report?.enabled ?? true) && (
        <DataTable
          columns={[
            { key: "job", header: t("backups.colJob"), className: "w-[130px]" },
            { key: "state", header: t("backups.colState"), className: "w-[150px]" },
            { key: "finished", header: t("backups.colFinished"), className: "w-[170px]" },
            { key: "object", header: t("backups.colObject"), className: "min-w-[220px]" },
            { key: "bytes", header: t("backups.colBytes"), className: "w-[100px]" },
            { key: "note", header: t("backups.colNote"), className: "min-w-[260px]" },
          ]}
          rows={jobs}
          rowKey={(r) => r.job}
          loading={loading && jobs.length === 0}
          emptyLabel={t("backups.empty")}
          minWidth="min-w-[1000px]"
          renderRow={(r) => (
            <>
              <td className="px-5 py-3.5 align-top font-medium text-ink">
                {r.job}
                <div className="caption text-muted-soft">{t("backups.maxAge", { age: maxAgeLabel(r.max_age_s, t) })}</div>
              </td>
              <td className="px-5 py-3.5 align-top">
                <span className={`badge-pill ${STATE_BADGE[r.state] ?? "badge-neutral"} text-[11px]`}>
                  {t(`backups.state.${r.state}` as LocaleKey)}
                </span>
              </td>
              <td className="px-5 py-3.5 align-top whitespace-nowrap tabular-nums">
                {r.status?.finished_at ? formatDateTime(r.status.finished_at, locale) : "—"}
                {r.last_ok_at && r.state !== "ok" && (
                  <div className="caption text-muted">{t("backups.lastOk", { at: formatDateTime(r.last_ok_at, locale) })}</div>
                )}
              </td>
              <td className="px-5 py-3.5 align-top break-all font-mono text-[12px] text-muted">{r.status?.object || "—"}</td>
              <td className="px-5 py-3.5 align-top whitespace-nowrap tabular-nums">{formatBytes(r.status?.bytes ?? 0)}</td>
              <td className="px-5 py-3.5 align-top text-[13px] text-muted">{noteOf(r) || "—"}</td>
            </>
          )}
        />
      )}
      <p className="caption mt-4 text-muted-soft">{t("backups.hint")}</p>
    </ConsolePage>
  );
}
