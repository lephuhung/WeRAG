"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconRefresh, IconSearch, IconTrash } from "@/components/icons";
import { Select } from "@/components/select";
import {
  fetchAllTenantMembers,
  addMember,
  updateMemberRole,
  removeMember,
  leaveTenant,
  listTenantInvitations,
  createInviteLink,
  revokeInvitation,
  type TenantMember,
  type TenantRole,
  type TenantInvitation,
} from "@/lib/api/tenants";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { useRouter } from "next/navigation";
import { copyToClipboard } from "@/lib/clipboard";

const ROLES: { id: TenantRole; label: string; desc: string }[] = [
  { id: "owner", label: "Owner", desc: "Full administrative control, workspace deletion & billing" },
  { id: "admin", label: "Admin", desc: "Manage members, models, integrations, skills, and storage" },
  { id: "member", label: "Member", desc: "Read knowledge bases, documents, and chat with agents" },
];

export function TenantMembers() {
  const { t } = useT();
  const auth = useAuth();
  const router = useRouter();

  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const canManage = currentRole === "owner" || currentRole === "admin" || isSystemAdmin;
  const isOwner = currentRole === "owner" || isSystemAdmin;

  const [members, setMembers] = useState<TenantMember[]>([]);
  const [invitations, setInvitations] = useState<TenantInvitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState<string>("all");

  // Invite Modal
  const [inviteModalOpen, setInviteModalOpen] = useState(false);
  const [inviteTab, setInviteTab] = useState<"email" | "link">("email");
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<TenantRole>("member");
  const [inviteMsg, setInviteMsg] = useState("");
  const [generatedLink, setGeneratedLink] = useState("");
  const [linkCopied, setLinkCopied] = useState(false);
  const [inviting, setInviting] = useState(false);

  // Remove / Leave confirmation
  const [removingMember, setRemovingMember] = useState<TenantMember | null>(null);
  const [confirmLeave, setConfirmLeave] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);

  // RBAC info modal
  const [rbacModalOpen, setRbacModalOpen] = useState(false);

  const ownerCount = useMemo(() => {
    return members.filter((m) => m.role === "owner" && m.status === "active").length;
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
      setError(e instanceof Error ? e.message : "Network error");
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
    if (member.role === "owner" && newRole !== "owner" && ownerCount <= 1) {
      setError("Cannot demote the last remaining workspace owner");
      return;
    }
    setError("");
    setSuccess("");
    try {
      const res = await updateMemberRole(activeTenantId, member.user_id, newRole);
      if (res.success) {
        setSuccess(`Updated ${member.username || member.email}'s role to ${newRole}`);
        await loadData();
      } else {
        setError(res.message || "Failed to update role");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to update role");
    }
  };

  // Remove member
  const handleRemoveMember = async () => {
    if (!activeTenantId || !removingMember) return;
    if (removingMember.role === "owner" && ownerCount <= 1) {
      setError("Cannot remove the last remaining workspace owner");
      setRemovingMember(null);
      return;
    }
    setActionBusy(true);
    setError("");
    try {
      const res = await removeMember(activeTenantId, removingMember.user_id);
      if (res.success) {
        setSuccess(`Removed ${removingMember.username || removingMember.email}`);
        setRemovingMember(null);
        await loadData();
      } else {
        setError(res.message || "Failed to remove member");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to remove member");
    } finally {
      setActionBusy(false);
    }
  };

  // Leave workspace
  const handleLeave = async () => {
    if (!activeTenantId) return;
    if (currentRole === "owner" && ownerCount <= 1) {
      setError("You are the last owner. Transfer ownership before leaving.");
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
        setError(res.message || "Failed to leave workspace");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to leave workspace");
    } finally {
      setActionBusy(false);
      setConfirmLeave(false);
    }
  };

  // Send email invite
  const handleInviteEmail = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!activeTenantId || !inviteEmail.trim()) return;
    setInviting(true);
    setError("");
    try {
      const res = await addMember(activeTenantId, {
        email: inviteEmail.trim(),
        role: inviteRole,
      });
      if (res.success) {
        setSuccess(`Invitation sent to ${inviteEmail.trim()}`);
        setInviteModalOpen(false);
        setInviteEmail("");
        await loadData();
      } else {
        setError(res.message || "Failed to send invitation");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to send invitation");
    } finally {
      setInviting(false);
    }
  };

  // Generate share link
  const handleCreateShareLink = async () => {
    if (!activeTenantId) return;
    setInviting(true);
    setError("");
    try {
      const res = await createInviteLink(activeTenantId, {
        role: inviteRole,
        message: inviteMsg.trim() || undefined,
      });
      if (res.success && res.data) {
        const fullUrl = res.data.invite_url
          ? new URL(res.data.invite_url, window.location.origin).toString()
          : `${window.location.origin}/invite/${res.data.id}`;
        setGeneratedLink(fullUrl);
        await loadData();
      } else {
        setError(res.message || "Failed to generate invite link");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to generate invite link");
    } finally {
      setInviting(false);
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
        setError(res.message || "Failed to revoke invitation");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to revoke invitation");
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
            <h2 className="title-md font-semibold text-ink">Workspace Members</h2>
            <button
              type="button"
              onClick={() => setRbacModalOpen(true)}
              className="text-muted hover:text-ink transition-colors"
              title="View permissions breakdown"
            >
              <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.8}>
                <circle cx="12" cy="12" r="10" />
                <path d="M12 16v-4M12 8h.01" />
              </svg>
            </button>
          </div>
          <p className="caption text-muted mt-1">
            Manage who has access to this workspace and control member roles.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {canManage && (
            <button
              type="button"
              onClick={() => {
                setInviteModalOpen(true);
                setGeneratedLink("");
                setLinkCopied(false);
              }}
              className="btn btn-primary btn-sm flex items-center gap-1.5"
            >
              <IconPlus className="h-3.5 w-3.5" />
              <span>Invite member</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => void loadData()}
            className="btn btn-outline btn-sm flex items-center gap-1.5"
            title="Refresh list"
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
              <span className="text-sm font-semibold text-ink">Pending Invitations</span>
              <span className="rounded-full bg-surface-strong px-2 py-0.5 text-xs font-medium text-muted">
                {invitations.length}
              </span>
            </div>
            <span className="caption text-muted">Valid for 7 days</span>
          </div>

          <div className="divide-y divide-hairline">
            {invitations.map((inv) => (
              <div key={inv.id} className="flex flex-wrap items-center justify-between gap-3 py-3 text-sm">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-ink truncate">
                      {inv.is_share_link
                        ? "Public share link"
                        : inv.invitee_email || inv.invitee_name || "Unknown user"}
                    </span>
                    <span className="badge-pill uppercase text-[10.5px]">
                      {inv.role}
                    </span>
                  </div>
                  <p className="caption text-muted truncate mt-0.5">
                    {inv.is_share_link
                      ? `${inv.accepted_count || 0} accepted`
                      : `Invited by ${inv.inviter_name || inv.inviter_email || "admin"}`}
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
                          setSuccess("Invitation link copied to clipboard");
                        }
                      }}
                      className="btn btn-outline btn-sm text-xs py-1"
                    >
                      Copy link
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => void handleRevokeInvite(inv.id)}
                    className="btn btn-outline btn-sm text-xs text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/40 py-1"
                  >
                    Revoke
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
            placeholder="Search by name or email…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        <div className="flex items-center gap-1.5">
          {["all", "owner", "admin", "member"].map((r) => (
            <button
              key={r}
              type="button"
              onClick={() => setRoleFilter(r)}
              className={`rounded-full px-3 py-1 text-xs font-medium capitalize transition-colors ${
                roleFilter === r
                  ? "bg-ink text-white dark:bg-white dark:text-ink"
                  : "text-muted hover:bg-surface-strong hover:text-ink"
              }`}
            >
              {r}
            </button>
          ))}
        </div>
      </div>

      {/* Members Table */}
      <div className="overflow-hidden rounded-xl border border-hairline">
        <table className="w-full text-left border-collapse text-sm">
          <thead>
            <tr className="border-b border-hairline bg-surface-strong/50 text-xs font-semibold text-muted">
              <th className="py-3 px-4">Member</th>
              <th className="py-3 px-4">Role</th>
              <th className="py-3 px-4">Joined</th>
              <th className="py-3 px-4 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {loading ? (
              <tr>
                <td colSpan={4} className="py-12 text-center text-muted">
                  Loading members…
                </td>
              </tr>
            ) : filteredMembers.length === 0 ? (
              <tr>
                <td colSpan={4} className="py-12 text-center text-muted">
                  No members found matching your search.
                </td>
              </tr>
            ) : (
              filteredMembers.map((m) => {
                const isMe = m.user_id === auth.user?.id;
                const canEditThisMember =
                  canManage && (!isMe || isOwner) && !(m.role === "owner" && !isOwner);

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
                                You
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
                      {canEditThisMember && m.role !== "owner" ? (
                        <Select
                          className="h-8 w-32 py-1 px-2.5 text-xs font-medium"
                          value={m.role}
                          onChange={(v) => void handleRoleChange(m, v as TenantRole)}
                          options={[
                            { value: "admin", label: "Admin" },
                            { value: "member", label: "Member" },
                          ]}
                        />
                      ) : (
                        <span
                          className={`badge-pill uppercase text-[11px] font-semibold ${
                            m.role === "owner"
                              ? "bg-amber-500/10 text-amber-600 border border-amber-500/20"
                              : m.role === "admin"
                              ? "bg-blue-500/10 text-blue-600 border border-blue-500/20"
                              : ""
                          }`}
                        >
                          {m.role}
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
                          Leave workspace
                        </button>
                      ) : canEditThisMember ? (
                        <button
                          type="button"
                          onClick={() => setRemovingMember(m)}
                          className="btn btn-ghost btn-sm text-muted hover:text-rose-600 p-1.5"
                          title="Remove member"
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

      {/* Invite Member Modal */}
      <Modal
        open={inviteModalOpen}
        title="Invite to Workspace"
        onClose={() => setInviteModalOpen(false)}
      >
        <div className="space-y-5">
          {/* Tabs */}
          <div className="flex border-b border-hairline pb-2 gap-4">
            <button
              type="button"
              onClick={() => setInviteTab("email")}
              className={`text-sm font-medium pb-2 transition-colors border-b-2 -mb-2.5 ${
                inviteTab === "email"
                  ? "border-brand text-brand font-semibold"
                  : "border-transparent text-muted hover:text-ink"
              }`}
            >
              Invite by Email
            </button>
            <button
              type="button"
              onClick={() => setInviteTab("link")}
              className={`text-sm font-medium pb-2 transition-colors border-b-2 -mb-2.5 ${
                inviteTab === "link"
                  ? "border-brand text-brand font-semibold"
                  : "border-transparent text-muted hover:text-ink"
              }`}
            >
              Shareable Link
            </button>
          </div>

          {inviteTab === "email" ? (
            <form onSubmit={handleInviteEmail} className="space-y-4">
              <label className="block">
                <span className="caption mb-1.5 block text-muted">Email address</span>
                <input
                  type="email"
                  required
                  placeholder="member@company.com"
                  className="input"
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                />
              </label>

              <label className="block">
                <span className="caption mb-1.5 block text-muted">Initial role</span>
                <Select
                  value={inviteRole}
                  onChange={(v) => setInviteRole(v as TenantRole)}
                  options={ROLES.map((r) => ({ value: r.id, label: `${r.label} — ${r.desc}` }))}
                />
              </label>

              <div className="flex justify-end gap-3 pt-3">
                <button
                  type="button"
                  className="btn btn-outline"
                  onClick={() => setInviteModalOpen(false)}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={inviting || !inviteEmail.trim()}
                  className="btn btn-primary"
                >
                  {inviting ? "Sending…" : "Send Invitation"}
                </button>
              </div>
            </form>
          ) : (
            <div className="space-y-4">
              <label className="block">
                <span className="caption mb-1.5 block text-muted">Assign role for joiners</span>
                <Select
                  value={inviteRole}
                  onChange={(v) => setInviteRole(v as TenantRole)}
                  options={ROLES.map((r) => ({ value: r.id, label: `${r.label} — ${r.desc}` }))}
                />
              </label>

              <label className="block">
                <span className="caption mb-1.5 block text-muted">Note / Message (optional)</span>
                <input
                  type="text"
                  placeholder="e.g. Engineering team onboarding"
                  className="input"
                  value={inviteMsg}
                  onChange={(e) => setInviteMsg(e.target.value)}
                />
              </label>

              {generatedLink ? (
                <div className="rounded-xl border border-hairline bg-surface-strong/50 p-4 space-y-2">
                  <span className="caption font-medium text-ink">Active invite link</span>
                  <div className="flex items-center gap-2">
                    <input
                      type="text"
                      readOnly
                      value={generatedLink}
                      className="input text-xs font-mono select-all"
                    />
                    <button
                      type="button"
                      onClick={async () => {
                        const ok = await copyToClipboard(generatedLink);
                        if (ok) {
                          setLinkCopied(true);
                          setTimeout(() => setLinkCopied(false), 2000);
                        }
                      }}
                      className="btn btn-primary shrink-0 text-xs"
                    >
                      {linkCopied ? "Copied!" : "Copy link"}
                    </button>
                  </div>
                  <p className="caption text-muted text-[11px]">
                    Anyone with this link can join this workspace as {inviteRole}.
                  </p>
                </div>
              ) : (
                <button
                  type="button"
                  onClick={() => void handleCreateShareLink()}
                  disabled={inviting}
                  className="btn btn-primary w-full"
                >
                  {inviting ? "Generating…" : "Generate Invite Link"}
                </button>
              )}

              <div className="flex justify-end pt-2">
                <button
                  type="button"
                  className="btn btn-outline"
                  onClick={() => setInviteModalOpen(false)}
                >
                  Close
                </button>
              </div>
            </div>
          )}
        </div>
      </Modal>

      {/* Remove Confirmation Modal */}
      <Modal
        open={removingMember !== null}
        title="Remove Member"
        onClose={() => setRemovingMember(null)}
      >
        <div className="space-y-4">
          <p className="body-sm text-body">
            Are you sure you want to remove{" "}
            <strong>{removingMember?.username || removingMember?.email}</strong> from this
            workspace? They will immediately lose access to all resources and knowledge bases.
          </p>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setRemovingMember(null)}
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={actionBusy}
              onClick={() => void handleRemoveMember()}
              className="btn btn-primary bg-rose-600 hover:bg-rose-700 text-white"
            >
              {actionBusy ? "Removing…" : "Remove member"}
            </button>
          </div>
        </div>
      </Modal>

      {/* Leave Workspace Confirmation Modal */}
      <Modal
        open={confirmLeave}
        title="Leave Workspace"
        onClose={() => setConfirmLeave(false)}
      >
        <div className="space-y-4">
          <p className="body-sm text-body">
            Are you sure you want to leave this workspace? You will need an invitation from an
            owner or administrator to rejoin.
          </p>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setConfirmLeave(false)}
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={actionBusy}
              onClick={() => void handleLeave()}
              className="btn btn-primary bg-rose-600 hover:bg-rose-700 text-white"
            >
              {actionBusy ? "Leaving…" : "Confirm Leave"}
            </button>
          </div>
        </div>
      </Modal>

      {/* RBAC Breakdown Modal */}
      <Modal
        open={rbacModalOpen}
        title="Workspace Role & Permissions Guide"
        onClose={() => setRbacModalOpen(false)}
      >
        <div className="space-y-4 text-sm max-h-[60vh] overflow-y-auto pr-1">
          {ROLES.map((r) => (
            <div key={r.id} className="rounded-xl border border-hairline p-4 space-y-1.5">
              <div className="flex items-center gap-2">
                <span className="font-semibold text-ink capitalize">{r.label}</span>
                {currentRole === r.id && (
                  <span className="badge-pill bg-brand/10 text-brand text-[10px] font-bold">
                    Current Role
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
