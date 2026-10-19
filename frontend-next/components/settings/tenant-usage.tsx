"use client";

/* The workspace's usage package as its members see it: plan, mode, the
 * limits and how much is used, and in seat mode who holds a seat. The
 * Tenant Admin hands seats out to members and takes them back. Plans
 * themselves are assigned by the SuperAdmin. */

import { useCallback, useEffect, useMemo, useState } from "react";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { IconRefresh } from "@/components/icons";
import { LimitStatusList } from "@/components/settings/usage-limits";
import { fetchAllTenantMembers, type TenantMember } from "@/lib/api/tenants";
import {
  assignTenantSeat,
  formatTokens,
  getMyUsage,
  getTenantUsage,
  revokeTenantSeat,
  type UsageSummary,
  type UsageTenantUsage,
} from "@/lib/api/usage";
import { formatDate } from "@/lib/datetime";

export function TenantUsage() {
  const { t } = useT();
  const auth = useAuth();
  const tenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const role = auth.memberships.find((m) => String(m.tenant_id) === String(tenantId))?.role ?? "";
  const isAdmin = role === "admin" || role === "owner" || auth.user?.is_system_admin === true;
  const myId = auth.user?.id ?? "";

  const [data, setData] = useState<UsageTenantUsage | null>(null);
  const [mine, setMine] = useState<UsageSummary | null>(null);
  const [members, setMembers] = useState<TenantMember[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [pick, setPick] = useState("");

  const load = useCallback(async () => {
    if (!tenantId) return;
    try {
      const [d, m] = await Promise.all([getTenantUsage(tenantId), getMyUsage().then((r) => r.data).catch(() => null)]);
      setData(d);
      setMine(m);
      setError("");
      if (d.subscription?.mode === "seat" && isAdmin) {
        setMembers(await fetchAllTenantMembers(tenantId));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("tus.loadFailed"));
    }
  }, [tenantId, isAdmin, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("tus.opFailed"));
    } finally {
      setBusy(false);
    }
  };

  const sub = data?.subscription ?? null;
  const seatHolders = useMemo(() => new Set((data?.seats ?? []).map((s) => s.user_id)), [data]);
  const memberByID = useMemo(() => new Map(members.map((m) => [m.user_id, m])), [members]);
  const free = members.filter((m) => !seatHolders.has(m.user_id) && m.status === "active");
  const myEntitlement = mine?.entitlement ?? null;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <div>
          <h3 className="title-sm">{t("tus.title")}</h3>
          <p className="caption mt-1 text-muted">{t("tus.desc")}</p>
        </div>
        <button className="btn btn-outline btn-sm ml-auto" disabled={busy} onClick={() => void load()}>
          <IconRefresh className="h-3.5 w-3.5" /> {t("common.refresh")}
        </button>
      </div>
      {error && <p className="caption text-error">{error}</p>}

      {!data ? (
        <p className="caption text-muted">…</p>
      ) : !sub ? (
        <div className="card p-4">
          <p className="text-[14px] text-ink">{t("tus.none")}</p>
          <p className="caption mt-1 text-muted">{data.enforced ? t("tus.noneEnforced") : t("tus.noneRelaxed")}</p>
        </div>
      ) : (
        <div className="card divide-y divide-hairline">
          <Row label={t("tus.plan")} desc={sub.plan?.description || ""}>
            <span className="text-[14px] text-ink">{sub.plan?.name ?? sub.plan_id}</span>
            {!sub.current && <span className="badge-pill ml-2 text-[11px] text-error">{t("usg.expired")}</span>}
          </Row>
          <Row label={t("tus.mode")} desc={sub.mode === "seat" ? t("usg.modeSeatHint") : t("usg.modePoolHint")}>
            <span className="caption text-body">
              {sub.mode === "seat" ? t("usg.seatsUsed", { used: sub.seats_used, total: sub.seats }) : t("usg.modePool")}
            </span>
          </Row>
          {sub.ends_at && (
            <Row label={t("tus.endsAt")} desc="">
              <span className="caption text-body">{formatDate(sub.ends_at)}</span>
            </Row>
          )}
          <Row label={t("tus.addons")} desc={t("tus.addonsDesc")}>
            {(data.addons ?? []).length === 0 ? (
              <span className="caption text-muted">{t("tus.noAddons")}</span>
            ) : (
              <ul className="caption space-y-0.5 text-right text-body">
                {(data.addons ?? []).map((a) => (
                  <li key={a.agent_id}>
                    {a.agent_name || a.agent_id}
                    {a.ends_at ? ` — ${t("tus.addonUntil", { date: formatDate(a.ends_at) })}` : ""}
                  </li>
                ))}
              </ul>
            )}
          </Row>
          <Row label={sub.mode === "seat" ? t("tus.limitsPerSeat") : t("tus.limitsPool")} desc={t("tus.limitsDesc")}>
            <LimitStatusList items={sub.mode === "seat" ? (myEntitlement?.limits ?? []) : sub.limit_status} />
          </Row>
          <Row label={t("tus.monthUsage")} desc={t("tus.monthUsageDesc")}>
            <span className="caption text-body">
              {(() => {
                const month = sub.mode === "seat" && mine?.user ? mine.user.month : mine?.tenant?.month;
                if (!month) return "—";
                const line = t("acct.usageLine", { questions: month.questions.toLocaleString(), tokens: formatTokens(month.tokens) });
                return month.ocr_tokens
                  ? line + t("acct.usageLineOCR", { tokens: formatTokens(month.ocr_tokens), pages: (month.ocr_pages ?? 0).toLocaleString() })
                  : line;
              })()}
            </span>
          </Row>
        </div>
      )}

      {sub?.mode === "seat" && (
        <div className="card p-4">
          <div className="mb-3 flex items-center gap-3">
            <div>
              <h4 className="text-[14px] font-medium text-ink">{t("tus.seats")}</h4>
              <p className="caption text-muted">
                {myEntitlement?.has_seat ? t("tus.youHaveSeat") : t("tus.youLackSeat")}
              </p>
            </div>
            {isAdmin && (
              <form
                className="ml-auto flex items-center gap-2"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (!pick) return;
                  void run(async () => {
                    await assignTenantSeat(tenantId, pick);
                    setPick("");
                  });
                }}
              >
                <select className="input h-9 w-56 py-0" value={pick} onChange={(e) => setPick(e.target.value)} disabled={busy || data!.seats.length >= sub.seats}>
                  <option value="">{t("tus.pickMember")}</option>
                  {free.map((m) => (
                    <option key={m.user_id} value={m.user_id}>
                      {m.username || m.email}
                    </option>
                  ))}
                </select>
                <button type="submit" className="btn btn-primary btn-sm" disabled={busy || !pick || data!.seats.length >= sub.seats}>
                  {t("tus.assignSeat")}
                </button>
              </form>
            )}
          </div>
          {data!.seats.length === 0 ? (
            <p className="caption text-muted-soft">{t("tus.noSeats")}</p>
          ) : (
            <div className="divide-y divide-hairline">
              {data!.seats.map((s) => {
                const m = memberByID.get(s.user_id);
                return (
                  <div key={s.user_id} className="flex items-center gap-3 py-2 text-[14px]">
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-ink">
                        {m?.username || m?.email || s.user_id}
                        {s.user_id === myId ? ` · ${t("tus.you")}` : ""}
                      </div>
                      <div className="caption text-muted">{formatDate(s.assigned_at)}</div>
                    </div>
                    {isAdmin && (
                      <button type="button" className="btn btn-outline btn-sm text-rose-600" disabled={busy} onClick={() => void run(() => revokeTenantSeat(tenantId, s.user_id))}>
                        {t("tus.revokeSeat")}
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          )}
          {isAdmin && data!.seats.length >= sub.seats && <p className="caption mt-2 text-muted-soft">{t("tus.allSeatsUsed")}</p>}
        </div>
      )}
    </div>
  );
}

function Row({ label, desc, children }: { label: string; desc: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-8 px-4 py-3">
      <div className="min-w-0">
        <div className="text-[13px] font-medium text-ink">{label}</div>
        {desc && <p className="caption mt-0.5 text-muted">{desc}</p>}
      </div>
      <div className="shrink-0 text-right">{children}</div>
    </div>
  );
}
