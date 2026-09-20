"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconSearch } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { getCurrentUser } from "@/lib/api/auth";
import {
  createSystemUser,
  listSystemAdmins,
  promoteUserToSystemAdmin,
  resetUserPassword,
  revokeSystemAdmin,
  type SystemAdminUser,
} from "@/lib/api/system";
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
  const [users, setUsers] = useState<SystemAdminUser[]>([]);
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

  const load = useCallback(async () => {
    try {
      const res = await listSystemAdmins({ limit: 200 });
      setUsers(res.admins ?? []);
      setError("");
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) setAllowed(false);
      else setError(e instanceof Error ? e.message : "Failed to load administrators");
    }
  }, []);

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
        setAllowed(true);
        await load();
      } catch {
        if (alive) setAllowed(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [load]);

  const rows = users.filter(
    (u) => q === "" || `${u.username} ${u.email}`.toLowerCase().includes(q.toLowerCase()),
  );

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      await load();
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

  return (
    <div className="mx-auto w-full max-w-[1100px]">
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">
          <span className="font-medium text-ink">{users.length}</span> system administrators
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

      <div className="card overflow-hidden">
        <div className="caption-uppercase flex items-center gap-4 border-b border-hairline px-5 py-3 text-muted-soft">
          <span className="flex-1">User</span>
          <span className="w-28">Status</span>
          <span className="w-40">Created</span>
          <span className="w-56 text-right">{t("users.actions")}</span>
        </div>
        {rows.map((u, i) => (
          <div
            key={u.id}
            className={`flex items-center gap-4 px-5 py-3.5 ${i > 0 ? "border-t border-hairline" : ""}`}
          >
            <div className="flex min-w-0 flex-1 items-center gap-3">
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[12px] font-medium text-ink">
                {initials(u.username)}
              </div>
              <div className="min-w-0">
                <div className="truncate text-[14px] font-medium text-ink">
                  {u.username}
                  <span className="badge-pill ml-2 bg-surface-dark text-on-dark">
                    {t("users.superadmin")}
                  </span>
                </div>
                <div className="caption truncate text-muted">{u.email}</div>
              </div>
            </div>
            <span className="caption w-28 text-muted">
              {u.is_active ? "Active" : "Disabled"}
            </span>
            <span className="caption w-40 text-muted">
              {u.created_at ? new Date(u.created_at).toLocaleDateString() : "—"}
            </span>
            <span className="flex w-56 justify-end gap-1.5">
              <button
                className="btn btn-outline btn-sm"
                onClick={() => {
                  setNewPassword("");
                  setResetting(u);
                }}
              >
                Reset password
              </button>
              <button
                className="btn btn-tertiary text-[13px] text-error!"
                onClick={() => setRevoking(u)}
                disabled={busy}
              >
                Revoke
              </button>
            </span>
          </div>
        ))}
        {rows.length === 0 && (
          <div className="px-5 py-12 text-center text-[14px] text-muted">
            No system administrators match this filter.
          </div>
        )}
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
    </div>
  );
}
