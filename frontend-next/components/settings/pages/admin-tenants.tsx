"use client";

/* SuperAdmin unit (workspace) management: only SuperAdmins create units,
 * and each unit is created together with its Tenant Admin — an existing
 * account or a new one. SuperAdmins can also add or remove members of any
 * unit without joining it themselves. */

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconSearch } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { getCurrentUser } from "@/lib/api/auth";
import {
  addSystemTenantMember,
  listSystemTenantMembers,
  provisionTenant,
  removeSystemTenantMember,
  type SystemTenantMember,
  type TenantMemberTarget,
} from "@/lib/api/system";
import { listTenantDirectory, type TenantDirectoryEntry } from "@/lib/api/tenants";

type PersonMode = "existing" | "new";

interface PersonForm {
  mode: PersonMode;
  email: string;
  username: string;
  password: string;
}

const emptyPerson: PersonForm = { mode: "existing", email: "", username: "", password: "" };

function toTarget(p: PersonForm): TenantMemberTarget | null {
  if (p.mode === "existing") return p.email.trim() ? { email: p.email.trim() } : null;
  if (!p.username.trim() || !p.email.trim()) return null;
  return {
    new_user: {
      username: p.username.trim(),
      email: p.email.trim(),
      ...(p.password ? { password: p.password } : {}),
    },
  };
}

function PersonFields({ value, onChange }: { value: PersonForm; onChange: (p: PersonForm) => void }) {
  const { t } = useT();
  return (
    <div className="space-y-3">
      <div className="inline-flex items-center gap-0.5 rounded-full border border-hairline bg-surface-strong/60 p-0.5">
        {(["existing", "new"] as const).map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => onChange({ ...value, mode: m })}
            className={`rounded-full px-3 py-1 text-[12px] font-medium transition-colors ${
              value.mode === m ? "bg-surface-card text-ink shadow-[0_1px_3px_rgba(0,0,0,0.08)]" : "text-muted hover:text-ink"
            }`}
          >
            {m === "existing" ? t("tnp.adminExisting") : t("tnp.adminNew")}
          </button>
        ))}
      </div>
      {value.mode === "new" && (
        <label className="block">
          <span className="caption mb-1 block text-muted">{t("tnp.username")}</span>
          <input
            className="input"
            value={value.username}
            onChange={(e) => onChange({ ...value, username: e.target.value })}
          />
        </label>
      )}
      <label className="block">
        <span className="caption mb-1 block text-muted">{t("tnp.email")}</span>
        <input
          type="email"
          className="input"
          value={value.email}
          onChange={(e) => onChange({ ...value, email: e.target.value })}
        />
      </label>
      {value.mode === "new" && (
        <label className="block">
          <span className="caption mb-1 block text-muted">{t("tnp.password")}</span>
          <input
            type="password"
            className="input"
            autoComplete="new-password"
            value={value.password}
            onChange={(e) => onChange({ ...value, password: e.target.value })}
          />
        </label>
      )}
    </div>
  );
}

