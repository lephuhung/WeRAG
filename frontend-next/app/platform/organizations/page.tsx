/* Workspaces (tenants) card grid — orgs are tenants in this backend.
 * Data comes from GET /auth/me memberships + /tenants/:id/members;
 * the Vue app's /organizations API does not exist here.
 */
"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { IconOrg, IconPlus, IconSearch } from "@/components/icons";
import { Modal } from "@/components/modal";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { getCurrentUser } from "@/lib/api/auth";
import {
  createTenant,
  fetchAllTenantMembers,
  listMembers,
  type TenantMember,
} from "@/lib/api/tenants";
import { useT } from "@/lib/i18n";

type Workspace = {
  tenant_id: number;
  name: string;
  description?: string;
  my_role?: string;
  member_count?: number;
};

function WorkspaceMembersPanel({
  workspace,
  open,
  onClose,
}: {
  workspace: Workspace | null;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useT();
  const [members, setMembers] = useState<TenantMember[] | null>(null);
  const [error, setError] = useState("");
  const [searchQuery, setSearchQuery] = useState("");

  useEffect(() => {
    if (!open || !workspace) return;
    let alive = true;
    setMembers(null);
    setError("");
    setSearchQuery("");
    fetchAllTenantMembers(workspace.tenant_id)
      .then((list) => {
        if (alive) setMembers(list);
      })
      .catch((e) => {
        if (alive) setError(e instanceof Error ? e.message : "Failed to load members");
      });
    return () => {
      alive = false;
    };
  }, [open, workspace]);

  const filteredMembers = useMemo(() => {
    if (!members) return [];
    if (!searchQuery.trim()) return members;
    const q = searchQuery.toLowerCase().trim();
    return members.filter(
      (m) =>
        m.username?.toLowerCase().includes(q) ||
        m.email?.toLowerCase().includes(q),
    );
  }, [members, searchQuery]);

  return (
    <SlidePanel open={open} onClose={onClose} label={workspace?.name ?? ""} width="w-[500px]">
      <SlidePanelHeader
        title={workspace?.name ?? "Workspace"}
        subtitle={
          members !== null
            ? `${members.length} thành viên tham gia`
            : "Đang tải danh sách thành viên..."
        }
        onClose={onClose}
      />

      <div className="flex min-h-0 flex-1 flex-col px-6 pb-6 pt-2">
        {/* Search bar */}
        {members && members.length > 3 && (
          <div className="relative mb-4">
            <IconSearch className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-muted" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Tìm theo tên hoặc email thành viên..."
              className="input w-full pl-9 text-xs"
            />
          </div>
        )}

        {error && (
          <div className="rounded-xl border border-red-500/20 bg-red-500/5 p-3 text-xs text-red-500">
            {error}
          </div>
        )}

        {members === null && !error && (
          <div className="flex flex-col items-center justify-center py-16 text-muted">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent mb-2" />
            <p className="caption">Đang tải danh sách thành viên...</p>
          </div>
        )}

        {members !== null && (
          <div className="flex-1 overflow-y-auto space-y-2.5 pr-1">
            {filteredMembers.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-16 text-center text-muted">
                <IconOrg className="h-10 w-10 opacity-30 stroke-[1.2] mb-2" />
                <p className="caption">
                  {searchQuery.trim()
                    ? "Không tìm thấy thành viên phù hợp"
                    : "Chưa có thành viên nào trong workspace này."}
                </p>
              </div>
            ) : (
              filteredMembers.map((m) => {
                const initials = (m.username || m.email || "U")
                  .trim()
                  .slice(0, 2)
                  .toUpperCase();
                const isOwner = m.role === "owner";
                const isAdmin = m.role === "admin";

                return (
                  <div
                    key={m.user_id}
                    className="flex items-center gap-3.5 rounded-xl border border-hairline bg-surface-card p-3.5 hover:border-hairline-strong transition-colors"
                  >
                    <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-primary/15 to-primary/5 text-primary text-[13px] font-semibold">
                      {initials}
                    </div>

                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-[13.5px] font-semibold text-ink">
                          {m.username || m.email}
                        </span>
                      </div>
                      <div className="caption truncate text-muted mt-0.5">{m.email}</div>
                    </div>

                    <span
                      className={`badge-pill text-[11px] uppercase font-semibold tracking-wider ${
                        isOwner
                          ? "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20"
                          : isAdmin
                            ? "bg-primary/10 text-primary border-primary/20"
                            : "bg-surface-strong text-muted"
                      }`}
                    >
                      {m.role}
                    </span>
                  </div>
                );
              })
            )}
          </div>
        )}
      </div>
    </SlidePanel>
  );
}

