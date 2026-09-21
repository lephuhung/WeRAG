"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconEdit, IconPlus, IconRefresh, IconTrash } from "@/components/icons";
import {
  listOrgs,
  createOrg,
  updateOrg,
  deleteOrg,
  listOrgMembers,
  addOrgMember,
  updateOrgMemberRole,
  removeOrgMember,
  createOrgInviteLink,
  fetchAllTenantMembers,
  type TenantOrg,
  type TenantOrgMember,
  type TenantOrgRole,
  type TenantMember,
  type TenantRole,
} from "@/lib/api/tenants";
import { useAuth } from "@/lib/auth";

export function TenantOrgs() {
  const auth = useAuth();
  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const canManageTenant = currentRole === "owner" || currentRole === "admin" || isSystemAdmin;

  const [orgs, setOrgs] = useState<TenantOrg[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  // Create / Edit Org Modal
  const [orgModalOpen, setOrgModalOpen] = useState(false);
  const [editingOrg, setEditingOrg] = useState<TenantOrg | null>(null);
  const [orgName, setOrgName] = useState("");
  const [orgDesc, setOrgDesc] = useState("");
  const [savingOrg, setSavingOrg] = useState(false);

  // Delete Org Confirmation
  const [deletingOrg, setDeletingOrg] = useState<TenantOrg | null>(null);
  const [deletingBusy, setDeletingBusy] = useState(false);

  // Org Members Modal
  const [activeOrg, setActiveOrg] = useState<TenantOrg | null>(null);
  const [members, setMembers] = useState<TenantOrgMember[]>([]);
  const [allTenantMembers, setAllTenantMembers] = useState<TenantMember[]>([]);
  const [loadingMembers, setLoadingMembers] = useState(false);
  const [newMemberUserId, setNewMemberUserId] = useState("");
  const [newMemberRole, setNewMemberRole] = useState<TenantOrgRole>("member");
  const [addingMember, setAddingMember] = useState(false);

  // Invite Link Modal
  const [inviteOrg, setInviteOrg] = useState<TenantOrg | null>(null);
  const [inviteRole, setInviteRole] = useState<TenantRole>("member");
  const [inviteMsg, setInviteMsg] = useState("");
  const [inviteUrl, setInviteUrl] = useState("");
  const [linkCopied, setLinkCopied] = useState(false);
  const [generatingLink, setGeneratingLink] = useState(false);

  const loadOrgs = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listOrgs();
      if (res.success && res.data) {
        setOrgs(res.data);
      } else {
        setError(res.message || "Failed to load organizations");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load organizations");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadOrgs();
  }, [loadOrgs]);

  // Open Create Modal
  const openCreate = () => {
    setEditingOrg(null);
    setOrgName("");
    setOrgDesc("");
    setOrgModalOpen(true);
  };

  // Open Edit Modal
  const openEdit = (org: TenantOrg) => {
    setEditingOrg(org);
    setOrgName(org.name);
    setOrgDesc(org.description || "");
    setOrgModalOpen(true);
  };

  // Save Org
  const handleSaveOrg = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!orgName.trim()) return;
    setSavingOrg(true);
    setError("");
    try {
      if (editingOrg) {
        const res = await updateOrg(editingOrg.id, {
          name: orgName.trim(),
          description: orgDesc.trim() || undefined,
        });
        if (res.success) {
          setSuccess(`Updated organization ${orgName}`);
          setOrgModalOpen(false);
          await loadOrgs();
        } else {
          setError(res.message || "Failed to update organization");
        }
      } else {
        const res = await createOrg({
          name: orgName.trim(),
          description: orgDesc.trim() || undefined,
        });
        if (res.success) {
          setSuccess(`Created organization ${orgName}`);
          setOrgModalOpen(false);
          await loadOrgs();
        } else {
          setError(res.message || "Failed to create organization");
        }
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save organization");
    } finally {
      setSavingOrg(false);
    }
  };

  // Delete Org
  const handleDeleteOrg = async () => {
    if (!deletingOrg) return;
    setDeletingBusy(true);
    setError("");
    try {
      const res = await deleteOrg(deletingOrg.id);
      if (res.success) {
        setSuccess(`Deleted organization ${deletingOrg.name}`);
        setDeletingOrg(null);
        await loadOrgs();
      } else {
        setError(res.message || "Failed to delete organization");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete organization");
    } finally {
      setDeletingBusy(false);
    }
  };

  // Open Members Modal
  const openMembersModal = async (org: TenantOrg) => {
    setActiveOrg(org);
    setLoadingMembers(true);
    setNewMemberUserId("");
    setNewMemberRole("member");
    try {
      const [mRes, tmRes] = await Promise.all([
        listOrgMembers(org.id),
        activeTenantId ? fetchAllTenantMembers(activeTenantId) : Promise.resolve([]),
      ]);
      if (mRes.success && mRes.data) {
        setMembers(mRes.data);
      }
      setAllTenantMembers(tmRes);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load org members");
    } finally {
      setLoadingMembers(false);
    }
  };

  // Add Member to Org
  const handleAddMember = async () => {
    if (!activeOrg || !newMemberUserId) return;
    setAddingMember(true);
    setError("");
    try {
      const res = await addOrgMember(activeOrg.id, {
        user_id: newMemberUserId,
        role: newMemberRole,
      });
      if (res.success) {
        setNewMemberUserId("");
        const refreshed = await listOrgMembers(activeOrg.id);
        if (refreshed.success && refreshed.data) setMembers(refreshed.data);
        await loadOrgs();
      } else {
        setError(res.message || "Failed to add member to organization");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to add member");
    } finally {
      setAddingMember(false);
    }
  };

  // Update Org Member Role
  const handleToggleMemberRole = async (member: TenantOrgMember) => {
    if (!activeOrg) return;
    const nextRole: TenantOrgRole = member.role === "manager" ? "member" : "manager";
    try {
      const res = await updateOrgMemberRole(activeOrg.id, member.user_id, { role: nextRole });
      if (res.success) {
        const refreshed = await listOrgMembers(activeOrg.id);
        if (refreshed.success && refreshed.data) setMembers(refreshed.data);
      } else {
        setError(res.message || "Failed to update role");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to update role");
    }
  };

  // Remove Member from Org
  const handleRemoveMember = async (userId: string) => {
    if (!activeOrg) return;
    try {
      const res = await removeOrgMember(activeOrg.id, userId);
      if (res.success) {
        const refreshed = await listOrgMembers(activeOrg.id);
        if (refreshed.success && refreshed.data) setMembers(refreshed.data);
        await loadOrgs();
      } else {
        setError(res.message || "Failed to remove member");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to remove member");
    }
  };

  // Open Invite Link Modal
  const openInviteModal = (org: TenantOrg) => {
    setInviteOrg(org);
    setInviteRole("member");
    setInviteMsg("");
    setInviteUrl("");
    setLinkCopied(false);
  };

  // Generate Org Invite Link
  const handleGenerateOrgInvite = async () => {
    if (!inviteOrg) return;
    setGeneratingLink(true);
    setError("");
    try {
      const res = await createOrgInviteLink(inviteOrg.id, {
        role: inviteRole,
        message: inviteMsg.trim() || undefined,
      });
      if (res.success && res.data?.url) {
        const full = new URL(res.data.url, window.location.origin).toString();
        setInviteUrl(full);
      } else {
        setError(res.message || "Failed to generate invite link");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to generate invite link");
    } finally {
      setGeneratingLink(false);
    }
  };

  // Candidates for org members: tenant members not yet in this org
  const memberCandidates = allTenantMembers.filter(
    (tm) => !members.some((om) => om.user_id === tm.user_id),
  );

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">Organizations</h2>
          <p className="caption text-muted mt-1">
            Group members into organizations to manage access to sensitive knowledge bases.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {canManageTenant && (
            <button
              type="button"
              onClick={openCreate}
              className="btn btn-primary btn-sm flex items-center gap-1.5"
            >
              <IconPlus className="h-3.5 w-3.5" />
              <span>Create organization</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => void loadOrgs()}
            className="btn btn-outline btn-sm flex items-center gap-1.5"
            title="Refresh organizations"
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

      {/* Orgs Table */}
      <div className="overflow-hidden rounded-xl border border-hairline">
        <table className="w-full text-left border-collapse text-sm">
          <thead>
            <tr className="border-b border-hairline bg-surface-strong/50 text-xs font-semibold text-muted">
              <th className="py-3 px-4">Name</th>
              <th className="py-3 px-4">Description</th>
              <th className="py-3 px-4">Members</th>
              <th className="py-3 px-4">Created</th>
              <th className="py-3 px-4 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {loading ? (
              <tr>
                <td colSpan={5} className="py-12 text-center text-muted">
                  Loading organizations…
                </td>
              </tr>
            ) : orgs.length === 0 ? (
              <tr>
                <td colSpan={5} className="py-12 text-center text-muted">
                  No organizations found. Create one to organize members and access.
                </td>
              </tr>
            ) : (
              orgs.map((org) => (
                <tr key={org.id} className="hover:bg-surface-strong/30 transition-colors">
                  <td className="py-3.5 px-4 font-medium text-ink">
                    {org.name}
                  </td>
                  <td className="py-3.5 px-4 text-muted text-xs max-w-xs truncate">
                    {org.description || "—"}
                  </td>
                  <td className="py-3.5 px-4">
                    <button
                      type="button"
                      onClick={() => void openMembersModal(org)}
                      className="badge-pill hover:bg-surface-strong transition-colors"
                    >
                      {org.member_count || 0} members
                    </button>
                  </td>
                  <td className="py-3.5 px-4 text-muted text-xs">
                    {org.created_at ? new Date(org.created_at).toLocaleDateString() : "—"}
                  </td>
                  <td className="py-3.5 px-4 text-right space-x-1">
                    <button
                      type="button"
                      onClick={() => void openMembersModal(org)}
                      className="btn btn-outline btn-sm text-xs py-1"
                    >
                      Members
                    </button>
                    <button
                      type="button"
                      onClick={() => openInviteModal(org)}
                      className="btn btn-outline btn-sm text-xs py-1"
                    >
                      Invite
                    </button>
                    {canManageTenant && (
                      <>
                        <button
                          type="button"
                          onClick={() => openEdit(org)}
                          className="btn btn-ghost btn-sm p-1.5 text-muted hover:text-ink"
                          title="Edit"
                        >
                          <IconEdit className="h-3.5 w-3.5" />
                        </button>
                        <button
                          type="button"
                          onClick={() => setDeletingOrg(org)}
                          className="btn btn-ghost btn-sm p-1.5 text-muted hover:text-rose-600"
                          title="Delete"
                        >
                          <IconTrash className="h-3.5 w-3.5" />
                        </button>
                      </>
                    )}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Create / Edit Org Modal */}
      <Modal
        open={orgModalOpen}
        title={editingOrg ? "Edit Organization" : "Create Organization"}
        onClose={() => setOrgModalOpen(false)}
      >
        <form onSubmit={handleSaveOrg} className="space-y-4">
          <label className="block">
            <span className="caption mb-1.5 block text-muted">Organization Name</span>
            <input
              type="text"
              required
              placeholder="e.g. Core Engineering"
              className="input"
              value={orgName}
              onChange={(e) => setOrgName(e.target.value)}
            />
          </label>

          <label className="block">
            <span className="caption mb-1.5 block text-muted">Description (optional)</span>
            <textarea
              rows={3}
              placeholder="Brief summary of this group's responsibility"
              className="input resize-none"
              value={orgDesc}
              onChange={(e) => setOrgDesc(e.target.value)}
            />
          </label>

          <div className="flex justify-end gap-3 pt-3">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setOrgModalOpen(false)}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={savingOrg || !orgName.trim()}
              className="btn btn-primary"
            >
              {savingOrg ? "Saving…" : editingOrg ? "Save changes" : "Create organization"}
            </button>
          </div>
        </form>
      </Modal>

      {/* Delete Org Confirmation Modal */}
      <Modal
        open={deletingOrg !== null}
        title="Delete Organization"
        onClose={() => setDeletingOrg(null)}
      >
        <div className="space-y-4">
          <p className="body-sm text-body">
            Are you sure you want to delete <strong>{deletingOrg?.name}</strong>? Any knowledge
            bases restricted specifically to this organization will become inaccessible to its
            members.
          </p>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setDeletingOrg(null)}
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={deletingBusy}
              onClick={() => void handleDeleteOrg()}
              className="btn btn-primary bg-rose-600 hover:bg-rose-700 text-white"
            >
              {deletingBusy ? "Deleting…" : "Delete organization"}
            </button>
          </div>
        </div>
      </Modal>

      {/* Org Members Modal */}
      <Modal
        open={activeOrg !== null}
        title={`Members of ${activeOrg?.name || "Organization"}`}
        onClose={() => setActiveOrg(null)}
      >
        <div className="space-y-6">
          {/* Add member section */}
          {canManageTenant && (
            <div className="rounded-xl border border-hairline bg-surface-strong/30 p-4 space-y-3">
              <span className="caption font-medium text-ink">Add Member from Workspace</span>
              <div className="flex flex-wrap items-center gap-2">
                <select
                  className="input flex-1 min-w-[180px] text-sm"
                  value={newMemberUserId}
                  onChange={(e) => setNewMemberUserId(e.target.value)}
                >
                  <option value="">Select workspace member…</option>
                  {memberCandidates.map((tm) => (
                    <option key={tm.user_id} value={tm.user_id}>
                      {tm.username || tm.email} ({tm.role})
                    </option>
                  ))}
                </select>
                <select
                  className="input w-32 text-sm"
                  value={newMemberRole}
                  onChange={(e) => setNewMemberRole(e.target.value as TenantOrgRole)}
                >
                  <option value="member">Member</option>
                  <option value="manager">Manager</option>
                </select>
                <button
                  type="button"
                  disabled={addingMember || !newMemberUserId}
                  onClick={() => void handleAddMember()}
                  className="btn btn-primary btn-sm"
                >
                  {addingMember ? "Adding…" : "Add"}
                </button>
              </div>
            </div>
          )}

          {/* Members list */}
          <div className="max-h-80 overflow-y-auto space-y-2">
            {loadingMembers ? (
              <p className="caption text-muted text-center py-6">Loading members…</p>
            ) : members.length === 0 ? (
              <p className="caption text-muted text-center py-6">
                No members in this organization yet.
              </p>
            ) : (
              members.map((m) => (
                <div
                  key={m.user_id}
                  className="flex items-center justify-between gap-3 rounded-lg border border-hairline p-3 text-sm"
                >
                  <div className="min-w-0">
                    <span className="font-medium text-ink truncate block">
                      {m.username || m.email || m.user_id}
                    </span>
                    <span className="caption text-muted truncate block">{m.email}</span>
                  </div>

                  <div className="flex items-center gap-2">
                    <span
                      className={`badge-pill text-xs uppercase ${
                        m.role === "manager"
                          ? "bg-brand/10 text-brand border border-brand/20"
                          : ""
                      }`}
                    >
                      {m.role}
                    </span>

                    {canManageTenant && (
                      <>
                        <button
                          type="button"
                          onClick={() => void handleToggleMemberRole(m)}
                          className="btn btn-outline btn-sm text-xs py-1"
                        >
                          {m.role === "manager" ? "Set as Member" : "Make Manager"}
                        </button>
                        <button
                          type="button"
                          onClick={() => void handleRemoveMember(m.user_id)}
                          className="btn btn-ghost btn-sm p-1.5 text-muted hover:text-rose-600"
                          title="Remove from org"
                        >
                          <IconTrash className="h-3.5 w-3.5" />
                        </button>
                      </>
                    )}
                  </div>
                </div>
              ))
            )}
          </div>

          <div className="flex justify-end pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setActiveOrg(null)}
            >
              Close
            </button>
          </div>
        </div>
      </Modal>

      {/* Invite Link Modal */}
      <Modal
        open={inviteOrg !== null}
        title={`Invite Link for ${inviteOrg?.name || "Organization"}`}
        onClose={() => setInviteOrg(null)}
      >
        <div className="space-y-4">
          <p className="caption text-muted">
            Generate a registration link that enrols joiners directly into {inviteOrg?.name}.
          </p>

          <label className="block">
            <span className="caption mb-1.5 block text-muted">Workspace Role</span>
            <select
              className="input"
              value={inviteRole}
              onChange={(e) => setInviteRole(e.target.value as TenantRole)}
            >
              <option value="member">Member</option>
              <option value="admin">Admin</option>
            </select>
          </label>

          <label className="block">
            <span className="caption mb-1.5 block text-muted">Message / Context (optional)</span>
            <input
              type="text"
              placeholder="e.g. Join our data science group"
              className="input"
              value={inviteMsg}
              onChange={(e) => setInviteMsg(e.target.value)}
            />
          </label>

          {inviteUrl ? (
            <div className="rounded-xl border border-hairline bg-surface-strong/50 p-4 space-y-2">
              <span className="caption font-medium text-ink">Organization Invite Link</span>
              <div className="flex items-center gap-2">
                <input
                  type="text"
                  readOnly
                  value={inviteUrl}
                  className="input text-xs font-mono select-all"
                />
                <button
                  type="button"
                  onClick={() => {
                    void navigator.clipboard.writeText(inviteUrl);
                    setLinkCopied(true);
                    setTimeout(() => setLinkCopied(false), 2000);
                  }}
                  className="btn btn-primary shrink-0 text-xs"
                >
                  {linkCopied ? "Copied!" : "Copy link"}
                </button>
              </div>
            </div>
          ) : (
            <button
              type="button"
              disabled={generatingLink}
              onClick={() => void handleGenerateOrgInvite()}
              className="btn btn-primary w-full"
            >
              {generatingLink ? "Generating…" : "Generate Organization Link"}
            </button>
          )}

          <div className="flex justify-end pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setInviteOrg(null)}
            >
              Close
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
