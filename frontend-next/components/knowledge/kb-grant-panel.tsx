/* Owner-issued, read-only share of one knowledge base with a whole unit
 * (kb_access_grants). The owning unit's Tenant Admin — or a SuperAdmin —
 * picks a unit and an optional expiry; every member of that unit can then
 * read and search the KB. Revoking stops access at once. */
"use client";

import { useCallback, useEffect, useState } from "react";
import { grantKBToTenant, listKBGrants, revokeKBGrant, type KBGrant } from "@/lib/api/knowledge";
import { useT } from "@/lib/i18n";
import { TenantPicker } from "@/components/knowledge/tenant-picker";

export function KBGrantPanel({ kbId, ownerTenantId }: { kbId: string; ownerTenantId?: number }) {
  const { t } = useT();
  const [grants, setGrants] = useState<KBGrant[]>([]);
  const [unit, setUnit] = useState("");
  const [expires, setExpires] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      setGrants(await listKBGrants(kbId));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load grants");
    }
  }, [kbId]);

  useEffect(() => {
    void load();
  }, [load]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const grantee = Number(unit);
    if (!grantee) return;
    setBusy(true);
    setError("");
    try {
      await grantKBToTenant(kbId, {
        grantee_tenant_id: grantee,
        ...(expires ? { expires_at: new Date(`${expires}T23:59:59`).toISOString() } : {}),
      });
      setUnit("");
      setExpires("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("kbGrant.failed"));
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (grantId: string) => {
    setError("");
    try {
      await revokeKBGrant(kbId, grantId);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("kbGrant.failed"));
    }
  };

  const active = grants.filter((g) => g.status === "approved");

  return (
    <div className="space-y-4">
      <p className="caption text-muted">{t("kbGrant.intro")}</p>
      {error && <p className="body-sm text-error">{error}</p>}

      <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
        <label className="block min-w-52 flex-1">
          <span className="caption mb-1 block text-muted">{t("kbGrant.unit")}</span>
          <TenantPicker value={unit} onChange={setUnit} excludeId={ownerTenantId} required />
        </label>
        <label className="block w-44">
          <span className="caption mb-1 block text-muted">{t("kbGrant.expires")}</span>
          <input type="date" className="input" value={expires} onChange={(e) => setExpires(e.target.value)} />
        </label>
        <button type="submit" disabled={busy || !unit} className="btn btn-primary btn-sm">
          {t("kbGrant.grant")}
        </button>
      </form>

      {active.length === 0 ? (
        <p className="caption text-muted-soft">{t("kbGrant.empty")}</p>
      ) : (
        <div className="divide-y divide-hairline rounded-xl border border-hairline">
          {active.map((g) => (
            <div key={g.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
              <div className="min-w-0">
                <span className="font-medium text-ink">{g.grantee_tenant_name || `#${g.grantee_tenant_id}`}</span>
                {g.expires_at && (
                  <span className="caption ml-2 text-muted">
                    {t("kbGrant.until", { date: new Date(g.expires_at).toLocaleDateString() })}
                  </span>
                )}
              </div>
              <button
                type="button"
                onClick={() => void revoke(g.id)}
                className="btn btn-outline btn-sm py-1 text-xs text-rose-600"
              >
                {t("kbGrant.revoke")}
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
