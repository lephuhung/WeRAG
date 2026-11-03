"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  getLegalGraphFocus,
  type LegalGraphDoc,
  type LegalGraphFocus,
  type LegalGraphNeighbour,
} from "@/lib/api/graph";
import { GRAPH_FAMILY } from "@/components/graph/graph-palette";
import {
  LEGAL_FAMILY,
  REL_COLOR,
  StatusChip,
  docLabel,
  legalGroup,
  relColor,
  relFamily,
  type RelFamily,
} from "@/components/graph/legal-style";
import { IconChevronLeft, IconMinus, IconPlus, IconSearch } from "@/components/icons";
import { LegalProvisionPanel } from "@/components/graph/legal-provision-panel";
import { useT, type LocaleKey } from "@/lib/i18n";
import { formatDate } from "@/lib/datetime";

/* The "Quan hệ văn bản" view of a KB page: one catalogue document in focus,
 * the documents it acts on in the left column, the documents acting on it
 * in the right column, and its provision-level links in a side panel.
 * Deterministic column layout of HTML cards over an SVG edge layer, in one
 * pan/zoom plane (no force simulation: nothing overlaps). */

/** Group order down a column: strongest relation first, "căn cứ" last. */
const REL_ORDER = [
  "replaces", "repeals", "expires", "partially_expires", "suspends", "amends", "corrects",
  "consolidates", "guides", "interprets", "based_on",
];

/* ---------- Layout ---------- */

const CARD_W = 216;
const CARD_H = 78;
const CARD_GAP = 12;
const GROUP_HEAD = 30;
const GROUP_GAP = 22;
const MORE_H = 30;
const COL_GAP = 124;
const CENTRE_W = 268;
const CENTRE_H = 160;
const TOP = 44;
/** Cards a column shows per relation once it holds more than SIDE_LIMIT. */
const SIDE_LIMIT = 12;
const GROUP_PREVIEW = 4;

const X_LEFT = 0;
const X_CENTRE = CARD_W + COL_GAP;
const X_RIGHT = X_CENTRE + CENTRE_W + COL_GAP;
const WIDTH = X_RIGHT + CARD_W;

interface PlacedCard {
  n: LegalGraphNeighbour;
  x: number;
  y: number;
}
interface PlacedGroup {
  key: string;
  relation: string;
  direction: "out" | "in";
  x: number;
  y: number;
  total: number;
  shown: number;
  /** Loaded but folded away (the "+ k more" button). */
  folded: number;
  /** Beyond the server's per-group cap. */
  notLoaded: number;
  expanded: boolean;
  moreY: number;
}
interface Layout {
  cards: PlacedCard[];
  groups: PlacedGroup[];
  height: number;
  centreY: number;
}

function layoutColumns(
  data: LegalGraphFocus,
  showBasis: boolean,
  expanded: Set<string>,
): Layout {
  const cards: PlacedCard[] = [];
  const groups: PlacedGroup[] = [];
  const heights: number[] = [];
  for (const direction of ["out", "in"] as const) {
    const x = direction === "out" ? X_LEFT : X_RIGHT;
    const byRel = new Map<string, LegalGraphNeighbour[]>();
    for (const n of data.neighbours) {
      if (n.direction !== direction) continue;
      const rel = n.relations[0] ?? "amends";
      if (rel === "based_on" && !showBasis) continue;
      byRel.set(rel, [...(byRel.get(rel) ?? []), n]);
    }
    const rels = [...byRel.keys()].sort((a, b) => rank(a) - rank(b));
    const sideTotal = rels.reduce((s, r) => s + byRel.get(r)!.length, 0);
    let y = TOP;
    for (const rel of rels) {
      const list = byRel.get(rel)!;
      const key = `${direction}:${rel}`;
      const isOpen = expanded.has(key);
      const limit = sideTotal > SIDE_LIMIT && !isOpen ? GROUP_PREVIEW : list.length;
      const shown = list.slice(0, limit);
      const total = Math.max(data.neighbour_totals[key] ?? 0, list.length);
      const g: PlacedGroup = {
        key, relation: rel, direction, x, y, total, shown: shown.length,
        folded: list.length - shown.length, notLoaded: total - list.length, expanded: isOpen, moreY: 0,
      };
      y += GROUP_HEAD;
      for (const n of shown) {
        cards.push({ n, x, y });
        y += CARD_H + CARD_GAP;
      }
      if (g.folded > 0 || g.notLoaded > 0 || (isOpen && list.length > GROUP_PREVIEW)) {
        g.moreY = y;
        y += MORE_H;
      }
      groups.push(g);
      y += GROUP_GAP;
    }
    heights.push(y);
  }
  const height = Math.max(...heights, TOP + CENTRE_H + 60);
  // The focus sits level with the first cards, not lost mid-way down a
  // long column.
  const centreY = Math.min(Math.max(TOP, (height - CENTRE_H) / 2), TOP + 140);
  return { cards, groups, height, centreY };
}

