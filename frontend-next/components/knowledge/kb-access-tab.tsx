/* "Access" tab of a knowledge base: owner, scope, what owner-unit members
 * may do, the caller's effective permissions and where they come from,
 * and — for managers — who it is shared with plus the share actions. All
 * of it reads GET /knowledge-bases/:id/access; the backend decides.
 * Replaces the former share modal, unit-grant and invite panels. Owner
 * transfer is a separate SuperAdmin action. */
"use client";

import { useCallback, useEffect, useState } from "react";
import { getKBAccess, setKBScope } from "@/lib/api/kb-access";
import {
  scopeOptions,
  showsMemberDefault,
  sourceLabelKey,
  type KBAccessOverview,
  type KBMemberDefault,
  type KBScope,
} from "@/lib/kb-access";
import { copyToClipboard } from "@/lib/clipboard";
import { useTenantRole } from "@/lib/auth";
import { useT, type LocaleKey } from "@/lib/i18n";
import { useToast } from "@/components/toast";
import { KbAccessGrantees } from "@/components/knowledge/kb-access-grantees";
import { KbGrantForm, KbInviteForm } from "@/components/knowledge/kb-access-share-forms";
import { KbTransferOwnerDialog } from "@/components/knowledge/kb-transfer-owner-dialog";

const SCOPE_KEYS: Record<KBScope, { label: LocaleKey; desc: LocaleKey }> = {
  tenant: { label: "kbAccess.scope.tenant", desc: "kbAccess.scope.tenantDesc" },
  restricted: { label: "kbAccess.scope.restricted", desc: "kbAccess.scope.restrictedDesc" },
  published: { label: "kbAccess.scope.published", desc: "kbAccess.scope.publishedDesc" },
  public: { label: "kbAccess.scope.public", desc: "kbAccess.scope.publicDesc" },
};

const PERM_KEYS = [
  ["view", "kbAccess.perm.view"],
  ["contribute", "kbAccess.perm.contribute"],
  ["manage", "kbAccess.perm.manage"],
  ["download_original", "kbAccess.perm.download"],
] as const;

