"use client";

import { useCallback, useEffect, useState, type PointerEvent as ReactPointerEvent } from "react";
import { ProcessingTimeline } from "@/components/knowledge/processing-timeline";
import { useT } from "@/lib/i18n";

/* The trace drawer's width bounds (the drag handle and its keyboard
 * controls share them). */
const DRAWER_MIN_WIDTH = 480;
const DRAWER_MAX_WIDTH = 1100;

/**
 * Secondary drawer hosting the full waterfall — port of the resizable
 * t-drawer used by doc-content.vue. Owns its own backdrop, Escape
 * handling and left-edge drag resize so it stacks above the doc panel.
 */
export function ProcessingTimelineDrawer({
  open,
  knowledgeId,
  parseStatus,
  docTitle,
  onClose,
}: {
  open: boolean;
  knowledgeId: string;
  parseStatus?: string;
  docTitle?: string;
  onClose: () => void;
}) {
  const { t } = useT();
  const [width, setWidth] = useState(() =>
    typeof window === "undefined"
      ? 860
      : Math.round(Math.min(1100, Math.max(640, window.innerWidth * 0.62))),
  );
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  const startResize = useCallback((e: ReactPointerEvent) => {
    e.preventDefault();
    setDragging(true);
    const onMove = (ev: PointerEvent) => {
      /* Drawer is right-anchored: width grows as the cursor moves left. */
      const w = window.innerWidth - ev.clientX;
      setWidth(Math.round(Math.min(Math.max(DRAWER_MIN_WIDTH, w), window.innerWidth - 48)));
    };
    const onUp = () => {
      setDragging(false);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }, []);

  if (!open) return null;

  return (
    <>
      <div
        className="fixed inset-0 z-[55] bg-scrim/10"
        onClick={onClose}
        aria-hidden="true"
      />
      <aside
        role="dialog"
        aria-label={t("ks.title")}
        className={`fixed right-0 top-0 z-[60] flex h-full max-w-[96vw] flex-col border-l border-hairline bg-surface-card shadow-[0_4px_24px_rgba(0,0,0,0.12)] ${
          dragging ? "" : "transition-transform duration-200 ease-out"
        }`}
        style={{ width }}
      >
        {/* left-edge resize handle */}
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label={t("ks.resizeDrawer")}
          aria-valuemin={DRAWER_MIN_WIDTH}
          aria-valuemax={DRAWER_MAX_WIDTH}
          aria-valuenow={width}
          aria-valuetext={t("ks.resizeDrawerValue", { n: width })}
          title={t("ks.resizeDrawer")}
          tabIndex={0}
          onPointerDown={startResize}
          onKeyDown={(e) => {
            const step = e.shiftKey ? 40 : 16;
            if (e.key === "ArrowLeft") {
              e.preventDefault();
              setWidth((w) => Math.round(Math.min(Math.max(DRAWER_MIN_WIDTH, w + step), DRAWER_MAX_WIDTH)));
            } else if (e.key === "ArrowRight") {
              e.preventDefault();
              setWidth((w) => Math.round(Math.min(Math.max(DRAWER_MIN_WIDTH, w - step), DRAWER_MAX_WIDTH)));
            } else if (e.key === "Home") {
              e.preventDefault();
              setWidth(DRAWER_MIN_WIDTH);
            } else if (e.key === "End") {
              e.preventDefault();
              setWidth(DRAWER_MAX_WIDTH);
            }
          }}
          className="absolute inset-y-0 -left-1.5 z-10 w-3 cursor-ew-resize outline-none focus-visible:bg-ink/10"
        >
          <div className="mx-auto h-full w-px bg-transparent transition-colors hover:bg-ink/30" />
        </div>
        <ProcessingTimeline
          knowledgeId={knowledgeId}
          parseStatus={parseStatus}
          docTitle={docTitle}
          showClose
          onClose={onClose}
        />
      </aside>
    </>
  );
}
