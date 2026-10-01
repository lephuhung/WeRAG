"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconRefresh, IconSearch, IconTrash } from "@/components/icons";
import { Select } from "@/components/select";
import {
  fetchAllTenantMembers,
  updateMemberRole,
  removeMember,
  leaveTenant,
  listTenantInvitations,
  revokeInvitation,
  type TenantMember,
  type TenantRole,
  type TenantInvitation,
} from "@/lib/api/tenants";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { useRouter } from "next/navigation";
import { copyToClipboard } from "@/lib/clipboard";
import { InviteMemberModal } from "@/components/invite-member-modal";

export function TenantMembers() {
  const { t } = useT();

  /* Workspace roles: Tenant Admin and Member (plus platform SuperAdmin, which is
   * not a membership). The legacy owner role is retired and not assignable. */
  const ROLES: { id: TenantRole; label: string; desc: string }[] = [
    { id: "admin", label: t("acct.roleAdmin"), desc: t("mem.roleAdminDesc") },
    { id: "member", label: t("acct.roleMember"), desc: t("mem.roleMemberDesc") },
  ];
  const auth = useAuth();
  const router = useRouter();

  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const isTenantAdmin = currentRole === "admin" || currentRole === "owner" || isSystemAdmin;
  const canManage = isTenantAdmin;

  const [members, setMembers] = useState<TenantMember[]>([]);
  const [invitations, setInvitations] = useState<TenantInvitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState<string>("all");

  // Invite Modal — the email/link form lives in InviteMemberModal so the
  // organizations page can reuse it.
  const [inviteModalOpen, setInviteModalOpen] = useState(false);

  // Remove / Leave confirmation
  const [removingMember, setRemovingMember] = useState<TenantMember | null>(null);
  const [confirmLeave, setConfirmLeave] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);

  // RBAC info modal
  const [rbacModalOpen, setRbacModalOpen] = useState(false);

  // Active Tenant Admins (legacy owner rows count as admin until migrated).
  const adminCount = useMemo(() => {
    return members.filter(
      (m) => (m.role === "admin" || m.role === "owner") && m.status === "active",
    ).length;
  }, [members]);

  const loadData = useCallback(async () => {
    if (!activeTenantId) return;
    setLoading(true);
    setError("");
    try {
      const [allMembers, invRes] = await Promise.all([
        fetchAllTenantMembers(activeTenantId),
        canManage ? listTenantInvitations(activeTenantId) : Promise.resolve({ success: true, data: { invitations: [] } }),
      ]);

      setMembers(allMembers);

      if (invRes.success && invRes.data) {
        setInvitations(invRes.data.invitations ?? []);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("mem.networkError"));
    } finally {
      setLoading(false);
    }
  }, [activeTenantId, canManage]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  // Member role change
  const handleRoleChange = async (member: TenantMember, newRole: TenantRole) => {
    if (!activeTenantId || !canManage) return;
    if ((member.role === "admin" || member.role === "owner") && newRole !== "admin" && adminCount <= 1) {
      setError(t("mem.errLastAdminDemote"));
      return;
    }
    setError("");
    setSuccess("");
    try {
      const res = await updateMemberRole(activeTenantId, member.user_id, newRole);
      if (res.success) {
        setSuccess(
          t("mem.roleUpdated")
            .replace("{name}", member.username || member.email)
            .replace("{role}", newRole),
        );
        await loadData();
      } else {
        setError(res.message || t("mem.updateRoleFailed"));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("mem.updateRoleFailed"));
    }
  };

  // Remove member
  const handleRemoveMember = async () => {
    if (!activeTenantId || !removingMember) return;
    if ((removingMember.role === "admin" || removingMember.role === "owner") && adminCount <= 1) {
      setError(t("mem.errLastAdminRemove"));
      setRemovingMember(null);
      return;
    }
    setActionBusy(true);
    setError("");
    try {
      const res = await removeMember(activeTenantId, removingMember.user_id);
      if (res.success) {
        setSuccess(
          t("mem.removed").replace("{name}", removingMember.username || removingMember.email),
        );
        setRemovingMember(null);
        await loadData();
      } else {
        setError(res.message || t("mem.removeFailed"));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("mem.removeFailed"));
    } finally {
      setActionBusy(false);
    }
  };

  // Leave workspace
  const handleLeave = async () => {
    if (!activeTenantId) return;
    if ((currentRole === "admin" || currentRole === "owner") && adminCount <= 1) {
      setError(t("mem.errLastAdminLeave"));
      setConfirmLeave(false);
      return;
    }
    setActionBusy(true);
    setError("");
    try {
      const res = await leaveTenant(activeTenantId);
      if (res.success) {
        router.push("/platform/knowledge-bases");
      } else {
        setError(res.message || t("mem.leaveFailed"));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("mem.leaveFailed"));
    } finally {
      setActionBusy(false);
      setConfirmLeave(false);
    }
  };

  // Revoke invite
  const handleRevokeInvite = async (invId: number) => {
    if (!activeTenantId) return;
    try {
      const res = await revokeInvitation(activeTenantId, invId);
      if (res.success) {
        await loadData();
      } else {
        setError(res.message || t("mem.revokeFailed"));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("mem.revokeFailed"));
    }
  };

  const filteredMembers = useMemo(() => {
    return members.filter((m) => {
      const matchRole = roleFilter === "all" || m.role === roleFilter;
      const q = query.trim().toLowerCase();
      const matchQuery =
        !q ||
        m.username?.toLowerCase().includes(q) ||
        m.email?.toLowerCase().includes(q);
      return matchRole && matchQuery;
    });
  }, [members, roleFilter, query]);

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <div className="flex items-center gap-2.5">
            <h2 className="title-md font-semibold text-ink">{t("mem.title")}</h2>
            <button
              type="button"
              onClick={() => setRbacModalOpen(true)}
              className="text-muted hover:text-ink transition-colors"
              title={t("mem.rbacTooltip")}
            >
              <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.8}>
                <circle cx="12" cy="12" r="10" />
                <path d="M12 16v-4M12 8h.01" />
              </svg>
            </button>
          </div>
          <p className="caption text-muted mt-1">
            {t("mem.subtitle")}
          </p>
        </div>

        <div className="flex items-center gap-2">
          {canManage && (
            <button
              type="button"
              onClick={() => setInviteModalOpen(true)}
              className="btn btn-primary btn-sm flex items-center gap-1.5"
            >
              <IconPlus className="h-3.5 w-3.5" />
              <span>{t("mem.invite")}</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => void loadData()}
            className="btn btn-outline btn-sm flex items-center gap-1.5"
            title={t("common.refresh")}
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
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

      {/* Pending Invitations Section (Managers only) */}
      {canManage && invitations.length > 0 && (
        <div className="rounded-xl border border-hairline bg-surface-card p-5 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="text-sm font-semibold text-ink">{t("mem.pendingInv")}</span>
              <span className="rounded-full bg-surface-strong px-2 py-0.5 text-xs font-medium text-muted">
                {invitations.length}
              </span>
            </div>
            <span className="caption text-muted">{t("mem.valid7d")}</span>
          </div>

          <div className="divide-y divide-hairline">
            {invitations.map((inv) => (
              <div key={inv.id} className="flex flex-wrap items-center justify-between gap-3 py-3 text-sm">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-ink truncate">
                      {inv.is_share_link
                        ? t("mem.shareLink")
                        : inv.invitee_email || inv.invitee_name || t("mem.unknownUser")}
                    </span>
                    <span className="badge-pill uppercase text-[10.5px]">
                      {inv.role === "admin" || inv.role === "owner" ? t("acct.roleAdmin") : t("acct.roleMember")}
                    </span>
                  </div>
                  <p className="caption text-muted truncate mt-0.5">
                    {inv.is_share_link
                      ? t("mem.acceptedCount").replace("{n}", String(inv.accepted_count || 0))
                      : t("mem.invitedBy").replace("{name}", inv.inviter_name || inv.inviter_email || "admin")}
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  {inv.invite_url && (
                    <button
                      type="button"
                      onClick={async () => {
                        const full = new URL(inv.invite_url!, window.location.origin).toString();
                        const ok = await copyToClipboard(full);
                        if (ok) {
                          setSuccess(t("mem.linkCopied"));
                        }
                      }}
                      className="btn btn-outline btn-sm text-xs py-1"
                    >
                      {t("mem.copyLink")}
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => void handleRevokeInvite(inv.id)}
                    className="btn btn-outline btn-sm text-xs text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/40 py-1"
                  >
                    {t("mem.revoke")}
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Filter and Search Bar */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="relative w-full max-w-sm">
          <IconSearch className="absolute left-3 top-1/2 -translate-y-1/2 text-muted h-4 w-4" />
          <input
            type="text"
            className="input pl-9 text-sm"
            placeholder={t("mem.searchPh")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        <div className="flex items-center gap-1.5">
          {[
            { id: "all", label: t("common.all") },
            { id: "admin", label: t("acct.roleAdmin") },
            { id: "member", label: t("acct.roleMember") },
          ].map((r) => (
            <button
              key={r.id}
              type="button"
              onClick={() => setRoleFilter(r.id)}
              className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                roleFilter === r.id
                  ? "bg-ink text-white dark:bg-white dark:text-ink"
                  : "text-muted hover:bg-surface-strong hover:text-ink"
              }`}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      {/* Members Table */}
      <div className="overflow-hidden rounded-xl border border-hairline">
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse text-sm">
          <thead>
            <tr className="border-b border-hairline bg-surface-strong/50 text-xs font-semibold text-muted">
              <th className="py-3 px-4">{t("mem.colMember")}</th>
              <th className="py-3 px-4">{t("mem.colRole")}</th>
              <th className="py-3 px-4">{t("mem.colJoined")}</th>
              <th className="py-3 px-4 text-right">{t("mem.colActions")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {loading ? (
              <tr>
                <td colSpan={4} className="py-12 text-center text-muted">
                  {t("common.loading")}
                </td>
              </tr>
            ) : filteredMembers.length === 0 ? (
              <tr>
                <td colSpan={4} className="py-12 text-center text-muted">
                  {t("mem.empty")}
                </td>
              </tr>
            ) : (
              filteredMembers.map((m) => {
                const isMe = m.user_id === auth.user?.id;
                const canEditThisMember = canManage && (!isMe || isTenantAdmin);

                return (
                  <tr key={m.user_id} className="hover:bg-surface-strong/30 transition-colors">
                    {/* Member info */}
                    <td className="py-3.5 px-4">
                      <div className="flex items-center gap-3">
                        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-surface-strong text-ink font-semibold uppercase text-xs border border-hairline">
                          {m.username?.slice(0, 2) || m.email?.slice(0, 2) || "U"}
                        </div>
                        <div className="min-w-0">
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-ink truncate">
                              {m.username || m.email}
                            </span>
                            {isMe && (
                              <span className="rounded bg-brand/10 px-1.5 py-0.5 text-[10px] font-bold text-brand uppercase">
                                {t("mem.you")}
                              </span>
                            )}
                          </div>
                          <span className="caption text-muted truncate block">
                            {m.email}
                          </span>
                        </div>
                      </div>
                    </td>

                    {/* Role */}
                    <td className="py-3.5 px-4">
                      {canEditThisMember ? (
                        <Select
                          className="h-8 w-32 py-1 px-2.5 text-xs font-medium"
                          value={m.role}
                          onChange={(v) => void handleRoleChange(m, v as TenantRole)}
                          options={[
                            { value: "admin", label: t("acct.roleAdmin") },
                            { value: "member", label: t("acct.roleMember") },
                          ]}
                        />
                      ) : (
                        <span
                          className={`badge-pill uppercase text-[11px] font-semibold ${
                            m.role === "admin" || m.role === "owner"
                              ? "bg-blue-500/10 text-blue-600 border border-blue-500/20"
                              : ""
                          }`}
                        >
                          {m.role === "admin" || m.role === "owner" ? t("acct.roleAdmin") : t("acct.roleMember")}
                        </span>
                      )}
                    </td>

                    {/* Joined date */}
                    <td className="py-3.5 px-4 text-muted text-xs">
                      {m.joined_at ? new Date(m.joined_at).toLocaleDateString() : "—"}
                    </td>

                    {/* Actions */}
                    <td className="py-3.5 px-4 text-right">
                      {isMe ? (
                        <button
                          type="button"
                          onClick={() => setConfirmLeave(true)}
                          className="btn btn-outline btn-sm text-xs text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/30"
                        >
                          {t("mem.leaveWs")}
                        </button>
                      ) : canEditThisMember ? (
                        <button
                          type="button"
                          onClick={() => setRemovingMember(m)}
                          className="btn btn-ghost btn-sm text-muted hover:text-rose-600 p-1.5"
                          title={t("common.remove")}
                        >
                          <IconTrash className="h-4 w-4" />
                        </button>
                      ) : null}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
          </table>
        </div>
      </div>

      {/* Invite Member Modal */}
      <InviteMemberModal
        tenantId={activeTenantId}
        open={inviteModalOpen}
        onClose={() => setInviteModalOpen(false)}
        onInvited={() => {
          setSuccess(t("mem.inviteSent"));
          void loadData();
        }}
      />

      {/* Remove Confirmation Modal */}
      <Modal
        open={removingMember !== null}
        title={t("mem.removeTitle")}
        onClose={() => setRemovingMember(null)}
      >
        <div className="space-y-4">
          <p className="body-sm text-body">
            {t("mem.removeBody").replace("{name}", removingMember?.username || removingMember?.email || "")}
          </p>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setRemovingMember(null)}
            >
              {t("common.cancel")}
            </button>
            <button
              type="button"
              disabled={actionBusy}
              onClick={() => void handleRemoveMember()}
              className="btn btn-primary bg-rose-600 hover:bg-rose-700 text-white"
            >
              {actionBusy ? `${t("common.remove")}…` : t("mem.removeCta")}
            </button>
          </div>
        </div>
      </Modal>

      {/* Leave Workspace Confirmation Modal */}
      <Modal
        open={confirmLeave}
        title={t("mem.leaveTitle")}
        onClose={() => setConfirmLeave(false)}
      >
        <div className="space-y-4">
          <p className="body-sm text-body">
            {t("mem.leaveBody")}
          </p>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setConfirmLeave(false)}
            >
              {t("common.cancel")}
            </button>
            <button
              type="button"
              disabled={actionBusy}
              onClick={() => void handleLeave()}
              className="btn btn-primary bg-rose-600 hover:bg-rose-700 text-white"
            >
              {actionBusy ? `${t("mem.leaveCta")}…` : t("mem.leaveCta")}
            </button>
          </div>
        </div>
      </Modal>

      {/* RBAC Breakdown Modal */}
      <Modal
        open={rbacModalOpen}
        title={t("mem.rbacTitle")}
        onClose={() => setRbacModalOpen(false)}
      >
        <div className="space-y-4 text-sm max-h-[60vh] overflow-y-auto pr-1">
          {ROLES.map((r) => (
            <div key={r.id} className="rounded-xl border border-hairline p-4 space-y-1.5">
              <div className="flex items-center gap-2">
                <span className="font-semibold text-ink">{r.label}</span>
                {currentRole === r.id && (
                  <span className="badge-pill bg-brand/10 text-brand text-[10px] font-bold">
                    {t("mem.currentRole")}
                  </span>
                )}
              </div>
              <p className="caption text-muted">{r.desc}</p>
            </div>
          ))}
        </div>
      </Modal>
    </div>
  );
}