export default function AdminTenants() {
  const { t } = useT();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [units, setUnits] = useState<TenantDirectoryEntry[]>([]);
  const [q, setQ] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const [selected, setSelected] = useState<TenantDirectoryEntry | null>(null);
  const [members, setMembers] = useState<SystemTenantMember[]>([]);

  const [createOpen, setCreateOpen] = useState(false);
  const [unitName, setUnitName] = useState("");
  const [unitDesc, setUnitDesc] = useState("");
  const [admin, setAdmin] = useState<PersonForm>(emptyPerson);

  const [addOpen, setAddOpen] = useState(false);
  const [person, setPerson] = useState<PersonForm>(emptyPerson);
  const [role, setRole] = useState<"admin" | "member">("member");

  const [oneTime, setOneTime] = useState<{ user: string; password: string } | null>(null);

  const loadUnits = useCallback(async () => {
    try {
      setUnits(await listTenantDirectory());
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("tnp.loadFailed"));
    }
  }, []);

  const loadMembers = useCallback(async (unit: TenantDirectoryEntry) => {
    try {
      setMembers(await listSystemTenantMembers(unit.id));
    } catch (e) {
      setMembers([]);
      setError(e instanceof Error ? e.message : t("tnp.loadFailed"));
    }
  }, []);

  useEffect(() => {
    let alive = true;
    getCurrentUser()
      .then((me) => {
        if (!alive) return;
        const ok = me.data?.user?.is_system_admin === true;
        setAllowed(ok);
        if (ok) void loadUnits();
      })
      .catch(() => alive && setAllowed(false));
    return () => {
      alive = false;
    };
  }, [loadUnits]);

  useEffect(() => {
    if (selected) void loadMembers(selected);
  }, [selected, loadMembers]);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : t("tnp.opFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const submitCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    const target = toTarget(admin);
    if (!unitName.trim() || !target) return;
    await run(async () => {
      const res = await provisionTenant({
        name: unitName.trim(),
        description: unitDesc.trim() || undefined,
        admin: target,
      });
      if (res.generated_password) {
        setOneTime({ user: res.admin?.email ?? "", password: res.generated_password });
      }
      setCreateOpen(false);
      setUnitName("");
      setUnitDesc("");
      setAdmin(emptyPerson);
      await loadUnits();
      if (res.tenant) setSelected({ id: res.tenant.id, name: res.tenant.name });
    });
  };

  const submitAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    const target = toTarget(person);
    if (!selected || !target) return;
    await run(async () => {
      const res = await addSystemTenantMember(selected.id, { ...target, role });
      if (res.generated_password) {
        setOneTime({ user: person.email.trim(), password: res.generated_password });
      }
      setAddOpen(false);
      setPerson(emptyPerson);
      setRole("member");
      await loadMembers(selected);
    });
  };

  const remove = (m: SystemTenantMember) => {
    if (!selected) return;
    void run(async () => {
      await removeSystemTenantMember(selected.id, m.user_id);
      await loadMembers(selected);
    });
  };

  if (allowed === null) {
    return <div className="mx-auto w-full max-w-[1100px] px-5 py-12 text-muted">…</div>;
  }
  if (!allowed) {
    return (
      <div className="mx-auto w-full max-w-[1100px] px-5 py-16 text-center">
        <h2 className="title-md mb-2">{t("tnp.title")}</h2>
        <p className="body-sm text-muted">{t("tnp.denied")}</p>
      </div>
    );
  }

  const filtered = units.filter((u) => !q || u.name.toLowerCase().includes(q.toLowerCase()));

  return (
    <div className="mx-auto w-full max-w-[1400px]">
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">{t("tnp.summary", { n: units.length })}</span>
        <button className="btn btn-primary btn-sm ml-auto" onClick={() => setCreateOpen(true)}>
          <IconPlus className="h-3.5 w-3.5" /> {t("tnp.new")}
        </button>
      </div>

      {error && <p className="caption mb-4 text-error">{error}</p>}
      {oneTime && (
        <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-300">
          <p>{t("tnp.generatedPassword", { user: oneTime.user })}</p>
          <div className="mt-2 flex items-center gap-2">
            <input readOnly className="input font-mono text-xs" value={oneTime.password} onFocus={(e) => e.target.select()} />
            <button type="button" className="btn btn-outline btn-sm" onClick={() => setOneTime(null)}>
              ✕
            </button>
          </div>
        </div>
      )}

      <div className="grid gap-5 md:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
        <div className="card p-4">
          <div className="relative mb-3">
            <IconSearch className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
            <input className="input h-9 pl-9" placeholder={t("tnp.searchPh")} value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          <div className="max-h-[520px] divide-y divide-hairline overflow-y-auto">
            {filtered.map((u) => (
              <button
                key={u.id}
                type="button"
                onClick={() => setSelected(u)}
                className={`flex w-full items-center justify-between px-2 py-2.5 text-left text-[14px] transition-colors hover:bg-surface-strong/40 ${
                  selected?.id === u.id ? "bg-surface-strong/60 font-medium text-ink" : "text-body"
                }`}
              >
                <span className="truncate">{u.name}</span>
                <span className="caption text-muted-soft">#{u.id}</span>
              </button>
            ))}
          </div>
        </div>

        <div className="card p-4">
          {!selected ? (
            <p className="caption py-10 text-center text-muted">{t("tnp.select")}</p>
          ) : (
            <>
              <div className="mb-4 flex items-center gap-3">
                <h3 className="title-sm truncate">{selected.name}</h3>
                <span className="caption text-muted">{t("tnp.members")}</span>
                <button className="btn btn-outline btn-sm ml-auto" onClick={() => setAddOpen(true)}>
                  <IconPlus className="h-3.5 w-3.5" /> {t("tnp.addMember")}
                </button>
              </div>
              {members.length === 0 ? (
                <p className="caption text-muted-soft">{t("tnp.noMembers")}</p>
              ) : (
                <div className="divide-y divide-hairline">
                  {members.map((m) => (
                    <div key={m.user_id} className="flex items-center gap-3 py-2.5 text-[14px]">
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium text-ink">{m.username || m.user_id}</div>
                        <div className="caption truncate text-muted">{m.email}</div>
                      </div>
                      <span className="badge-pill text-[11px]">
                        {m.role === "admin" || m.role === "owner" ? t("usrp.roleAdmin") : t("usrp.roleMember")}
                      </span>
                      <button
                        type="button"
                        className="btn btn-outline btn-sm py-1 text-xs text-rose-600"
                        disabled={busy}
                        onClick={() => remove(m)}
                      >
                        {t("tnp.remove")}
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      </div>

      <Modal open={createOpen} title={t("tnp.new")} onClose={() => setCreateOpen(false)}>
        <form className="space-y-4" onSubmit={submitCreate}>
          <label className="block">
            <span className="caption mb-1 block text-muted">{t("tnp.name")}</span>
            <input className="input" required value={unitName} onChange={(e) => setUnitName(e.target.value)} />
          </label>
          <label className="block">
            <span className="caption mb-1 block text-muted">{t("tnp.desc")}</span>
            <input className="input" value={unitDesc} onChange={(e) => setUnitDesc(e.target.value)} />
          </label>
          <div className="border-t border-hairline pt-4">
            <span className="caption mb-3 block font-medium text-ink">{t("tnp.adminSection")}</span>
            <PersonFields value={admin} onChange={setAdmin} />
          </div>
          <div className="flex justify-end">
            <button type="submit" className="btn btn-primary" disabled={busy || !unitName.trim() || !toTarget(admin)}>
              {t("tnp.create")}
            </button>
          </div>
        </form>
      </Modal>

      <Modal open={addOpen} title={t("tnp.addMember")} onClose={() => setAddOpen(false)}>
        <form className="space-y-4" onSubmit={submitAdd}>
          <PersonFields value={person} onChange={setPerson} />
          <label className="block">
            <span className="caption mb-1 block text-muted">{t("tnp.role")}</span>
            <select className="input" value={role} onChange={(e) => setRole(e.target.value as "admin" | "member")}>
              <option value="member">{t("usrp.roleMember")}</option>
              <option value="admin">{t("usrp.roleAdmin")}</option>
            </select>
          </label>
          <div className="flex justify-end">
            <button type="submit" className="btn btn-primary" disabled={busy || !toTarget(person)}>
              {t("tnp.addMember")}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
