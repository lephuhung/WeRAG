"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { IconInfoCircle, IconPlus, IconRefresh, IconSearch, IconTrash } from "@/components/icons";
import { Select } from "@/components/select";
import { Notice } from "@/components/settings/notice";
import { DataTable } from "@/components/settings/data-table";
import { Pill, PillGroup } from "@/components/settings/pill";
import { SectionHeader } from "@/components/settings/section-header";
import { useToast } from "@/components/toast";
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
import { formatDate } from "@/lib/datetime";

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
  const toast = useToast();

  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState<string>("all");

  // Invite Modal — the email/link form lives in InviteMemberModal.
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
    try {
      const res = await updateMemberRole(activeTenantId, member.user_id, newRole);
      if (res.success) {
        toast.success(
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
        toast.success(
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
      <SectionHeader
        title={t("mem.title")}
        subtitle={t("mem.subtitle")}
        actions={
          <>
            <button
              type="button"
              onClick={() => setRbacModalOpen(true)}
              className="text-muted transition-colors hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
              title={t("mem.rbacTooltip")}
              aria-label={t("mem.rbacTooltip")}
            >
              <IconInfoCircle className="h-4 w-4" />
            </button>
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
              aria-label={t("common.refresh")}
            >
              <IconRefresh className="h-3.5 w-3.5" />
            </button>
          </>
        }
      />

      {error && (
        <Notice
          tone="error"
          action={
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() => void loadData()}
            >
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
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
                    <span
                      className={`badge-pill uppercase text-[10.5px] ${
                        inv.role === "admin" || inv.role === "owner" ? "badge-accent" : "badge-neutral"
                      }`}
                    >
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
                          toast.success(t("mem.linkCopied"));
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
                    className="btn btn-danger btn-sm text-xs py-1"
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
            id="member-search"
            className="input pl-9 text-sm"
            placeholder={t("mem.searchPh")}
            aria-label={t("mem.searchPh")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        <PillGroup ariaLabel={t("mem.colRole")}>
          {[
            { id: "all", label: t("common.all") },
            { id: "admin", label: t("acct.roleAdmin") },
            { id: "member", label: t("acct.roleMember") },
          ].map((r) => (
            <Pill key={r.id} active={roleFilter === r.id} onClick={() => setRoleFilter(r.id)}>
              {r.label}
            </Pill>
          ))}
        </PillGroup>
      </div>

      {/* Members Table */}
      <DataTable<TenantMember>
        columns={[
          { key: "member", header: t("mem.colMember") },
          { key: "role", header: t("mem.colRole") },
          { key: "joined", header: t("mem.colJoined") },
          { key: "actions", header: t("mem.colActions"), align: "right" },
        ]}
        rows={filteredMembers}
        rowKey={(m) => String(m.user_id)}
        loading={loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("mem.empty")}
        caption={t("mem.title")}
        renderRow={(m) => {
          const isMe = m.user_id === auth.user?.id;
          const canEditThisMember = canManage && (!isMe || isTenantAdmin);
          const memberName = m.username || m.email;
          const isAdminRole = m.role === "admin" || m.role === "owner";

          return (
            <>
              {/* Member info */}
              <td className="px-5 py-3.5 align-middle">
                <div className="flex items-center gap-3">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-surface-strong text-ink font-semibold uppercase text-xs border border-hairline">
                    {m.username?.slice(0, 2) || m.email?.slice(0, 2) || "U"}
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-ink truncate">
                        {memberName}
                      </span>
                      {isMe && (
                        <span className="badge-pill badge-neutral inline-flex shrink-0 items-center whitespace-nowrap uppercase text-[10px] font-bold leading-4 tracking-wide">
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
              <td className="px-5 py-3.5 align-middle">
                {canEditThisMember ? (
                  <Select
                    id={`member-role-${m.user_id}`}
                    ariaLabel={`${t("mem.colRole")} — ${memberName}`}
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
                      isAdminRole ? "badge-accent" : "badge-neutral"
                    }`}
                  >
                    {isAdminRole ? t("acct.roleAdmin") : t("acct.roleMember")}
                  </span>
                )}
              </td>

              {/* Joined date */}
              <td className="px-5 py-3.5 align-middle text-muted text-xs whitespace-nowrap">
                {m.joined_at ? formatDate(m.joined_at) : "—"}
              </td>

              {/* Actions */}
              <td className="px-5 py-3.5 align-middle text-right">
                {isMe ? (
                  <button
                    type="button"
                    onClick={() => setConfirmLeave(true)}
                    className="btn btn-danger btn-sm text-xs"
                  >
                    {t("mem.leaveWs")}
                  </button>
                ) : canEditThisMember ? (
                  <button
                    type="button"
                    onClick={() => setRemovingMember(m)}
                    className="btn btn-danger btn-sm p-1.5"
                    title={t("common.remove")}
                    aria-label={t("common.remove")}
                  >
                    <IconTrash className="h-4 w-4" />
                  </button>
                ) : null}
              </td>
            </>
          );
        }}
      />

      {/* Invite Member Modal */}
      <InviteMemberModal
        tenantId={activeTenantId}
        open={inviteModalOpen}
        onClose={() => setInviteModalOpen(false)}
        onInvited={() => {
          toast.success(t("mem.inviteSent"));
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
              className="btn btn-danger"
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
              className="btn btn-danger"
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
                  <span className="badge-pill bg-primary/10 text-primary text-[10px] font-bold">
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
