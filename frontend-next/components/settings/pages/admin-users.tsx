"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconSearch } from "@/components/icons";
import { useToast } from "@/components/toast";
import { DataTable } from "@/components/settings/data-table";
import { Notice } from "@/components/settings/notice";
import { FieldLabel } from "@/components/settings/field";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Pager } from "@/components/settings/pager";
import { SystemSection } from "@/components/settings/system-section";
import { useT } from "@/lib/i18n";
import { useAuth } from "@/lib/auth";
import { getCurrentUser } from "@/lib/api/auth";
import {
  createSystemUser,
  listSystemUsers,
  promoteUserToSystemAdmin,
  resetUserPassword,
  revokeSystemAdmin,
  updateSystemUserRole,
  type SystemAdminUser,
} from "@/lib/api/system";
import type { TenantRole } from "@/lib/api/tenants";
import { ApiError } from "@/lib/api-client";
import { formatDate } from "@/lib/datetime";

const initials = (name: string) =>
  name
    .split(" ")
    .map((w) => w[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();

type AddMode = "create" | "promote";

/* Assignable workspace roles: admin/member (owner is retired). */
const WORKSPACE_ROLES: TenantRole[] = ["member", "admin"];

const ROLE_BADGE_STYLES: Record<TenantRole, string> = {
  owner: "border-warning/30 bg-warning-surface text-warning",
  admin: "border-info/30 bg-info-surface text-info",
  member: "border-hairline bg-surface-strong/70 text-body",
};

/* Page size of the user list — the API is offset/limit based. */
const USERS_PAGE_SIZE = 200;

/* Three-role segmented pill — click a segment to switch the user's role. */
function RolePillToggle({
  value,
  busy,
  onChange,
}: {
  value: TenantRole;
  busy?: boolean;
  onChange: (role: TenantRole) => void;
}) {
  const { t } = useT();
  return (
    <PillGroup ariaLabel={t("usrp.wsRolesTitle")} className="shrink-0">
      {WORKSPACE_ROLES.map((r) => (
        <Pill
          key={r}
          active={r === value}
          disabled={busy || r === value}
          onClick={() => onChange(r)}
          className="capitalize"
        >
          {r === "admin" ? t("usrp.roleAdmin") : r === "owner" ? t("usrp.roleOwner") : t("usrp.roleMember")}
        </Pill>
      ))}
    </PillGroup>
  );
}

export default function SystemUsers() {
  const { t } = useT();
  const toast = useToast();
  const auth = useAuth();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [meId, setMeId] = useState("");
  const [users, setUsers] = useState<SystemAdminUser[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  /* A request has completed at least once — until then the table shows the
   * loading row instead of the (still unknown) empty state. */
  const [loaded, setLoaded] = useState(false);
  const [q, setQ] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [detail, setDetail] = useState<SystemAdminUser | null>(null);
  const [roleBusy, setRoleBusy] = useState<string | null>(null);
  const [detailError, setDetailError] = useState("");

  const [modalOpen, setModalOpen] = useState(false);
  const [addMode, setAddMode] = useState<AddMode>("create");
  const [form, setForm] = useState({ username: "", email: "", password: "" });
  const [generatedPassword, setGeneratedPassword] = useState("");
  const [resetting, setResetting] = useState<SystemAdminUser | null>(null);
  const [newPassword, setNewPassword] = useState("");
  const [revoking, setRevoking] = useState<SystemAdminUser | null>(null);
  const [promoting, setPromoting] = useState<SystemAdminUser | null>(null);

  const load = useCallback(async (query: string, pageNum: number) => {
    setLoading(true);
    try {
      const res = await listSystemUsers({
        limit: USERS_PAGE_SIZE,
        offset: (pageNum - 1) * USERS_PAGE_SIZE,
        q: query || undefined,
      });
      setUsers(res.users ?? []);
      setTotal(res.total ?? 0);
      setError("");
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) setAllowed(false);
      else setError(e instanceof Error ? e.message : t("usrp.loadFailed"));
    } finally {
      setLoading(false);
      setLoaded(true);
    }
  }, []);

  const searchTimer = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (!allowed) return;
    clearTimeout(searchTimer.current);
    searchTimer.current = window.setTimeout(() => void load(q.trim(), page), 250);
    return () => {
      clearTimeout(searchTimer.current);
    };
  }, [q, page, allowed, load]);

  /* A new search always starts on the first page. */
  useEffect(() => {
    setPage(1);
  }, [q]);

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

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      await load(q.trim(), page);
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : t("usrp.opFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const submitAdd = () => {
    if (addMode === "create") {
      if (!form.username.trim() || !form.email.trim()) return;
      void run(async () => {
        const res = await createSystemUser({
          username: form.username.trim(),
          email: form.email.trim(),
          ...(form.password ? { password: form.password } : {}),
        });
        // Show the one-time generated password in-place instead of closing.
        if (res.generated_password) setGeneratedPassword(res.generated_password);
        else setModalOpen(false);
        toast.success(t("common.saved"));
      });
      return;
    }
    if (!form.email.trim()) return;
    void run(() => promoteUserToSystemAdmin({ email: form.email.trim() })).then((ok) => {
      if (ok) {
        setModalOpen(false);
        toast.success(t("common.saved"));
      }
    });
  };

  const submitReset = () => {
    if (!resetting || !newPassword) return;
    void run(() =>
      resetUserPassword({ email: resetting.email, new_password: newPassword }),
    ).then((ok) => {
      if (ok) {
        setResetting(null);
        setNewPassword("");
        toast.success(t("common.saved"));
      }
    });
  };

  const submitRevoke = () => {
    if (!revoking) return;
    void run(() => revokeSystemAdmin(revoking.id)).then((ok) => {
      if (ok) {
        setRevoking(null);
        toast.success(t("common.saved"));
      }
    });
  };

  const submitPromote = () => {
    if (!promoting) return;
    void run(() => promoteUserToSystemAdmin({ user_id: promoting.id })).then((ok) => {
      if (ok) {
        setPromoting(null);
        toast.success(t("common.saved"));
      }
    });
  };

  const currentTenantId = String(auth.selectedTenantId ?? auth.tenant?.id ?? "");

  const patchMembershipRole = (
    user: SystemAdminUser,
    tenantId: number,
    role: TenantRole,
  ): SystemAdminUser => ({
    ...user,
    memberships: (user.memberships ?? []).map((m) =>
      m.tenant_id === tenantId ? { ...m, role } : m,
    ),
  });

  const changeWorkspaceRole = async (u: SystemAdminUser, tenantId: number, role: TenantRole) => {
    const key = `${u.id}:${tenantId}`;
    if (roleBusy === key) return;
    setRoleBusy(key);
    setDetailError("");
    try {
      await updateSystemUserRole(tenantId, u.id, role);
      setUsers((prev) => prev.map((x) => (x.id === u.id ? patchMembershipRole(x, tenantId, role) : x)));
      setDetail((d) => (d && d.id === u.id ? patchMembershipRole(d, tenantId, role) : d));
      toast.success(t("common.saved"));
    } catch (e) {
      setDetailError(e instanceof Error ? e.message : t("usrp.roleFailed"));
    } finally {
      setRoleBusy(null);
    }
  };

  /* Units column: the current unit first, then admin roles, then by name. */
  const sortedMemberships = (u: SystemAdminUser) =>
    [...(u.memberships ?? [])].sort((a, b) => {
      const cur = Number(String(b.tenant_id) === currentTenantId) - Number(String(a.tenant_id) === currentTenantId);
      if (cur !== 0) return cur;
      const adm = Number(b.role !== "member") - Number(a.role !== "member");
      return adm !== 0 ? adm : a.tenant_name.localeCompare(b.tenant_name);
    });

  const adminCount = users.filter((u) => u.is_system_admin).length;
  const pageCount = Math.max(1, Math.ceil(total / USERS_PAGE_SIZE));

  return (
    <SystemSection allowed={allowed} title={t("usrp.title")} deniedMessage={t("usrp.denied")}>
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">
          {t("usrp.summary", {
            total,
            admins: adminCount,
            adminWord: adminCount === 1 ? t("usrp.adminOne") : t("usrp.adminMany"),
          })}
        </span>
        <button
          className="btn btn-primary btn-sm ml-auto"
          onClick={() => {
            setForm({ username: "", email: "", password: "" });
            setGeneratedPassword("");
            setModalOpen(true);
          }}
        >
          <IconPlus className="h-3.5 w-3.5" /> {t("users.addUser")}
        </button>
      </div>

      {error && (
        <Notice
          tone="error"
          className="mb-4"
          action={
            <button className="btn btn-outline btn-sm" onClick={() => void load(q.trim(), page)}>
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
            id="usrp-search"
            className="input h-10 pl-10 text-[14px]"
            placeholder={t("usrp.searchPh")}
            aria-label={t("usrp.searchPh")}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
      </div>

      <DataTable
        columns={[
          { key: "user", header: t("usrp.colUser"), className: "min-w-[260px]" },
          {
            key: "units",
            header: (
              <span className="inline-flex flex-wrap items-center gap-2">
                {t("usrp.colUnits")}
                {/* Badge colors = role in that unit. */}
                <span className={`badge-pill border px-1.5 py-0 text-[10px] normal-case ${ROLE_BADGE_STYLES.admin}`}>
                  {t("usrp.roleAdmin")}
                </span>
                <span className={`badge-pill border px-1.5 py-0 text-[10px] normal-case ${ROLE_BADGE_STYLES.member}`}>
                  {t("usrp.roleMember")}
                </span>
              </span>
            ),
            className: "min-w-[280px]",
          },
          { key: "status", header: t("usrp.colStatus"), className: "w-28" },
          { key: "created", header: t("usrp.colCreated"), className: "w-32" },
          { key: "actions", header: t("users.actions"), className: "w-[280px]", align: "right" },
        ]}
        rows={users}
        rowKey={(u) => u.id}
        minWidth="min-w-[960px]"
        loading={!loaded || loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("usrp.empty")}
        onRowClick={(u) => {
          setDetailError("");
          setDetail(u);
        }}
        renderRow={(u) => {
          const memberships = sortedMemberships(u);
          return (
            <>
              <td className="px-5 py-3.5 align-middle">
                <div className="flex min-w-0 items-center gap-3">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[12px] font-semibold text-ink border border-hairline">
                    {initials(u.username)}
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 text-[14px] font-medium text-ink">
                      <span className="truncate">{u.username}</span>
                      {u.is_system_admin ? (
                        <span className="badge-pill shrink-0 bg-surface-dark text-on-dark text-[10px] py-0.5 px-2">
                          {t("users.superadmin")}
                        </span>
                      ) : (
                        <span className="badge-pill shrink-0 bg-surface-strong text-muted-soft text-[10px] py-0.5 px-2">
                          {t("usrp.roleUser")}
                        </span>
                      )}
                      {u.id === meId && (
                        <span className="caption shrink-0 text-muted-soft font-normal">{t("usrp.you")}</span>
                      )}
                    </div>
                    <div className="caption truncate text-muted">{u.email}</div>
                  </div>
                </div>
              </td>
              <td className="px-5 py-3.5 align-middle">
                {memberships.length > 0 ? (
                  <div className="flex max-w-[420px] flex-wrap gap-1">
                    {memberships.map((m) => {
                      const roleLabel =
                        m.role === "admin" ? t("usrp.roleAdmin") : m.role === "owner" ? t("usrp.roleOwner") : t("usrp.roleMember");
                      return (
                        <span
                          key={m.tenant_id}
                          title={`${m.tenant_name} — ${roleLabel}`}
                          className={`badge-pill inline-block max-w-[160px] truncate border text-[11px] ${ROLE_BADGE_STYLES[m.role] ?? ROLE_BADGE_STYLES.member} ${
                            String(m.tenant_id) === currentTenantId ? "font-semibold" : ""
                          }`}
                        >
                          {m.tenant_name}
                        </span>
                      );
                    })}
                  </div>
                ) : (
                  <span className="caption text-muted-soft">—</span>
                )}
              </td>
              <td className="px-5 py-3.5 align-middle whitespace-nowrap">
                <span
                  className={`inline-flex items-center gap-1.5 text-xs font-medium ${
                    u.is_active ? "text-success" : "text-muted"
                  }`}
                >
                  <span
                    className={`h-1.5 w-1.5 rounded-full ${
                      u.is_active ? "bg-success" : "bg-muted-soft"
                    }`}
                  />
                  {u.is_active ? t("mdl.active") : t("mdl.disabled")}
                </span>
              </td>
              <td className="px-5 py-3.5 align-middle whitespace-nowrap">
                <span className="caption text-muted">
                  {u.created_at ? formatDate(u.created_at) : "—"}
                </span>
              </td>
              <td
                className="px-5 py-3.5 align-middle text-right whitespace-nowrap"
                onClick={(e) => e.stopPropagation()}
              >
                <div className="flex items-center justify-end gap-2">
                  {!(u.is_system_admin && u.id === meId) && (
                    <button
                      className={`btn btn-sm px-3 text-xs font-medium ${
                        u.is_system_admin ? "btn-danger" : "btn-outline"
                      }`}
                      onClick={() => (u.is_system_admin ? setRevoking(u) : setPromoting(u))}
                      disabled={busy}
                      title={
                        u.is_system_admin
                          ? t("usrp.revokeTip")
                          : t("usrp.promoteTip")
                      }
                      aria-label={
                        u.is_system_admin
                          ? t("usrp.revokeTip")
                          : t("usrp.promoteTip")
                      }
                    >
                      {u.is_system_admin ? t("usrp.revokeCta") : t("usrp.promoteCta")}
                    </button>
                  )}
                  {!u.is_system_admin && (
                    <button
                      className="btn btn-outline btn-sm px-3 text-xs font-medium"
                      onClick={() => {
                        setNewPassword("");
                        setResetting(u);
                      }}
                      disabled={u.id === meId}
                      title={u.id === meId ? t("usrp.resetSelfTip") : t("usrp.resetCta")}
                      aria-label={t("usrp.resetCta")}
                    >
                      {t("usrp.resetCta")}
                    </button>
                  )}
                </div>
              </td>
            </>
          );
        }}
      />

      <Pager page={page} pageCount={pageCount} onPage={setPage} loading={loading} />

      {/* user detail — per-workspace role pills */}
      <Modal
        open={detail !== null}
        title={t("usrp.wsRolesTitle")}
        onClose={() => setDetail(null)}
        width="w-[520px]"
      >
        {detail && (
          <div className="flex flex-col gap-4">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[13px] font-semibold text-ink border border-hairline">
                {initials(detail.username)}
              </div>
              <div className="min-w-0">
                <div className="flex items-center gap-2 text-[14px] font-medium text-ink">
                  <span className="truncate">{detail.username}</span>
                  {detail.is_system_admin && (
                    <span className="badge-pill shrink-0 bg-surface-dark text-on-dark text-[10px] py-0.5 px-2">
                      {t("users.superadmin")}
                    </span>
                  )}
                </div>
                <div className="caption truncate text-muted">{detail.email}</div>
              </div>
            </div>

            {detailError && <Notice tone="error">{detailError}</Notice>}

            <div className="flex flex-col gap-2">
              {(detail.memberships ?? []).length === 0 && (
                <p className="caption py-4 text-center text-muted-soft">
                  {t("usrp.noMembership")}
                </p>
              )}
              {[...(detail.memberships ?? [])]
                .sort((a, b) =>
                  String(a.tenant_id) === currentTenantId
                    ? -1
                    : String(b.tenant_id) === currentTenantId
                      ? 1
                      : a.tenant_name.localeCompare(b.tenant_name),
                )
                .map((m) => (
                  <div
                    key={m.tenant_id}
                    className="flex items-center justify-between gap-3 rounded-lg border border-hairline/80 bg-surface-card/70 px-3 py-2.5"
                  >
                    <div className="flex min-w-0 items-center gap-2.5">
                      <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded bg-surface-strong text-[11px] font-semibold text-muted">
                        {m.tenant_name.charAt(0).toUpperCase()}
                      </span>
                      <span className="truncate text-[13px] font-medium text-ink" title={m.tenant_name}>
                        {m.tenant_name}
                      </span>
                      {String(m.tenant_id) === currentTenantId && (
                        <span className="caption shrink-0 text-muted-soft">{t("usrp.currentWs")}</span>
                      )}
                    </div>
                    <RolePillToggle
                      value={m.role}
                      busy={roleBusy === `${detail.id}:${m.tenant_id}`}
                      onChange={(role) => void changeWorkspaceRole(detail, m.tenant_id, role)}
                    />
                  </div>
                ))}
            </div>
          </div>
        )}
      </Modal>

      {/* add admin / create user */}
      <Modal
        open={modalOpen}
        title={t("users.addUser")}
        onClose={() => setModalOpen(false)}
        width="w-[500px]"
      >
        <div className="flex flex-col gap-4">
          <PillGroup ariaLabel={t("users.addUser")}>
            {(
              [
                { id: "create", label: t("usrp.modeCreate") },
                { id: "promote", label: t("usrp.modePromote") },
              ] as const
            ).map((m) => (
              <Pill
                key={m.id}
                active={addMode === m.id}
                onClick={() => setAddMode(m.id)}
                className="px-3.5 py-1.5 text-[13px]"
              >
                {m.label}
              </Pill>
            ))}
          </PillGroup>

          {addMode === "create" && (
            <div>
              <FieldLabel htmlFor="usrp-new-username" required>
                {t("users.fullName")}
              </FieldLabel>
              <input
                id="usrp-new-username"
                className="input"
                required
                aria-required="true"
                value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })}
                placeholder={t("usrp.namePh")}
              />
            </div>
          )}
          <div>
            <FieldLabel htmlFor="usrp-new-email" required>
              {t("userSettings.email")}
            </FieldLabel>
            <input
              id="usrp-new-email"
              className="input"
              type="email"
              required
              aria-required="true"
              value={form.email}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
          </div>
          {addMode === "create" && (
            <div>
              <FieldLabel htmlFor="usrp-new-password">{t("usrp.pwOptional")}</FieldLabel>
              <input
                id="usrp-new-password"
                className="input"
                type="password"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                placeholder={t("usrp.pwPh")}
              />
            </div>
          )}

          {generatedPassword && (
            <div className="rounded-[12px] border border-hairline bg-canvas-soft p-4">
              <div className="caption-uppercase mb-1.5 text-muted">
                {t("usrp.generatedPw")}
              </div>
              <code className="text-[14px] font-medium text-ink">{generatedPassword}</code>
            </div>
          )}

          <div className="mt-1 flex justify-end gap-3">
            <button className="btn btn-outline" onClick={() => setModalOpen(false)}>
              {generatedPassword ? t("usrp.done") : t("common.cancel")}
            </button>
            {!generatedPassword && (
              <button className="btn btn-primary" onClick={submitAdd} disabled={busy}>
                {addMode === "create" ? t("users.addUser") : t("users.promote")}
              </button>
            )}
          </div>
        </div>
      </Modal>

      {/* reset password */}
      <Modal
        open={resetting !== null}
        title={t("usrp.resetTitle", { name: resetting?.username ?? "" })}
        onClose={() => setResetting(null)}
        width="w-[420px]"
      >
        <div className="flex flex-col gap-4">
          <p className="body-sm text-body">
            {t("usrp.resetBody1")}{" "}
            <span className="font-medium text-ink">{resetting?.email}</span>.{" "}
            {t("usrp.resetBody2")}
          </p>
          <div>
            <FieldLabel htmlFor="usrp-reset-password" required>
              {t("usrp.newPwPh")}
            </FieldLabel>
            <input
              id="usrp-reset-password"
              className="input"
              type="password"
              required
              aria-required="true"
              placeholder={t("usrp.newPwPh")}
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
            />
          </div>
          <div className="flex justify-end gap-3">
            <button className="btn btn-outline" onClick={() => setResetting(null)}>
              {t("common.cancel")}
            </button>
            <button className="btn btn-primary" onClick={submitReset} disabled={busy || !newPassword}>
              {t("usrp.resetBtn")}
            </button>
          </div>
        </div>
      </Modal>

      {/* revoke confirm */}
      <Modal
        open={revoking !== null}
        title={t("usrp.revokeTitle")}
        onClose={() => setRevoking(null)}
        width="w-[420px]"
      >
        <p className="body-sm mb-6 text-body">
          {t("usrp.revokeBody1")}{" "}
          <span className="font-medium text-ink">{revoking?.username}</span> ({revoking?.email})?{" "}
          {t("usrp.revokeBody2")}
        </p>
        <div className="flex justify-end gap-3">
          <button className="btn btn-outline" onClick={() => setRevoking(null)}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-danger" onClick={submitRevoke} disabled={busy}>
            {t("usrp.revokeBtn")}
          </button>
        </div>
      </Modal>

      {/* promote confirm */}
      <Modal
        open={promoting !== null}
        title={t("usrp.promoteTitle")}
        onClose={() => setPromoting(null)}
        width="w-[420px]"
      >
        <p className="body-sm mb-6 text-body">
          {t("usrp.promoteBody1")}{" "}
          <span className="font-medium text-ink">{promoting?.username}</span> ({promoting?.email})?{" "}
          {t("usrp.promoteBody2")}
        </p>
        <div className="flex justify-end gap-3">
          <button className="btn btn-outline" onClick={() => setPromoting(null)}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary" onClick={submitPromote} disabled={busy}>
            {t("usrp.promoteBtn")}
          </button>
        </div>
      </Modal>
    </SystemSection>
  );
}