export default function Organizations() {
  const { t } = useT();
  const [workspaces, setWorkspaces] = useState<Workspace[] | null>(null);
  const [error, setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [draft, setDraft] = useState({ name: "", description: "" });

  const [membersWorkspace, setMembersWorkspace] = useState<Workspace | null>(null);

  const load = useCallback(async () => {
    const me = await getCurrentUser();
    const memberships = me.data?.memberships ?? [];
    const items: Workspace[] = memberships.map((m) => ({
      tenant_id: m.tenant_id,
      name: m.tenant_name ?? `Workspace #${m.tenant_id}`,
      my_role: m.role,
    }));
    // Best-effort member counts; foreign/inaccessible tenants keep "—".
    await Promise.all(
      items.map(async (w) => {
        try {
          const res = await listMembers(w.tenant_id, { page_size: 1 });
          if (res.success && res.data) w.member_count = res.data.total;
        } catch {
          /* not a member — leave count unset */
        }
      }),
    );
    return items;
  }, []);

  useEffect(() => {
    let alive = true;
    load()
      .then((items) => {
        if (alive) setWorkspaces(items);
      })
      .catch((e) => {
        if (alive) {
          setWorkspaces([]);
          setError(e instanceof Error ? e.message : "Failed to load workspaces");
        }
      });
    return () => {
      alive = false;
    };
  }, [load]);

  const submit = async () => {
    if (!draft.name.trim()) return;
    try {
      const res = await createTenant({
        name: draft.name.trim(),
        description: draft.description || undefined,
      });
      if (res.success && res.data) {
        setWorkspaces((prev) => [
          ...(prev ?? []),
          { tenant_id: Number(res.data!.id), name: res.data!.name, description: res.data!.description, my_role: "owner" },
        ]);
      } else {
        setError(res.message || "Create failed");
        return;
      }
      setCreateOpen(false);
      setDraft({ name: "", description: "" });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Create failed");
    }
  };

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[1200px] px-10 py-10">
        <div className="mb-10 flex items-end justify-between gap-6">
          <div>
            <div className="caption-uppercase mb-2.5 text-muted font-medium tracking-wider">
              Workspace
            </div>
            <h1 className="display-xl">{t("nav.organizations")}</h1>
            <p className="mt-2.5 max-w-[540px] text-sm text-body leading-relaxed">
              Shared spaces with their own knowledge bases and members.
            </p>
          </div>
          <button className="btn btn-primary" onClick={() => setCreateOpen(true)}>
            <IconPlus className="h-4 w-4" /> {t("agent.orgsCreate")}
          </button>
        </div>

        {error && (
          <div className="mb-6 rounded-xl border border-red-500/20 bg-red-500/5 p-3 text-xs text-red-500">
            {error}
          </div>
        )}

        {workspaces !== null && workspaces.length === 0 && !error && (
          <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-hairline py-16 text-center text-muted">
            <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-surface-strong text-muted mb-3">
              <IconOrg className="h-6 w-6" />
            </div>
            <p className="body-sm font-medium text-ink">Chưa có workspace nào</p>
            <p className="caption mt-1 text-muted">Tạo workspace mới để chia sẻ tri thức và làm việc cùng đồng đội.</p>
            <button
              className="btn btn-primary btn-sm mt-4"
              onClick={() => setCreateOpen(true)}
            >
              <IconPlus className="h-3.5 w-3.5" /> Tạo workspace đầu tiên
            </button>
          </div>
        )}

        {/* Card Grid */}
        <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {(workspaces ?? []).map((w) => (
            <div
              key={w.tenant_id}
              onClick={() => setMembersWorkspace(w)}
              className="group relative flex flex-col justify-between rounded-2xl border border-hairline bg-surface-card p-5 hover:border-hairline-strong hover:shadow-md transition-all cursor-pointer select-none"
            >
              {/* Card Top */}
              <div>
                <div className="flex items-start justify-between gap-3">
                  <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-primary/10 text-primary group-hover:bg-primary group-hover:text-white transition-all duration-200 shadow-xs">
                    <IconOrg className="h-5 w-5" />
                  </div>
                  {w.my_role && (
                    <span className="badge-pill bg-surface-strong text-muted text-[11px] font-semibold uppercase tracking-wider">
                      {w.my_role}
                    </span>
                  )}
                </div>

                <h3 className="mt-3.5 text-[15.5px] font-semibold text-ink group-hover:text-primary transition-colors truncate">
                  {w.name}
                </h3>

                <p className="caption mt-1.5 text-muted line-clamp-2 min-h-[34px] leading-relaxed">
                  {w.description || "Không có mô tả cho workspace này."}
                </p>
              </div>

              {/* Card Bottom / Footer */}
              <div className="mt-5 border-t border-hairline pt-3.5 flex items-center justify-between">
                <div className="flex items-center gap-1.5 text-xs text-muted">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" className="h-4 w-4">
                    <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
                    <circle cx="9" cy="7" r="4" />
                    <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
                    <path d="M16 3.13a4 4 0 0 1 0 7.75" />
                  </svg>
                  <span className="font-medium text-ink/80">{w.member_count ?? "—"}</span>
                  <span>thành viên</span>
                </div>

                <div className="flex items-center gap-1 text-xs font-medium text-primary group-hover:translate-x-0.5 transition-transform">
                  <span>Xem thành viên</span>
                  <svg viewBox="0 0 20 20" fill="currentColor" className="h-3.5 w-3.5">
                    <path
                      fillRule="evenodd"
                      d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.5 4.25a.75.75 0 01-1.06-.02z"
                      clipRule="evenodd"
                    />
                  </svg>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      <Modal open={createOpen} title={t("agent.orgsCreate")} onClose={() => setCreateOpen(false)} width="w-[480px]">
        <div className="flex flex-col gap-4">
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("agent.orgsName")} *</span>
            <input
              className="input w-full"
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              placeholder="Nhập tên workspace..."
              autoFocus
            />
          </label>
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("agent.orgsDescription")}</span>
            <textarea
              className="input w-full h-auto min-h-[64px] resize-y"
              value={draft.description}
              onChange={(e) => setDraft({ ...draft, description: e.target.value })}
              placeholder="Mô tả về mục đích hoạt động của workspace..."
            />
          </label>
          <div className="mt-2 flex justify-end gap-2">
            <button className="btn btn-outline btn-sm" onClick={() => setCreateOpen(false)}>
              {t("common.cancel")}
            </button>
            <button className="btn btn-primary btn-sm" disabled={!draft.name.trim()} onClick={() => void submit()}>
              {t("common.save")}
            </button>
          </div>
        </div>
      </Modal>

      <WorkspaceMembersPanel
        workspace={membersWorkspace}
        open={membersWorkspace !== null}
        onClose={() => setMembersWorkspace(null)}
      />
    </div>
  );
}
