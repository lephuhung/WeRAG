"use client";

/* SuperAdmin "Bộ kỹ năng chuyên môn": the domain packs shipped in this build and,
 * per pack, the single place to see what must be updated — its cited
 * documents ("Văn bản trích dẫn") checked against the legal catalogue, its
 * formulas ("Công thức") with the state of their legal basis, and the
 * agents using it. Packs are files in the repo (domain-packs/<name>); only
 * a formula's wording can be edited here (the calculation stays Go code,
 * see domain-pack-formula-editor). "Đối chiếu danh mục" records legal document
 * requests for texts no public KB holds (the legal requests queue).
 * Backend: /api/v1/system/domain-packs (+ /:name, /:name/reconcile,
 * /:name/formulas/:id). The
 * open pack lives in ?pack= so the detail is deep-linkable. */

import { useCallback, useEffect, useState, type ReactNode } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { IconRefresh } from "@/components/icons";
import { SystemSection } from "@/components/settings/system-section";
import { Notice } from "@/components/settings/notice";
import { Loading } from "@/components/settings/loading";
import { DataTable } from "@/components/settings/data-table";
import { TabStrip } from "@/components/settings/tab-strip";
import { Pill, PillGroup } from "@/components/settings/pill";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { getCurrentUser } from "@/lib/api/auth";
import { formatDate, formatDateTime } from "@/lib/datetime";
import { FormulaEditor } from "@/components/settings/domain-pack-formula-editor";
import { normalizeDistribution } from "@/lib/agent-tenant-scope";
import { formatPriceVND } from "@/lib/api/usage";
import { adminSectionHref } from "@/components/admin/admin-routes";
import {
  getDomainPack,
  listDomainPacks,
  reconcileDomainPack,
  type DomainPackAgent,
  type DomainPackCatalogueDocument,
  type DomainPackDetail,
  type DomainPackDocumentView,
  type DomainPackFormulaView,
  type DomainPackReconcileResult,
  type DomainPackSummary,
} from "@/lib/api/domain-packs-admin";
import {
  documentTitle,
  formatFormulaValue,
  heldInKB,
  reconcileRequests,
  visibleDocuments,
} from "@/lib/domain-pack-view";

const REQUESTS_HREF = `${adminSectionHref("legal-documents")}&view=requests`;

type Tab = "documents" | "formulas" | "agents";

const TABS: { key: Tab; labelKey: LocaleKey }[] = [
  { key: "documents", labelKey: "dpk.tab.documents" },
  { key: "formulas", labelKey: "dpk.tab.formulas" },
  { key: "agents", labelKey: "dpk.tab.agents" },
];

const WARN_ROW = "bg-warning/10";
const BADGE_WARN = "badge-warning";
const BADGE_OK = "badge-success";
const BADGE_BAD = "badge-error";
const BADGE_NEUTRAL = "badge-neutral";

function statusTone(status: string): string {
  if (status.startsWith("Còn hiệu lực")) return BADGE_OK;
  if (status === "Hết hiệu lực toàn bộ" || status.startsWith("Ngưng")) return BADGE_BAD;
  if (status.startsWith("Hết hiệu lực")) return BADGE_WARN;
  return BADGE_NEUTRAL;
}

function errorText(e: unknown, fallback: string): string {
  if (e instanceof ApiError || e instanceof Error) return e.message || fallback;
  return fallback;
}

export default function AdminDomainPacks() {
  const { t } = useT();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const searchParams = useSearchParams();
  const pack = searchParams.get("pack") ?? "";

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const me = await getCurrentUser();
        if (alive) setAllowed(me.data?.user?.is_system_admin === true);
      } catch {
        if (alive) setAllowed(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  return (
    <SystemSection allowed={allowed} title={t("systemNav.domainPacks")} deniedMessage={t("dpk.denied")}>
      {pack ? <PackDetail name={pack} /> : <PackList />}
    </SystemSection>
  );
}

/* ---- list ------------------------------------------------------------------ */

function usePackHref() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  return (name: string) => {
    const q = new URLSearchParams(searchParams.toString());
    if (name) q.set("pack", name);
    else q.delete("pack");
    return `${pathname}?${q.toString()}`;
  };
}

