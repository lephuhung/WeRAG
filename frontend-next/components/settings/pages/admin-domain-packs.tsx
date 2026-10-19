"use client";

/* SuperAdmin "Bộ kỹ năng chuyên môn": the domain packs shipped in this build and,
 * per pack, the single place to see what must be updated — its cited
 * documents ("Văn bản trích dẫn") checked against the legal catalogue, its
 * formulas ("Công thức") with the state of their legal basis, and the
 * agents using it. Packs are files in the repo (domain-packs/<name>):
 * nothing is edited here. "Đối chiếu danh mục" records legal document
 * requests for texts no public KB holds (the legal requests queue).
 * Backend: /api/v1/system/domain-packs (+ /:name, /:name/reconcile). The
 * open pack lives in ?pack= so the detail is deep-linkable. */

import { useCallback, useEffect, useState, type ReactNode } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { IconRefresh } from "@/components/icons";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { getCurrentUser } from "@/lib/api/auth";
import { formatDate } from "@/lib/datetime";
import { normalizeDistribution } from "@/lib/agent-tenant-scope";
import { formatPriceVND } from "@/lib/api/usage";
import { adminSectionHref } from "@/components/admin/admin-routes";
import {
  getDomainPack,
  listDomainPacks,
  reconcileDomainPack,
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

const WARN_ROW = "bg-amber-500/[0.07]";
const BADGE_WARN = "bg-amber-500/15 text-amber-700 dark:text-amber-400";
const BADGE_OK = "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400";
const BADGE_BAD = "bg-red-500/10 text-red-700 dark:text-red-400";
const BADGE_NEUTRAL = "bg-surface-strong text-muted";

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

  if (allowed === null) {
    return <div className="px-5 py-12 text-muted">{t("usrp.loading")}</div>;
  }
  if (!allowed) {
    return (
      <div className="px-5 py-16 text-center">
        <p className="body-sm text-muted">{t("dpk.denied")}</p>
      </div>
    );
  }
  return pack ? <PackDetail name={pack} /> : <PackList />;
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

  useEffect(() => {
    let alive = true;
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
  }, [t]);

  return (
    <div className="mx-auto w-full max-w-[1400px]">
      <p className="body-sm mb-4 max-w-[860px] text-muted">{t("dpk.intro")}</p>
      {error && <div className="caption mb-3 text-red-600">{error}</div>}
      <div className="card overflow-x-auto shadow-sm">
        <table className="w-full min-w-[900px] border-collapse text-left">
          <thead>
            <tr className="caption-uppercase border-b border-hairline bg-surface-strong/30 text-muted">
              <th className="min-w-[220px] px-5 py-3 font-medium">{t("dpk.col.pack")}</th>
              <th className="w-[90px] px-5 py-3 font-medium">{t("dpk.col.version")}</th>
              <th className="w-[120px] px-5 py-3 font-medium">{t("dpk.col.asOf")}</th>
              <th className="w-[170px] px-5 py-3 font-medium">{t("dpk.col.reviewBy")}</th>
              <th className="w-[100px] px-5 py-3 font-medium">{t("dpk.col.documents")}</th>
              <th className="w-[100px] px-5 py-3 font-medium">{t("dpk.col.formulas")}</th>
              <th className="w-[90px] px-5 py-3 font-medium">{t("dpk.col.agents")}</th>
              <th className="min-w-[220px] px-5 py-3 font-medium">{t("dpk.col.attribution")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {items.map((p) => (
              <tr key={p.name} className="align-top">
                <td className="px-5 py-3.5">
                  <Link href={packHref(p.name)} className="text-[13px] font-semibold text-[#1f6feb] hover:underline">
                    {p.name}
                  </Link>
                  {p.description && <div className="caption mt-0.5 line-clamp-2 text-body">{p.description}</div>}
                </td>
                <td className="px-5 py-3.5 text-[13px] tabular-nums text-ink">{p.version}</td>
                <td className="px-5 py-3.5 caption tabular-nums text-body">{formatDate(p.as_of)}</td>
                <td className="px-5 py-3.5">
                  <span className="caption tabular-nums text-body">{formatDate(p.review_by)}</span>
                  {p.overdue && <span className={`badge-pill ml-2 text-[10.5px] ${BADGE_BAD}`}>{t("dpk.overdue")}</span>}
                </td>
                <td className="px-5 py-3.5 text-[13px] tabular-nums text-ink">{p.document_count}</td>
                <td className="px-5 py-3.5 text-[13px] tabular-nums text-ink">{p.formula_count}</td>
                <td className="px-5 py-3.5 text-[13px] tabular-nums text-ink">{p.agent_count}</td>
                <td className="px-5 py-3.5 caption text-body">{p.attribution}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={8} className="px-5 py-12 text-center text-[14px] text-muted">
                  {loading ? t("usrp.loading") : t("dpk.empty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
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
    <div className="mx-auto w-full max-w-[1400px]">
      <button type="button" className="btn btn-ghost btn-sm mb-3 px-2" onClick={() => router.push(packHref(""))}>
        ← {t("dpk.back")}
      </button>

      {error && <div className="caption mb-3 text-red-600">{error}</div>}
      {!detail ? (
        <div className="py-12 text-muted">{loading ? t("usrp.loading") : t("dpk.loadFailed")}</div>
      ) : (
        <>
          <PackHeader detail={detail} reconciling={reconciling} onReconcile={() => void reconcile()} onReload={() => void load()} loading={loading} />
          {result && <ReconcilePanel result={result} onClose={() => setResult(null)} />}

          <div className="mb-4 mt-5 flex items-center gap-1 border-b border-hairline">
            {TABS.map(({ key, labelKey }) => {
              const count = key === "documents" ? warnDocs : key === "formulas" ? warnFormulas : 0;
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => setTab(key)}
                  className={`-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-[13px] font-medium transition-colors ${
                    tab === key ? "border-ink text-ink" : "border-transparent text-muted hover:text-ink"
                  }`}
                >
                  {t(labelKey)}
                  {count > 0 && <span className={`badge-pill text-[10.5px] tabular-nums ${BADGE_WARN}`}>{count}</span>}
                </button>
              );
            })}
          </div>

          {tab === "documents" && <DocumentsTab docs={detail.documents} />}
          {tab === "formulas" && <FormulasTab formulas={detail.formulas} />}
          {tab === "agents" && <AgentsTab detail={detail} />}
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
    <div className="card p-5 shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="title-md">
            {detail.name} <span className="caption tabular-nums text-muted">v{detail.version}</span>
          </h3>
          {detail.description && <p className="body-sm mt-1 max-w-[900px] text-body">{detail.description}</p>}
        </div>
        <div className="flex items-center gap-2">
          <button type="button" className="btn btn-outline btn-sm h-8 w-8 p-0" onClick={onReload} disabled={loading} title={t("dpk.reload")}>
            <IconRefresh className="h-4 w-4" />
          </button>
          <button type="button" className="btn btn-primary btn-sm" onClick={onReconcile} disabled={reconciling}>
            {reconciling ? t("dpk.reconciling") : t("dpk.reconcile")}
          </button>
        </div>
      </div>
      <dl className="mt-4 grid grid-cols-1 gap-x-8 gap-y-2 text-[13px] sm:grid-cols-2 lg:grid-cols-3">
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
        <Field label={t("dpk.col.attribution")} value={detail.attribution} />
        <Field label={t("dpk.origin")} value={<span className="break-all">{detail.origin}</span>} />
        <Field label={t("dpk.requiredTools")} value={detail.required_tools.join(", ") || "—"} />
      </dl>
      {detail.draft_documents.length > 0 && (
        <div className="mt-4">
          <div className="caption-uppercase mb-1 text-muted">{t("dpk.drafts")}</div>
          <ul className="space-y-0.5 text-[13px] text-body">
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
        <div className="caption mt-3 text-red-600">
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
      <dd className="text-ink">{value || "—"}</dd>
    </div>
  );
}

function ReconcilePanel({ result, onClose }: { result: DomainPackReconcileResult; onClose: () => void }) {
  const { t } = useT();
  const { created, existing } = reconcileRequests(result);
  return (
    <div className="card mt-4 border-[#1f6feb]/30 p-4 shadow-sm">
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
          <div className="caption mt-2 text-red-600">{result.errors.map((e) => `${e.number}: ${e.error}`).join(" · ")}</div>
        )}
        <Link href={REQUESTS_HREF} className="caption mt-2 inline-block text-[#1f6feb] hover:underline">
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
      <label className="caption mb-3 flex w-fit cursor-pointer items-center gap-2 text-body">
        <input type="checkbox" checked={onlyWarnings} onChange={(e) => setOnlyWarnings(e.target.checked)} />
        {t("dpk.onlyWarnings")}
      </label>
      <div className="card overflow-x-auto shadow-sm">
        <table className="w-full min-w-[1150px] border-collapse text-left">
          <thead>
            <tr className="caption-uppercase border-b border-hairline bg-surface-strong/30 text-muted">
              <th className="min-w-[240px] px-4 py-3 font-medium">{t("dpk.doc.document")}</th>
              <th className="min-w-[140px] px-4 py-3 font-medium">{t("dpk.doc.articles")}</th>
              <th className="min-w-[170px] px-4 py-3 font-medium">{t("dpk.doc.status")}</th>
              <th className="w-[110px] px-4 py-3 font-medium">{t("dpk.doc.inKB")}</th>
              <th className="min-w-[220px] px-4 py-3 font-medium">{t("dpk.doc.note")}</th>
              <th className="min-w-[160px] px-4 py-3 font-medium">{t("dpk.doc.citedIn")}</th>
              <th className="min-w-[150px] px-4 py-3 font-medium">{t("dpk.doc.formulas")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {rows.map((d) => (
              <tr key={d.number} className={`align-top ${d.warning ? WARN_ROW : ""}`}>
                <td className="px-4 py-3">
                  <div className="text-[13px] font-semibold text-ink">{d.number}</div>
                  {documentTitle(d) && <div className="caption mt-0.5 text-body">{documentTitle(d)}</div>}
                  {d.used_for && <div className="caption mt-0.5 text-muted">{t("dpk.doc.usedFor", { text: d.used_for })}</div>}
                </td>
                <td className="px-4 py-3 caption text-body">{d.articles.length ? d.articles.join(", ") : "—"}</td>
                <td className="px-4 py-3">
                  <CatalogueStatus doc={d} />
                </td>
                <td className="px-4 py-3">
                  {d.documents.length === 0 ? (
                    <span className="caption text-muted-soft">—</span>
                  ) : heldInKB(d.documents) ? (
                    <span className={`badge-pill text-[10.5px] ${BADGE_OK}`}>{t("dpk.doc.held")}</span>
                  ) : (
                    <span className={`badge-pill text-[10.5px] ${BADGE_WARN}`}>{t("dpk.doc.notHeld")}</span>
                  )}
                </td>
                <td className="px-4 py-3 caption text-body">
                  {d.documents.map((r) => r.status_note).filter(Boolean).join(" · ") || "—"}
                </td>
                <td className="px-4 py-3 caption text-body">{d.cited_in.length ? d.cited_in.join(", ") : "—"}</td>
                <td className="px-4 py-3 caption text-body">{d.formulas.length ? d.formulas.join(", ") : "—"}</td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={7} className="px-5 py-10 text-center text-[14px] text-muted">
                  {t("dpk.doc.empty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

function CatalogueStatus({ doc }: { doc: DomainPackDocumentView }) {
  const { t } = useT();
  if (doc.lookup === "missing") return <span className={`badge-pill text-[10.5px] ${BADGE_WARN}`}>{t("dpk.doc.missing")}</span>;
  if (doc.lookup === "") return <span className="caption text-red-600">{t("dpk.doc.lookupFailed")}</span>;
  const rows: DomainPackCatalogueDocument[] = doc.documents;
  return (
    <div className="space-y-1">
      {doc.lookup === "ambiguous" && <div className="caption text-amber-700 dark:text-amber-400">{t("dpk.doc.ambiguous")}</div>}
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

function FormulasTab({ formulas }: { formulas: DomainPackFormulaView[] }) {
  const { t } = useT();
  if (formulas.length === 0) {
    return <div className="card px-5 py-10 text-center text-[14px] text-muted shadow-sm">{t("dpk.formula.empty")}</div>;
  }
  return (
    <div className="space-y-3">
      {formulas.map((f) => (
        <div key={f.id} className={`card p-4 shadow-sm ${f.warning ? WARN_ROW : ""}`}>
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <div className="text-[14px] font-semibold text-ink">{f.name}</div>
            <code className="caption text-muted">
              {f.id} · {f.tool}
            </code>
          </div>
          <div className="mt-2 rounded-md bg-surface-strong/50 px-3 py-2 text-[13px] text-ink">{f.expression}</div>

          <div className="mt-3 grid gap-4 lg:grid-cols-3">
            <div>
              <div className="caption-uppercase mb-1 text-muted">{t("dpk.formula.variables")}</div>
              <ul className="space-y-1 text-[13px]">
                {f.variables.map((v) => (
                  <li key={v.name}>
                    <code className="text-ink">{v.name}</code>{" "}
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
            <div>
              <div className="caption-uppercase mb-1 text-muted">{t("dpk.formula.basis")}</div>
              {f.basis.length === 0 ? (
                <span className="caption text-muted">—</span>
              ) : (
                <ul className="space-y-1 text-[13px]">
                  {f.basis.map((b) => (
                    <li key={`${b.number}-${b.article ?? ""}`}>
                      <span className="text-ink">
                        {b.article ? `${b.article} ` : ""}
                        {b.number}
                      </span>{" "}
                      <span className={`badge-pill text-[10px] ${b.status === "ok" ? BADGE_OK : BADGE_WARN}`}>
                        {b.status === "ok" ? t("dpk.formula.basisOk") : t("dpk.formula.basisWarning")}
                      </span>
                      {b.note && <div className="caption text-body">{b.note}</div>}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div>
              <div className="caption-uppercase mb-1 text-muted">{t("dpk.formula.examples")}</div>
              <ul className="space-y-1.5">
                {f.examples.map((ex, i) => (
                  <li key={i} className="caption text-body">
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
    <div className="card overflow-x-auto shadow-sm">
      <table className="w-full min-w-[600px] border-collapse text-left">
        <thead>
          <tr className="caption-uppercase border-b border-hairline bg-surface-strong/30 text-muted">
            <th className="px-5 py-3 font-medium">{t("dpk.agent.name")}</th>
            <th className="px-5 py-3 font-medium">{t("dpk.agent.org")}</th>
            <th className="px-5 py-3 font-medium">{t("dpk.agent.distribution")}</th>
            <th className="px-5 py-3 font-medium">{t("dpk.agent.sale")}</th>
            <th className="px-5 py-3 font-medium">{t("dpk.agent.holders")}</th>
            <th className="px-5 py-3 font-medium">{t("dpk.agent.scope")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-hairline">
          {detail.agents.map((a) => (
            <tr key={`${a.tenant_id}-${a.id}`}>
              <td className="px-5 py-3 text-[13px] text-ink">{a.name || a.id}</td>
              <td className="px-5 py-3 text-[13px] text-body">
                {a.tenant_name || "—"} <span className="caption tabular-nums text-muted">#{a.tenant_id}</span>
              </td>
              <td className="px-5 py-3 text-[13px] text-body">
                {t(`agent.distribution.${normalizeDistribution(a.distribution)}` as LocaleKey)}
              </td>
              <td className="px-5 py-3 text-[13px] text-body">
                {a.addon_price_vnd == null ? (
                  <span className="text-muted">{t("dpk.agent.notListed")}</span>
                ) : (
                  <>
                    {a.addon_price_vnd > 0 ? formatPriceVND(a.addon_price_vnd) : t("usg.addonFree")}
                    {t("usg.perMonth")}{" "}
                    <span className={`caption ${a.addon_active ? "text-primary" : "text-muted"}`}>
                      · {a.addon_active ? t("dpk.agent.onSale") : t("dpk.agent.offSale")}
                    </span>
                  </>
                )}
              </td>
              <td className="px-5 py-3 text-[13px] tabular-nums text-body">{a.addon_holders ?? 0}</td>
              <td className="px-5 py-3 text-[13px] text-body">
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
            </tr>
          ))}
          {detail.agents.length === 0 && (
            <tr>
              <td colSpan={6} className="px-5 py-10 text-center text-[14px] text-muted">
                {t("dpk.agent.empty")}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
