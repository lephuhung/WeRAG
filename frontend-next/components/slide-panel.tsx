"use client";

import { useEffect } from "react";

/**
 * Generic right slide-in panel: backdrop + translate-x drawer.
 * Always mounted so open/close both animate.
 */
export function SlidePanel({
  open,
  onClose,
  label,
  width = "w-[440px]",
  children,
}: {
  open: boolean;
  onClose: () => void;
  label: string;
  width?: string;
  children: React.ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  return (
    <>
      <div
        onClick={onClose}
        className={`fixed inset-0 z-40 bg-ink/10 transition-opacity duration-200 ${
          open ? "opacity-100" : "pointer-events-none opacity-0"
        }`}
      />
      <aside
        role="dialog"
        aria-label={label}
        className={`fixed right-0 top-0 z-50 flex h-full ${width} max-w-[92vw] flex-col border-l border-hairline bg-surface-card shadow-[0_4px_16px_rgba(0,0,0,0.04)] transition-transform duration-300 ease-out ${
          open ? "translate-x-0" : "translate-x-full"
        }`}
      >
        {children}
      </aside>
    </>
  );
}

export function SlidePanelHeader({
  title,
  subtitle,
  onClose,
  children,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex shrink-0 items-center gap-3 border-b border-hairline px-5 py-4">
      {children}
      <div className="min-w-0 flex-1">
        <div className="truncate text-[15px] font-medium text-ink">{title}</div>
        {subtitle && <div className="caption truncate text-muted">{subtitle}</div>}
      </div>
      <button
        onClick={onClose}
        aria-label="Close"
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-strong hover:text-ink"
      >
        <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </div>
  );
}