function PackList() {
  const { t } = useT();
  const packHref = usePackHref();
  const [items, setItems] = useState<DomainPackSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let alive = true;
    setError("");
    (async () => {
      try {
        const res = await listDomainPacks();
        if (alive) setItems(res.data ?? []);
      } catch (e) {
        if (alive) setError(errorText(e, t("dpk.loadFailed")));
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [t, reloadKey]);

  return (
    <div>
      <p className="body-sm mb-4 max-w-[860px] text-muted">{t("dpk.intro")}</p>
      {error && (
        <Notice
          tone="error"
          className="mb-3"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => setReloadKey((k) => k + 1)}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}
      <DataTable<DomainPackSummary>
        columns={[
          { key: "pack", header: t("dpk.col.pack"), className: "min-w-[220px]" },
          { key: "version", header: t("dpk.col.version"), className: "w-[90px]" },
          { key: "asOf", header: t("dpk.col.asOf"), className: "w-[120px]" },
          { key: "reviewBy", header: t("dpk.col.reviewBy"), className: "w-[170px]" },
          { key: "documents", header: t("dpk.col.documents"), className: "w-[100px]" },
          { key: "formulas", header: t("dpk.col.formulas"), className: "w-[100px]" },
          { key: "agents", header: t("dpk.col.agents"), className: "w-[90px]" },
          { key: "attribution", header: t("dpk.col.attribution"), className: "min-w-[220px]" },
        ]}
        rows={items}
        rowKey={(p) => p.name}
        loading={loading}
        loadingLabel={t("usrp.loading")}
        emptyLabel={t("dpk.empty")}
        renderRow={(p) => (
          <>
            <td className="px-5 py-3.5 align-top">
              <Link href={packHref(p.name)} className="text-[13px] font-semibold text-accent hover:underline">
                {p.name}
              </Link>
              {p.description && <div className="caption mt-0.5 line-clamp-2 text-body">{p.description}</div>}
            </td>
            <td className="px-5 py-3.5 align-top text-[13px] tabular-nums text-ink">{p.version}</td>
            <td className="px-5 py-3.5 align-top caption tabular-nums text-body">{formatDate(p.as_of)}</td>
            <td className="px-5 py-3.5 align-top">
              <span className="caption tabular-nums text-body">{formatDate(p.review_by)}</span>
              {p.overdue && <span className={`badge-pill ml-2 text-[10.5px] ${BADGE_BAD}`}>{t("dpk.overdue")}</span>}
            </td>
            <td className="px-5 py-3.5 align-top text-[13px] tabular-nums text-ink">{p.document_count}</td>
            <td className="px-5 py-3.5 align-top text-[13px] tabular-nums text-ink">{p.formula_count}</td>
            <td className="px-5 py-3.5 align-top text-[13px] tabular-nums text-ink">{p.agent_count}</td>
            <td className="px-5 py-3.5 align-top caption text-body">{p.attribution}</td>
          </>
        )}
      />
    </div>
  );
}

/* ---- detail ---------------------------------------------------------------- */

function PackDetail({ name }: { name: string }) {
  const { t } = useT();
  const router = useRouter();
  const packHref = usePackHref();
  const [detail, setDetail] = useState<DomainPackDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [tab, setTab] = useState<Tab>("documents");
  const [reconciling, setReconciling] = useState(false);
  const [result, setResult] = useState<DomainPackReconcileResult | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await getDomainPack(name);
      setDetail(res.data ?? null);
    } catch (e) {
      setError(errorText(e, t("dpk.loadFailed")));
    } finally {
      setLoading(false);
    }
  }, [name, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const reconcile = async () => {
    setReconciling(true);
    setError("");
    try {
      const res = await reconcileDomainPack(name);
      setResult(res.data ?? null);
      await load();
    } catch (e) {
      setError(errorText(e, t("dpk.reconcileFailed")));
    } finally {
      setReconciling(false);
    }
  };

  const warnDocs = detail?.documents.filter((d) => d.warning).length ?? 0;
  const warnFormulas = detail?.formulas.filter((f) => f.warning).length ?? 0;

  return (
    <div>
      <button type="button" className="btn btn-ghost btn-sm mb-3 px-2" onClick={() => router.push(packHref(""))}>
        ← {t("dpk.back")}
      </button>

      {error && (
        <Notice
          tone="error"
          className="mb-3"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}
      {!detail ? (
        <Loading label={loading ? t("usrp.loading") : t("dpk.loadFailed")} className="py-12" />
      ) : (
        <>
          <PackHeader detail={detail} reconciling={reconciling} onReconcile={() => void reconcile()} onReload={() => void load()} loading={loading} />
          {result && <ReconcilePanel result={result} onClose={() => setResult(null)} />}

          <TabStrip
            className="mb-4 mt-5"
            tabs={TABS.map(({ key, labelKey }) => {
              const count = key === "documents" ? warnDocs : key === "formulas" ? warnFormulas : 0;
              return {
                id: key,
                label: t(labelKey),
                badge: count > 0 ? <span className={`badge-pill text-[10.5px] tabular-nums ${BADGE_WARN}`}>{count}</span> : undefined,
              };
            })}
            value={tab}
            onChange={setTab}
            ariaLabel={t("dpk.col.pack")}
          />

          {tab === "documents" && (
            <div id="tabpanel-documents" role="tabpanel" aria-labelledby="tab-documents">
              <DocumentsTab docs={detail.documents} />
            </div>
          )}
          {tab === "formulas" && (
            <div id="tabpanel-formulas" role="tabpanel" aria-labelledby="tab-formulas">
              <FormulasTab
                pack={detail.name}
                formulas={detail.formulas}
                catalogueNumbers={detail.documents.map((d) => d.number)}
                onSaved={setDetail}
              />
            </div>
          )}
          {tab === "agents" && (
            <div id="tabpanel-agents" role="tabpanel" aria-labelledby="tab-agents">
              <AgentsTab detail={detail} />
            </div>
          )}
        </>
      )}
    </div>
  );
}

function PackHeader({
  detail,
  reconciling,
  loading,
  onReconcile,
  onReload,
}: {
  detail: DomainPackDetail;
  reconciling: boolean;
  loading: boolean;
  onReconcile: () => void;
  onReload: () => void;
}) {
  const { t } = useT();
  return (
    <div className="card divide-y divide-hairline overflow-hidden shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-4 p-5 sm:p-6">
        <div className="min-w-0">
          <h3 className="title-md break-words">
            {detail.name} <span className="caption tabular-nums text-muted">v{detail.version}</span>
          </h3>
          {detail.description && <p className="body-sm mt-3 max-w-[900px] leading-relaxed text-body">{detail.description}</p>}
        </div>
        <div className="flex items-center gap-2">
          <button type="button" className="btn btn-outline btn-sm h-8 w-8 p-0" onClick={onReload} disabled={loading} title={t("dpk.reload")} aria-label={t("dpk.reload")}>
            <IconRefresh className="h-4 w-4" />
          </button>
          <button type="button" className="btn btn-primary btn-sm" onClick={onReconcile} disabled={reconciling}>
            {reconciling ? t("dpk.reconciling") : t("dpk.reconcile")}
          </button>
        </div>
      </div>
      <dl className="grid grid-cols-1 gap-4 p-5 text-[13px] sm:grid-cols-3 sm:gap-6 sm:p-6">
        <Field label={t("dpk.col.asOf")} value={formatDate(detail.as_of)} />
        <Field
          label={t("dpk.col.reviewBy")}
          value={
            <>
              {formatDate(detail.review_by)}
              {detail.overdue && <span className={`badge-pill ml-2 text-[10.5px] ${BADGE_BAD}`}>{t("dpk.overdue")}</span>}
            </>
          }
        />
        <Field label={t("dpk.licence")} value={detail.licence} />
      </dl>
      <dl className="p-5 text-[13px] sm:p-6">
        <Field label={t("dpk.requiredTools")} value={detail.required_tools.length ? <div className="flex flex-wrap gap-2">{detail.required_tools.map((tool) => <code key={tool} className="max-w-full break-all rounded-md border border-hairline bg-surface-strong/60 px-2 py-1 text-xs">{tool}</code>)}</div> : "—"} />
      </dl>
      {detail.draft_documents.length > 0 && (
        <div className="p-5 sm:p-6">
          <div className="caption-uppercase mb-3 text-muted">{t("dpk.drafts")}</div>
          <ul className="list-disc space-y-2 pl-4 text-[13px] leading-relaxed text-body marker:text-muted-soft">
            {detail.draft_documents.map((d) => (
              <li key={d.title}>
                {d.title} — {d.version}
                {d.date ? `, ${formatDate(d.date)}` : ""}
              </li>
            ))}
          </ul>
        </div>
      )}
      {detail.errors.length > 0 && (
        <div className="caption p-5 text-error sm:p-6">
          {detail.errors.map((e) => `${e.number}: ${e.error}`).join(" · ")}
        </div>
      )}
    </div>
  );
}

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="caption text-muted">{label}</dt>
      <dd className="mt-1 break-words leading-relaxed text-ink">{value || "—"}</dd>
    </div>
  );
}

