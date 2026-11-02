"use client";

/* Platform audit log (SystemAdmin only). Backend:
 * GET /api/v1/system/admin/audit-log, cursor-paginated newest-first. */

import { useCallback, useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { IconRefresh, IconSearch } from "@/components/icons";
import { DataTable } from "@/components/settings/data-table";
import { LoadMore } from "@/components/settings/pager";
import { Notice } from "@/components/settings/notice";
import { Pill, PillGroup } from "@/components/settings/pill";
import { SystemSection } from "@/components/settings/system-section";
import { useT } from "@/lib/i18n";
import { vnIntl } from "@/lib/datetime";
import { useAuth } from "@/lib/auth";
import { getCurrentUser } from "@/lib/api/auth";
import {
  listSystemAuditLog,
  type AuditLog,
  type AuditOutcome,
} from "@/lib/api/audit";
import { ApiError } from "@/lib/api-client";

type Level = "info" | "warn" | "error";

const LEVELS: Level[] = ["info", "warn", "error"];

/* Outcome → severity bucket used by the level filter and row badge. */
const OUTCOME_LEVEL: Record<AuditOutcome, Level> = {
  accepted: "info",
  success: "info",
  partial: "warn",
  canceled: "warn",
  failed: "error",
  denied: "error",
};

/* Actions rendered in the danger/warning tone; everything else stays neutral. */
const DANGER_ACTIONS = new Set([
  "rbac.access_denied",
  "system.user_password_reset",
  "system.queue_task_deleted",
  "system.queue_task_cancelled",
  "system.queue_archived_purged",
]);
const WARN_ACTIONS = new Set([
  "system.admin_revoked",
  "system.setting_changed",
  "system.queue_task_retried",
  "system.queue_task_run_now",
]);

const LEVEL_BADGE_STYLES: Record<Level, string> = {
  info: "badge-success",
  warn: "badge-warning",
  error: "badge-error",
};

const ROLE_LABEL_KEYS = {
  superadmin: "ulogs.role.superadmin",
  system: "ulogs.role.system",
  admin: "ulogs.role.admin",
  user: "ulogs.role.user",
} as const;

function roleLabelKey(actorRole: string): (typeof ROLE_LABEL_KEYS)[keyof typeof ROLE_LABEL_KEYS] {
  const r = actorRole.toLowerCase();
  if (r === "superadmin" || r === "system_admin" || r === "owner") return ROLE_LABEL_KEYS.superadmin;
  if (r === "admin") return ROLE_LABEL_KEYS.admin;
  if (r === "system") return ROLE_LABEL_KEYS.system;
  return ROLE_LABEL_KEYS.user;
}

function levelFor(row: AuditLog): Level {
  return OUTCOME_LEVEL[row.outcome] ?? "info";
}

function actionTone(row: AuditLog): string {
  if (DANGER_ACTIONS.has(row.action) || levelFor(row) === "error") {
    return "badge-error";
  }
  if (WARN_ACTIONS.has(row.action) || levelFor(row) === "warn") {
    return "badge-warning";
  }
  return "badge-info";
}

/* Human-readable target: prefer the username/email recorded in details,
 * fall back to type:id — mirrors frontend/src/views/system/SystemAuditLog.vue. */
function targetKey(row: AuditLog): string {
  const details =
    row.details && typeof row.details === "object"
      ? (row.details as Record<string, unknown>)
      : null;
  const username = details && typeof details.target_username === "string" ? details.target_username : "";
  const email = details && typeof details.target_email === "string" ? details.target_email : "";
  if (username || email) return email ? `${username} (${email})` : username;
  if (row.target_id) return row.target_type ? `${row.target_type}:${row.target_id}` : row.target_id;
  if (row.target_user_id) return row.target_user_id.slice(0, 8);
  return "";
}

function shortId(id: string): string {
  return id ? id.slice(0, 8) : "";
}

function formatTime(s: string | undefined, locale: string): string {
  if (!s) return "—";
  try {
    return new Intl.DateTimeFormat(locale, vnIntl({
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    })).format(new Date(s));
  } catch {
    return s;
  }
}

function detailsJSON(row: AuditLog): string {
  if (row.details === null || row.details === undefined) return "{}";
  if (typeof row.details === "string") return row.details;
  try {
    return JSON.stringify(row.details, null, 2);
  } catch {
    return String(row.details);
  }
}

const PAGE_SIZE = 50;

export default function SystemAuditLogs() {
  const { t, locale } = useT();
  const auth = useAuth();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [meId, setMeId] = useState("");
  const [entries, setEntries] = useState<AuditLog[]>([]);
  const [cursor, setCursor] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(false);
  /* A request has completed at least once — until then the list shows the
   * loading row instead of the (still unknown) empty state. */
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState("");
  const [q, setQ] = useState("");
  const [level, setLevel] = useState<Level | "all">("all");
  const [detail, setDetail] = useState<AuditLog | null>(null);

  const load = useCallback(async (reset: boolean) => {
    if (loading) return;
    setLoading(true);
    setError("");
    try {
      const res = await listSystemAuditLog({
        after_id: reset ? undefined : cursor || undefined,
        limit: PAGE_SIZE,
      });
      const rows = res.data ?? [];
      setEntries((prev) => (reset ? rows : [...prev, ...rows]));
      setCursor(res.next_cursor ?? 0);
      setHasMore(!!res.next_cursor && rows.length > 0);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) setAllowed(false);
      else setError(e instanceof Error ? e.message : t("ulogs.loadFailed"));
    } finally {
      setLoading(false);
      setLoaded(true);
    }
  }, [cursor, loading, t]);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const me = await getCurrentUser();
        if (!alive) return;
        if (me.data?.user?.is_system_admin !== true) {
          setAllowed(false);
          return;
        }
        setMeId(me.data.user.id ?? "");
        setAllowed(true);
      } catch {
        if (alive) setAllowed(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (allowed) void load(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allowed]);

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return entries.filter((row) => {
      if (level !== "all" && levelFor(row) !== level) return false;
      if (!needle) return true;
      const hay = [
        row.action,
        row.actor_user_id,
        row.actor_role,
        targetKey(row),
        row.request_path,
        detailsJSON(row),
      ]
        .join(" ")
        .toLowerCase();
      return hay.includes(needle);
    });
  }, [entries, q, level]);

  return (
    <SystemSection allowed={allowed} title={t("adm.cardLogs")} deniedMessage={t("ulogs.denied")}>
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">{t("adm.cardLogsDesc")}</span>
        <button
          className="btn btn-outline btn-sm ml-auto h-8 w-8 p-0"
          onClick={() => void load(true)}
          disabled={loading}
          title={t("common.refresh")}
          aria-label={t("common.refresh")}
        >
          <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
        </button>
      </div>

      {error && (
        <Notice
          tone="error"
          className="mb-4"
          action={
            <button className="btn btn-outline btn-sm" onClick={() => void load(true)}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      <div className="mb-5 flex flex-wrap items-center gap-3">
        <div className="relative min-w-[220px] flex-1">
          <IconSearch className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
          <input
            id="ulogs-filter"
            className="input h-10 pl-10 text-[14px]"
            placeholder={t("ulogs.filterPh")}
            aria-label={t("ulogs.filterPh")}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
        <PillGroup ariaLabel={t("common.filters")}>
          {(["all", ...LEVELS] as const).map((lv) => (
            <Pill key={lv} active={level === lv} onClick={() => setLevel(lv)}>
              {lv === "all" ? t("ulogs.level.all") : t(`ulogs.level.${lv}`)}
            </Pill>
          ))}
        </PillGroup>
      </div>

      <DataTable
        columns={[
          { key: "time", header: t("ulogs.colTime"), className: "w-[170px]" },
          { key: "actor", header: t("ulogs.colActor"), className: "w-[170px]" },
          { key: "action", header: t("ulogs.colAction"), className: "w-[200px]" },
          { key: "target", header: t("ulogs.colTarget"), className: "min-w-[220px]" },
          { key: "level", header: t("ulogs.colLevel"), className: "w-24" },
        ]}
        rows={filtered}
        rowKey={(row) => String(row.id)}
        minWidth="min-w-[900px]"
        loading={!loaded || loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("ulogs.empty")}
        onRowClick={(row) => setDetail(row)}
        renderRow={(row) => {
          const actorName =
            row.actor_user_id === meId && (auth.user?.username || auth.user?.email)
              ? auth.user?.username || auth.user?.email
              : shortId(row.actor_user_id);
          return (
            <>
              <td className="px-5 py-3.5 align-middle whitespace-nowrap">
                <span className="caption text-muted tabular-nums">
                  {formatTime(row.created_at, locale)}
                </span>
              </td>
              <td className="px-5 py-3.5 align-middle">
                {row.actor_user_id ? (
                  <div className="min-w-0">
                    <div className="truncate text-[13px] font-medium text-ink">{actorName}</div>
                    {row.actor_role && (
                      <div className="caption truncate text-muted">{t(roleLabelKey(row.actor_role))}</div>
                    )}
                  </div>
                ) : (
                  <span className="caption text-muted">{t("ulogs.role.system")}</span>
                )}
              </td>
              <td className="px-5 py-3.5 align-middle">
                <span className={`badge-pill inline-block max-w-full truncate font-mono text-[11px] ${actionTone(row)}`}>
                  {row.action}
                </span>
              </td>
              <td className="px-5 py-3.5 align-middle">
                <span className="block truncate font-mono text-[12px] text-body" title={targetKey(row)}>
                  {targetKey(row) || <span className="text-muted-soft">—</span>}
                </span>
              </td>
              <td className="px-5 py-3.5 align-middle">
                <span className={`badge-pill inline-block text-[10px] ${LEVEL_BADGE_STYLES[levelFor(row)]}`}>
                  {t(`ulogs.level.${levelFor(row)}`)}
                </span>
              </td>
            </>
          );
        }}
      />

      {hasMore && !q && level === "all" && (
        <LoadMore onMore={() => void load(false)} loading={loading} label={t("ulogs.loadMore")} />
      )}

      <Modal
        open={detail !== null}
        title={detail?.action ?? ""}
        onClose={() => setDetail(null)}
        width="w-[560px]"
      >
        {detail && (
          <div className="flex flex-col gap-4">
            <dl className="flex flex-col gap-2.5">
              {(
                [
                  [t("ulogs.colTime"), formatTime(detail.created_at, locale)],
                  [t("ulogs.colActor"), detail.actor_user_id || t("ulogs.role.system")],
                  [t("ulogs.colRole"), detail.actor_role ? t(roleLabelKey(detail.actor_role)) : ""],
                  [t("ulogs.colAction"), detail.action],
                  [t("ulogs.colTarget"), targetKey(detail)],
                  [t("ulogs.colLevel"), detail.outcome],
                  [t("ulogs.colPath"), `${detail.request_method} ${detail.request_path}`.trim()],
                ] as const
              )
                .filter(([, v]) => v)
                .map(([label, value]) => (
                  <div key={label} className="grid grid-cols-[110px_minmax(0,1fr)] items-baseline gap-3">
                    <dt className="caption whitespace-nowrap text-muted">{label}</dt>
                    <dd className="break-all text-[13px] text-body">{value}</dd>
                  </div>
                ))}
            </dl>

            <div>
              <div className="caption-uppercase mb-1.5 text-muted">{t("ulogs.detailsJson")}</div>
              <pre className="max-h-[50vh] overflow-auto whitespace-pre-wrap break-all rounded-[12px] border border-hairline bg-canvas-soft p-3 font-mono text-[12px] leading-relaxed text-body">
                {detailsJSON(detail)}
              </pre>
            </div>
          </div>
        )}
      </Modal>
    </SystemSection>
  );
}
