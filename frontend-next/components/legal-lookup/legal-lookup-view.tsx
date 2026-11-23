/* Validity lookup page: a member or a guest types a số hiệu or title words
 * and sees each matching catalogue document with its status, dates and the
 * documents replacing, ending or amending it — straight from the legal
 * catalogue, without the assistant (no AI, no question quota). The query
 * lives in ?q= so a result can be linked and the back button works. */
"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { IconFileSearch, IconGraph, IconSearch } from "@/components/icons";
import { LegalMap, MAP_DEPTHS } from "@/components/graph/legal-map";
import type { LegalGraphDoc } from "@/lib/api/graph";
import { ApiError } from "@/lib/api-client";
import { useAuth, useTenantRole } from "@/lib/auth";
import { useT, type LocaleKey } from "@/lib/i18n";
import {
  lookupLegalValidity,
  suggestLegalDocuments,
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
  // the document whose map is drawn under the results (a result's button,
  // a clicked card on the map, or a deep link), and the map's tiers
  const doc = (searchParams.get("doc") ?? "").trim();
  const depthParam = Number(searchParams.get("depth") ?? 2);
  const depth = (MAP_DEPTHS as readonly number[]).includes(depthParam) ? depthParam : 2;
  const [input, setInput] = useState(q);
  const mapRef = useRef<HTMLDivElement>(null);
  // type-ahead: the documents the typed text may mean, under the box
  const [suggestions, setSuggestions] = useState<LegalValidityRef[]>([]);
  const [suggestOpen, setSuggestOpen] = useState(false);
  const [suggestIdx, setSuggestIdx] = useState(-1);
  const suggestFor = useRef("");
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
        // server text is not localized: only the rate limit gets its own words
        setError(e instanceof ApiError && e.status === 429 ? t("llookup.err.tooMany") : t("llookup.err.failed"));
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false);
      });
    return () => ctrl.abort();
    // t changes with the locale only; a re-fetch then is harmless but useless
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, isGuest, ready]);

  useEffect(() => {
    const term = input.trim();
    // the text already looked up needs no list; short text matches too much
    if (!ready || term.length < 2 || term === q) {
      setSuggestions([]);
      setSuggestOpen(false);
      return;
    }
    const ctrl = new AbortController();
    const timer = setTimeout(() => {
      suggestFor.current = term;
      suggestLegalDocuments(term, { isGuest, signal: ctrl.signal })
        .then((list) => {
          if (suggestFor.current !== term) return;
          setSuggestions(list);
          setSuggestIdx(-1);
          setSuggestOpen(list.length > 0);
        })
        .catch(() => {
          /* a failed suggestion is nothing to show */
        });
    }, 250);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [input, q, isGuest, ready]);

  const pickSuggestion = (d: LegalValidityRef) => {
    setSuggestOpen(false);
    setSuggestions([]);
    const sp = new URLSearchParams({ q: d.document_number || input.trim(), doc: d.id });
    router.replace(`/platform/legal-lookup?${sp.toString()}`);
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (suggestOpen && suggestIdx >= 0 && suggestions[suggestIdx]) {
      pickSuggestion(suggestions[suggestIdx]);
      return;
    }
    const next = input.trim();
    if (!next) return;
    setSuggestOpen(false);
    router.replace(legalLookupHref(next));
  };

  const onInputKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!suggestOpen || suggestions.length === 0) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSuggestIdx((i) => (i + 1) % suggestions.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSuggestIdx((i) => (i <= 0 ? suggestions.length - 1 : i - 1));
    } else if (e.key === "Escape") {
      setSuggestOpen(false);
    }
  };

  const hrefWith = (next: { doc?: string; depth?: number }) => {
    const sp = new URLSearchParams();
    if (q) sp.set("q", q);
    const d = next.doc ?? doc;
    if (d) sp.set("doc", d);
    const dp = next.depth ?? depth;
    if (dp !== 2) sp.set("depth", String(dp));
    return `/platform/legal-lookup${sp.toString() ? "?" + sp.toString() : ""}`;
  };
  const showMap = (id: string) => router.replace(hrefWith({ doc: id }));

  // one match: its map opens by itself
  useEffect(() => {
    if (!doc && result && result.matches.length === 1) showMap(result.matches[0].id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [result, doc]);
  // a chosen document scrolls its map into view
  useEffect(() => {
    if (doc) mapRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, [doc]);

  const sharedNumber = result?.by === "number" && result.matches.length > 1;

  /* The status line, notes and result cards: under the search box, or in
   * a column right of the map once a document is chosen. */
  const resultsBlock = (
    <>
      {(loading || (q && !ready)) && (
        <p role="status" className="caption text-muted">
          {t("llookup.loading")}
        </p>
      )}
      {error && (
        <p role="alert" className="caption text-error">
          {error}
        </p>
      )}

      {!loading && ready && !error && result && (
        <>
          <p className="caption mb-4 text-muted" role="status" aria-live="polite">
            {result.matches.length === 0
              ? result.number_missing
                ? t("llookup.noneShort")
                : t("llookup.none", { q: result.query })
              : t(result.more ? "llookup.countMore" : "llookup.count", { n: result.matches.length })}
            {result.catalog_snapshot_date && (
              <> · {t("llookup.snapshot", { date: formatLegalDate(result.catalog_snapshot_date) })}</>
            )}
          </p>
          {result.number_missing && (
            <p role="note" className="mb-4 rounded-[12px] border border-hairline bg-surface-strong px-4 py-3 text-[14px]">
              {t(result.matches.length > 0 ? "llookup.numberMissingMentions" : "llookup.numberMissing", {
                number: result.number_missing,
              })}
            </p>
          )}
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
                <MatchCard m={m} mapOpen={doc === m.id} onMap={() => showMap(m.id)} />
              </li>
            ))}
          </ul>
          {result.more && <p className="caption mt-4 text-muted">{t("llookup.moreMatches", { n: result.matches.length })}</p>}
        </>
      )}

      {!q && !result && !doc && (
        <p className="caption text-muted">{t("llookup.empty")}</p>
      )}
    </>
  );

  return (
    <div className="flex-1 overflow-y-auto">
      <div className={`mx-auto w-full px-4 sm:px-8 ${doc ? "max-w-[1680px] py-6" : "max-w-[880px] py-6 sm:py-10"}`}>
        <div className="mb-6">
          <h1 className="display-xl max-sm:text-[calc(30px*var(--app-font-scale,1))]">{t("llookup.title")}</h1>
          <p className="mt-3 max-w-[620px] text-body">{t("llookup.subtitle")}</p>
        </div>

        <form onSubmit={submit} role="search" className="mb-6 flex gap-2">
          <label htmlFor="legal-lookup-q" className="sr-only">
            {t("llookup.inputLabel")}
          </label>
          <div className="relative flex-1">
            <div className="card flex items-center gap-2 px-3 py-2">
              <IconSearch className="h-4 w-4 shrink-0 text-muted" />
              <input
                id="legal-lookup-q"
                value={input}
                maxLength={LEGAL_LOOKUP_MAX_QUERY}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={onInputKey}
                onFocus={() => suggestions.length > 0 && setSuggestOpen(true)}
                onBlur={() => setTimeout(() => setSuggestOpen(false), 150)}
                placeholder={t("llookup.placeholder")}
                className="min-w-0 flex-1 bg-transparent text-[15px] text-ink outline-none placeholder:text-muted"
                autoComplete="off"
                role="combobox"
                aria-expanded={suggestOpen}
                aria-controls="legal-lookup-suggest"
                aria-autocomplete="list"
                autoFocus={!q}
              />
            </div>
            {suggestOpen && suggestions.length > 0 && (
              <ul
                id="legal-lookup-suggest"
                role="listbox"
                className="absolute left-0 right-0 top-full z-20 mt-1 max-h-80 overflow-y-auto rounded-xl border border-hairline bg-surface-card p-1 shadow-lg"
              >
                {suggestions.map((d, i) => (
                  <li key={d.id} role="option" aria-selected={i === suggestIdx}>
                    <button
                      type="button"
                      onMouseDown={(e) => e.preventDefault()}
                      onClick={() => pickSuggestion(d)}
                      onMouseEnter={() => setSuggestIdx(i)}
                      className={`flex w-full flex-col items-start gap-0.5 rounded-lg px-3 py-1.5 text-left ${
                        i === suggestIdx ? "bg-surface-strong" : "hover:bg-surface-strong"
                      }`}
                    >
                      <span className="flex w-full items-center gap-2">
                        <span className="truncate text-[13px] font-semibold text-ink">{legalDocShortLabel(d)}</span>
                        <StatusBadge kind={d.status} small />
                        {d.issued_on && <span className="ml-auto shrink-0 caption text-muted-soft">{formatLegalDate(d.issued_on)}</span>}
                      </span>
                      {d.title.trim() && d.title.trim() !== legalDocShortLabel(d) && (
                        <span className="w-full truncate text-[12px] text-muted">{d.title}</span>
                      )}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <button type="submit" className="btn btn-primary" disabled={!input.trim() || loading}>
            {t("llookup.submit")}
          </button>
        </form>

        {!doc && resultsBlock}
      </div>

      {doc && (
        <div className="@container mx-auto w-full max-w-[1680px] px-4 pb-10 sm:px-8">
        <div className="flex flex-col gap-6 @[1000px]:flex-row @[1000px]:items-start">
          <div ref={mapRef} className="min-w-0 flex-1 scroll-mt-4">
            <LegalMap
              docId={doc}
              depth={depth}
              onFocus={(d: LegalGraphDoc) => showMap(d.id)}
              onDepthChange={(dp) => router.replace(hrefWith({ depth: dp }))}
            />
          </div>
          {/* the results sit right of the map (scrolling on their own) when
          the column is wide, under it otherwise */}
          <aside className="w-full shrink-0 @[1000px]:sticky @[1000px]:top-4 @[1000px]:max-h-[min(82dvh,820px)] @[1000px]:w-[340px] @[1000px]:overflow-y-auto @[1000px]:pr-1 @[1360px]:w-[380px]">
            {resultsBlock}
          </aside>
        </div>
        </div>
      )}
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

function MatchCard({ m, mapOpen, onMap }: { m: LegalValidityMatch; mapOpen: boolean; onMap: () => void }) {
  const { t } = useT();
  const label = legalDocShortLabel(m);
  const title = m.title.trim() && m.title.trim() !== label ? m.title.trim() : "";
  const dates: string[] = [];
  if (m.issued_on) dates.push(t("llookup.issued", { date: formatLegalDate(m.issued_on) }));
  if (m.effective_on) dates.push(t("llookup.effective", { date: formatLegalDate(m.effective_on) }));
  if (m.expired_on) dates.push(t("llookup.expired", { date: formatLegalDate(m.expired_on) }));
  const knowledge = m.knowledge ?? [];
  return (
    <article className={`card px-4 py-4 sm:px-5 ${mapOpen ? "ring-1 ring-ink" : ""}`}>
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

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button type="button" onClick={onMap} className={`btn btn-row ${mapOpen ? "btn-primary" : "btn-outline"}`} title={t("lmap.openMapTitle")}>
          <IconGraph className="h-3.5 w-3.5" />
          {mapOpen ? t("lmap.mapShown") : t("lmap.openMap")}
        </button>
      </div>
      {knowledge.length > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <span className="caption text-muted">{t("llookup.openIn")}</span>
          {knowledge.map((k) => (
            <Link
              key={k.knowledge_id}
              href={knowledgeDocHref(k.knowledge_base_id, k.knowledge_id)}
              className="btn btn-row btn-soft"
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