export function KbAccessTab({ kbId, onChanged }: { kbId: string; onChanged?: () => void }) {
  const { t } = useT();
  const toast = useToast();
  const { isSystemAdmin } = useTenantRole();
  const [overview, setOverview] = useState<KBAccessOverview | null>(null);
  /* null: no error; "": failed without a server message (translated at
   * render, so `load` does not depend on `t`, which the i18n provider
   * recreates on every render and would refetch /access). */
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmPublic, setConfirmPublic] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    try {
      setOverview(await getKBAccess(kbId));
      setError(null);
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : "");
    }
  }, [kbId]);
  const errorText = error === null ? "" : error || t("kbAccess.loadFailed");

  useEffect(() => {
    void load();
  }, [load]);

  const refresh = () => {
    void load();
    onChanged?.();
  };

  const save = async (data: { visibility?: KBScope; member_default?: KBMemberDefault }) => {
    setBusy(true);
    try {
      await setKBScope(kbId, data);
      setConfirmPublic(false);
      toast.success(t("kbAccess.saved"));
      refresh();
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t("kbAccess.failed"));
    } finally {
      setBusy(false);
    }
  };

  if (!overview) {
    return <p className={`caption ${errorText ? "text-error" : "text-muted-soft"}`}>{errorText || "…"}</p>;
  }

  const canManage = overview.effective_permissions.manage;
  const options = scopeOptions({
    current: overview.visibility,
    ownerTenantId: overview.owner_tenant_id,
    isSystemAdmin,
    canManage,
  });
  const lockedTitle = canManage ? undefined : t("kbAccess.scopeLocked");
  const shareUrl =
    typeof window !== "undefined"
      ? `${window.location.origin}/platform/knowledge-bases/${kbId}`
      : `/platform/knowledge-bases/${kbId}`;

  return (
    <div className="mx-auto w-full max-w-[760px] space-y-6 overflow-y-auto pb-10">
      {errorText && <p className="caption text-error">{errorText}</p>}

      <section className="card space-y-1 p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <span className="caption text-muted">{t("kbAccess.owner")}</span>
            <p className="title-sm text-ink">
              {overview.owner_tenant_id === 0
                ? t("kbAccess.ownerPlatform")
                : overview.owner_tenant_name || `#${overview.owner_tenant_id}`}
            </p>
          </div>
          {isSystemAdmin && (
            <button type="button" className="btn btn-outline btn-sm" onClick={() => setTransferOpen(true)}>
              {t("kbAccess.transfer")}
            </button>
          )}
        </div>
      </section>

      <section className="card space-y-3 p-5" title={lockedTitle}>
        <h3 className="text-sm font-semibold text-ink">{t("kbAccess.scope")}</h3>
        {!canManage && <p className="caption text-muted">{t("kbAccess.scopeLocked")}</p>}
        <div className="space-y-2">
          {options.map((o) => (
            <label
              key={o.value}
              title={
                o.reason === "tenantFirst"
                  ? t("kbAccess.scope.publicNeedsTenant")
                  : o.disabled
                    ? lockedTitle
                    : undefined
              }
              className={`flex items-start gap-2.5 rounded-xl border px-3 py-2 ${
                overview.visibility === o.value ? "border-ink/40 bg-surface-strong/50" : "border-hairline"
              } ${o.disabled ? "opacity-70" : "cursor-pointer"}`}
            >
              <input
                type="radio"
                name="kb-scope"
                className="mt-1"
                checked={overview.visibility === o.value}
                disabled={o.disabled || busy}
                onChange={() => {
                  if (o.value === overview.visibility) return;
                  if (o.value === "public") setConfirmPublic(true);
                  else void save({ visibility: o.value });
                }}
              />
              <span>
                <span className="block text-[13px] font-medium text-ink">{t(SCOPE_KEYS[o.value].label)}</span>
                <span className="caption block text-muted">{t(SCOPE_KEYS[o.value].desc)}</span>
                {o.reason === "tenantFirst" && (
                  <span className="caption block text-muted-soft">{t("kbAccess.scope.publicNeedsTenant")}</span>
                )}
              </span>
            </label>
          ))}
        </div>
        {overview.visibility === "public" && isSystemAdmin && (
          <p className="caption text-muted">{t("kbAccess.publicMoveHint")}</p>
        )}
        {confirmPublic && (
          <div className="flex flex-wrap items-center gap-2 rounded-lg bg-amber-50 px-3 py-2 text-amber-800 dark:bg-amber-500/10 dark:text-amber-300">
            <span className="caption">{t("kbAccess.scope.publicConfirm")}</span>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={busy}
              onClick={() => void save({ visibility: "public" })}
            >
              {t("common.confirm")}
            </button>
            <button type="button" className="btn btn-outline btn-sm" onClick={() => setConfirmPublic(false)}>
              {t("common.cancel")}
            </button>
          </div>
        )}
        {showsMemberDefault(overview.visibility) && (
          <label className="block pt-1" title={lockedTitle}>
            <span className="caption mb-1 block text-muted">{t("kbAccess.memberDefault")}</span>
            <select
              className="input max-w-sm"
              value={overview.member_default}
              disabled={!canManage || busy}
              onChange={(e) => void save({ member_default: e.target.value as KBMemberDefault })}
            >
              <option value="contribute">{t("kbAccess.memberDefault.contribute")}</option>
              <option value="view">{t("kbAccess.memberDefault.view")}</option>
            </select>
          </label>
        )}
      </section>

      <section className="card space-y-3 p-5">
        <h3 className="text-sm font-semibold text-ink">{t("kbAccess.yours")}</h3>
        <div className="flex flex-wrap gap-2">
          {PERM_KEYS.map(([k, label]) => {
            const on = overview.effective_permissions[k];
            return (
              <span
                key={k}
                className={`badge-pill text-[11px] ${
                  on ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400" : "text-muted line-through"
                }`}
              >
                {on ? "✓" : "✕"} {t(label)}
              </span>
            );
          })}
        </div>
        <p className="caption text-muted">
          {t("kbAccess.via")}:{" "}
          {overview.sources.length === 0
            ? t("kbAccess.noAccess")
            : overview.sources
                .map((s) => sourceLabelKey(s))
                .filter((k): k is NonNullable<typeof k> => k !== null)
                .map((k) => t(k as LocaleKey))
                .join(" · ")}
        </p>
      </section>

      <section className="card space-y-2 p-5">
        <span className="caption font-medium text-muted">{t("kbShare.linkLabel")}</span>
        <div className="flex items-center gap-2">
          <input type="text" readOnly value={shareUrl} className="input select-all font-mono text-xs" />
          <button
            type="button"
            className="btn btn-primary shrink-0 text-xs"
            onClick={async () => {
              if (await copyToClipboard(shareUrl)) {
                setCopied(true);
                setTimeout(() => setCopied(false), 2000);
              }
            }}
          >
            {copied ? t("kbShare.copied") : t("kbShare.copyLink")}
          </button>
        </div>
        <p className="caption text-[11px] text-muted">{t("kbShare.linkNoteNoGrant")}</p>
      </section>

      {canManage && overview.owner_tenant_id !== 0 && (
        <section className="card space-y-6 p-5">
          <KbAccessGrantees overview={overview} onChanged={() => void load()} />
          <KbInviteForm overview={overview} onDone={() => void load()} />
          <KbGrantForm overview={overview} onDone={() => void load()} />
        </section>
      )}

      {isSystemAdmin && (
        <KbTransferOwnerDialog
          kbId={kbId}
          ownerTenantId={overview.owner_tenant_id}
          open={transferOpen}
          onClose={() => setTransferOpen(false)}
          onTransferred={refresh}
        />
      )}
    </div>
  );
}