function rank(rel: string) {
  const i = REL_ORDER.indexOf(rel);
  return i < 0 ? REL_ORDER.length : i;
}

/* ---------- Small parts ---------- */

function Card({
  doc,
  relation,
  fromProvisions,
  highlighted,
  dimmed,
  onFocus,
  onHover,
}: {
  doc: LegalGraphDoc;
  relation: string;
  fromProvisions?: boolean;
  highlighted: boolean;
  dimmed: boolean;
  onFocus: () => void;
  onHover: (on: boolean) => void;
}) {
  const { t } = useT();
  const fam = GRAPH_FAMILY[LEGAL_FAMILY[legalGroup(doc.doc_type)]];
  const date = formatDate(doc.issued_on);
  return (
    <button
      type="button"
      data-card
      disabled={!doc.focusable}
      onClick={onFocus}
      onMouseEnter={() => onHover(true)}
      onMouseLeave={() => onHover(false)}
      title={doc.focusable ? doc.title : `${doc.title}\n${t("graph.lf.notFocusable")}`}
      className={`group absolute flex overflow-hidden rounded-xl border bg-surface-card text-left shadow-sm transition-[box-shadow,opacity,border-color] ${
        highlighted ? "border-ink shadow-md" : "border-hairline"
      } ${dimmed ? "opacity-35" : ""} ${
        doc.focusable ? "cursor-pointer hover:border-hairline-strong hover:shadow-md" : "cursor-default"
      } ${relation === "based_on" ? "border-dashed" : ""}`}
      style={{ width: CARD_W, height: CARD_H }}
    >
      <span className="w-1.5 shrink-0" style={{ background: fam.fill }} />
      <span className="flex min-w-0 flex-1 flex-col justify-center gap-0.5 px-3 py-2">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-[13.5px] font-semibold text-ink">{doc.document_number || doc.title}</span>
          {doc.in_kb && (
            <span className="shrink-0 rounded bg-info-surface px-1 text-[10px] font-medium text-info">
              {t("graph.lf.inKb")}
            </span>
          )}
        </span>
        <span className="truncate text-[11.5px] text-muted">
          {[doc.doc_type, date].filter(Boolean).join(" · ")}
          {fromProvisions && <span className="text-muted-soft"> · {t("graph.lf.fromProvisions")}</span>}
        </span>
        <span className="flex min-w-0">
          <StatusChip status={doc.status} />
        </span>
      </span>
    </button>
  );
}

function CentreCard({ doc, onOpenKnowledge, related }: { doc: LegalGraphDoc; onOpenKnowledge?: (id: string) => void; related: number }) {
  const { t } = useT();
  const fam = GRAPH_FAMILY[LEGAL_FAMILY[legalGroup(doc.doc_type)]];
  return (
    <div
      data-card
      className="absolute flex flex-col overflow-hidden rounded-2xl border-2 bg-surface-card shadow-lg"
      style={{ width: CENTRE_W, height: CENTRE_H, borderColor: fam.stroke }}
    >
      <div className="h-1.5 shrink-0" style={{ background: fam.fill }} />
      <div className="flex min-h-0 flex-1 flex-col gap-1 px-4 py-2.5">
        <span className="flex items-center gap-2 text-[10.5px] font-medium uppercase tracking-wider text-muted">
          {t("graph.lf.focus")}
          <span className="ml-auto normal-case tracking-normal text-muted-soft">{t("graph.lf.related", { n: related })}</span>
        </span>
        <span className="truncate text-[17px] font-semibold leading-tight text-ink" title={docLabel(doc)}>
          {docLabel(doc)}
        </span>
        <span className="line-clamp-2 text-[11.5px] leading-snug text-muted" title={doc.title}>
          {doc.title}
        </span>
        <span className="mt-auto flex min-w-0 items-center gap-2">
          <StatusChip status={doc.status} />
          {doc.issued_on && <span className="shrink-0 text-[11px] text-muted">{formatDate(doc.issued_on)}</span>}
        </span>
      </div>
      {doc.in_kb && doc.knowledge_id && onOpenKnowledge && (
        <button
          type="button"
          className="border-t border-hairline px-4 py-1.5 text-left text-[12px] font-medium text-info hover:bg-surface-strong"
          onClick={() => onOpenKnowledge(doc.knowledge_id!)}
        >
          {t("graph.lf.openInKb")} →
        </button>
      )}
    </div>
  );
}