function ReconcilePanel({ result, onClose }: { result: DomainPackReconcileResult; onClose: () => void }) {
  const { t } = useT();
  const { created, existing } = reconcileRequests(result);
  return (
    <div className="card mt-4 border-accent/30 p-4 shadow-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="text-[13px] text-ink">
          {t("dpk.result.summary", {
            found: result.found.length,
            missing: result.missing.length,
            ambiguous: result.ambiguous.length,
          })}
        </div>
        <button type="button" className="btn btn-ghost btn-sm py-0.5 text-[12px]" onClick={onClose}>
          {t("dpk.result.close")}
        </button>
      </div>
      <div className="mt-2 text-[13px]">
        {created.length === 0 ? (
          <span className="text-muted">{t("dpk.result.noneCreated")}</span>
        ) : (
          <>
            <span className="font-medium text-ink">{t("dpk.result.created", { count: created.length })}</span>
            <ul className="caption mt-1 space-y-0.5 text-body">
              {created.map((r) => (
                <li key={r.request.id}>
                  {r.number}
                  {r.title ? ` — ${r.title}` : ""}
                </li>
              ))}
            </ul>
          </>
        )}
        {existing.length > 0 && (
          <div className="caption mt-2 text-muted">
            {t("dpk.result.existing", { count: existing.length })}{" "}
            {existing.map((r) => `${r.number} (${r.request.status})`).join(", ")}
          </div>
        )}
        {result.errors.length > 0 && (
          <div className="caption mt-2 text-error">{result.errors.map((e) => `${e.number}: ${e.error}`).join(" · ")}</div>
        )}
        <Link href={REQUESTS_HREF} className="caption mt-2 inline-block text-accent hover:underline">
          {t("dpk.result.openQueue")}
        </Link>
      </div>
    </div>
  );
}

