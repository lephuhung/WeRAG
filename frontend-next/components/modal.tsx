"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

export function Modal({
  open,
  title,
  onClose,
  children,
  width = "w-[460px]",
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  width?: string;
}) {
  /* Portal to <body>: modals nested inside SlidePanel (or any other
   * transformed/animated container) would otherwise be trapped by the
   * ancestor's transform — `position: fixed` resolves against that box,
   * squashing the dialog into the panel and breaking the Select menu's
   * viewport coordinates inside it. */
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open || !mounted) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-6">
      <div className="absolute inset-0 bg-ink/20" onClick={onClose} />
      <div
        role="dialog"
        aria-label={title}
        className={`card relative ${width} max-h-[calc(100dvh-1.5rem)] max-w-full overflow-y-auto p-4 sm:max-h-[calc(100dvh-3rem)] sm:p-6`}
      >
        <div className="mb-5 flex items-center justify-between">
          <h2 className="title-md">{title}</h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className="flex h-8 w-8 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-strong hover:text-ink"
          >
            <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  );
}
