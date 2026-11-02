"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import Link from "next/link";
import { useT } from "@/lib/i18n";
import styles from "./split-pane.module.css";
import { DEFAULT_SPLIT_BOUNDS, clampLeftPct, type SplitBounds } from "./split-pane-math";
import { DOCUMENT_EDITOR, DOCUMENT_SIDE_BY_SIDE_QUERY, DOCUMENT_STACKED_QUERY } from "./document-layout-policy";

const DEFAULT_STORAGE_KEY = "werag.docWorkspace.leftPct";

function readStoredPct(key: string, bounds: SplitBounds): number {
  try {
    const v = Number(localStorage.getItem(key));
    if (Number.isFinite(v)) return clampLeftPct(v, bounds);
  } catch {
    return bounds.def;
  }
  return bounds.def;
}

function useIsWide(query = DOCUMENT_STACKED_QUERY): boolean {
  const [wide, setWide] = useState(true);
  // Layout effect: the real breakpoint is applied before the first paint, so
  // phones never flash the side-by-side layout.
  useLayoutEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) return;
    const mql = window.matchMedia(query);
    const apply = () => setWide(mql.matches);
    apply();
    mql.addEventListener("change", apply);
    return () => mql.removeEventListener("change", apply);
  }, [query]);
  return wide;
}

/* Two-pane layout for the document assistant: editor on the left (55–75%,
 * draggable, persisted per browser), chat on the right. Document mode needs
 * a viewport of at least 1440 × 800 CSS pixels; a smaller document viewport
 * stacks the editor above the assistant and covers the editor area with a
 * notice — the assistant itself stays usable.
 *
 * `left={null}` renders the right pane alone with no divider and never
 * blocks: there is no editor to make room for. The element tree is
 * identical in every mode (left slot / divider / right wrapper keep their
 * child positions), so toggling the editor never remounts `right`.
 * Other pages pass their own `bounds` and `storageKey` (the legal draft
 * review: PDF left, editor right). */
