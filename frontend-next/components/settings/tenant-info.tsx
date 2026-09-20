/* Ported from frontend/src/views/settings/TenantInfo.vue.
 * Info rows (id/name/description/status/created/storage) + leave/delete
 * danger zones with the same gating: name/description editing is owner-only;
 * leave hidden for the last owner; delete owner-only with name-typed
 * confirmation.
 */
"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { getCurrentUser } from "@/lib/api/auth";
import { deleteTenant, fetchAllTenantMembers, leaveTenant, updateTenant, type TenantInfo as TenantRecord } from "@/lib/api/tenants";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { useRouter } from "next/navigation";

const ownerCount = (members: { role: string }[]) => members.filter((m) => m.role === "owner").length;

function formatBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

export function TenantInfo() {
  const { t, locale } = useT();
  const router = useRouter();
  const auth = useAuth();

  const [info, setInfo] = useState<TenantRecord | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [ownerSnapshot, setOwnerSnapshot] = useState<{ count: number; ready: boolean }>({ count: 0, ready: false });

  const currentRole = auth.memberships.find(
    (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
  )?.role;

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await getCurrentUser();
      if (res.success && res.data?.tenant) {
        setInfo(res.data.tenant as unknown as TenantRecord);
      } else {
        setError(res.message || "Failed to load workspace");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Network error");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load, auth.selectedTenantId]);

  /* Leave gate: owners need another owner to keep the workspace alive. */
  useEffect(() => {
    const tid = info ? Number(info.id) : 0;
    const activeTid = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
    if (!tid || tid !== activeTid || currentRole !== "owner") {
      setOwnerSnapshot({ count: 0, ready: true });
      return;
    }
    let alive = true;
    fetchAllTenantMembers(tid)
      .then((members) => {
        if (alive) setOwnerSnapshot({ count: ownerCount(members), ready: true });
      })
      .catch(() => {
        if (alive) setOwnerSnapshot({ count: 0, ready: true });
      });
    return () => {
      alive = false;
    };
  }, [info, auth.selectedTenantId, auth.tenant?.id, currentRole]);

  const isSystemAdmin = auth.user?.is_system_admin === true;
  const effectiveRole = isSystemAdmin ? "owner" : (currentRole ?? "");
  const canEditTenant = ({ viewer: 10, contributor: 20, admin: 30, owner: 40 }[effectiveRole] ?? 0) >= 40;
  const activeTenantMatch = info && Number(info.id) === Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const canLeave = activeTenantMatch && effectiveRole && effectiveRole !== "owner" ? true
    : ownerSnapshot.ready && ownerSnapshot.count > 1;
  const canDelete = activeTenantMatch && canEditTenant;

  const [editingName, setEditingName] = useState(false);
  const [nameDraft, setNameDraft] = useState("");
  const [savingName, setSavingName] = useState(false);

  const [editingDesc, setEditingDesc] = useState(false);
  const [descDraft, setDescDraft] = useState("");
  const [savingDesc, setSavingDesc] = useState(false);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState("");
  const [deleting, setDeleting] = useState(false);

  const saveName = async () => {
    const name = nameDraft.trim();
    if (!info || !name || name === info.name) {
      setEditingName(false);
      return;
    }
    setSavingName(true);
    const res = await updateTenant(Number(info.id), { name });
    setSavingName(false);
    if (res.success) {
      setInfo({ ...info, name });
      setEditingName(false);
    } else {
      setError(res.message || "Failed to save name");
    }
  };

  const saveDesc = async () => {
    if (!info) return;
    const description = descDraft.trim() ? descDraft.trim() : undefined;
    if (description === (info.description || "")) {
      setEditingDesc(false);
      return;
    }
    setSavingDesc(true);
    const res = await updateTenant(Number(info.id), { description });
    setSavingDesc(false);
    if (res.success) {
      setInfo({ ...info, description });
      setEditingDesc(false);
    } else {
      setError(res.message || "Failed to save description");
    }
  };

  const leave = async () => {
    if (!info) return;
    if (!window.confirm(t("tenant.leaveConfirm"))) return;
    const res = await leaveTenant(Number(info.id));
    if (res.success) {
      await auth.logout();
      router.push("/login");
    } else {
      setError(res.message || "Failed to leave workspace");
    }
  };

  const remove = async () => {
    if (!info || deleteConfirm.trim() !== info.name) return;
    setDeleting(true);
    const res = await deleteTenant(Number(info.id));
    setDeleting(false);
    if (res.success) {
      setDeleteOpen(false);
      await auth.refreshMe();
      router.push("/platform");
    } else {
      setError(res.message || "Failed to delete workspace");
    }
  };

  const statusText = (status?: string) =>
    status === "active" ? t("tenant.statusActive")
    : status === "inactive" ? t("tenant.statusInactive")
    : status === "suspended" ? t("tenant.statusSuspended")
    : t("tenant.statusUnknown");

  const usage = info?.storage_quota
    ? Math.min(Math.round(((info.storage_used || 0) / info.storage_quota) * 10000) / 100, 100)
    : 0;

  if (loading) {
    return <p className="caption text-muted">{t("tenant.loading")}</p>;
  }
  if (error && !info) {
    return (
      <div className="flex items-center gap-3">
        <p className="caption text-error">{error}</p>
        <button className="btn btn-outline btn-sm" onClick={() => void load()}>{t("tenant.retry")}</button>
      </div>
    );
  }
  if (!info) return null;

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col divide-y divide-hairline">
        <Row label={t("tenant.id")} desc={t("tenant.idDesc")} value={String(info.id)} />
        <Row label={t("tenant.name")} desc={t("tenant.nameDesc")}>
          {editingName ? (
            <div className="flex items-center gap-2">
              <input
                className="input h-9 w-[240px]"
                value={nameDraft}
                autoFocus
                onChange={(e) => setNameDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") void saveName();
                  if (e.key === "Escape") setEditingName(false);
                }}
              />
              <button className="btn btn-primary btn-sm" disabled={savingName} onClick={() => void saveName()}>
                {t("common.save")}
              </button>
              <button className="btn btn-outline btn-sm" onClick={() => setEditingName(false)}>{t("common.cancel")}</button>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <span className="caption text-body">{info.name || "-"}</span>
              {canEditTenant && (
                <button className="btn btn-tertiary btn-sm" onClick={() => { setNameDraft(info.name); setEditingName(true); }}>
                  {t("common.edit")}
                </button>
              )}
            </div>
          )}
        </Row>
        <Row label={t("tenant.description")} desc={t("tenant.descriptionDesc")}>
          {editingDesc ? (
            <div className="flex flex-col items-end gap-2">
              <textarea
                className="input h-auto min-h-[64px] w-[320px]"
                value={descDraft}
                autoFocus
                onChange={(e) => setDescDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") setEditingDesc(false);
                  if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) void saveDesc();
                }}
              />
              <div className="flex gap-2">
                <button className="btn btn-primary btn-sm" disabled={savingDesc} onClick={() => void saveDesc()}>
                  {t("common.save")}
                </button>
                <button className="btn btn-outline btn-sm" onClick={() => setEditingDesc(false)}>{t("common.cancel")}</button>
              </div>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <span className="caption max-w-[320px] text-body">{info.description || t("tenant.descriptionEmpty")}</span>
              {canEditTenant && (
                <button className="btn btn-tertiary btn-sm" onClick={() => { setDescDraft(info.description || ""); setEditingDesc(true); }}>
                  {t("common.edit")}
                </button>
              )}
            </div>
          )}
        </Row>
        <Row label={t("tenant.status")} desc={t("tenant.statusDesc")}>
          <span className={`badge-pill ${info.status === "active" ? "text-success" : "text-muted"}`}>
            {statusText(info.status)}
          </span>
        </Row>
        <Row label={t("tenant.createdAt")} desc={t("tenant.createdAtDesc")}>
          <span className="caption text-body">
            {info.created_at
              ? new Intl.DateTimeFormat(locale === "vi" ? "vi-VN" : "en-US", {
                  year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit",
                }).format(new Date(info.created_at))
              : "-"}
          </span>
        </Row>
        {info.storage_quota !== undefined && (
          <>
            <Row label={t("tenant.storage")} desc={t("tenant.storageDesc")}>
              <span className="caption text-body">{formatBytes(info.storage_quota)}</span>
            </Row>
            <Row label={t("tenant.storageUsed")} desc={t("tenant.storageUsedDesc")}>
              <span className="caption text-body">{formatBytes(info.storage_used || 0)}</span>
            </Row>
            <Row label={t("tenant.storageUsage")} desc={t("tenant.storageUsageDesc")}>
              <div className="flex w-[240px] items-center gap-3">
                <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-surface-strong">
                  <div
                    className={`h-full rounded-full ${usage > 80 ? "bg-[var(--color-error)]" : "bg-[var(--color-success)]"}`}
                    style={{ width: `${usage}%` }}
                  />
                </div>
                <span className="caption text-muted">{usage}%</span>
              </div>
            </Row>
          </>
        )}
      </div>

      {error && <p className="caption text-error">{error}</p>}

      {activeTenantMatch && canLeave && (
        <DangerZone
          title={t("tenant.leaveTitle")}
          desc={t("tenant.leaveDesc")}
          cta={t("tenant.leaveCta")}
          onClick={() => void leave()}
        />
      )}
      {canDelete && (
        <DangerZone
          title={t("tenant.deleteTitle")}
          desc={t("tenant.deleteDesc")}
          cta={t("tenant.deleteCta")}
          variant="danger"
          onClick={() => { setDeleteConfirm(""); setDeleteOpen(true); }}
        />
      )}

      <Modal open={deleteOpen} title={t("tenant.deleteConfirmTitle")} onClose={() => setDeleteOpen(false)} width="w-[440px]">
        <div className="flex flex-col gap-3">
          <p className="body-sm text-body">{t("tenant.deleteConfirmBody").replace("{name}", info.name)}</p>
          <p className="caption text-muted">
            {t("tenant.deleteConfirmHint").replace("{name}", info.name)}
          </p>
          <input
            className="input"
            value={deleteConfirm}
            placeholder={info.name}
            onChange={(e) => setDeleteConfirm(e.target.value)}
          />
          <div className="mt-2 flex justify-end gap-2">
            <button className="btn btn-outline btn-sm" disabled={deleting} onClick={() => setDeleteOpen(false)}>
              {t("common.cancel")}
            </button>
            <button
              className="btn btn-sm bg-[var(--color-error)] text-white"
              disabled={deleting || deleteConfirm.trim() !== info.name}
              onClick={() => void remove()}
            >
              {t("tenant.deleteCta")}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}


function Row({ label, desc, value, children }: {
  label: string;
  desc: string;
  value?: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-8 py-4">
      <div className="min-w-0">
        <label className="title-sm block">{label}</label>
        <p className="caption mt-1 text-muted">{desc}</p>
      </div>
      <div className="shrink-0">{children ?? <span className="caption text-body">{value}</span>}</div>
    </div>
  );
}

function DangerZone({ title, desc, cta, onClick, variant }: {
  title: string;
  desc: string;
  cta: string;
  onClick: () => void;
  variant?: "danger";
}) {
  return (
    <div className={`card flex items-center justify-between gap-6 border p-5 ${
      variant === "danger" ? "border-[color-mix(in_srgb,var(--color-error)_35%,transparent)]" : "border-hairline-strong"
    }`}>
      <div className="min-w-0">
        <div className="title-sm">{title}</div>
        <p className="caption mt-1 text-muted">{desc}</p>
      </div>
      <button
        className={`btn btn-sm shrink-0 ${variant === "danger" ? "bg-[var(--color-error)] text-white" : "border border-[var(--color-error)] text-error"}`}
        onClick={onClick}
      >
        {cta}
      </button>
    </div>
  );
}
