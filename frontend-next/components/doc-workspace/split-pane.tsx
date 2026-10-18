"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { useT } from "@/lib/i18n";
import { DEFAULT_SPLIT_BOUNDS, clampLeftPct, type SplitBounds } from "./split-pane-math";

const DEFAULT_STORAGE_KEY = "werag.docWorkspace.leftPct";
const WIDE_QUERY = "(min-width: 1024px)";

function readStoredPct(key: string, bounds: SplitBounds): number {
  try {
    const raw = localStorage.getItem(key);
    return raw === null ? bounds.def : clampLeftPct(Number(raw), bounds);
  } catch {
    return bounds.def;
  }
}

function useIsWide(): boolean {
  const [wide, setWide] = useState(true);
  // Layout effect: the real breakpoint is applied before the first paint, so
  // phones never flash the side-by-side layout.
  useLayoutEffect(() => {
    const mq = window.matchMedia(WIDE_QUERY);
    const sync = () => setWide(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);
  return wide;
}

/* Two-pane layout for the document assistant: editor on the left (55–75%,
 * draggable, persisted per browser), chat on the right. Below 1024px the
 * panes stack: editor on top at a fixed 55vh, chat underneath.
 *
 * `left={null}` renders the right pane alone with no divider. The element
 * tree is identical in every mode (left slot / divider / right wrapper keep
 * their child positions), so toggling the editor never remounts `right`.
 * Other pages pass their own `bounds` and `storageKey` (the legal draft
 * review: PDF left, editor right). */
export function SplitPane({
  left,
  right,
  bounds = DEFAULT_SPLIT_BOUNDS,
  storageKey = DEFAULT_STORAGE_KEY,
}: {
  left: ReactNode | null;
  right: ReactNode;
  bounds?: SplitBounds;
  storageKey?: string;
}) {
  const { t } = useT();
  const wide = useIsWide();
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
    }
  };

  const hasLeft = left !== null && left !== undefined && left !== false;
  const sideBySide = hasLeft && wide;

  return (
    <div
      ref={containerRef}
      className={sideBySide ? "relative flex flex-1 overflow-hidden" : "flex flex-1 flex-col overflow-hidden"}
    >
      {hasLeft ? (
        <div
          className={
            sideBySide
              ? "flex min-w-0 flex-col overflow-hidden"
              : "hairline-b flex h-[55vh] shrink-0 flex-col overflow-hidden"
          }
          style={sideBySide ? { width: `${leftPct}%` } : undefined}
        >
          {left}
        </div>
      ) : null}
      {sideBySide ? (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-valuemin={bounds.min}
          aria-valuemax={bounds.max}
          aria-valuenow={Math.round(leftPct)}
          aria-label={t("docws.resize")}
          title={t("docws.resize")}
          tabIndex={0}
          onPointerDown={startResize}
          onKeyDown={onKeyDown}
          className="group relative z-10 w-1.5 shrink-0 cursor-col-resize bg-hairline/60 outline-none focus-visible:bg-ink/30"
        >
          <div className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-hairline transition-colors group-hover:bg-ink/30" />
        </div>
      ) : null}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">{right}</div>
      {/* While dragging, a transparent shield keeps the editor iframe from
          swallowing pointermove events. */}
      {dragging && sideBySide ? <div className="fixed inset-0 z-50 cursor-col-resize" /> : null}
    </div>
  );
}