export function SplitPane({
  left,
  right,
  bounds = DEFAULT_SPLIT_BOUNDS,
  storageKey = DEFAULT_STORAGE_KEY,
  documentMode = false,
}: {
  left: ReactNode | null;
  right: ReactNode;
  bounds?: SplitBounds;
  storageKey?: string;
  documentMode?: boolean;
}) {
  const { t } = useT();
  const wide = useIsWide(documentMode ? DOCUMENT_SIDE_BY_SIDE_QUERY : DOCUMENT_STACKED_QUERY);
  const [leftPct, setLeftPct] = useState(bounds.def);
  const boundsRef = useRef(bounds);
  boundsRef.current = bounds;
  const [dragging, setDragging] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const pctRef = useRef(leftPct);

  useEffect(() => {
    const v = readStoredPct(storageKey, boundsRef.current);
    pctRef.current = v;
    setLeftPct(v);
  }, [storageKey]);

  const persist = (v: number) => {
    try {
      localStorage.setItem(storageKey, String(Math.round(v * 10) / 10));
    } catch {
      /* ignore */
    }
  };

  const update = (v: number) => {
    pctRef.current = v;
    setLeftPct(v);
  };

  const startResize = useCallback((e: ReactPointerEvent) => {
    e.preventDefault();
    const box = containerRef.current?.getBoundingClientRect();
    if (!box || box.width <= 0) return;
    setDragging(true);
    const onMove = (ev: PointerEvent) => {
      update(clampLeftPct(((ev.clientX - box.left) / box.width) * 100, boundsRef.current));
    };
    const onUp = () => {
      setDragging(false);
      persist(pctRef.current);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onUp);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey]);

  const onKeyDown = (e: React.KeyboardEvent) => {
    const step = e.shiftKey ? 5 : 1;
    if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
      e.preventDefault();
      const next = clampLeftPct(leftPct + (e.key === "ArrowLeft" ? -step : step), bounds);
      update(next);
      persist(next);
      return;
    }
    if (e.key === "Home") {
      e.preventDefault();
      update(bounds.min);
      persist(bounds.min);
      return;
    }
    if (e.key === "End") {
      e.preventDefault();
      update(bounds.max);
      persist(bounds.max);
      return;
    }
    if (e.key === "Enter" || e.key === " ") {
      /* Double-tap on the separator resets it to the default width. */
      e.preventDefault();
      update(bounds.def);
      persist(bounds.def);
    }
  };

  const resetWidth = () => {
    update(bounds.def);
    persist(bounds.def);
  };

  const hasLeft = left !== null && left !== undefined && left !== false;
  /* Only block when there is an editor to make room for: with no left pane
   * (read-only mode) the assistant must stay usable on a phone. */
  const blocked = documentMode && hasLeft && !wide;
  const sideBySide = hasLeft && !blocked && (documentMode || wide);

  return (
    <div className={styles.root}>
    <div
      ref={containerRef}
      className={`${styles.panes} ${sideBySide ? "relative flex flex-1 overflow-hidden" : "flex flex-1 flex-col overflow-hidden"}`}
    >
      {hasLeft ? (
        <div
          className={
            sideBySide
              ? "relative flex min-w-0 flex-col overflow-hidden"
              : "hairline-b relative flex shrink-0 flex-col overflow-hidden"
          }
          style={sideBySide ? { width: `${leftPct}%` } : { height: DOCUMENT_EDITOR.stackedEditorHeight }}
        >
          {/* The editor stays mounted (so an open document keeps its state)
              but is inert and invisible while the viewport is too small. */}
          <div
            className={blocked ? "absolute inset-0 invisible" : "absolute inset-0"}
            inert={blocked}
            aria-hidden={blocked ? true : undefined}
          >
            {left}
          </div>
          {blocked ? (
            <>
              <div className={styles.placeholder} aria-hidden="true" />
              <div className={styles.overlay}>
                <section className={styles.notice} role="status" aria-live="polite">
                  <svg className={styles.monitor} width="44" height="40" viewBox="0 0 44 40" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><rect x="3" y="3" width="38" height="26" rx="4" /><path d="M16 37h12M22 29v8M14 12l-4 4 4 4m16-8 4 4-4 4M10 16h24" /></svg>
                  <h2>{t("docws.tooSmallTitle")}</h2>
                  <p>{t("docws.tooSmallDesc")}</p>
                  <div className={styles.minimum}>
                    {DOCUMENT_EDITOR.minSizeLabel}
                    <span>{t("docws.tooSmallMinLabel")}</span>
                  </div>
                  <p>{t("docws.tooSmallHint")}</p>
                  <Link href="/platform/creatChat">{t("docws.tooSmallBack")}</Link>
                </section>
              </div>
            </>
          ) : null}
        </div>
      ) : null}
      {sideBySide ? (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-valuemin={bounds.min}
          aria-valuemax={bounds.max}
          aria-valuenow={Math.round(leftPct)}
          aria-valuetext={t("docws.resizeValue", { n: Math.round(leftPct) })}
          aria-label={t("docws.resize")}
          title={t("docws.resize")}
          tabIndex={0}
          onPointerDown={startResize}
          onDoubleClick={resetWidth}
          onKeyDown={onKeyDown}
          className="group relative z-10 w-1.5 shrink-0 cursor-col-resize bg-hairline/60 outline-none focus-visible:bg-ink/30"
        >
          <div className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-hairline transition-colors group-hover:bg-ink/30" />
        </div>
      ) : null}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">{right}</div>
      {/* While dragging, a transparent shield keeps the editor iframe from
          swallowing pointermove events. */}
      {dragging && sideBySide && !blocked ? <div className="fixed inset-0 z-50 cursor-col-resize" /> : null}
    </div>
    </div>
  );
}
