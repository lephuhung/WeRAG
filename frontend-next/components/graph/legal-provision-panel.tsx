"use client";

import { useMemo, useState } from "react";
import type { LegalGraphDoc, LegalGraphFocus, LegalGraphProvisionRow } from "@/lib/api/graph";
import { IconChevronDown, IconChevronRight } from "@/components/icons";
import { docLabel, opColor } from "@/components/graph/legal-style";
import { compareArticles, comparePaths, parsePath, pathLabel } from "@/lib/legal-paths";
import { useT, type LocaleKey } from "@/lib/i18n";
import { formatDate } from "@/lib/datetime";

/* The "bảng đối chiếu" of the focused legal document: the provisions of
 * it that other documents act on (a tree Điều > Khoản > Điểm), and the
 * provisions of other documents it acts on (grouped by document). Hovering
 * a row lights its document's edge in the graph; clicking the document
 * focuses it. */

/** Rows open by default up to this many (beyond, articles start folded). */
const OPEN_ALL_UP_TO = 40;

interface Props {
  data: LegalGraphFocus;
  onFocusDoc: (d: LegalGraphDoc) => void;
  onHoverDoc: (id: string | null) => void;
  hoverDoc: string | null;
}

export function LegalProvisionPanel({ data, onFocusDoc, onHoverDoc, hoverDoc }: Props) {
  const { t } = useT();
  const [tab, setTab] = useState<"in" | "out">(
    data.provisions_in.length === 0 && data.provisions_out.length > 0 ? "out" : "in",
  );
  const tabs: ["in" | "out", LocaleKey, number][] = [
    ["in", "graph.lf.secIn", data.provisions_in_total],
    ["out", "graph.lf.secOut", data.provisions_out_total],
  ];
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="px-4 pt-3">
        <h3 className="text-[13px] font-semibold text-ink">{t("graph.lf.panel")}</h3>
        <div className="mt-2 flex gap-1 overflow-x-auto border-b border-hairline">
          {tabs.map(([key, label, n]) => (
            <button
              key={key}
              type="button"
              onClick={() => setTab(key)}
              className={`-mb-px flex shrink-0 items-center gap-1.5 whitespace-nowrap border-b-2 px-2 pb-2 text-[12px] font-medium ${
                tab === key ? "border-ink text-ink" : "border-transparent text-muted hover:text-ink"
              }`}
            >
              {t(label)}
              <span className="rounded-full bg-surface-strong px-1.5 text-[10.5px] text-muted">{n}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="min-h-0 flex-1 max-h-[70vh] overflow-y-auto px-2 pb-4 lg:max-h-none" onMouseLeave={() => onHoverDoc(null)}>
        {tab === "in" ? (
          <IncomingTree data={data} onFocusDoc={onFocusDoc} onHoverDoc={onHoverDoc} hoverDoc={hoverDoc} />
        ) : (
          <OutgoingGroups data={data} onFocusDoc={onFocusDoc} onHoverDoc={onHoverDoc} hoverDoc={hoverDoc} />
        )}
      </div>
    </div>
  );
}

function OpChip({ op }: { op: string }) {
  const { t } = useT();
  const key = `graph.lf.op.${op}` as LocaleKey;
  const label = t(key);
  const color = opColor(op);
  return (
    <span
      className="inline-block shrink-0 rounded px-1.5 py-px text-[10px] font-semibold tracking-wide"
      style={{ color, background: `color-mix(in srgb, ${color} 14%, transparent)` }}
    >
      {label === key ? op.toUpperCase() : label}
    </span>
  );
}

function DocLink({ id, docs, onFocusDoc }: { id: string; docs: Record<string, LegalGraphDoc>; onFocusDoc: (d: LegalGraphDoc) => void }) {
  const { t } = useT();
  const d = id ? docs[id] : undefined;
  if (!d) return <span className="italic text-muted-soft">{t("graph.lf.unknownDoc")}</span>;
  if (!d.focusable) return <span className="font-medium text-ink" title={`${d.title}\n${t("graph.lf.notFocusable")}`}>{d.document_number || docLabel(d)}</span>;
  return (
    <button type="button" className="font-medium text-info hover:underline" title={d.title} onClick={() => onFocusDoc(d)}>
      {d.document_number || docLabel(d)}
    </button>
  );
}

function Dates({ row }: { row: LegalGraphProvisionRow }) {
  const { t } = useT();
  const parts: string[] = [];
  if (row.eff_from) parts.push(t("graph.lf.from", { date: formatDate(row.eff_from) }));
  if (row.eff_to) parts.push(t("graph.lf.until", { date: formatDate(row.eff_to) }));
  if (!parts.length) return null;
  return <span className="shrink-0 text-muted-soft">{parts.join(" · ")}</span>;
}

function rowClass(active: boolean) {
  return `rounded-lg px-2 py-1.5 text-[12px] leading-snug transition-colors ${active ? "bg-surface-strong" : "hover:bg-surface-strong/60"}`;
}

/* ---------- Provisions of the focus acted on ---------- */

function IncomingTree({ data, onFocusDoc, onHoverDoc, hoverDoc }: Props) {
  const { t, locale } = useT();
  const articles = useMemo(() => {
    const map = new Map<string, LegalGraphProvisionRow[]>();
    for (const r of data.provisions_in) {
      const a = parsePath(r.target_path)?.article ?? "";
      map.set(a, [...(map.get(a) ?? []), r]);
    }
    for (const rows of map.values()) rows.sort((a, b) => comparePaths(a.target_path, b.target_path) || a.id - b.id);
    return [...map.entries()].sort(([a], [b]) => (!a ? 1 : !b ? -1 : compareArticles(a, b)));
  }, [data.provisions_in]);
  const allOpen = data.provisions_in.length <= OPEN_ALL_UP_TO;
  const [open, setOpen] = useState<Set<string>>(() => new Set(allOpen ? articles.map(([a]) => a) : []));

  if (data.provisions_in.length === 0) return <p className="caption px-2 py-4 text-muted">{t("graph.lf.secInEmpty")}</p>;
  const everyOpen = open.size === articles.length;
  return (
    <div className="pt-2">
      <div className="flex items-center justify-between px-2 pb-1 text-[11px] text-muted-soft">
        <span>
          {data.provisions_in.length < data.provisions_in_total
            ? t("graph.lf.truncated", { shown: data.provisions_in.length, total: data.provisions_in_total })
            : t("graph.lf.rows", { n: data.provisions_in_total })}
        </span>
        <button
          type="button"
          className="font-medium text-muted hover:text-ink"
          onClick={() => setOpen(everyOpen ? new Set() : new Set(articles.map(([a]) => a)))}
        >
          {everyOpen ? t("graph.lf.collapseAll") : t("graph.lf.expandAll")}
        </button>
      </div>
      {articles.map(([article, rows]) => {
        const isOpen = open.has(article);
        const docsHere = new Set(rows.map((r) => r.doc_id));
        const lit = !!hoverDoc && docsHere.has(hoverDoc);
        return (
          <div key={article || "?"} className="border-b border-hairline-soft last:border-b-0">
            <button
              type="button"
              className={`flex w-full items-center gap-1.5 rounded-lg px-2 py-1.5 text-left text-[12.5px] font-semibold text-ink hover:bg-surface-strong/60 ${lit ? "bg-surface-strong/60" : ""}`}
              onClick={() =>
                setOpen((s) => {
                  const n = new Set(s);
                  if (n.has(article)) n.delete(article);
                  else n.add(article);
                  return n;
                })
              }
            >
              {isOpen ? <IconChevronDown className="h-3 w-3 text-muted" /> : <IconChevronRight className="h-3 w-3 text-muted" />}
              {article ? t("graph.lf.article", { n: article }) : t("graph.lf.unknownPlace")}
              <span className="ml-1 flex gap-1">
                {[...new Set(rows.map((r) => r.op))].slice(0, 3).map((op) => (
                  <span key={op} className="inline-block h-1.5 w-1.5 rounded-full" style={{ background: opColor(op) }} />
                ))}
              </span>
              <span className="ml-auto rounded-full bg-surface-strong px-1.5 text-[10.5px] font-medium text-muted">{rows.length}</span>
            </button>
            {isOpen && (
              <ul className="pb-1 pl-5">
                {rows.map((r) => {
                  const p = parsePath(r.target_path);
                  const place = !p ? "" : p.clause || p.point ? pathLabel(r.target_path, locale, false) : t("graph.lf.wholeArticle");
                  const src = pathLabel(r.source_path, locale);
                  return (
                    <li
                      key={r.id}
                      className={rowClass(hoverDoc === r.doc_id && !!r.doc_id)}
                      onMouseEnter={() => onHoverDoc(r.doc_id || null)}
                    >
                      <div className="flex min-w-0 items-center gap-1.5">
                        <OpChip op={r.op} />
                        <span className="truncate text-ink">{place}</span>
                      </div>
                      <div className="mt-0.5 flex flex-wrap items-baseline gap-x-1.5 text-muted">
                        <span className="text-muted-soft">←</span>
                        <span>
                          <DocLink id={r.doc_id} docs={data.documents} onFocusDoc={onFocusDoc} />
                          {src && <>, {src}</>}
                        </span>
                        <Dates row={r} />
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        );
      })}
    </div>
  );
}

/* ---------- Provisions of other documents the focus acts on ---------- */

function OutgoingGroups({ data, onFocusDoc, onHoverDoc, hoverDoc }: Props) {
  const { t, locale } = useT();
  const groups = useMemo(() => {
    const map = new Map<string, LegalGraphProvisionRow[]>();
    for (const r of data.provisions_out) map.set(r.doc_id, [...(map.get(r.doc_id) ?? []), r]);
    for (const rows of map.values()) rows.sort((a, b) => comparePaths(a.target_path, b.target_path) || a.id - b.id);
    return [...map.entries()].sort((a, b) => b[1].length - a[1].length);
  }, [data.provisions_out]);
  const [open, setOpen] = useState<Set<string>>(
    () => new Set(data.provisions_out.length <= OPEN_ALL_UP_TO ? groups.map(([id]) => id) : groups.slice(0, 1).map(([id]) => id)),
  );

  if (data.provisions_out.length === 0) return <p className="caption px-2 py-4 text-muted">{t("graph.lf.secOutEmpty")}</p>;
  return (
    <div className="pt-2">
      <p className="px-2 pb-1 text-[11px] text-muted-soft">
        {data.provisions_out.length < data.provisions_out_total
          ? t("graph.lf.truncated", { shown: data.provisions_out.length, total: data.provisions_out_total })
          : t("graph.lf.rows", { n: data.provisions_out_total })}
      </p>
      {groups.map(([docId, rows]) => {
        const d = data.documents[docId];
        const isOpen = open.has(docId);
        return (
          <div
            key={docId || "?"}
            className="border-b border-hairline-soft last:border-b-0"
            onMouseEnter={() => onHoverDoc(docId || null)}
          >
            <div className={`flex items-center gap-1.5 rounded-lg px-2 py-1.5 ${hoverDoc === docId && docId ? "bg-surface-strong/60" : ""}`}>
              <button
                type="button"
                className="shrink-0 text-muted"
                onClick={() =>
                  setOpen((s) => {
                    const n = new Set(s);
                    if (n.has(docId)) n.delete(docId);
                    else n.add(docId);
                    return n;
                  })
                }
              >
                {isOpen ? <IconChevronDown className="h-3 w-3" /> : <IconChevronRight className="h-3 w-3" />}
              </button>
              <span className="min-w-0 flex-1 truncate text-[12.5px]">
                {d && <span className="text-muted">{d.doc_type} </span>}
                <DocLink id={docId} docs={data.documents} onFocusDoc={onFocusDoc} />
              </span>
              <span className="shrink-0 rounded-full bg-surface-strong px-1.5 text-[10.5px] font-medium text-muted">{rows.length}</span>
            </div>
            {isOpen && (
              <ul className="pb-1 pl-5">
                {rows.map((r) => (
                  <li key={r.id} className={rowClass(false)}>
                    <div className="flex min-w-0 items-center gap-1.5">
                      <OpChip op={r.op} />
                      <span className="truncate text-ink">{pathLabel(r.target_path, locale) || t("graph.lf.unknownPlace")}</span>
                    </div>
                    <div className="mt-0.5 flex flex-wrap items-baseline gap-x-1.5 text-muted">
                      {r.source_path && (
                        <span>
                          <span className="text-muted-soft">← </span>
                          {pathLabel(r.source_path, locale)}
                        </span>
                      )}
                      <Dates row={r} />
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        );
      })}
    </div>
  );
}
