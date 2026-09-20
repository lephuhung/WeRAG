/* Ported from frontend/src/views/organization/OrganizationList.vue +
 * OrganizationSettingsModal.vue (subset): list with my_role badges,
 * create-organization modal, and a members drawer per org. Invite-code
 * rotation and share/KB management stay in the Vue app until ported.
 */
"use client";

import { useEffect, useState } from "react";
import { IconOrg, IconPlus } from "@/components/icons";
import { Modal } from "@/components/modal";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import {
  listMyOrganizations,
  createOrganization,
  listOrganizationMembers,
  type Organization,
  type OrganizationMember,
} from "@/lib/api/organizations";
import { useT } from "@/lib/i18n";

function OrgMembersPanel({ org, open, onClose }: {
  org: { id: string; name: string } | null;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useT();
  const [members, setMembers] = useState<OrganizationMember[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open || !org) return;
    let alive = true;
    setMembers(null);
    listOrganizationMembers(org.id)
      .then((res) => setMembers(res.data?.members ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load members"));
    return () => {
      alive = false;
    };
  }, [open, org]);

  return (
    <SlidePanel open={open} onClose={onClose} label={org?.name ?? ""} width="w-[520px]">
      <SlidePanelHeader title={`${org?.name ?? ""} — ${t("agent.members")}`} onClose={onClose} />
      <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5">
        {error && <p className="caption text-error">{error}</p>}
        {members === null && !error && <p className="caption text-muted">…</p>}
        {(members ?? []).map((m) => (
          <div key={m.id} className="flex items-center gap-3 border-b border-hairline py-3">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[12px] font-medium text-ink">
              {m.username.slice(0, 2).toUpperCase()}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-[14px] font-medium text-ink">{m.username}</div>
              <div className="caption truncate text-muted">{m.email}</div>
            </div>
            <span className="badge-pill">{m.role}</span>
          </div>
        ))}
        {members !== null && members.length === 0 && !error && (
          <p className="caption text-muted-soft">No members yet.</p>
        )}
      </div>
    </SlidePanel>
  );
}

export default function Organizations() {
  const { t } = useT();
  const [orgs, setOrgs] = useState<Organization[] | null>(null);
  const [error, setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [draft, setDraft] = useState({ name: "", description: "", member_limit: 50 });

  const [membersOrg, setMembersOrg] = useState<Organization | null>(null);

  useEffect(() => {
    let alive = true;
    listMyOrganizations()
      .then((res) => {
        if (alive) setOrgs(res.data?.organizations ?? []);
      })
      .catch((e) => {
        if (alive) {
          setOrgs([]);
          setError(e instanceof Error ? e.message : "Failed to load organizations");
        }
      });
    return () => {
      alive = false;
    };
  }, []);

  const submit = async () => {
    if (!draft.name.trim()) return;
    try {
      const res = await createOrganization({
        name: draft.name.trim(),
        description: draft.description || undefined,
        member_limit: draft.member_limit,
      });
      if (res.data) setOrgs((prev) => [...(prev ?? []), res.data!]);
      setCreateOpen(false);
      setDraft({ name: "", description: "", member_limit: 50 });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Create failed");
    }
  };

  void membersOrg;

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[1200px] px-12 py-12">
        <div className="mb-10 flex items-end justify-between gap-6">
          <div>
            <div className="caption-uppercase mb-3 text-muted">Workspace</div>
            <h1 className="display-xl">{t("nav.organizations")}</h1>
            <p className="mt-3 max-w-[520px] text-body">
              Shared spaces with their own knowledge bases and members.
            </p>
          </div>
          <button className="btn btn-primary" onClick={() => setCreateOpen(true)}>
            <IconPlus className="h-4 w-4" /> {t("agent.orgsCreate")}
          </button>
        </div>

        {error && <p className="caption mb-6 text-error">{error}</p>}
        {orgs !== null && orgs.length === 0 && !error && (
          <p className="caption mb-6 text-muted-soft">No organizations yet.</p>
        )}

        <div className="card overflow-hidden">
          {(orgs ?? []).map((o, i) => (
            <div
              key={o.id}
              className={`flex items-center gap-4 px-5 py-4 ${
                i > 0 ? "border-t border-hairline" : ""
              }`}
            >
              <div className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-strong text-ink">
                <IconOrg className="h-4 w-4" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-[15px] font-medium text-ink">{o.name}</div>
                <div className="caption text-muted">
                  {t("agent.members")}: {o.member_count ?? 0}
                </div>
              </div>
              {o.my_role && <span className="badge-pill">{o.my_role}</span>}
              <button
                className="btn btn-outline btn-sm"
                onClick={() => setMembersOrg(o)}
              >
                {t("agent.members")}
              </button>
            </div>
          ))}
        </div>
      </div>

      <Modal open={createOpen} title={t("agent.orgsCreate")} onClose={() => setCreateOpen(false)} width="w-[480px]">
        <div className="flex flex-col gap-4">
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("agent.orgsName")}</span>
            <input
              className="input"
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              autoFocus
            />
          </label>
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("agent.orgsDescription")}</span>
            <textarea
              className="input h-auto min-h-[64px] resize-y"
              value={draft.description}
              onChange={(e) => setDraft({ ...draft, description: e.target.value })}
            />
          </label>
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("agent.orgsMemberLimit")}</span>
            <input
              className="input w-[160px]"
              type="number"
              min={0}
              value={draft.member_limit}
              onChange={(e) => setDraft({ ...draft, member_limit: Number(e.target.value) })}
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

      <OrgMembersPanel
        org={membersOrg ? { id: membersOrg.id, name: membersOrg.name } : null}
        open={membersOrg !== null}
        onClose={() => setMembersOrg(null)}
      />
    </div>
  );
}
