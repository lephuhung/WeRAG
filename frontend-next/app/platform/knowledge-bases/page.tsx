"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";
import { listKnowledgeBases, listPublicCatalog, type KnowledgeBaseRow } from "@/lib/api/knowledge";
import { getKBViewerCapabilities, type KBViewerCapabilities } from "@/lib/kb-capabilities";
import { groupCatalogRows, selectPublicSection } from "@/lib/kb-public";
import { useAuth, useTenantRole } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { Orb } from "@/components/orb";
import { Modal } from "@/components/modal";
import { KbShareModal } from "@/components/knowledge/kb-share-modal";
import { KBInvitePanel } from "@/components/knowledge/kb-invite-panel";
import { MyKBInvites } from "@/components/knowledge/my-kb-invites";
import { ParseDefaultsEditor } from "@/components/knowledge/parse-defaults-editor";
import {
  IconBook,
  IconDoc,
  IconExternal,
  IconParserEngine,
  IconPlus,
  IconSearch,
} from "@/components/icons";

const ORBS = ["mint", "lavender", "peach", "sky", "rose"] as const;
const PUBLIC_PAGE_SIZE = 12;

interface CardModel {
  id: string;
  name: string;
  description: string;
  docs: number;
  updatedAt: string;
  orb: (typeof ORBS)[number];
  caps: KBViewerCapabilities;
  row: KnowledgeBaseRow;
}

