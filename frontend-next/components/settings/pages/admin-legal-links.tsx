"use client";

/* SuperAdmin review of provision links pushed by sources other than
 * vbpl.vn (thuvienphapluat…): "khoản X of A is amended by khoản Y of B".
 * Shown as the "Provision links" tab of the legal documents page
 * (admin-legal-documents.tsx). Reviewed per amended document A: a list of
 * the documents with links on the left, A's diagram on the right
 * (components/legal-links/), each provision's wording on hover, a confirm
 * panel per link and bulk approval per amending document. The flat list
 * stays as a second view for browsing decided links. Approve copies a link
 * into the links the current-wording resolver reads; reject keeps it out
 * (the feed's next push does not ask again). Backend:
 * /api/v1/system/legal-link-candidates — SuperAdmin-only. Spec:
 * docs/legal-document-feed-spec.md §9. */

import { useCallback, useEffect, useMemo, useState } from "react";
import { IconRefresh } from "@/components/icons";
import { useToast } from "@/components/toast";
import { ConsolePage } from "@/components/settings/console-page";
import { DataTable } from "@/components/settings/data-table";
import { Pager } from "@/components/settings/pager";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Notice } from "@/components/settings/notice";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { userErrorMessage } from "@/lib/user-error";
import {
  decideLegalLinkCandidate,
  listLegalLinkCandidates,
  listLegalLinkDocuments,
  recheckLegalLinkCandidates,
  type LegalLinkCandidate,
  type LegalLinkCounts,
  type LegalLinkMatchStatus,
  type LegalLinkReviewFilter,
  type LegalLinkTargetGroup,
} from "@/lib/api/legal-link-candidates";
import { provisionLabel } from "@/lib/legal-provision-label";
import { amenderChips, buildLinkGraph, defaultFocus, isApprovable, nextApprovable } from "@/lib/legal-link-graph";
import { LinkDiagram, LinkDiagramLegend } from "@/components/legal-links/link-diagram";
import { LinkConfirmPanel, MatchBadge } from "@/components/legal-links/link-confirm-panel";
import { LinkGroupPanel } from "@/components/legal-links/link-group-panel";
import { clearProvisionTextCache, docName } from "@/components/legal-links/provision-text";

const PAGE_SIZE = 50;
/** Candidates of one document read for its diagram (pages of 500). */
const DIAGRAM_MAX = 2000;

const REVIEW_FILTERS: { key: LegalLinkReviewFilter; labelKey: LocaleKey }[] = [
  { key: "pending", labelKey: "llink.filter.pending" },
  { key: "approved", labelKey: "llink.filter.approved" },
  { key: "rejected", labelKey: "llink.filter.rejected" },
  { key: "auto", labelKey: "llink.filter.auto" },
  { key: "all", labelKey: "llink.filter.all" },
];

const MATCH_STATUSES: LegalLinkMatchStatus[] = ["matched", "doc_only", "target_missing", "unresolved", "duplicate"];

type View = "diagram" | "list";
type Panel = { kind: "edge"; id: number; edit?: boolean } | { kind: "group"; id: string } | null;

