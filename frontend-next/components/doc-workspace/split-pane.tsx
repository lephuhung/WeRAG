"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { useT } from "@/lib/i18n";
import styles from "./split-pane.module.css";
import { DEFAULT_SPLIT_BOUNDS, clampLeftPct, type SplitBounds } from "./split-pane-math";

const DEFAULT_STORAGE_KEY = "werag.docWorkspace.leftPct";
const WIDE_QUERY = "(min-width: 1024px)";
const DOCUMENT_QUERY = "(min-width: 1440px) and (min-height: 800px)";

function readStoredPct(key: string, bounds: SplitBounds): number {
  try {
    const raw = localStorage.getItem(key);
    return raw === null ? bounds.def : clampLeftPct(Number(raw), bounds);
  } catch {
    return bounds.def;
  }
}

function useIsWide(query = WIDE_QUERY): boolean {
  const [wide, setWide] = useState(true);
  // Layout effect: the real breakpoint is applied before the first paint, so
  // phones never flash the side-by-side layout.
  useLayoutEffect(() => {
    const mq = window.matchMedia(query);
    const sync = () => setWide(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, [query]);
  return wide;
}

/* Two-pane layout for the document assistant: editor on the left (55–75%,
 * draggable, persisted per browser), chat on the right. Document mode needs
 * a viewport of at least 1440 × 800 CSS pixels; smaller document viewports keep the
 * panes mounted side by side beneath an inert, blurred overlay.
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
  documentMode = false,
}: {
  left: ReactNode | null;
  right: ReactNode;
  bounds?: SplitBounds;
  storageKey?: string;
  documentMode?: boolean;
}) {
  const { t, locale } = useT();
  const wide = useIsWide(documentMode ? DOCUMENT_QUERY : WIDE_QUERY);
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
  const sideBySide = hasLeft && (documentMode || wide);
  const blocked = documentMode && !wide;

  return (
    <div className={styles.root}>
    <div
      ref={containerRef}
      inert={blocked}
      aria-hidden={blocked ? true : undefined}
      className={`${styles.panes} ${sideBySide ? "relative flex flex-1 overflow-hidden" : "flex flex-1 flex-col overflow-hidden"} ${blocked ? styles.blocked : ""}`}
    >
      {hasLeft ? (
        <div
          className={sideBySide ? "flex min-w-0 flex-col overflow-hidden" : "hairline-b flex h-[55vh] shrink-0 flex-col overflow-hidden"}
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
      {dragging && sideBySide && !blocked ? <div className="fixed inset-0 z-50 cursor-col-resize" /> : null}
    </div>
    {blocked && (
      <div className={styles.overlay}>
        <section className={styles.notice} role="status" aria-live="polite">
          <svg className={styles.monitor} width="44" height="40" viewBox="0 0 44 40" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><rect x="3" y="3" width="38" height="26" rx="4" /><path d="M16 37h12M22 29v8M14 12l-4 4 4 4m16-8 4 4-4 4M10 16h24" /></svg>
          <h2>{locale === "vi" ? "Màn hình quá nhỏ" : "Your screen is too small"}</h2>
          <p>{locale === "vi" ? "Tính năng soạn thảo cần đủ không gian để hiển thị văn bản và trợ lý AI cạnh nhau." : "The document editor needs enough space to display your document and AI assistant side by side."}</p>
          <div className={styles.minimum}>1440 × 800 px <span>{locale === "vi" ? "Kích thước cửa sổ tối thiểu" : "Minimum window size"}</span></div>
          <p>{locale === "vi" ? "Hãy mở rộng cửa sổ, giảm mức thu phóng trình duyệt hoặc chuyển sang màn hình lớn hơn. Giao diện tự mở lại khi đủ kích thước." : "Enlarge the window, zoom out in your browser, or use a larger screen. The editor resumes automatically when there is enough space."}</p>
          <a href="/platform/creatChat">{locale === "vi" ? "← Về trang chat" : "← Back to chat"}</a>
        </section>
      </div>
    )}
    </div>
  );
}
