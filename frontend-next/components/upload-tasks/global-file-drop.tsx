"use client";

import { useEffect, useRef, useState } from "react";
import { usePathname } from "next/navigation";
import { IconArrowUp } from "@/components/icons";
import { collectDroppedFiles } from "@/lib/collect-dropped-files";
import { useT } from "@/lib/i18n";

/* Ported from the global drop zone in frontend/src/views/platform/index.vue
 * (KB-detail branch): OS file drags anywhere over a knowledge base page show
 * a mask, and the drop forwards the collected files — folders included — to
 * the page via `weknora:knowledge-file-drop`, which opens the upload dialog.
 * Chat-route drops are not ported (no consumer in this app yet). */

const isFileDrag = (event: DragEvent): boolean => {
  const types = event.dataTransfer?.types;
  if (!types) return false;
  return Array.from(types).includes("Files");
};

export function GlobalFileDrop() {
  const { t } = useT();
  const pathname = usePathname();
  const [mask, setMask] = useState(false);
  // Counts nested dragenter/dragleave pairs so hovering a child doesn't
  // flicker the mask off.
  const counter = useRef(0);

  const kbId = pathname?.startsWith("/platform/knowledge-bases/")
    ? pathname.split("/")[3] || null
    : null;

  useEffect(() => {
    counter.current = 0;
    setMask(false);
    if (!kbId) return;

    const enter = (e: DragEvent) => {
      if (!isFileDrag(e)) return;
      e.preventDefault();
      counter.current++;
      setMask(true);
    };
    const over = (e: DragEvent) => {
      if (!isFileDrag(e)) return;
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
    };
    const leave = (e: DragEvent) => {
      if (!isFileDrag(e)) return;
      counter.current--;
      if (counter.current <= 0) {
        counter.current = 0;
        setMask(false);
      }
    };
    const drop = (e: DragEvent) => {
      if (!isFileDrag(e)) return;
      e.preventDefault();
      counter.current = 0;
      setMask(false);
      void collectDroppedFiles(e).then((files) => {
        if (files.length === 0) return;
        window.dispatchEvent(
          new CustomEvent("weknora:knowledge-file-drop", { detail: { kbId, files } }),
        );
      });
    };

    document.addEventListener("dragenter", enter, true);
    document.addEventListener("dragover", over, true);
    document.addEventListener("dragleave", leave, true);
    document.addEventListener("drop", drop, true);
    return () => {
      document.removeEventListener("dragenter", enter, true);
      document.removeEventListener("dragover", over, true);
      document.removeEventListener("dragleave", leave, true);
      document.removeEventListener("drop", drop, true);
    };
  }, [kbId]);

  if (!mask) return null;

  return (
    <div className="pointer-events-none fixed inset-0 z-[60] grid place-items-center bg-ink/10">
      <div className="flex flex-col items-center rounded-[24px] border-2 border-dashed border-ink/30 bg-surface-card/95 px-16 py-12">
        <span className="grid h-14 w-14 place-items-center rounded-full bg-surface-strong text-ink">
          <IconArrowUp className="h-6 w-6" />
        </span>
        <span className="mt-4 text-[22px] font-semibold text-ink">{t("uploadTasks.dropTitle")}</span>
        <span className="mt-2 text-[12.5px] text-muted-soft">{t("uploadTasks.dropSubtitle")}</span>
      </div>
    </div>
  );
}
