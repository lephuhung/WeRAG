"use client";

/* SuperAdmin unit (workspace) management: only SuperAdmins create units,
 * and each unit is created together with its Tenant Admin — an existing
 * account or a new one. SuperAdmins can also add or remove members of any
 * unit without joining it themselves. */

import { useCallback, useEffect, useRef, useState } from "react";
import { Modal } from "@/components/modal";
import { IconEdit, IconPlus, IconSearch } from "@/components/icons";
import { useToast } from "@/components/toast";
import { useConfirm } from "@/components/confirm-dialog";
import { FieldLabel } from "@/components/settings/field";
import { Loading } from "@/components/settings/loading";
import { Notice } from "@/components/settings/notice";
import { Pill, PillGroup } from "@/components/settings/pill";
import { ROW_HOVER } from "@/components/settings/data-table";
import { SystemSection } from "@/components/settings/system-section";
import { useT } from "@/lib/i18n";
import { getCurrentUser } from "@/lib/api/auth";
import {
  addSystemTenantMember,
  listSystemTenantMembers,
  provisionTenant,
  removeSystemTenantMember,
  updateSystemTenant,
  type SystemTenantMember,
  type TenantMemberTarget,
} from "@/lib/api/system";
import { listTenantDirectory, type TenantDirectoryEntry } from "@/lib/api/tenants";
import { ApiError } from "@/lib/api-client";
import {
  TENANT_NAME_MAX_LENGTH,
  normalizeTenantName,
  tenantNameErrorKey,
  validateTenantName,
} from "@/lib/tenant-name";

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

function PersonFields({
  value,
  onChange,
  idPrefix,
}: {
  value: PersonForm;
  onChange: (p: PersonForm) => void;
  /* Prefix for the input ids — the form appears twice on the page. */
  idPrefix: string;
}) {
  const { t } = useT();
  return (
    <div className="space-y-3">
      <PillGroup ariaLabel={t("tnp.adminSection")}>
        {(["existing", "new"] as const).map((m) => (
          <Pill
            key={m}
            active={value.mode === m}
            onClick={() => onChange({ ...value, mode: m })}
          >
            {m === "existing" ? t("tnp.adminExisting") : t("tnp.adminNew")}
          </Pill>
        ))}
      </PillGroup>
      {value.mode === "new" && (
        <div>
          <FieldLabel htmlFor={`${idPrefix}-username`}>{t("tnp.username")}</FieldLabel>
          <input
            id={`${idPrefix}-username`}
            className="input"
            value={value.username}
            onChange={(e) => onChange({ ...value, username: e.target.value })}
          />
        </div>
      )}
      <div>
        <FieldLabel htmlFor={`${idPrefix}-email`} required>
          {t("tnp.email")}
        </FieldLabel>
        <input
          id={`${idPrefix}-email`}
          type="email"
          className="input"
          required
          aria-required="true"
          value={value.email}
          onChange={(e) => onChange({ ...value, email: e.target.value })}
        />
      </div>
      {value.mode === "new" && (
        <div>
          <FieldLabel htmlFor={`${idPrefix}-password`}>{t("tnp.password")}</FieldLabel>
          <input
            id={`${idPrefix}-password`}
            type="password"
            className="input"
            autoComplete="new-password"
            value={value.password}
            onChange={(e) => onChange({ ...value, password: e.target.value })}
          />
        </div>
      )}
    </div>
  );
}

