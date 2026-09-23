/* Tenant-to-tenant knowledge-base access grants (backend kb_access_grants).
 * Two directions:
 *   - incoming: requests other workspaces filed on KBs this workspace owns —
 *     the owner approves / rejects them, or revokes a live grant;
 *   - outgoing: requests this workspace filed on foreign KBs — status only,
 *     plus a form to request access to a KB id learned out-of-band.
 * Every endpoint underneath is workspace-Owner gated server-side; the page
 * mounting this panel applies the same gate (minRole="owner").
 */
"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useAuth } from "@/lib/auth";
import { useT, type LocaleKey } from "@/lib/i18n";
import { IconRefresh } from "@/components/icons";
import {
  listIncomingKBGrants,
  listOutgoingKBGrants,
  requestKBAccess,
  reviewKBGrant,
  revokeKBGrant,
  type KBAccessGrant,
  type KBGrantStatus,
} from "@/lib/api/knowledge";

const STATUS_ORDER: Record<KBGrantStatus, number> = {
  pending: 0,
  approved: 1,
  rejected: 2,
  revoked: 3,
  expired: 4,
};

const STATUS_BADGE: Record<KBGrantStatus, string> = {
  pending: "bg-amber-500/10 text-amber-600 border border-amber-500/20",
  approved: "bg-emerald-500/10 text-emerald-600 border border-emerald-500/20",
  rejected: "bg-rose-500/10 text-rose-600 border border-rose-500/20",
  revoked: "bg-surface-strong text-muted border border-hairline",
  expired: "bg-surface-strong text-muted border border-hairline",
};

function statusKey(s: KBGrantStatus): LocaleKey {
  return `kbGrants.status.${s}` as LocaleKey;
}