export default function LegalLinksSection({ onDenied }: { onDenied?: () => void }) {
  const { t } = useT();
  const toast = useToast();
  const [view, setView] = useState<View>("diagram");
  const [docs, setDocs] = useState<LegalLinkTargetGroup[]>([]);
  const [docsLoading, setDocsLoading] = useState(false);
  const [selected, setSelected] = useState<string>("");
  const [cands, setCands] = useState<LegalLinkCandidate[]>([]);
  const [listCands, setListCands] = useState<LegalLinkCandidate[]>([]);
  const [candsLoading, setCandsLoading] = useState(false);
  const [error, setError] = useState("");
  const [undecidedOnly, setUndecidedOnly] = useState(true);
  /** per document: the focused amending document (null = all); unset → defaultFocus */
  const [focusByDoc, setFocusByDoc] = useState<Record<string, string | null>>({});
  const [collapsed, setCollapsed] = useState<Record<string, Set<string>>>({});
  const [panel, setPanel] = useState<Panel>(null);
  const [skipped, setSkipped] = useState<Set<number>>(new Set());
  const [rechecking, setRechecking] = useState(false);
  const [listKey, setListKey] = useState(0);

  const fail = useCallback(
    (e: unknown, key: LocaleKey) => {
      if (e instanceof ApiError && e.status === 403) onDenied?.();
      else setError(userErrorMessage(e, t, key));
    },
    [onDenied, t],
  );

  const loadDocs = useCallback(async () => {
    setDocsLoading(true);
    try {
      const res = await listLegalLinkDocuments();
      const items = res.data?.items ?? [];
      setDocs(items);
      setSelected((cur) => {
        if (cur && items.some((d) => d.target_doc_id === cur)) return cur;
        const first = items.find((d) => d.pending_approvable > 0) ?? items.find((d) => d.pending_blocked > 0) ?? items[0];
        return first?.target_doc_id ?? "";
      });
    } catch (e) {
      fail(e, "llink.loadFailed");
    } finally {
      setDocsLoading(false);
    }
  }, [fail]);

  const loadCands = useCallback(
    async (docId: string) => {
      if (!docId) {
        setCands([]);
        return;
      }
      setCandsLoading(true);
      try {
        const all: LegalLinkCandidate[] = [];
        for (let page = 1; all.length < DIAGRAM_MAX; page++) {
          const res = await listLegalLinkCandidates({ reviewStatus: "all", legalDocumentId: docId, page, pageSize: 500 });
          const items = res.data?.items ?? [];
          all.push(...items);
          if (items.length < 500 || all.length >= (res.data?.total ?? 0)) break;
        }
        setCands(all);
      } catch (e) {
        fail(e, "llink.loadFailed");
      } finally {
        setCandsLoading(false);
      }
    },
    [fail],
  );

  useEffect(() => {
    void loadDocs();
  }, [loadDocs]);
  useEffect(() => {
    setSkipped(new Set());
    void loadCands(selected);
  }, [selected, loadCands]);

  const refresh = useCallback(async () => {
    setError("");
    await Promise.all([loadDocs(), loadCands(selected)]);
    setListKey((k) => k + 1);
  }, [loadDocs, loadCands, selected]);

  const doc = docs.find((d) => d.target_doc_id === selected);
  const docLabel = doc ? docName(doc.document, doc.target_doc_id) : "";
  const byId = useMemo(() => new Map(cands.map((c) => [c.id, c])), [cands]);
  const chips = useMemo(() => amenderChips(cands), [cands]);
  const focus = selected in focusByDoc ? focusByDoc[selected] : defaultFocus(chips);
  const setFocus = (id: string | null) => setFocusByDoc((cur) => ({ ...cur, [selected]: id }));
  const graph = useMemo(
    () => buildLinkGraph(cands, { undecidedOnly, focusGroup: focus, collapsedGroups: collapsed[selected] }),
    [cands, undecidedOnly, focus, collapsed, selected],
  );
  /** every undecided link of A: where "review in turn" goes once the
   * focused document is done */
  const allUndecided = useMemo(() => buildLinkGraph(cands, { undecidedOnly: true }), [cands]);

  const toggleGroup = (gid: string) => {
    setCollapsed((cur) => {
      const now = new Set(cur[selected] ?? graph.groups.filter((g) => g.collapsed).map((g) => g.id));
      if (now.has(gid)) now.delete(gid);
      else now.add(gid);
      return { ...cur, [selected]: now };
    });
  };
  const setAllCollapsed = (fold: boolean) =>
    setCollapsed((cur) => ({ ...cur, [selected]: new Set(fold ? graph.groups.map((g) => g.id) : []) }));

  const recheck = async () => {
    setRechecking(true);
    setError("");
    try {
      const res = await recheckLegalLinkCandidates();
      const r = res.data;
      toast.success(
        r && (r.resolved > 0 || r.rematched > 0)
          ? t("llink.recheckDone", { n: r.resolved, m: r.rematched, auto: r.auto_approved })
          : t("llink.recheckNone", { n: (r?.checked ?? 0) + (r?.rematch_checked ?? 0) }),
      );
      clearProvisionTextCache();
      await refresh();
    } catch (e) {
      fail(e, "llink.recheckFailed");
    } finally {
      setRechecking(false);
    }
  };

  /** The next link to decide: in the focused document first, then in the
   * next document (the focus follows it). */
  const findNext = (after: number | null, skip: Set<number>) => {
    const here = nextApprovable(graph, after, skip);
    if (here || !focus) return here;
    return nextApprovable(allUndecided, null, skip);
  };
  const goTo = (next: ReturnType<typeof findNext>) => {
    if (next && focus && next.groupId !== focus) setFocus(next.groupId);
    setPanel(next ? { kind: "edge", id: next.candidateId } : null);
  };
  const openNext = (after: number | null, skip: Set<number>) => goTo(findNext(after, skip));

  const afterDecision = async (id: number, action: "approve" | "reject") => {
    toast.success(t(action === "approve" ? "llink.approved" : "llink.rejected"));
    const skip = new Set(skipped).add(id);
    // the next link to decide in the graph as it was, then reload
    goTo(view === "diagram" ? findNext(id, skip) : null);
    await refresh();
  };

  const pendingEdges = graph.edges.filter((e) => e.state === "pending");
  const panelCand =
    panel?.kind === "edge"
      ? view === "list"
        ? listCands.find((c) => c.id === panel.id)
        : byId.get(panel.id)
      : undefined;
  const panelGroup = panel?.kind === "group" ? graph.groups.find((g) => g.id === panel.id) : undefined;
  const position = (() => {
    if (!panelCand || panelCand.review_status !== "pending") return undefined;
    const i = pendingEdges.findIndex((e) => e.candidateId === panelCand.id);
    return i >= 0 ? t("llink.panel.position", { i: i + 1, n: pendingEdges.length }) : undefined;
  })();

  return (
    <ConsolePage size="wide">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <span className="caption max-w-[640px] text-muted">{t("llink.desc")}</span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <PillGroup ariaLabel={t("llink.view.aria")}>
            <Pill active={view === "diagram"} onClick={() => setView("diagram")}>
              {t("llink.view.diagram")}
            </Pill>
            <Pill active={view === "list"} onClick={() => setView("list")}>
              {t("llink.view.list")}
            </Pill>
          </PillGroup>
          <button
            type="button"
            className="btn btn-outline btn-sm h-8"
            onClick={() => void recheck()}
            disabled={rechecking}
            title={t("llink.recheckHint")}
          >
            {rechecking ? t("common.loading") : t("llink.recheck")}
          </button>
          <button
            className="btn btn-outline btn-sm h-8 w-8 p-0"
            onClick={() => void refresh()}
            disabled={docsLoading || candsLoading}
            title={t("common.refresh")}
            aria-label={t("common.refresh")}
          >
            <IconRefresh className={`h-3.5 w-3.5 ${docsLoading || candsLoading ? "animate-spin" : ""}`} />
          </button>
        </div>
      </div>

      {error && <Notice tone="error" className="mb-3">{error}</Notice>}

      {view === "list" ? (
        <LinkList key={listKey} onDenied={onDenied} onOpen={(id, edit) => setPanel({ kind: "edge", id, edit })} onCands={setListCands} />
      ) : (
        <div className="grid gap-4 lg:grid-cols-[250px_minmax(0,1fr)]">
          <nav aria-label={t("llink.docs.aria")} className="flex flex-col gap-1.5 lg:max-h-[75vh] lg:overflow-y-auto">
            {docsLoading && docs.length === 0 && <p className="caption text-muted">{t("common.loading")}</p>}
            {!docsLoading && docs.length === 0 && <p className="caption text-muted">{t("llink.empty")}</p>}
            {docs.map((d) => {
              const on = d.target_doc_id === selected;
              return (
                <button
                  key={d.target_doc_id}
                  type="button"
                  aria-current={on ? "true" : undefined}
                  onClick={() => {
                    setSelected(d.target_doc_id);
                    setPanel(null);
                  }}
                  className={`rounded-[12px] border px-3 py-2.5 text-left transition-colors ${
                    on ? "border-ink bg-surface-strong" : "border-hairline hover:bg-surface-strong"
                  }`}
                >
                  <div className="text-[13px] font-semibold text-ink">{docName(d.document, d.target_doc_id)}</div>
                  {d.document?.title && <div className="caption mt-0.5 line-clamp-2 text-body">{d.document.title}</div>}
                  <div className="mt-1.5 flex flex-wrap gap-1">
                    {d.pending_approvable > 0 && (
                      <span className="badge-pill badge-warning text-[10.5px] tabular-nums">
                        {t("llink.docs.toReview", { n: d.pending_approvable })}
                      </span>
                    )}
                    {d.pending_blocked > 0 && (
                      <span className="badge-pill badge-neutral text-[10.5px] tabular-nums">
                        {t("llink.docs.waiting", { n: d.pending_blocked })}
                      </span>
                    )}
                    {d.pending_approvable === 0 && d.pending_blocked === 0 && (
                      <span className="badge-pill badge-success text-[10.5px]">{t("llink.docs.done")}</span>
                    )}
                  </div>
                  {!d.held && <div className="caption mt-1 text-muted">{t("llink.docs.notHeld")}</div>}
                </button>
              );
            })}
          </nav>

          <section className="min-w-0">
            {doc && (
              <>
                <div className="mb-3 flex flex-wrap items-start gap-3">
                  <div className="min-w-0 flex-1">
                    <h3 className="text-[15px] font-semibold text-ink">{docLabel}</h3>
                    {doc.document?.title && <p className="caption text-body">{doc.document.title}</p>}
                    <p className="caption mt-0.5 text-muted">
                      {t("llink.docs.summary", {
                        review: doc.pending_approvable,
                        waiting: doc.pending_blocked,
                        approved: doc.approved,
                        rejected: doc.rejected,
                      })}
                    </p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    {pendingEdges.length > 0 && (
                      <button type="button" className="btn btn-primary btn-sm" onClick={() => openNext(null, skipped)}>
                        {t("llink.diagram.start", { n: pendingEdges.length })}
                      </button>
                    )}
                    {!focus && (
                      <>
                        <button type="button" className="btn btn-ghost btn-sm" onClick={() => setAllCollapsed(false)}>
                          {t("llink.diagram.expandAll")}
                        </button>
                        <button type="button" className="btn btn-ghost btn-sm" onClick={() => setAllCollapsed(true)}>
                          {t("llink.diagram.collapseAll")}
                        </button>
                      </>
                    )}
                    <label className="caption inline-flex items-center gap-1.5 text-body">
                      <input type="checkbox" checked={undecidedOnly} onChange={(e) => setUndecidedOnly(e.target.checked)} />
                      {t("llink.diagram.undecidedOnly")}
                    </label>
                  </div>
                </div>
                {!doc.held && (
                  <Notice tone="info" className="mb-3">
                    {t("llink.docs.notHeldHelp", { doc: docLabel })}
                  </Notice>
                )}
                {chips.length > 0 && (
                  <PillGroup ariaLabel={t("llink.chips.aria")} className="mb-3 flex-wrap">
                    {chips.map((c) => (
                      <Pill key={c.id} active={focus === c.id} onClick={() => setFocus(c.id)}>
                        <span className="font-semibold">{c.number || "—"}</span>
                        <span className="ml-1.5 text-[11px] opacity-80">
                          {c.approvable > 0
                            ? t("llink.docs.toReview", { n: c.approvable })
                            : c.blocked > 0
                              ? t("llink.chips.waiting")
                              : c.pending > 0
                                ? t("llink.chips.needFix", { n: c.pending })
                                : t("llink.chips.done")}
                        </span>
                      </Pill>
                    ))}
                    <Pill active={!focus} onClick={() => setFocus(null)}>
                      {t("llink.chips.all")}
                    </Pill>
                  </PillGroup>
                )}
                <div className="mb-3">
                  <LinkDiagramLegend />
                </div>
                <div data-diagram-scroll className="max-h-[75vh] overflow-y-auto rounded-[14px] border border-hairline bg-canvas-soft p-3">
                  {candsLoading && cands.length === 0 ? (
                    <p className="caption py-8 text-center text-muted">{t("common.loading")}</p>
                  ) : graph.edges.length === 0 ? (
                    <p className="caption py-8 text-center text-muted">
                      {undecidedOnly && cands.length > 0 ? t("llink.diagram.allDecided") : t("llink.empty")}
                    </p>
                  ) : (
                    <>
                      <div className="caption mb-2 flex justify-between text-muted">
                        <span>{t("llink.diagram.leftHead", { doc: docLabel })}</span>
                        <span>{focus ? t("llink.diagram.rightHeadOne") : t("llink.diagram.rightHead")}</span>
                      </div>
                      <LinkDiagram
                        graph={graph}
                        cands={byId}
                        targetDocId={selected}
                        targetLabel={docLabel}
                        selectedEdge={panel?.kind === "edge" ? panel.id : null}
                        onOpenEdge={(id) => setPanel({ kind: "edge", id })}
                        onOpenGroup={(id) => setPanel({ kind: "group", id })}
                        onToggleGroup={toggleGroup}
                      />
                    </>
                  )}
                </div>
                <p className="caption mt-2 text-muted-soft">{t("llink.diagram.help")}</p>
              </>
            )}
          </section>
        </div>
      )}

      {panelCand && (
        <LinkConfirmPanel
          c={panelCand}
          targetHeld={view === "list" ? true : doc?.held ?? true}
          position={position}
          startEditing={panel?.kind === "edge" && !!panel.edit}
          onClose={() => setPanel(null)}
          onDecided={(action) => void afterDecision(panelCand.id, action)}
          onSkip={
            view === "diagram" && isApprovable(panelCand)
              ? () => {
                  const skip = new Set(skipped).add(panelCand.id);
                  setSkipped(skip);
                  openNext(panelCand.id, skip);
                }
              : undefined
          }
        />
      )}
      {panelGroup && (
        <LinkGroupPanel
          group={panelGroup}
          cands={panelGroup.edgeIds.map((id) => byId.get(Number(id.slice(2)))).filter((c): c is LegalLinkCandidate => !!c)}
          onClose={() => setPanel(null)}
          onOpenEdge={(id) => setPanel({ kind: "edge", id })}
          onChanged={refresh}
          onRecheck={() => void recheck()}
          rechecking={rechecking}
        />
      )}
    </ConsolePage>
  );
}

