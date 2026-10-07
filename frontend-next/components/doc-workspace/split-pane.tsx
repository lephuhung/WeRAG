"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { useT } from "@/lib/i18n";
import { SPLIT_DEFAULT_PCT, SPLIT_MAX_PCT, SPLIT_MIN_PCT, clampLeftPct } from "./split-pane-math";

const STORAGE_KEY = "werag.docWorkspace.leftPct";
const WIDE_QUERY = "(min-width: 1024px)";

function readStoredPct(): number {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw === null ? SPLIT_DEFAULT_PCT : clampLeftPct(Number(raw));
  } catch {
    return SPLIT_DEFAULT_PCT;
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
 * their child positions), so toggling the editor never remounts `right`. */
export function SplitPane({ left, right }: { left: ReactNode | null; right: ReactNode }) {
  const { t } = useT();
  const wide = useIsWide();
  const [leftPct, setLeftPct] = useState(SPLIT_DEFAULT_PCT);
  const [dragging, setDragging] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const pctRef = useRef(leftPct);

  useEffect(() => {
    const v = readStoredPct();
    pctRef.current = v;
    setLeftPct(v);
  }, []);

  const persist = (v: number) => {
    try {
      localStorage.setItem(STORAGE_KEY, String(Math.round(v * 10) / 10));
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
      update(clampLeftPct(((ev.clientX - box.left) / box.width) * 100));
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
  }, []);

  const onKeyDown = (e: React.KeyboardEvent) => {
    const step = e.shiftKey ? 5 : 1;
    if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
      e.preventDefault();
      const next = clampLeftPct(leftPct + (e.key === "ArrowLeft" ? -step : step));
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
          aria-valuemin={SPLIT_MIN_PCT}
          aria-valuemax={SPLIT_MAX_PCT}
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
