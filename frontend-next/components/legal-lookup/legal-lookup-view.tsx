/* Validity lookup page: a member or a guest types a số hiệu or title words
 * and sees each matching catalogue document with its status, dates and the
 * documents replacing, ending or amending it — straight from the legal
 * catalogue, without the assistant (no AI, no question quota). The query
 * lives in ?q= so a result can be linked and the back button works. */
"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { IconFileSearch, IconSearch } from "@/components/icons";
import { ApiError } from "@/lib/api-client";
import { useAuth, useTenantRole } from "@/lib/auth";
import { useT, type LocaleKey } from "@/lib/i18n";
import {
  lookupLegalValidity,
  type LegalValidityLookup,
  type LegalValidityMatch,
  type LegalValidityRef,
} from "@/lib/api/legal-lookup";
import {
  formatLegalDate,
  knowledgeDocHref,
  legalDocShortLabel,
  legalLookupHref,
  legalStatusBadgeClass,
  legalStatusLabelKey,
} from "@/lib/legal-status";

export const LEGAL_LOOKUP_MAX_QUERY = 200;

export function LegalLookupView() {
  const { t } = useT();
  const router = useRouter();
  const searchParams = useSearchParams();
  const { ready } = useAuth();
  const { isGuest } = useTenantRole();
  const q = (searchParams.get("q") ?? "").trim();
  const [input, setInput] = useState(q);
  const [result, setResult] = useState<LegalValidityLookup | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setInput(q);
    if (!q) {
      setResult(null);
      setError("");
      return;
    }
    // whether to send the organization depends on /auth/me
    if (!ready) return;
    const ctrl = new AbortController();
    setLoading(true);
    setError("");
    lookupLegalValidity(q, { isGuest, signal: ctrl.signal })
      .then((res) => setResult(res))
      .catch((e: unknown) => {
        if (ctrl.signal.aborted) return;
        setResult(null);
        setError(
          e instanceof ApiError && e.status === 429
            ? t("llookup.err.tooMany")
            : e instanceof Error && e.message
              ? e.message
              : t("llookup.err.failed"),
        );
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false);
      });
    return () => ctrl.abort();
    // t changes with the locale only; a re-fetch then is harmless but useless
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, isGuest, ready]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const next = input.trim();
    if (!next) return;
    router.replace(legalLookupHref(next));
  };

  const sharedNumber = result?.by === "number" && result.matches.length > 1;

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[880px] px-4 py-6 sm:px-8 sm:py-10">
        <div className="mb-6">
          <h1 className="display-xl">{t("llookup.title")}</h1>
          <p className="mt-3 max-w-[620px] text-body">{t("llookup.subtitle")}</p>
        </div>

        <form onSubmit={submit} role="search" className="mb-6 flex gap-2">
          <label htmlFor="legal-lookup-q" className="sr-only">
            {t("llookup.inputLabel")}
          </label>
          <div className="card flex flex-1 items-center gap-2 px-3 py-2">
            <IconSearch className="h-4 w-4 shrink-0 text-muted" />
            <input
              id="legal-lookup-q"
              value={input}
              maxLength={LEGAL_LOOKUP_MAX_QUERY}
              onChange={(e) => setInput(e.target.value)}
              placeholder={t("llookup.placeholder")}
              className="min-w-0 flex-1 bg-transparent text-[15px] text-ink outline-none placeholder:text-muted"
              autoFocus={!q}
            />
          </div>
          <button type="submit" className="btn btn-primary" disabled={!input.trim() || loading}>
            {t("llookup.submit")}
          </button>
        </form>

        {loading && (
          <p role="status" className="caption text-muted">
            {t("llookup.loading")}
          </p>
        )}
        {error && (
          <p role="alert" className="caption text-error">
            {error}
          </p>
        )}

        {!loading && !error && result && (
          <>
            <p className="caption mb-4 text-muted" role="status" aria-live="polite">
              {result.matches.length === 0
                ? t("llookup.none", { q: result.query })
                : t("llookup.count", { n: result.matches.length })}
              {result.catalog_snapshot_date && (
                <> · {t("llookup.snapshot", { date: formatLegalDate(result.catalog_snapshot_date) })}</>
              )}
            </p>
            {result.matches.length === 0 && (
              <p className="mb-4 text-body text-muted">{t("llookup.noneHint")}</p>
            )}
            {sharedNumber && (
              <p className="mb-4 rounded-[12px] border border-hairline bg-surface-strong px-4 py-3 text-[14px]">
                {t("llookup.sharedNumber", { n: result.matches.length })}
              </p>
            )}
            <ul className="flex flex-col gap-3">
              {result.matches.map((m) => (
                <li key={m.id}>
                  <MatchCard m={m} />
                </li>
              ))}
            </ul>
          </>
        )}

        {!q && !result && (
          <p className="caption text-muted">{t("llookup.empty")}</p>
        )}
      </div>
    </div>
  );
}