/* The flat list (second view): every candidate with the filters, for
 * browsing decided links; a row opens the same confirm panel. */
function LinkList({
  onDenied,
  onOpen,
  onCands,
}: {
  onDenied?: () => void;
  onOpen: (id: number, edit?: boolean) => void;
  onCands: (c: LegalLinkCandidate[]) => void;
}) {
  const { t, locale } = useT();
  const toast = useToast();
  const [review, setReview] = useState<LegalLinkReviewFilter>("pending");
  const [match, setMatch] = useState<LegalLinkMatchStatus | "">("");
  const [items, setItems] = useState<LegalLinkCandidate[]>([]);
  const [total, setTotal] = useState(0);
  const [counts, setCounts] = useState<LegalLinkCounts>({});
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<number | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listLegalLinkCandidates({ reviewStatus: review, matchStatus: match, page, pageSize: PAGE_SIZE });
      const list = res.data?.items ?? [];
      setItems(list);
      onCands(list); // the confirm panel reads its candidate from here
      setTotal(res.data?.total ?? 0);
      setCounts(res.data?.counts ?? {});
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) onDenied?.();
      else setError(userErrorMessage(e, t, "llink.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [review, match, page, t, onDenied, onCands]);

  useEffect(() => {
    void load();
  }, [load]);

  const decide = async (c: LegalLinkCandidate, action: "approve" | "reject") => {
    setBusy(c.id);
    setError("");
    try {
      await decideLegalLinkCandidate(c.id, { action });
      toast.success(t(action === "approve" ? "llink.approved" : "llink.rejected"));
      await load();
    } catch (e) {
      setError(userErrorMessage(e, t, "llink.decideFailed"));
    } finally {
      setBusy(null);
    }
  };

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <PillGroup ariaLabel={t("common.filters")} className="flex-wrap">
          {REVIEW_FILTERS.map((f) => (
            <Pill
              key={f.key}
              active={review === f.key}
              onClick={() => {
                setReview(f.key);
                setPage(1);
              }}
            >
              {t(f.labelKey)}
              {f.key !== "all" && (counts[f.key] ?? 0) > 0 ? ` (${counts[f.key]})` : ""}
            </Pill>
          ))}
        </PillGroup>
        <select
          className="input ml-auto h-8 py-0 text-[12.5px]"
          value={match}
          aria-label={t("llink.col.match")}
          onChange={(e) => {
            setMatch(e.target.value as LegalLinkMatchStatus | "");
            setPage(1);
          }}
        >
          <option value="">{t("llink.match.any")}</option>
          {MATCH_STATUSES.map((s) => (
            <option key={s} value={s}>
              {t(`llink.match.${s}` as LocaleKey)}
            </option>
          ))}
        </select>
      </div>

      {error && <Notice tone="error" className="mb-3">{error}</Notice>}

      <DataTable<LegalLinkCandidate>
        columns={[
          { key: "target", header: t("llink.col.target"), className: "min-w-[220px]" },
          { key: "op", header: t("llink.col.op"), className: "w-[130px]" },
          { key: "amender", header: t("llink.col.amender"), className: "min-w-[220px]" },
          { key: "match", header: t("llink.col.match"), className: "min-w-[200px]" },
          { key: "action", header: t("llink.col.action"), className: "w-[220px]" },
        ]}
        rows={items}
        rowKey={(c) => String(c.id)}
        renderRow={(c) => {
          const pending = c.review_status === "pending";
          const canApprove = isApprovable(c);
          return (
            <>
              <td className="px-5 py-3.5">
                <div className="text-[13px] font-semibold text-ink">{provisionLabel(c.target_path, locale) || "—"}</div>
                <div className="caption mt-0.5 line-clamp-2 text-body" title={c.target?.title}>
                  {docName(c.target, c.target_doc_id)}
                </div>
              </td>
              <td className="px-5 py-3.5">
                <span className="badge-pill badge-neutral inline-block w-fit text-[10.5px]">{t(`llink.op.${c.op}` as LocaleKey)}</span>
                {c.eff_from && (
                  <div className="caption mt-1 tabular-nums text-muted">
                    {t("llink.effFrom", { day: c.eff_from.slice(0, 10).split("-").reverse().join("/") })}
                  </div>
                )}
              </td>
              <td className="px-5 py-3.5">
                <div className="text-[13px] font-semibold text-ink">
                  {c.source_path ? provisionLabel(c.source_path, locale) : t("llink.noPath")}
                </div>
                <div className="caption mt-0.5 line-clamp-2 text-body" title={c.amender?.title}>
                  {docName(c.amender, c.amender_doc_number)}
                </div>
              </td>
              <td className="px-5 py-3.5">
                <MatchBadge c={c} />
                {c.note && (
                  <div className="caption mt-1 line-clamp-2 text-muted" title={c.note}>
                    {c.note}
                  </div>
                )}
              </td>
              <td className="px-5 py-3.5">
                <div className="flex flex-wrap gap-1.5">
                  <button type="button" className="btn btn-outline btn-sm py-0.5 text-[12px]" onClick={() => onOpen(c.id)}>
                    {t("llink.view.open")}
                  </button>
                  {pending ? (
                    <>
                      <button
                        type="button"
                        className="btn btn-primary btn-sm py-0.5 text-[12px]"
                        disabled={busy === c.id || !canApprove}
                        title={canApprove ? undefined : t("llink.needFix")}
                        onClick={() => void decide(c, "approve")}
                      >
                        {t("llink.approve")}
                      </button>
                      <button
                        type="button"
                        className="btn btn-danger btn-sm py-0.5 text-[12px]"
                        disabled={busy === c.id}
                        onClick={() => void decide(c, "reject")}
                      >
                        {t("llink.reject")}
                      </button>
                    </>
                  ) : (
                    <span className="caption self-center text-muted">
                      {t(`llink.filter.${c.review_status}` as LocaleKey)}
                      {c.reviewed_by ? ` · ${c.reviewed_by === "auto:matched" ? t("llink.autoReviewer") : c.reviewed_by}` : ""}
                    </span>
                  )}
                </div>
              </td>
            </>
          );
        }}
        rowClassName="align-top"
        loading={loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("llink.empty")}
        minWidth="min-w-[1000px]"
      />

      <Pager page={page} pageCount={pages} onPage={setPage} loading={loading} />
    </>
  );
}