export default function AdminTenants() {
  const { t } = useT();
  const toast = useToast();
  const confirm = useConfirm();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [units, setUnits] = useState<TenantDirectoryEntry[]>([]);
  const [unitsLoading, setUnitsLoading] = useState(false);
  const [q, setQ] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const [selected, setSelected] = useState<TenantDirectoryEntry | null>(null);
  const [members, setMembers] = useState<SystemTenantMember[]>([]);
  const [membersLoading, setMembersLoading] = useState(false);
  const memberRequest = useRef(0);

  const [createOpen, setCreateOpen] = useState(false);
  const [unitName, setUnitName] = useState("");
  const [unitDesc, setUnitDesc] = useState("");
  const [admin, setAdmin] = useState<PersonForm>(emptyPerson);

  const [addOpen, setAddOpen] = useState(false);
  const [person, setPerson] = useState<PersonForm>(emptyPerson);
  const [role, setRole] = useState<"admin" | "member">("member");

  /* Inline rename of the selected unit: null = not editing. */
  const [renameTo, setRenameTo] = useState<string | null>(null);
  /* Name problem (missing, too long, already used) shown next to the name
   * input of the create form or the rename form. */
  const [nameError, setNameError] = useState("");

  const [oneTime, setOneTime] = useState<{ user: string; password: string } | null>(null);

  const loadUnits = useCallback(async () => {
    setUnitsLoading(true);
    try {
      setUnits(await listTenantDirectory());
      setError("");
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) setAllowed(false);
      else setError(e instanceof Error ? e.message : t("tnp.loadFailed"));
    } finally {
      setUnitsLoading(false);
    }
  }, []);

  const loadMembers = useCallback(async (unit: TenantDirectoryEntry) => {
    const request = ++memberRequest.current;
    setMembers([]);
    setMembersLoading(true);
    try {
      const result = await listSystemTenantMembers(unit.id);
      if (request === memberRequest.current) setMembers(result);
    } catch (e) {
      if (request !== memberRequest.current) return;
      setMembers([]);
      if (e instanceof ApiError && e.status === 403) setAllowed(false);
      else setError(e instanceof Error ? e.message : t("tnp.loadFailed"));
    } finally {
      if (request === memberRequest.current) setMembersLoading(false);
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
    setRenameTo(null);
    setNameError("");
    if (selected) void loadMembers(selected);
    return () => { memberRequest.current += 1; };
  }, [selected, loadMembers]);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      return true;
    } catch (e) {
      const nameKey = tenantNameErrorKey(e);
      if (nameKey) setNameError(t(nameKey));
      else setError(e instanceof Error ? e.message : t("tnp.opFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const submitCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    const target = toTarget(admin);
    const nameKey = validateTenantName(unitName);
    if (nameKey) {
      setNameError(t(nameKey));
      return;
    }
    if (!target) return;
    setNameError("");
    await run(async () => {
      const res = await provisionTenant({
        name: normalizeTenantName(unitName),
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
      toast.success(t("tnp.created"));
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
      toast.success(t("common.saved"));
    });
  };

  const submitRename = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selected || renameTo === null) return;
    const nameKey = validateTenantName(renameTo);
    if (nameKey) {
      setNameError(t(nameKey));
      return;
    }
    const name = normalizeTenantName(renameTo);
    setNameError("");
    if (name === selected.name) {
      setRenameTo(null);
      return;
    }
    await run(async () => {
      const res = await updateSystemTenant(selected.id, { name });
      const renamed = { id: selected.id, name: res.data?.name ?? name };
      setUnits((list) => list.map((u) => (u.id === renamed.id ? renamed : u)));
      setSelected(renamed);
      setRenameTo(null);
      toast.success(t("common.saved"));
    });
  };

  const remove = async (m: SystemTenantMember) => {
    if (!selected) return;
    const ok = await confirm({
      title: t("mem.removeTitle"),
      message: t("mem.removeBody", { name: m.username || m.email }),
      confirmLabel: t("mem.removeCta"),
      danger: true,
    });
    if (!ok) return;
    const name = m.username || m.email;
    const removed = await run(async () => {
      await removeSystemTenantMember(selected.id, m.user_id);
      await loadMembers(selected);
    });
    if (removed) toast.success(t("mem.removed", { name }));
  };

  const filtered = units.filter((u) => !q || u.name.toLowerCase().includes(q.toLowerCase()));

  return (
    <SystemSection allowed={allowed} title={t("tnp.title")} deniedMessage={t("tnp.denied")}>
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <span className="caption text-muted">{t("tnp.summary", { n: units.length })}</span>
        <button className="btn btn-primary btn-sm ml-auto" onClick={() => {
            setNameError("");
            setCreateOpen(true);
          }}>
          <IconPlus className="h-3.5 w-3.5" /> {t("tnp.new")}
        </button>
      </div>

      {error && (
        <Notice
          tone="error"
          className="mb-4"
          action={
            <button className="btn btn-outline btn-sm" onClick={() => void loadUnits()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}
      {oneTime && (
        <Notice
          tone="warning"
          className="mb-4"
          action={
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() => setOneTime(null)}
              aria-label={t("common.close")}
              title={t("common.close")}
            >
              ✕
            </button>
          }
        >
          <div className="flex flex-col gap-2">
            <p>{t("tnp.generatedPassword", { user: oneTime.user })}</p>
            <input
              readOnly
              id="tnp-one-time-password"
              aria-label={t("tnp.password")}
              className="input font-mono text-xs"
              value={oneTime.password}
              onFocus={(e) => e.target.select()}
            />
          </div>
        </Notice>
      )}

      <div className="relative mb-5 max-w-md">
        <IconSearch className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
        <input
          id="tnp-search"
          className="input h-9 pl-9"
          placeholder={t("tnp.searchPh")}
          aria-label={t("tnp.searchPh")}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>
      {unitsLoading ? (
        <Loading className="py-12" />
      ) : filtered.length === 0 ? (
        <p className="card px-5 py-12 text-center text-[14px] text-muted">{t("common.noResults")}</p>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,260px),1fr))] gap-4">
          {filtered.map((u) => (
            <button
              key={u.id}
              type="button"
              onClick={() => { setError(""); setSelected(u); }}
              aria-haspopup="dialog"
              className="card flex min-w-0 flex-col items-start gap-4 p-5 text-left transition-colors hover:border-ink/30 hover:bg-surface-strong/30 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
            >
              <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-surface-strong text-lg font-semibold text-ink" aria-hidden="true">{u.name.slice(0, 1).toUpperCase()}</span>
              <span className="w-full break-words text-[15px] font-semibold leading-snug text-ink">{u.name}</span>
              <span className="caption font-mono text-muted-soft">#{u.id}</span>
            </button>
          ))}
        </div>
      )}
      <Modal open={!!selected && !addOpen && !oneTime} title={t("tnp.title")} onClose={() => setSelected(null)} width="w-[880px]">
          {!selected ? (
            <p className="caption py-10 text-center text-muted">{t("tnp.select")}</p>
          ) : (
            <>
              <div className="mb-4 flex flex-wrap items-center gap-3">
                {renameTo === null ? (
                  <>
                    <h3 className="title-sm min-w-0 break-words">{selected.name}</h3>
                    <button
                      type="button"
                      onClick={() => {
                        setNameError("");
                        setRenameTo(selected.name);
                      }}
                      title={t("tnp.rename")}
                      aria-label={t("tnp.rename")}
                      className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-muted-soft transition-colors hover:bg-surface-strong hover:text-ink"
                    >
                      <IconEdit className="h-3.5 w-3.5" />
                    </button>
                  </>
                ) : (
                  <form className="flex min-w-0 flex-1 items-center gap-2" onSubmit={submitRename}>
                    <input
                      autoFocus
                      className="input h-8 min-w-0 flex-1"
                      aria-label={t("tnp.name")}
                      aria-invalid={nameError ? true : undefined}
                      required
                      maxLength={TENANT_NAME_MAX_LENGTH}
                      placeholder={t("tnp.namePh")}
                      value={renameTo}
                      onChange={(e) => {
                        setRenameTo(e.target.value);
                        setNameError("");
                      }}
                      onKeyDown={(e) => {
                        if (e.key === "Escape") {
                          e.stopPropagation();
                          setRenameTo(null);
                        }
                      }}
                    />
                    <button type="submit" className="btn btn-primary btn-sm" disabled={busy || !renameTo.trim()}>
                      {t("common.save")}
                    </button>
                    <button
                      type="button"
                      className="btn btn-outline btn-sm"
                      onClick={() => {
                        setRenameTo(null);
                        setNameError("");
                      }}
                    >
                      {t("common.cancel")}
                    </button>
                  </form>
                )}
                <button className="btn btn-outline btn-sm ml-auto shrink-0" onClick={() => setAddOpen(true)}>
                  <IconPlus className="h-3.5 w-3.5" /> {t("tnp.addMember")}
                </button>
              </div>
              <p className="caption mb-4 font-mono text-muted-soft">#{selected.id}</p>
              {error && (
                <Notice
                  tone="error"
                  className="mb-3"
                  action={
                    <button className="btn btn-outline btn-sm" onClick={() => void loadMembers(selected)}>
                      {t("common.retry")}
                    </button>
                  }
                >
                  {error}
                </Notice>
              )}
              <h4 className="mb-3 border-t border-hairline pt-4 text-[14px] font-medium text-ink">{t("tnp.members")}{!membersLoading && ` (${members.length})`}</h4>
              {renameTo !== null && nameError && (
                <p role="alert" className="caption -mt-2 mb-3 text-error">
                  {nameError}
                </p>
              )}
              {membersLoading ? (
                <Loading />
              ) : members.length === 0 ? (
                <p className="caption text-muted-soft">{t("tnp.noMembers")}</p>
              ) : (
                <div className="divide-y divide-hairline">
                  {members.map((m) => (
                    <div key={m.user_id} className={`flex flex-wrap items-center gap-3 py-2.5 text-[14px] ${ROW_HOVER}`}>
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium text-ink">{m.username || m.user_id}</div>
                        <div className="caption truncate text-muted">{m.email}</div>
                      </div>
                      <span className="badge-pill text-[11px]">
                        {m.role === "admin" || m.role === "owner" ? t("usrp.roleAdmin") : t("usrp.roleMember")}
                      </span>
                      <button
                        type="button"
                        className="btn btn-danger btn-sm py-1 text-xs"
                        disabled={busy}
                        onClick={() => void remove(m)}
                      >
                        {t("tnp.remove")}
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </>
          )}
      </Modal>

      <Modal
        open={createOpen}
        title={t("tnp.new")}
        onClose={() => {
          setCreateOpen(false);
          setNameError("");
        }}
      >
        <form className="space-y-4" onSubmit={submitCreate}>
          <div>
            <FieldLabel htmlFor="tnp-create-name" required>
              {t("tnp.name")}
            </FieldLabel>
            <input
              id="tnp-create-name"
              className="input"
              required
              aria-required="true"
              maxLength={TENANT_NAME_MAX_LENGTH}
              placeholder={t("tnp.namePh")}
              aria-invalid={nameError ? true : undefined}
              value={unitName}
              onChange={(e) => {
                setUnitName(e.target.value);
                setNameError("");
              }}
            />
            {nameError && <span className="caption mt-1 block text-error">{nameError}</span>}
          </div>
          <div>
            <FieldLabel htmlFor="tnp-create-desc">{t("tnp.desc")}</FieldLabel>
            <input
              id="tnp-create-desc"
              className="input"
              value={unitDesc}
              onChange={(e) => setUnitDesc(e.target.value)}
            />
          </div>
          <div className="border-t border-hairline pt-4">
            <span className="caption mb-3 block font-medium text-ink">{t("tnp.adminSection")}</span>
            <PersonFields idPrefix="tnp-create-admin" value={admin} onChange={setAdmin} />
          </div>
          {error && <Notice tone="error">{error}</Notice>}
          <div className="flex justify-end">
            <button type="submit" className="btn btn-primary" disabled={busy || !unitName.trim() || !toTarget(admin)}>
              {t("tnp.create")}
            </button>
          </div>
        </form>
      </Modal>

      <Modal open={addOpen} title={t("tnp.addMember")} onClose={() => setAddOpen(false)}>
        <form className="space-y-4" onSubmit={submitAdd}>
          <PersonFields idPrefix="tnp-add-member" value={person} onChange={setPerson} />
          <div>
            <FieldLabel htmlFor="tnp-add-role">{t("tnp.role")}</FieldLabel>
            <select
              id="tnp-add-role"
              className="input"
              value={role}
              onChange={(e) => setRole(e.target.value as "admin" | "member")}
            >
              <option value="member">{t("usrp.roleMember")}</option>
              <option value="admin">{t("usrp.roleAdmin")}</option>
            </select>
          </div>
          {error && <Notice tone="error">{error}</Notice>}
          <div className="flex justify-end">
            <button type="submit" className="btn btn-primary" disabled={busy || !toTarget(person)}>
              {t("tnp.addMember")}
            </button>
          </div>
        </form>
      </Modal>
    </SystemSection>
  );
}