function DocumentsTab({ docs }: { docs: DomainPackDocumentView[] }) {
  const { t } = useT();
  const [onlyWarnings, setOnlyWarnings] = useState(false);
  const rows = visibleDocuments(docs, onlyWarnings);
  return (
    <>
      <PillGroup className="mb-3" ariaLabel={t("common.filters")}>
        <Pill active={!onlyWarnings} onClick={() => setOnlyWarnings(false)}>
          {t("common.all")}
        </Pill>
        <Pill active={onlyWarnings} onClick={() => setOnlyWarnings(true)}>
          {t("dpk.onlyWarnings")}
        </Pill>
      </PillGroup>
      <DataTable<DomainPackDocumentView>
        minWidth="min-w-[1100px]"
        columns={[
          { key: "document", header: t("dpk.doc.document"), className: "w-[20%]" },
          { key: "articles", header: t("dpk.doc.articles"), className: "w-[11%]" },
          { key: "status", header: t("dpk.doc.status"), className: "w-[14%]" },
          { key: "inkb", header: t("dpk.doc.inKB"), className: "w-[6%]" },
          { key: "note", header: t("dpk.doc.note"), className: "w-[26%]" },
          { key: "citedIn", header: t("dpk.doc.citedIn"), className: "w-[15%]" },
          { key: "formulas", header: t("dpk.doc.formulas"), className: "w-[8%]" },
        ]}
        rows={rows}
        rowKey={(d) => d.number}
        rowClassName="align-top break-words"
        emptyLabel={t("dpk.doc.empty")}
        renderRow={(d) => {
          const warn = d.warning ? WARN_ROW : "";
          return (
            <>
              <td className={`px-5 py-3.5 align-top ${warn}`}>
                <div className="text-[13px] font-semibold text-ink">{d.number}</div>
                {documentTitle(d) && <div className="caption mt-0.5 text-body">{documentTitle(d)}</div>}
                {d.used_for && <div className="caption mt-0.5 line-clamp-2 text-muted" title={d.used_for}>{t("dpk.doc.usedFor", { text: d.used_for })}</div>}
              </td>
              <td className={`px-5 py-3.5 align-top caption text-body ${warn}`}>{d.articles.length ? d.articles.join(", ") : "—"}</td>
              <td className={`px-5 py-3.5 align-top ${warn}`}>
                <CatalogueStatus doc={d} />
              </td>
              <td className={`px-5 py-3.5 align-top ${warn}`}>
                {d.documents.length === 0 ? (
                  <span className="caption text-muted-soft">—</span>
                ) : heldInKB(d.documents) ? (
                  <span className={`badge-pill text-[10.5px] ${BADGE_OK}`}>{t("dpk.doc.held")}</span>
                ) : (
                  <span className={`badge-pill text-[10.5px] ${BADGE_WARN}`}>{t("dpk.doc.notHeld")}</span>
                )}
              </td>
              <td className={`px-5 py-3.5 align-top caption text-body ${warn}`}>
                <CompactPackText text={d.documents.map((r) => r.status_note).filter(Boolean).join(" · ")} />
              </td>
              <td className={`px-5 py-3.5 align-top caption text-body ${warn}`}><CompactPackText text={d.cited_in.join(", ")} /></td>
              <td className={`px-5 py-3.5 align-top text-[11px] leading-relaxed text-muted ${warn}`}><CompactPackText text={d.formulas.join(", ")} /></td>
            </>
          );
        }}
      />
    </>
  );
}