function StatusBadge({ kind, small = false }: { kind: string; small?: boolean }) {
  const { t } = useT();
  return (
    <span className={`badge-pill ${small ? "text-[10px]" : "text-[11px]"} ${legalStatusBadgeClass(kind)}`}>
      {t(legalStatusLabelKey(kind))}
    </span>
  );
}

const RELATION_KEYS: Record<string, LocaleKey> = {
  replaces: "llookup.rel.replaces",
  repeals: "llookup.rel.repeals",
  expires: "llookup.rel.expires",
  partially_expires: "llookup.rel.partially_expires",
  suspends: "llookup.rel.suspends",
  amends: "llookup.rel.amends",
  corrects: "llookup.rel.corrects",
};

function MatchCard({ m }: { m: LegalValidityMatch }) {
  const { t } = useT();
  const label = legalDocShortLabel(m);
  const title = m.title.trim() && m.title.trim() !== label ? m.title.trim() : "";
  const dates: string[] = [];
  if (m.issued_on) dates.push(t("llookup.issued", { date: formatLegalDate(m.issued_on) }));
  if (m.effective_on) dates.push(t("llookup.effective", { date: formatLegalDate(m.effective_on) }));
  if (m.expired_on) dates.push(t("llookup.expired", { date: formatLegalDate(m.expired_on) }));
  const knowledge = m.knowledge ?? [];
  return (
    <article className="card px-5 py-4">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-[16px] font-semibold text-ink">{label}</h2>
        <StatusBadge kind={m.status} />
        {m.derived && m.status_text.trim() && (
          <span className="caption text-muted">
            {t("llookup.sourceWording", { source: m.source_label || m.source, status: m.status_text })}
          </span>
        )}
      </div>
      {title && <p className="mt-1 text-[14px] text-ink">{title}</p>}
      <p className="caption mt-1 text-muted">
        {[m.issuer, ...dates].filter(Boolean).join(" · ")}
      </p>

      {m.derived && (
        <p className="mt-3 rounded-[10px] border border-hairline bg-surface-strong px-3 py-2 text-[13px]">
          {t(m.status === "partly_expired" ? "llookup.derivedMaybe" : "llookup.derived", {
            by: legalDocShortLabel(m.derived.by),
            date: formatLegalDate(m.derived.expired_on),
            rel: t(RELATION_KEYS[m.derived.relation] ?? "llookup.rel.replaces"),
          })}
        </p>
      )}
      {m.unverified && (
        <p className="mt-3 text-[13px] text-warning">{t("llookup.unverified", { source: m.source_label || m.source })}</p>
      )}

      <RefGroup title={t("llookup.replacedBy")} refs={m.replaced_by} total={m.replaced_by_total} />
      <RefGroup title={t("llookup.endedBy")} refs={m.ended_by} total={m.ended_by_total} showRelation />
      <RefGroup title={t("llookup.amendedBy")} refs={m.amended_by} total={m.amended_by_total} showRelation />

      {knowledge.length > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <span className="caption text-muted">{t("llookup.openIn")}</span>
          {knowledge.map((k) => (
            <Link
              key={k.knowledge_id}
              href={knowledgeDocHref(k.knowledge_base_id, k.knowledge_id)}
              className="btn btn-outline btn-sm py-0.5 text-[12px]"
            >
              <IconFileSearch className="h-3.5 w-3.5" />
              {k.knowledge_base_name || k.title}
            </Link>
          ))}
        </div>
      )}

      {m.source_updated_on && (
        <p className="caption mt-3 text-muted-soft">
          {t("llookup.sourceLine", { source: m.source_label || m.source, date: formatLegalDate(m.source_updated_on) })}
        </p>
      )}
    </article>
  );
}

function RefGroup({
  title,
  refs,
  total,
  showRelation = false,
}: {
  title: string;
  refs: LegalValidityRef[] | null;
  total: number;
  showRelation?: boolean;
}) {
  const { t } = useT();
  const list = refs ?? [];
  if (list.length === 0) return null;
  return (
    <div className="mt-3">
      <div className="caption-uppercase mb-1 text-muted-soft">{title}</div>
      <ul className="flex flex-col gap-1">
        {list.map((r) => (
          <li key={`${r.id}-${r.relation ?? ""}`} className="flex flex-wrap items-center gap-2 text-[14px]">
            <Link
              href={legalLookupHref(r.document_number || r.title)}
              className="font-medium text-ink underline-offset-2 hover:underline"
              title={r.title}
            >
              {legalDocShortLabel(r)}
            </Link>
            <StatusBadge kind={r.status} small />
            {showRelation && r.relation && RELATION_KEYS[r.relation] && (
              <span className="caption text-muted">{t(RELATION_KEYS[r.relation])}</span>
            )}
            {r.effective_on && (
              <span className="caption text-muted">
                {t("llookup.effective", { date: formatLegalDate(r.effective_on) })}
              </span>
            )}
          </li>
        ))}
        {total > list.length && (
          <li className="caption text-muted">{t("llookup.more", { n: total - list.length })}</li>
        )}
      </ul>
    </div>
  );
}