function fmtDate(v?: string | null): string {
  if (!v) return "";
  const d = new Date(v);
  return Number.isNaN(d.getTime())
    ? v
    : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

export function KBAccessGrants() {
  const { t } = useT();
  const auth = useAuth();
  const activeTenantId = auth.selectedTenantId ?? String(auth.tenant?.id ?? "");

  const [incoming, setIncoming] = useState<KBAccessGrant[]>([]);
  const [outgoing, setOutgoing] = useState<KBAccessGrant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [busyId, setBusyId] = useState("");

  const [reqKbId, setReqKbId] = useState("");
  const [reqMessage, setReqMessage] = useState("");
  const [reqBusy, setReqBusy] = useState(false);

  const load = useCallback(async () => {
    if (!activeTenantId) return;
    setLoading(true);
    setError("");
    try {
      const [inRows, outRows] = await Promise.all([
        listIncomingKBGrants(activeTenantId),
        listOutgoingKBGrants(),
      ]);
      const sort = (rows: KBAccessGrant[]) =>
        [...rows].sort(
          (a, b) => (STATUS_ORDER[a.status] ?? 9) - (STATUS_ORDER[b.status] ?? 9),
        );
      setIncoming(sort(inRows));
      setOutgoing(sort(outRows));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load access grants");
    } finally {
      setLoading(false);
    }
  }, [activeTenantId]);

  useEffect(() => {
    void load();
  }, [load]);

  const review = async (grant: KBAccessGrant, approved: boolean) => {
    setBusyId(grant.id);
    setError("");
    try {
      await reviewKBGrant(activeTenantId, grant.id, { approved });
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to update request");
    } finally {
      setBusyId("");
    }
  };

  const revoke = async (grant: KBAccessGrant) => {
    setBusyId(grant.id);
    setError("");
    try {
      await revokeKBGrant(activeTenantId, grant.id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to revoke access");
    } finally {
      setBusyId("");
    }
  };

  const submitRequest = async (e: React.FormEvent) => {
    e.preventDefault();
    const kbId = reqKbId.trim();
    if (!kbId) return;
    setReqBusy(true);
    setError("");
    setSuccess("");
    try {
      const res = await requestKBAccess(kbId, {
        message: reqMessage.trim() || undefined,
      });
      if (res.success) {
        setSuccess(t("kbGrants.requestSent"));
        setReqKbId("");
        setReqMessage("");
        await load();
      } else {
        setError(res.message || "Request failed");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Request failed");
    } finally {
      setReqBusy(false);
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">{t("kbGrants.title")}</h2>
          <p className="caption text-muted mt-1">{t("kbGrants.desc")}</p>
        </div>
        <button
          type="button"
          onClick={() => void load()}
          className="btn btn-outline btn-sm flex items-center gap-1.5"
          title={t("common.refresh")}
        >
          <IconRefresh className="h-3.5 w-3.5" />
        </button>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400">
          {error}
        </div>
      )}
      {success && (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/30 dark:text-emerald-400">
          {success}
        </div>
      )}

      {/* Incoming — requests on KBs this workspace owns */}
      <section className="rounded-xl border border-hairline bg-surface-card p-5 space-y-3">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-ink">{t("kbGrants.incoming")}</span>
          <span className="rounded-full bg-surface-strong px-2 py-0.5 text-xs font-medium text-muted">
            {incoming.length}
          </span>
        </div>

        {loading ? (
          <p className="caption text-muted py-4">{t("common.loading")}</p>
        ) : incoming.length === 0 ? (
          <p className="caption text-muted py-4">{t("kbGrants.emptyIncoming")}</p>
        ) : (
          <div className="divide-y divide-hairline">
            {incoming.map((g) => (
              <div
                key={g.id}
                className="flex flex-wrap items-center justify-between gap-3 py-3 text-sm"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-ink truncate">
                      {g.kb_name || g.kb_id}
                    </span>
                    <span
                      className={`badge-pill uppercase text-[10.5px] ${STATUS_BADGE[g.status] ?? ""}`}
                    >
                      {t(statusKey(g.status))}
                    </span>
                  </div>
                  <p className="caption text-muted truncate mt-0.5">
                    {t("kbGrants.fromTenant", {
                      name: g.grantee_tenant_name || `#${g.grantee_tenant_id}`,
                    })}
                    {g.expires_at ? ` · ${t("kbGrants.expires")} ${fmtDate(g.expires_at)}` : ""}
                    {g.message ? ` · “${g.message}”` : ""}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  {g.status === "pending" && (
                    <>
                      <button
                        type="button"
                        disabled={busyId === g.id}
                        onClick={() => void review(g, true)}
                        className="btn btn-primary btn-sm text-xs py-1"
                      >
                        {t("kbGrants.approve")}
                      </button>
                      <button
                        type="button"
                        disabled={busyId === g.id}
                        onClick={() => void review(g, false)}
                        className="btn btn-outline btn-sm text-xs text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/40 py-1"
                      >
                        {t("kbGrants.reject")}
                      </button>
                    </>
                  )}
                  {g.status === "approved" && (
                    <button
                      type="button"
                      disabled={busyId === g.id}
                      onClick={() => void revoke(g)}
                      className="btn btn-outline btn-sm text-xs text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/40 py-1"
                    >
                      {t("kbGrants.revoke")}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Outgoing — requests this workspace filed on foreign KBs */}
      <section className="rounded-xl border border-hairline bg-surface-card p-5 space-y-3">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-ink">{t("kbGrants.outgoing")}</span>
          <span className="rounded-full bg-surface-strong px-2 py-0.5 text-xs font-medium text-muted">
            {outgoing.length}
          </span>
        </div>

        {loading ? (
          <p className="caption text-muted py-4">{t("common.loading")}</p>
        ) : outgoing.length === 0 ? (
          <p className="caption text-muted py-4">{t("kbGrants.emptyOutgoing")}</p>
        ) : (
          <div className="divide-y divide-hairline">
            {outgoing.map((g) => (
              <div
                key={g.id}
                className="flex flex-wrap items-center justify-between gap-3 py-3 text-sm"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    {g.status === "approved" ? (
                      <Link
                        href={`/platform/knowledge-bases/${g.kb_id}`}
                        className="font-medium text-ink truncate hover:text-brand"
                      >
                        {g.kb_name || g.kb_id}
                      </Link>
                    ) : (
                      <span className="font-medium text-ink truncate">
                        {g.kb_name || g.kb_id}
                      </span>
                    )}
                    <span
                      className={`badge-pill uppercase text-[10.5px] ${STATUS_BADGE[g.status] ?? ""}`}
                    >
                      {t(statusKey(g.status))}
                    </span>
                  </div>
                  <p className="caption text-muted truncate mt-0.5">
                    {t("kbGrants.ownerTenant", {
                      name: g.owner_tenant_name || `#${g.owner_tenant_id}`,
                    })}
                    {g.created_at ? ` · ${fmtDate(g.created_at)}` : ""}
                    {g.message ? ` · “${g.message}”` : ""}
                  </p>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Request access by KB id — the owner shares the id out-of-band;
         * the KB itself is invisible until the grant is approved. */}
        <form
          onSubmit={submitRequest}
          className="mt-2 space-y-3 rounded-xl border border-dashed border-hairline-strong p-4"
        >
          <span className="caption font-medium text-ink">{t("kbGrants.requestTitle")}</span>
          <div className="flex flex-wrap items-center gap-2">
            <input
              className="input w-[280px] text-xs font-mono"
              placeholder={t("kbGrants.requestKbId")}
              value={reqKbId}
              onChange={(e) => setReqKbId(e.target.value)}
            />
            <input
              className="input min-w-[200px] flex-1 text-xs"
              placeholder={t("kbGrants.requestMessage")}
              value={reqMessage}
              onChange={(e) => setReqMessage(e.target.value)}
            />
            <button
              type="submit"
              disabled={reqBusy || !reqKbId.trim()}
              className="btn btn-primary btn-sm text-xs"
            >
              {reqBusy ? "…" : t("kbGrants.requestSubmit")}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