function CompactPackText({ text }: { text: string }) {
  const { t } = useT();
  if (!text) return <span className="text-muted-soft">—</span>;
  if (text.length <= 100) return <span className="break-words leading-relaxed">{text}</span>;
  return <details className="group">
    <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
      <span className="line-clamp-3 break-words text-left leading-relaxed group-open:hidden">{text}</span>
      <span className="mt-1 block font-medium text-accent group-open:hidden">{t("dpk.doc.expandText")}</span>
      <span className="hidden font-medium text-accent group-open:inline">{t("dpk.doc.collapseText")}</span>
    </summary>
    <p className="mt-2 break-words whitespace-pre-line text-left leading-relaxed">{text}</p>
  </details>;
}

function CatalogueStatus({ doc }: { doc: DomainPackDocumentView }) {
  const { t } = useT();
  if (doc.lookup === "missing") return <span className={`badge-pill text-[10.5px] ${BADGE_WARN}`}>{t("dpk.doc.missing")}</span>;
  if (doc.lookup === "") return <span className="caption text-error">{t("dpk.doc.lookupFailed")}</span>;
  const rows: DomainPackCatalogueDocument[] = doc.documents;
  return (
    <div className="space-y-1">
      {doc.lookup === "ambiguous" && <div className="caption text-warning">{t("dpk.doc.ambiguous")}</div>}
      {rows.map((r) => (
        <div key={r.id}>
          <span className={`badge-pill inline-block whitespace-normal text-[10.5px] leading-snug ${statusTone(r.status)}`}>
            {r.status || "—"}
          </span>
          {(r.effective_on || r.expired_on) && (
            <div className="caption mt-0.5 tabular-nums text-muted">
              {r.effective_on ? t("dpk.doc.effective", { date: formatDate(r.effective_on) }) : ""}
              {r.effective_on && r.expired_on ? " · " : ""}
              {r.expired_on ? t("dpk.doc.expired", { date: formatDate(r.expired_on) }) : ""}
            </div>
          )}
          {doc.lookup === "ambiguous" && r.title && <div className="caption text-body">{r.title}</div>}
        </div>
      ))}
    </div>
  );
}