/* ---------- Picker ---------- */

function Picker({ kbId, initial, onPick }: { kbId: string; initial: LegalGraphDoc[]; onPick: (d: LegalGraphDoc) => void }) {
  const { t } = useT();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [hits, setHits] = useState<LegalGraphDoc[] | null>(null);
  const boxRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const term = q.trim();
    if (!term) {
      setHits(null);
      return;
    }
    let alive = true;
    const timer = setTimeout(() => {
      getLegalGraphFocus(kbId, { q: term })
        .then((r) => alive && setHits(r.data.candidates ?? []))
        .catch(() => alive && setHits([]));
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [kbId, q]);

  useEffect(() => {
    if (!open) return;
    const close = (e: PointerEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, [open]);

  const list = q.trim() ? hits : initial;
  return (
    <div ref={boxRef} className="relative w-full sm:w-80">
      <IconSearch className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-soft" />
      <input
        type="text"
        className="input h-8 w-full pl-8 pr-2 text-[12.5px]"
        placeholder={t("graph.lf.picker")}
        value={q}
        onFocus={() => setOpen(true)}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
        }}
        onKeyDown={(e) => {
          if (e.key === "Escape") setOpen(false);
          if (e.key === "Enter" && list?.[0]) {
            onPick(list[0]);
            setOpen(false);
            setQ("");
          }
        }}
      />
      {open && list && (
        <div className="absolute left-0 right-0 top-9 z-30 max-h-80 overflow-y-auto rounded-xl border border-hairline bg-surface-card p-1 shadow-lg">
          {!q.trim() && <p className="px-2 pb-1 pt-1.5 text-[10.5px] font-medium uppercase tracking-wider text-muted-soft">{t("graph.lf.pickerTop")}</p>}
          {list.length === 0 && <p className="caption px-2 py-2 text-muted">{t("graph.lf.pickerEmpty")}</p>}
          {list.map((d) => (
            <button
              key={d.id}
              type="button"
              className="flex w-full flex-col rounded-lg px-2 py-1.5 text-left hover:bg-surface-strong"
              onClick={() => {
                onPick(d);
                setOpen(false);
                setQ("");
              }}
            >
              <span className="flex items-center gap-2">
                <span
                  className="inline-block h-2 w-2 shrink-0 rounded-full"
                  style={{ background: GRAPH_FAMILY[LEGAL_FAMILY[legalGroup(d.doc_type)]].fill }}
                />
                <span className="truncate text-[12.5px] font-medium text-ink">{docLabel(d)}</span>
                {!!d.degree && <span className="ml-auto shrink-0 text-[11px] text-muted-soft">{d.degree}</span>}
              </span>
              <span className="truncate pl-4 text-[11px] text-muted">{d.title}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/* ---------- Pan / zoom plane ---------- */

/** The smallest zoom "fit" picks: below it card text is unreadable. */
const MIN_FIT = 0.72;

interface View {
  zoom: number;
  x: number;
  y: number;
}

function usePanZoom(contentW: number, contentH: number, focusX: number, fitKey: string) {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const [view, setView] = useState<View>({ zoom: 1, x: 0, y: 0 });
  const drag = useRef<{ px: number; py: number; x: number; y: number } | null>(null);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setSize({ w: el.clientWidth, h: el.clientHeight }));
    ro.observe(el);
    setSize({ w: el.clientWidth, h: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  const fit = useCallback(() => {
    const { w, h } = size;
    if (!w || !h) return;
    const pad = 24;
    let zoom = Math.min((w - pad * 2) / contentW, (h - pad * 2) / contentH, 1.15);
    let y = (h - contentH * zoom) / 2;
    // A long column: fit the width and start at the top rather than
    // shrinking the cards past legibility; never below MIN_FIT (pan for
    // the rest).
    if (zoom < 0.75) {
      zoom = Math.max(Math.min((w - pad * 2) / contentW, 1), MIN_FIT);
      y = pad;
    }
    // Wider than the box (a phone): keep the focus card in the middle.
    const x = contentW * zoom > w ? w / 2 - focusX * zoom : (w - contentW * zoom) / 2;
    setView({ zoom, x, y });
  }, [size, contentW, contentH, focusX]);

  // Refit whenever the content or the box changes shape.
  useEffect(() => {
    fit();
  }, [fit, fitKey]);

  const zoomAt = useCallback((factor: number, cx: number, cy: number) => {
    setView((v) => {
      const zoom = Math.min(2.5, Math.max(0.25, v.zoom * factor));
      const k = zoom / v.zoom;
      return { zoom, x: cx - (cx - v.x) * k, y: cy - (cy - v.y) * k };
    });
  }, []);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? el.clientHeight : 1;
      if (e.ctrlKey || e.metaKey) {
        const rect = el.getBoundingClientRect();
        zoomAt(Math.exp(-e.deltaY * unit * 0.0015), e.clientX - rect.left, e.clientY - rect.top);
      } else {
        setView((v) => ({ ...v, x: v.x - e.deltaX * unit, y: v.y - e.deltaY * unit }));
      }
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [zoomAt]);

  const handlers = {
    onPointerDown: (e: React.PointerEvent) => {
      if ((e.target as HTMLElement).closest("[data-card],button")) return;
      drag.current = { px: e.clientX, py: e.clientY, x: view.x, y: view.y };
      (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    },
    onPointerMove: (e: React.PointerEvent) => {
      const d = drag.current;
      if (!d) return;
      setView((v) => ({ ...v, x: d.x + e.clientX - d.px, y: d.y + e.clientY - d.py }));
    },
    onPointerUp: () => {
      drag.current = null;
    },
  };
  return { ref, view, fit, zoomBy: (f: number) => zoomAt(f, size.w / 2, size.h / 2), handlers };
}

/* ---------- The view ---------- */

export function LegalFocusGraph({ kbId, onOpenKnowledge }: { kbId: string; onOpenKnowledge?: (knowledgeId: string) => void }) {
  const { t } = useT();
  const [target, setTarget] = useState("");
  const [trail, setTrail] = useState<LegalGraphDoc[]>([]);
  const [data, setData] = useState<LegalGraphFocus | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [showBasis, setShowBasis] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [hoverDoc, setHoverDoc] = useState<string | null>(null);
  const [topCandidates, setTopCandidates] = useState<LegalGraphDoc[]>([]);
  // The KB page leaves the graph tab little height: full screen gives the
  // columns the whole viewport.
  const [full, setFull] = useState(false);
  useEffect(() => {
    if (!full) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setFull(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [full]);

  useEffect(() => {
    setTarget("");
    setTrail([]);
    setTopCandidates([]);
  }, [kbId]);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError("");
    getLegalGraphFocus(kbId, { doc: target || undefined })
      .then((r) => {
        if (!alive) return;
        const d = r.data;
        setData(d);
        setExpanded(new Set());
        setHoverDoc(null);
        if (!target && d.candidates?.length) setTopCandidates(d.candidates);
        if (d.focus) {
          const f = d.focus;
          setTrail((tr) => (tr.length && tr[tr.length - 1].id === f.id ? [...tr.slice(0, -1), f] : tr.length ? tr : [f]));
        }
      })
      .catch((e) => alive && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [kbId, target]);

  const focusDoc = useCallback(
    (d: LegalGraphDoc) => {
      if (!d.focusable || d.id === data?.focus?.id) return;
      setTrail((tr) => [...tr, d].slice(-12));
      setTarget(d.id);
    },
    [data?.focus?.id],
  );
  const goTo = (i: number) => {
    const d = trail[i];
    if (!d) return;
    setTrail(trail.slice(0, i + 1));
    setTarget(d.id);
  };

  const layout = useMemo(() => (data?.focus ? layoutColumns(data, showBasis, expanded) : null), [data, showBasis, expanded]);
  const basisCount = useMemo(
    () => (data?.neighbours ?? []).filter((n) => n.relations[0] === "based_on").length,
    [data],
  );
  const fitKey = `${data?.focus?.id}|${showBasis}|${layout?.height}|${full}`;
  const pz = usePanZoom(WIDTH, layout?.height ?? 400, X_CENTRE + CENTRE_W / 2, fitKey);

  const toggleGroup = (key: string) =>
    setExpanded((s) => {
      const next = new Set(s);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const focus = data?.focus ?? null;
  const empty = !loading && !error && data && !focus;

  return (
    <div
      className={
        full
          ? "fixed inset-0 z-50 flex flex-col overflow-y-auto bg-surface-card lg:overflow-hidden"
          : "flex min-h-0 flex-1 flex-col overflow-y-auto lg:overflow-hidden"
      }
    >
      {/* toolbar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-hairline px-4 py-2.5 sm:px-6">
        <Picker kbId={kbId} initial={topCandidates} onPick={(d) => { setTrail([d]); setTarget(d.id); }} />
        {trail.length > 1 && (
          <button type="button" className="btn btn-ghost btn-sm h-8 gap-1 px-2 text-[12px]" onClick={() => goTo(trail.length - 2)}>
            <IconChevronLeft className="h-3.5 w-3.5" />
            {t("graph.lf.back")}
          </button>
        )}
        <nav className="flex min-w-0 flex-1 basis-40 items-center gap-1 overflow-x-auto text-[12px] text-muted">
          {trail.map((d, i) => (
            <span key={`${d.id}-${i}`} className="flex shrink-0 items-center gap-1">
              {i > 0 && <span className="text-muted-soft">›</span>}
              {i === trail.length - 1 ? (
                <span className="font-medium text-ink">{d.document_number || d.title}</span>
              ) : (
                <button type="button" className="hover:text-ink hover:underline" onClick={() => goTo(i)}>
                  {d.document_number || d.title}
                </button>
              )}
            </span>
          ))}
        </nav>
        <div className="flex flex-wrap items-center gap-2">
          {basisCount > 0 && (
            <label className="flex cursor-pointer items-center gap-1.5 whitespace-nowrap text-[12px] text-muted" title={t("graph.lf.basisTitle")}>
              <input type="checkbox" checked={showBasis} onChange={(e) => setShowBasis(e.target.checked)} />
              {t("graph.lf.basis", { n: basisCount })}
            </label>
          )}
          <div className="flex items-center rounded-lg border border-hairline bg-surface-card p-0.5">
            <button type="button" className="btn btn-ghost btn-sm h-7 w-7 p-0" onClick={() => pz.zoomBy(1.2)} title={t("graph.zoomIn")}>
              <IconPlus className="h-3.5 w-3.5" />
            </button>
            <button type="button" className="btn btn-ghost btn-sm h-7 w-7 p-0" onClick={() => pz.zoomBy(1 / 1.2)} title={t("graph.zoomOut")}>
              <IconMinus className="h-3.5 w-3.5" />
            </button>
          </div>
          <button type="button" className="btn btn-outline btn-sm h-7 text-[12px]" onClick={pz.fit} title={t("graph.fitTitle")}>
            {t("graph.fit")}
          </button>
          <span className="hidden w-10 text-right text-[12px] text-muted-soft sm:inline">{Math.round(pz.view.zoom * 100)}%</span>
          <button type="button" className="btn btn-outline btn-sm h-7 text-[12px]" onClick={() => setFull((f) => !f)}>
            {full ? t("graph.lf.exitFullscreen") : t("graph.lf.fullscreen")}
          </button>
        </div>
      </div>
      {error && <p className="caption px-6 pt-3 text-error">{error}</p>}

      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        {/* graph */}
        <div className="flex min-h-[520px] min-w-0 flex-1 flex-col lg:min-h-0">
          <div
            ref={pz.ref}
            className="relative min-h-0 flex-1 cursor-grab touch-none select-none overflow-hidden bg-canvas-soft active:cursor-grabbing"
            {...pz.handlers}
          >
            {loading && !layout && (
              <div className="absolute inset-0 flex items-center justify-center">
                <p className="caption animate-pulse text-muted">{t("graph.loading")}</p>
              </div>
            )}
            {empty && (
              <div className="absolute inset-0 flex items-center justify-center px-6">
                <p className="caption max-w-md text-center text-muted">{t("graph.legalEmpty")}</p>
              </div>
            )}
            {focus && layout && (
              <div
                className={`absolute left-0 top-0 origin-top-left transition-opacity ${loading ? "opacity-50" : ""}`}
                style={{
                  width: WIDTH,
                  height: layout.height,
                  transform: `translate(${pz.view.x}px, ${pz.view.y}px) scale(${pz.view.zoom})`,
                }}
              >
                <Edges layout={layout} hoverDoc={hoverDoc} />
                <ColumnHead x={X_LEFT} text={t("graph.lf.colOut")} />
                <ColumnHead x={X_RIGHT} text={t("graph.lf.colIn")} />
                {layout.groups.map((g) => (
                  <GroupHead key={g.key} g={g} onToggle={() => toggleGroup(g.key)} />
                ))}
                {!layout.groups.some((g) => g.direction === "out") && (
                  <SideEmpty x={X_LEFT} y={layout.centreY + CENTRE_H / 2 - 12} text={t("graph.lf.sideEmptyOut")} />
                )}
                {!layout.groups.some((g) => g.direction === "in") && (
                  <SideEmpty x={X_RIGHT} y={layout.centreY + CENTRE_H / 2 - 12} text={t("graph.lf.sideEmptyIn")} />
                )}
                {layout.cards.map(({ n, x, y }) => (
                  <div key={`${n.direction}:${n.doc.id}`} className="absolute" style={{ left: x, top: y }}>
                    <Card
                      doc={n.doc}
                      relation={n.relations[0]}
                      fromProvisions={n.from_provisions}
                      highlighted={hoverDoc === n.doc.id}
                      dimmed={!!hoverDoc && hoverDoc !== n.doc.id}
                      onFocus={() => focusDoc(n.doc)}
                      onHover={(on) => setHoverDoc(on ? n.doc.id : null)}
                    />
                  </div>
                ))}
                <div className="absolute" style={{ left: X_CENTRE, top: layout.centreY }}>
                  <CentreCard
                    doc={focus}
                    onOpenKnowledge={onOpenKnowledge}
                    related={new Set(layout.cards.map((c) => c.n.doc.id)).size}
                  />
                </div>
              </div>
            )}
          </div>
          <Legend />
        </div>

        {/* side panel */}
        {focus && data && (
          <aside data-legal-panel className="flex min-h-0 w-full shrink-0 flex-col border-t border-hairline lg:w-[360px] lg:border-l lg:border-t-0 xl:w-[400px]">
            <LegalProvisionPanel
              key={focus.id}
              data={data}
              onFocusDoc={focusDoc}
              onHoverDoc={setHoverDoc}
              hoverDoc={hoverDoc}
            />
          </aside>
        )}
      </div>
    </div>
  );
}

function ColumnHead({ x, text }: { x: number; text: string }) {
  return (
    <div className="absolute whitespace-nowrap text-[11px] font-semibold uppercase tracking-wider text-muted" style={{ left: x, top: 8 }}>
      {text}
    </div>
  );
}

function SideEmpty({ x, y, text }: { x: number; y: number; text: string }) {
  return (
    <div
      className="absolute rounded-xl border border-dashed border-hairline px-3 py-2 text-center text-[12px] text-muted-soft"
      style={{ left: x, top: y, width: CARD_W }}
    >
      {text}
    </div>
  );
}

function GroupHead({ g, onToggle }: { g: PlacedGroup; onToggle: () => void }) {
  const { t } = useT();
  const label = t(`graph.lf.${g.direction}.${g.relation}` as LocaleKey);
  let more: ReactNode = null;
  if (g.moreY) {
    more = (
      <div className="absolute flex items-center gap-2" style={{ left: g.x, top: g.moreY, width: CARD_W, height: MORE_H }}>
        {(g.folded > 0 || g.expanded) && (
          <button
            type="button"
            className="rounded-full border border-hairline bg-surface-card px-2.5 py-0.5 text-[11.5px] font-medium text-ink hover:bg-surface-strong"
            onClick={onToggle}
          >
            {g.expanded ? t("graph.lf.less") : t("graph.lf.more", { n: g.folded })}
          </button>
        )}
        {g.notLoaded > 0 && (g.expanded || g.folded === 0) && (
          <span className="text-[11px] text-muted-soft">{t("graph.lf.notLoaded", { n: g.notLoaded })}</span>
        )}
      </div>
    );
  }
  return (
    <>
      <div
        className="absolute flex items-center gap-2 text-[12px] font-semibold"
        style={{ left: g.x, top: g.y + 4, width: CARD_W, color: relColor(g.relation) }}
      >
        <span className="inline-block h-2 w-2 shrink-0 rounded-full" style={{ background: relColor(g.relation) }} />
        <span className="truncate">{label}</span>
        <span className="ml-auto shrink-0 rounded-full bg-surface-strong px-1.5 text-[10.5px] font-medium text-muted">{g.total}</span>
      </div>
      {more}
    </>
  );
}

function Edges({ layout, hoverDoc }: { layout: Layout; hoverDoc: string | null }) {
  const { t } = useT();
  const { cards, centreY } = layout;
  // Spread the attachment points along each side of the focus card.
  const anchors = (dir: "out" | "in") => {
    const list = cards.filter((c) => c.n.direction === dir);
    const top = centreY + 24;
    const span = CENTRE_H - 48;
    return new Map(list.map((c, i) => [c, list.length === 1 ? centreY + CENTRE_H / 2 : top + (span * i) / (list.length - 1)]));
  };
  const outA = anchors("out");
  const inA = anchors("in");
  return (
    <svg className="pointer-events-none absolute left-0 top-0 overflow-visible" width={WIDTH} height={layout.height}>
      <defs>
        {(["amends", "replaces", "repeals", "guides", "consolidates", "based_on"] as RelFamily[]).map((f) => (
          <marker key={f} id={`lf-arrow-${f}`} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M0,0 L10,5 L0,10 z" fill={GRAPH_FAMILY[REL_COLOR[f]].stroke} />
          </marker>
        ))}
      </defs>
      {cards.map((c) => {
        const rel = c.n.relations[0] ?? "amends";
        const fam = relFamily(rel);
        const color = relColor(rel);
        const cy = c.y + CARD_H / 2;
        const out = c.n.direction === "out";
        const ay = (out ? outA : inA).get(c)!;
        // out: focus → card (arrow at the card); in: card → focus.
        const cardX = out ? c.x + CARD_W : c.x;
        const focusX = out ? X_CENTRE : X_CENTRE + CENTRE_W;
        const mid = (cardX + focusX) / 2;
        const d = out
          ? `M${focusX},${ay} C${mid},${ay} ${mid},${cy} ${cardX + 2},${cy}`
          : `M${cardX},${cy} C${mid},${cy} ${mid},${ay} ${focusX - 2},${ay}`;
        const active = hoverDoc === c.n.doc.id;
        const faded = !!hoverDoc && !active;
        const label = t(`graph.lf.edge.${rel}` as LocaleKey);
        const lx = out ? cardX + 10 : cardX - 10;
        return (
          <g key={`${c.n.direction}:${c.n.doc.id}`} opacity={faded ? 0.15 : 1}>
            <path
              d={d}
              fill="none"
              stroke={color}
              strokeWidth={active ? 3 : 1.6}
              strokeDasharray={fam === "based_on" ? "5 4" : undefined}
              markerEnd={`url(#lf-arrow-${fam})`}
            />
            <text
              x={lx}
              y={cy - 6}
              textAnchor={out ? "start" : "end"}
              fontSize={10.5}
              fontWeight={500}
              fill={color}
              stroke="var(--color-canvas-soft)"
              strokeWidth={3}
              paintOrder="stroke"
            >
              {label}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

function Legend() {
  const { t } = useT();
  const fams: RelFamily[] = ["amends", "replaces", "repeals", "guides", "consolidates", "based_on"];
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-t border-hairline px-4 py-2 sm:px-6">
      {fams.map((f) => (
        <span key={f} className="caption flex items-center gap-1.5 text-muted">
          <svg width="22" height="8" aria-hidden>
            <line
              x1="0" y1="4" x2="22" y2="4"
              stroke={GRAPH_FAMILY[REL_COLOR[f]].stroke}
              strokeWidth="2.5"
              strokeDasharray={f === "based_on" ? "4 3" : undefined}
            />
          </svg>
          {t(`graph.lf.legend.${f}` as LocaleKey)}
        </span>
      ))}
      <span className="mx-1 hidden h-3 w-px bg-hairline sm:inline-block" />
      {Object.entries(LEGAL_FAMILY).map(([g, fam]) => (
        <span key={g} className="caption flex items-center gap-1.5 text-muted">
          <span className="inline-block h-2.5 w-2.5 rounded-sm" style={{ background: GRAPH_FAMILY[fam].fill }} />
          {t(`graph.legal.${g}` as LocaleKey)}
        </span>
      ))}
      <span className="caption ml-auto hidden text-muted-soft 2xl:inline">{t("graph.lf.hint")}</span>
    </div>
  );
}
