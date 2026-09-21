"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconSearch } from "@/components/icons";
import { useT } from "@/lib/i18n";
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

const initials = (name: string) =>
  name
    .split(" ")
    .map((w) => w[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();

type AddMode = "create" | "promote";

export default function SystemUsers() {
  const { t } = useT();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [meId, setMeId] = useState("");
  const [users, setUsers] = useState<SystemAdminUser[]>([]);
  const [total, setTotal] = useState(0);
  const [q, setQ] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const [modalOpen, setModalOpen] = useState(false);
  const [addMode, setAddMode] = useState<AddMode>("create");
  const [form, setForm] = useState({ username: "", email: "", password: "" });
  const [generatedPassword, setGeneratedPassword] = useState("");
  const [resetting, setResetting] = useState<SystemAdminUser | null>(null);
  const [newPassword, setNewPassword] = useState("");
  const [revoking, setRevoking] = useState<SystemAdminUser | null>(null);
  const [promoting, setPromoting] = useState<SystemAdminUser | null>(null);

  const load = useCallback(async (query: string) => {
    try {
      const res = await listSystemUsers({ limit: 200, q: query || undefined });
      setUsers(res.users ?? []);
      setTotal(res.total ?? 0);
      setError("");
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) setAllowed(false);
      else setError(e instanceof Error ? e.message : "Failed to load users");
    }
  }, []);

  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (!allowed) return;
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => void load(q.trim()), 250);
    return () => {
      if (searchTimer.current) clearTimeout(searchTimer.current);
    };
  }, [q, allowed, load]);

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
      await load(q.trim());
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : "Operation failed");
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
      });
      return;
    }
    if (!form.email.trim()) return;
    void run(() => promoteUserToSystemAdmin({ email: form.email.trim() })).then((ok) => {
      if (ok) setModalOpen(false);
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
      }
    });
  };

  const submitRevoke = () => {
    if (!revoking) return;
    void run(() => revokeSystemAdmin(revoking.id)).then((ok) => {
      if (ok) setRevoking(null);
    });
  };

  const submitPromote = () => {
    if (!promoting) return;
    void run(() => promoteUserToSystemAdmin({ user_id: promoting.id })).then((ok) => {
      if (ok) setPromoting(null);
    });
  };

  const WORKSPACE_ROLES: TenantRole[] = ["owner", "admin", "member"];

  const changeWorkspaceRole = (u: SystemAdminUser, tenantId: number, role: TenantRole) => {
    void run(() => updateSystemUserRole(tenantId, u.id, role));
  };

  if (allowed === null) {
    return <div className="mx-auto w-full max-w-[1100px] px-5 py-12 text-muted">Loading…</div>;
  }

  if (!allowed) {
    return (
      <div className="mx-auto w-full max-w-[1100px] px-5 py-16 text-center">
        <h2 className="title-md mb-2">User management</h2>
        <p className="body-sm text-muted">
          This page is only available to system administrators.
        </p>
      </div>
    );
  }

  const adminCount = users.filter((u) => u.is_system_admin).length;

  return (
    <div className="mx-auto w-full max-w-[1400px]">
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">
          <span className="font-medium text-ink">{total}</span> users ·{" "}
          {adminCount} system {adminCount === 1 ? "administrator" : "administrators"}
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

      {error && <p className="caption mb-4 text-error">{error}</p>}

      <div className="mb-5 flex flex-wrap items-center gap-3">
        <div className="relative min-w-[220px] flex-1">
          <IconSearch className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
          <input
            className="input h-10 pl-10 text-[14px]"
            placeholder="Search name or email…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
      </div>

      <div className="card overflow-x-auto">
        <table className="w-full min-w-[900px] border-collapse text-left">
          <thead>
            <tr className="caption-uppercase border-b border-hairline text-muted-soft">
              <th className="px-5 py-3 font-medium">User</th>
              <th className="w-[380px] px-5 py-3 font-medium">Workspaces</th>
              <th className="w-28 px-5 py-3 font-medium">Status</th>
              <th className="w-32 px-5 py-3 font-medium">Created</th>
              <th className="w-48 px-5 py-3 font-medium text-right">{t("users.actions")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {users.map((u) => (
              <tr key={u.id} className="transition-colors hover:bg-surface-strong/20">
                <td className="px-5 py-3.5 align-middle">
                  <div className="flex min-w-0 items-center gap-3">
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[12px] font-medium text-ink">
                      {initials(u.username)}
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 text-[14px] font-medium text-ink">
                        <span className="truncate">{u.username}</span>
                        {u.is_system_admin ? (
                          <span className="badge-pill shrink-0 bg-surface-dark text-on-dark">
                            {t("users.superadmin")}
                          </span>
                        ) : (
                          <span className="badge-pill shrink-0">User</span>
                        )}
                        {u.id === meId && (
                          <span className="caption shrink-0 text-muted-soft">(you)</span>
                        )}
                      </div>
                      <div className="caption truncate text-muted">{u.email}</div>
                    </div>
                  </div>
                </td>
                <td className="px-5 py-3.5 align-middle">
                  {u.memberships && u.memberships.length > 0 ? (
                    <div className="flex flex-col gap-1.5">
                      {u.memberships.map((m) => (
                        <div
                          key={m.tenant_id}
                          className="flex items-center justify-between gap-2"
                        >
                          <span
                            className="caption max-w-[220px] truncate text-body"
                            title={m.tenant_name}
                          >
                            {m.tenant_name}
                          </span>
                          <select
                            className="input h-6 w-[92px] shrink-0 px-1.5 py-0 text-[12px]"
                            value={m.role}
                            disabled={busy}
                            onChange={(e) =>
                              changeWorkspaceRole(u, m.tenant_id, e.target.value as TenantRole)
                            }
                          >
                            {WORKSPACE_ROLES.map((r) => (
                              <option key={r} value={r}>
                                {r}
                              </option>
                            ))}
                          </select>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <span className="caption text-muted-soft">—</span>
                  )}
                </td>
                <td className="px-5 py-3.5 align-middle whitespace-nowrap">
                  <span className="caption text-muted">
                    {u.is_active ? "Active" : "Disabled"}
                  </span>
                </td>
                <td className="px-5 py-3.5 align-middle whitespace-nowrap">
                  <span className="caption text-muted">
                    {u.created_at ? new Date(u.created_at).toLocaleDateString() : "—"}
                  </span>
                </td>
                <td className="px-5 py-3.5 align-middle text-right whitespace-nowrap">
                  <div className="flex flex-col items-end gap-1.5">
                    {!(u.is_system_admin && u.id === meId) && (
                      <button
                        className={`btn btn-outline btn-sm ${u.is_system_admin ? "text-error" : ""}`}
                        onClick={() => (u.is_system_admin ? setRevoking(u) : setPromoting(u))}
                        disabled={busy}
                        title={
                          u.is_system_admin
                            ? "Revoke global system-admin rights"
                            : "Grant global system-admin rights"
                        }
                      >
                        {u.is_system_admin ? "Revoke superadmin" : "Make superadmin"}
                      </button>
                    )}
                    {!u.is_system_admin && (
                      <button
                        className="btn btn-outline btn-sm"
                        onClick={() => {
                          setNewPassword("");
                          setResetting(u);
                        }}
                        disabled={u.id === meId}
                        title={u.id === meId ? "Use Settings → Security for your own password" : ""}
                      >
                        Reset password
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
            {users.length === 0 && (
              <tr>
                <td colSpan={5} className="px-5 py-12 text-center text-[14px] text-muted">
                  No users match this filter.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* add admin / create user */}
      <Modal
        open={modalOpen}
        title={t("users.addUser")}
        onClose={() => setModalOpen(false)}
        width="w-[500px]"
      >
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-1 rounded-full bg-surface-strong p-1">
            {(
              [
                { id: "create", label: "Create account" },
                { id: "promote", label: "Promote existing" },
              ] as const
            ).map((m) => (
              <button
                key={m.id}
                onClick={() => setAddMode(m.id)}
                className={`rounded-full px-3.5 py-1.5 text-[13px] font-medium transition-colors ${
                  addMode === m.id
                    ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.06)]"
                    : "text-muted hover:text-ink"
                }`}
              >
                {m.label}
              </button>
            ))}
          </div>

          {addMode === "create" && (
            <label className="block">
              <span className="caption mb-1.5 block text-muted">{t("users.fullName")}</span>
              <input
                className="input"
                value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })}
                placeholder="2–50 characters"
              />
            </label>
          )}
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("userSettings.email")}</span>
            <input
              className="input"
              type="email"
              value={form.email}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
          </label>
          {addMode === "create" && (
            <label className="block">
              <span className="caption mb-1.5 block text-muted">Password (optional)</span>
              <input
                className="input"
                type="password"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                placeholder="Leave empty to generate"
              />
            </label>
          )}

          {generatedPassword && (
            <div className="rounded-[12px] border border-hairline bg-canvas-soft p-4">
              <div className="caption-uppercase mb-1.5 text-muted">
                Generated password — shown once
              </div>
              <code className="text-[14px] font-medium text-ink">{generatedPassword}</code>
            </div>
          )}

          <div className="mt-1 flex justify-end gap-3">
            <button className="btn btn-outline" onClick={() => setModalOpen(false)}>
              {generatedPassword ? "Done" : t("common.cancel")}
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
        title={`Reset password — ${resetting?.username ?? ""}`}
        onClose={() => setResetting(null)}
        width="w-[420px]"
      >
        <div className="flex flex-col gap-4">
          <p className="body-sm text-body">
            Set a new password for <span className="font-medium text-ink">{resetting?.email}</span>.
            All active sessions for this account will be revoked.
          </p>
          <input
            className="input"
            type="password"
            placeholder="New password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
          />
          <div className="flex justify-end gap-3">
            <button className="btn btn-outline" onClick={() => setResetting(null)}>
              {t("common.cancel")}
            </button>
            <button className="btn btn-primary" onClick={submitReset} disabled={busy || !newPassword}>
              Reset
            </button>
          </div>
        </div>
      </Modal>

      {/* revoke confirm */}
      <Modal
        open={revoking !== null}
        title="Revoke system administrator"
        onClose={() => setRevoking(null)}
        width="w-[420px]"
      >
        <p className="body-sm mb-6 text-body">
          Remove system-admin rights from{" "}
          <span className="font-medium text-ink">{revoking?.username}</span> ({revoking?.email})?
          The account itself is kept.
        </p>
        <div className="flex justify-end gap-3">
          <button className="btn btn-outline" onClick={() => setRevoking(null)}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary" onClick={submitRevoke} disabled={busy}>
            Revoke
          </button>
        </div>
      </Modal>

      {/* promote confirm */}
      <Modal
        open={promoting !== null}
        title="Promote to system administrator"
        onClose={() => setPromoting(null)}
        width="w-[420px]"
      >
        <p className="body-sm mb-6 text-body">
          Grant system-admin rights to{" "}
          <span className="font-medium text-ink">{promoting?.username}</span> ({promoting?.email})?
          They will be able to manage all tenants and platform settings.
        </p>
        <div className="flex justify-end gap-3">
          <button className="btn btn-outline" onClick={() => setPromoting(null)}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary" onClick={submitPromote} disabled={busy}>
            Promote
          </button>
        </div>
      </Modal>
    </div>
  );
}