function FormulasTab({
  pack,
  formulas,
  catalogueNumbers,
  onSaved,
}: {
  pack: string;
  formulas: DomainPackFormulaView[];
  catalogueNumbers: string[];
  onSaved: (detail: DomainPackDetail) => void;
}) {
  const { t, locale } = useT();
  const [editing, setEditing] = useState<DomainPackFormulaView | null>(null);
  if (formulas.length === 0) {
    return <div className="card px-5 py-10 text-center text-[14px] text-muted shadow-sm">{t("dpk.formula.empty")}</div>;
  }
  return (
    <div className="space-y-5">
      {editing && (
        <FormulaEditor
          pack={pack}
          formula={editing}
          catalogueNumbers={catalogueNumbers}
          onClose={() => setEditing(null)}
          onSaved={(d) => {
            setEditing(null);
            onSaved(d);
          }}
        />
      )}
      {formulas.map((f) => (
        <div key={f.id} className="card min-w-0 overflow-hidden p-5 shadow-sm sm:p-6">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0 space-y-2">
              <h3 className="text-[16px] font-semibold leading-relaxed text-ink">{f.name}</h3>
              <code className="caption block break-all text-muted-soft">
                {f.id} · {f.tool}
              </code>
              {f.edited && (
                <span className={`badge-pill text-[10.5px] ${BADGE_WARN}`}>
                  {t("dpk.edit.editedBy", { who: f.edited_by ?? "—", at: formatDateTime(f.edited_at, locale) })}
                </span>
              )}
              {f.edit_not_applied && <span className={`badge-pill text-[10.5px] ${BADGE_BAD}`}>{t("dpk.edit.notApplied")}</span>}
            </div>
            <button type="button" className="btn btn-outline btn-sm" onClick={() => setEditing(f)}>
              {t("dpk.edit.open")}
            </button>
          </div>
          <div className="mt-4 rounded-r-lg border-l-4 border-accent bg-accent-surface px-4 py-3">
            <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-accent">{t("dpk.formula.expression")}</p>
            <p className="break-words text-[15px] font-medium leading-relaxed text-ink">{f.expression}</p>
          </div>

          <div className="mt-5 grid grid-cols-[repeat(auto-fit,minmax(min(100%,260px),1fr))] gap-x-6 gap-y-5">
            <div className="order-1 min-w-0">
              <div className="caption-uppercase mb-1 text-muted">{t("dpk.formula.variables")}</div>
              <ul className="divide-y divide-hairline text-[13px]">
                {f.variables.map((v) => (
                  <li key={v.name} className="py-2 first:pt-0">
                    <code className="break-all text-ink">{v.name}</code>{" "}
                    <span className={`badge-pill text-[10px] ${v.source === "kb" ? BADGE_WARN : BADGE_NEUTRAL}`}>
                      {v.source === "kb" ? t("dpk.formula.sourceKB") : t("dpk.formula.sourceInput")}
                    </span>
                    <div className="caption text-body">
                      {v.description} ({v.unit})
                    </div>
                  </li>
                ))}
              </ul>
            </div>
            <div className="order-3 col-span-full min-w-0 border-t border-hairline pt-4">
              <div className="caption-uppercase mb-1 text-muted">{t("dpk.formula.basis")}</div>
              {f.basis.length === 0 ? (
                <span className="caption text-muted">—</span>
              ) : (
                <ul className="space-y-3 text-[13px]">
                  {f.basis.map((b) => (
                    <li key={`${b.number}-${b.article ?? ""}`}>
                      <span className="text-ink">
                        {b.article ? `${b.article} ` : ""}
                        {b.number}
                      </span>{" "}
                      <span className={`badge-pill text-[10px] ${b.status === "ok" ? BADGE_OK : BADGE_WARN}`}>
                        {b.status === "ok" ? t("dpk.formula.basisOk") : t("dpk.formula.basisWarning")}
                      </span>
                      {b.note && <details className="group mt-2 border-l-2 border-warning pl-3">
                        <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
                          <span className="caption line-clamp-2 leading-relaxed text-body group-open:hidden">{b.note}</span>
                          <span className="caption mt-1 block font-medium text-warning group-open:hidden">{t("dpk.formula.expandNote")}</span>
                          <span className="caption hidden font-medium text-warning group-open:inline">{t("dpk.formula.collapseNote")}</span>
                        </summary>
                        <p className="caption mt-2 whitespace-pre-line leading-relaxed text-body">{b.note}</p>
                      </details>}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div className="order-2 min-w-0">
              <div className="caption-uppercase mb-1 text-muted">{t("dpk.formula.examples")}</div>
              <ul className="space-y-1.5">
                {f.examples.map((ex, i) => (
                  <li key={i} className="caption break-words leading-relaxed text-body">
                    {Object.entries(ex.inputs)
                      .map(([k, v]) => `${k} = ${formatFormulaValue(v)}`)
                      .join("; ")}
                    <div className="text-ink">
                      →{" "}
                      {Object.entries(ex.expected)
                        .map(([k, v]) => `${k} = ${formatFormulaValue(v)}`)
                        .join("; ")}
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

function AgentsTab({ detail }: { detail: DomainPackDetail }) {
  const { t } = useT();
  return (
    <DataTable<DomainPackAgent>
      minWidth="min-w-[600px]"
      columns={[
        { key: "name", header: t("dpk.agent.name") },
        { key: "org", header: t("dpk.agent.org") },
        { key: "distribution", header: t("dpk.agent.distribution") },
        { key: "sale", header: t("dpk.agent.sale") },
        { key: "holders", header: t("dpk.agent.holders") },
        { key: "scope", header: t("dpk.agent.scope") },
      ]}
      rows={detail.agents}
      rowKey={(a) => `${a.tenant_id}-${a.id}`}
      emptyLabel={t("dpk.agent.empty")}
      renderRow={(a) => (
        <>
          <td className="px-5 py-3.5 align-middle text-[13px] text-ink">{a.name || a.id}</td>
          <td className="px-5 py-3.5 align-middle text-[13px] text-body">
            {a.tenant_name || "—"} <span className="caption tabular-nums text-muted">#{a.tenant_id}</span>
          </td>
          <td className="px-5 py-3.5 align-middle text-[13px] text-body">
            {t(`agent.distribution.${normalizeDistribution(a.distribution)}` as LocaleKey)}
          </td>
          <td className="px-5 py-3.5 align-middle text-[13px] text-body">
            {a.addon_price_vnd == null ? (
              <span className="text-muted">{t("dpk.agent.notListed")}</span>
            ) : (
              <>
                {a.addon_price_vnd > 0 ? formatPriceVND(a.addon_price_vnd) : t("usg.addonFree")}
                {t("usg.perMonth")}{" "}
                <span className={`caption ${a.addon_active ? "text-accent" : "text-muted"}`}>
                  · {a.addon_active ? t("dpk.agent.onSale") : t("dpk.agent.offSale")}
                </span>
              </>
            )}
          </td>
          <td className="px-5 py-3.5 align-middle text-[13px] tabular-nums text-body">{a.addon_holders ?? 0}</td>
          <td className="px-5 py-3.5 align-middle text-[13px] text-body">
            {normalizeDistribution(a.distribution) !== "internal" ? (
              <span className="text-muted">—</span>
            ) : a.applies_to && a.applies_to.length > 0 ? (
              <ul className="space-y-0.5">
                {a.applies_to.map((s) => (
                  <li key={s.tenant_id}>
                    {s.tenant_name || "—"} <span className="caption tabular-nums text-muted">#{s.tenant_id}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <span className="text-muted">{t("dpk.agent.scopeAll")}</span>
            )}
          </td>
        </>
      )}
    />
  );
}