export default function KnowledgeBaseList() {
  const { isTenantAdmin, isSystemAdmin: isExplicitSuperAdmin } = useTenantRole();
  const { user, selectedTenantId, tenant } = useAuth();
  const { t } = useT();
  const [kbs, setKbs] = useState<KnowledgeBaseRow[] | null>(null);
  const [error, setError] = useState("");
  const [q, setQ] = useState("");
  const [pdOpen, setPdOpen] = useState(false);
  const [shareKb, setShareKb] = useState<KnowledgeBaseRow | null>(null);
  const [inviteKb, setInviteKb] = useState<KnowledgeBaseRow | null>(null);
  const [invitesOpen, setInvitesOpen] = useState(false);
  const isSystemAdmin = user?.is_system_admin === true;
  const activeTenantId = selectedTenantId ?? String(tenant?.id ?? "");
  const hasTenant = activeTenantId !== "";

  /* Dedicated public catalog: server-side search + paging through the
   * endpoint (never assume the mixed list's first-50 window is complete).
   * Loads independently so tenantless humans still see the public section
   * when the mixed tenant list is unavailable. */
  const [pubItems, setPubItems] = useState<KnowledgeBaseRow[]>([]);
  const [pubTotal, setPubTotal] = useState(0);
  const [pubPage, setPubPage] = useState(1);
  const [pubError, setPubError] = useState("");
  /* True once the dedicated endpoint answered successfully (even empty).
   * While loading or on error the mixed list's public window is the only
   * public data available. */
  const [pubOk, setPubOk] = useState(false);

  const reload = () => {
    listKnowledgeBases()
      .then((rows) => {
        setKbs(rows);
      })
      .catch((e) => {
        setKbs([]);
        setError(e instanceof Error ? e.message : "Failed to load knowledge bases");
      });
  };

  useEffect(() => {
    if (!hasTenant) {
      setKbs([]);
      return;
    }
    let alive = true;
    listKnowledgeBases()
      .then((rows) => {
        if (alive) setKbs(rows);
      })
      .catch((e) => {
        if (alive) {
          setKbs([]);
          setError(e instanceof Error ? e.message : "Failed to load knowledge bases");
        }
      });
    return () => {
      alive = false;
    };
  }, [hasTenant]);

  useEffect(() => {
    let alive = true;
    const timer = setTimeout(() => {
      listPublicCatalog({ page: pubPage, pageSize: PUBLIC_PAGE_SIZE, q })
        .then((res) => {
          if (!alive) return;
          setPubItems(res.items);
          setPubTotal(res.total);
          setPubError("");
          setPubOk(true);
        })
        .catch((e) => {
          if (!alive) return;
          setPubItems([]);
          setPubTotal(0);
          setPubError(e instanceof Error ? e.message : "Failed to load public catalog");
          setPubOk(false);
        });
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [pubPage, q]);

  /* One shared search box scopes all three sections: workspace + invited
   * filter client-side by name; the public section goes through the
   * endpoint's q param (it's server-paginated, so client filtering would
   * only see the current page). The mixed-list public fallback is filtered
   * the same way so typing still narrows it while loading. */
  const matchesQ = (k: KnowledgeBaseRow) =>
    !q || k.name.toLowerCase().includes(q.toLowerCase());

  /* Owner-aware grouping: workspace rows (explicit owner match or legacy
   * ownerless rows in this data scope), platform-public rows, and invited
   * foreign rows. Ownership keys off owner_tenant_id + visibility — a
   * converted row's data tenant_id never decides its section. */
  const groups = groupCatalogRows(kbs ?? [], hasTenant ? activeTenantId : "");
  /* Public section rows: after a successful endpoint response the selected
   * endpoint page is authoritative on its own (a page-2 render must not
   * re-append the mixed list's first-50 window the pager no longer
   * describes); while loading or on error, fall back to the mixed list's
   * public window. Deduped by id within the chosen source. */
  const publicRows = selectPublicSection(groups.public.filter(matchesQ), { items: pubItems, ok: pubOk });
  const pubPages = Math.max(1, Math.ceil(pubTotal / PUBLIC_PAGE_SIZE));

  const toCard = (k: KnowledgeBaseRow, i: number): CardModel => {
    const caps = getKBViewerCapabilities(k, {
      activeTenantId: hasTenant ? activeTenantId : "",
      isTenantAdmin,
      isSystemAdmin,
    });
    return {
      id: k.id,
      name: k.name,
      description: k.description ?? "",
      docs: k.knowledge_count ?? k.document_count ?? 0,
      updatedAt: k.updated_at ?? "",
      orb: ORBS[i % ORBS.length],
      caps,
      row: k,
    };
  };

  const workspaceCards = groups.workspace.filter(matchesQ).map(toCard);
  const invitedCards = groups.invited.filter(matchesQ).map(toCard);
  const publicCards = publicRows.map(toCard);

  const renderCards = (cards: CardModel[], opts?: { hideShare?: boolean; append?: ReactNode }) => (
    <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
      {cards.map((kb) => (
        <Link
          key={kb.id}
          href={`/platform/knowledge-bases/${kb.id}`}
          className="card card-hover group relative min-w-0 overflow-hidden p-6"
        >
          <Orb color={kb.orb} size={220} className="-right-16 -top-16 opacity-50" />
          {/* Share shortcut — hidden on public rows (grants don't expand
           * public visibility); stopPropagation so it doesn't follow the
           * card's Link into the detail page. */}
          {!opts?.hideShare && (
            <button
              type="button"
              title={t("kbShare.title")}
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setShareKb(kb.row);
              }}
              className="absolute right-4 top-4 z-10 flex h-7 w-7 items-center justify-center rounded-full text-muted opacity-0 transition-opacity hover:bg-surface-strong hover:text-ink group-hover:opacity-100"
            >
              <IconExternal className="h-4 w-4" />
            </button>
          )}
          {/* Invite shortcut — own-tenant Tenant Admins invite one
           * external member to read (recipient-bound). Never on public or
           * invited rows. */}
          {kb.caps.canInvite && (
            <button
              type="button"
              title="Invite an external member to read"
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setInviteKb(kb.row);
              }}
              className="absolute right-12 top-4 z-10 flex h-7 items-center justify-center rounded-full px-2 text-[11px] font-medium text-muted opacity-0 transition-opacity hover:bg-surface-strong hover:text-ink group-hover:opacity-100"
            >
              Invite
            </button>
          )}
          <div className="relative min-w-0">
            <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-ink">
              <IconBook className="h-5 w-5" />
            </div>
            <div className="flex items-center gap-2">
              <h2 className="title-md truncate">{kb.name}</h2>
              {/* Public identity keys off owner + visibility — never the
               * data tenant_id, so a converted row's foreign data scope
               * doesn't mislabel it "Shared". */}
              {kb.caps.kind === "public" && (
                <span className="badge-pill shrink-0">{t("kbList.publicBadge")}</span>
              )}
              {kb.caps.kind === "invited" && (
                <span className="badge-pill shrink-0">{t("kbList.sharedBadge")}</span>
              )}
            </div>
            <p className="body-sm mt-1.5 line-clamp-2 text-body">{kb.description}</p>
            <div className="caption mt-5 flex items-center gap-4 text-muted">
              <span className="flex items-center gap-1.5">
                <IconDoc className="h-3.5 w-3.5" />
                {kb.docs} documents
              </span>
              <span className="ml-auto whitespace-nowrap">{fmtShortDate(kb.updatedAt)}</span>
            </div>
          </div>
        </Link>
      ))}
      {opts?.append}
    </div>
  );

  return (
    <div className="relative flex-1 overflow-y-auto">
      {/* Orbs live in a clipped overlay so their negative offsets can't
       * expand the scroll area (a horizontal scrollbar + dead space). */}
      <div className="pointer-events-none absolute inset-0 overflow-hidden">
        <Orb color="mint" size={520} className="-top-40 right-[-120px]" />
        <Orb color="lavender" size={420} className="bottom-[-160px] left-[-120px]" />
      </div>

      <div className="relative mx-auto w-full max-w-[1200px] px-4 py-6 sm:px-8 sm:py-10 lg:px-12">
        <div className="mb-6 flex flex-wrap items-end justify-between gap-4 sm:mb-10 sm:gap-6">
          <div>
            <div className="caption-uppercase mb-3 text-muted">Workspace</div>
            <h1 className="display-xl">Knowledge bases</h1>
            <p className="mt-3 max-w-[520px] text-body">
              Collections of documents indexed for retrieval-augmented chat.
            </p>
          </div>
          <div className="flex items-center gap-2.5">
            {/* Platform-wide ingestion defaults — superadmin only. */}
            {isSystemAdmin && (
              <button
                type="button"
                className="btn btn-outline"
                onClick={() => setPdOpen(true)}
              >
                <IconParserEngine className="h-4 w-4" />
                {t("pd.title")}
              </button>
            )}
            {/* Distinct SuperAdmin-only public creation (dedicated route). */}
            {isExplicitSuperAdmin && (
              <Link href="/platform/knowledge-bases/new-public" className="btn btn-outline">
                <IconPlus className="h-4 w-4" />
                {t("kbPublic.newPublic")}
              </Link>
            )}
            {isTenantAdmin && hasTenant && (
              <Link href="/platform/knowledge-bases/new" className="btn btn-primary">
                <IconPlus className="h-4 w-4" />
                New knowledge base
              </Link>
            )}
          </div>
        </div>

        {!hasTenant && (
          <p className="body-sm mb-6 rounded-xl border border-hairline bg-surface px-4 py-3 text-body">
            {t("kbPublic.tenantlessNotice")}
          </p>
        )}

        <div className="relative mb-8 max-w-[420px]">
          <IconSearch className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
          <input
            className="input pl-10"
            placeholder="Search knowledge bases…"
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setPubPage(1);
            }}
          />
        </div>

        {kbs === null && hasTenant && (
          <p className="caption mb-6 text-muted-soft">Loading knowledge bases…</p>
        )}

        {hasTenant && (
          <>
            <h2 className="title-sm mb-4">{t("kbPublic.workspaceSection")}</h2>
            {workspaceCards.length === 0 && kbs !== null && !error && (
              <p className="caption mb-6 text-muted-soft">No knowledge bases yet.</p>
            )}
            {renderCards(workspaceCards, {
              append: isTenantAdmin ? (
                <Link
                  href="/platform/knowledge-bases/new"
                  className="flex min-h-[190px] items-center justify-center rounded-[16px] border border-dashed border-hairline-strong text-muted transition-colors hover:border-ink hover:text-ink"
                >
                  <span className="flex items-center gap-2 text-[15px] font-medium">
                    <IconPlus className="h-4 w-4" /> Create knowledge base
                  </span>
                </Link>
              ) : undefined,
            })}
            {invitedCards.length > 0 && (
              <>
                <h2 className="title-sm mb-4 mt-10">{t("kbPublic.invitedSection")}</h2>
                {renderCards(invitedCards)}
              </>
            )}
          </>
        )}

        <div className="mb-4 mt-10">
          <h2 className="title-sm">{t("kbPublic.section")}</h2>
          <p className="caption mt-1 text-muted">{t("kbPublic.sectionDesc")}</p>
        </div>
        {pubError ? (
          <p className="caption mb-6 text-error">{pubError}</p>
        ) : (
          <>
            {renderCards(publicCards, { hideShare: true })}
            {pubTotal === 0 && (
              <p className="caption mb-6 mt-2 text-muted-soft">No public knowledge bases yet.</p>
            )}
            {pubTotal > PUBLIC_PAGE_SIZE && (
              <div className="caption mt-4 flex items-center gap-3 text-muted">
                <button
                  type="button"
                  className="btn btn-outline btn-sm"
                  disabled={pubPage <= 1}
                  onClick={() => setPubPage((p) => Math.max(1, p - 1))}
                >
                  ←
                </button>
                <span>
                  {pubPage} / {pubPages} · {pubTotal}
                </span>
                <button
                  type="button"
                  className="btn btn-outline btn-sm"
                  disabled={pubPage >= pubPages}
                  onClick={() => setPubPage((p) => p + 1)}
                >
                  →
                </button>
              </div>
            )}
          </>
        )}

        <div className="mt-10">
          <button
            type="button"
            onClick={() => setInvitesOpen((v) => !v)}
            className="btn btn-outline btn-sm"
          >
            {invitesOpen ? "Hide shared-with-me invitations" : "Show KBs shared with me"}
          </button>
          {invitesOpen && (
            <div className="mt-4 rounded-xl border border-hairline p-5">
              <MyKBInvites onAccepted={reload} />
            </div>
          )}
        </div>
      </div>

      <Modal
        open={pdOpen}
        title={t("pd.title")}
        onClose={() => setPdOpen(false)}
        width="w-[1240px]"
      >
        <ParseDefaultsEditor />
      </Modal>

      <Modal
        open={inviteKb !== null}
        title="Invite to read"
        onClose={() => setInviteKb(null)}
      >
        {inviteKb && <KBInvitePanel kbId={inviteKb.id} kbName={inviteKb.name} />}
      </Modal>

      <KbShareModal
        kb={shareKb}
        open={shareKb !== null}
        onClose={() => setShareKb(null)}
        canManage={
          !!shareKb &&
          getKBViewerCapabilities(shareKb, {
            activeTenantId: hasTenant ? activeTenantId : "",
            isTenantAdmin,
            isSystemAdmin,
          }).canManage
        }
      />
    </div>
  );
}

/* Compact card timestamp: locale date for recent items, ISO date otherwise.
 * The raw RFC3339 string from the API is too long for the card footer. */
function fmtShortDate(v?: string): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleDateString(undefined, { year: "2-digit", month: "short", day: "numeric" });
}
