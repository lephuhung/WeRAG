"use client";

import { useEffect } from "react";
import { HostedSubPage, useInSubPageHost } from "@/components/settings/sub-page-host";

type SlidePanelProps = {
  open: boolean;
  onClose: () => void;
  label: string;
  width?: string;
  children: React.ReactNode;
};

/**
 * Generic right slide-in panel: backdrop + translate-x drawer.
 * Always mounted so open/close both animate. No backdrop blur: re-blurring
 * the whole page under a fading overlay made every open stutter.
 * Inside the settings modal the drawer opens as a sub-page of the modal
 * (label as title, back button) instead of sliding over it — see
 * sub-page-host.
 */
export function SlidePanel(props: SlidePanelProps) {
  const hosted = useInSubPageHost();
  if (hosted) {
    return props.open ? (
      <HostedSubPage title={props.label} onClose={props.onClose}>
        <div className="flex flex-col">{props.children}</div>
      </HostedSubPage>
    ) : null;
  }
  return <OverlaySlidePanel {...props} />;
}

function OverlaySlidePanel({
  open,
  onClose,
  label,
  width = "w-[440px]",
  children,
}: SlidePanelProps) {
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
        className={`fixed inset-0 z-40 bg-ink/30 transition-opacity duration-200 ${
          open ? "opacity-100" : "pointer-events-none opacity-0"
        }`}
      />
      <aside
        role="dialog"
        aria-label={label}
        className={`fixed right-0 top-0 z-50 flex h-full ${width} max-w-[92vw] flex-col border-l border-hairline bg-surface-card shadow-[0_4px_16px_rgba(0,0,0,0.04)] transition-transform duration-200 ease-out will-change-transform ${
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
  actions,
  children,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  /** Extra controls rendered between the title and the close button. */
  actions?: React.ReactNode;
  children?: React.ReactNode;
}) {
  /* Hosted in the settings modal, whose header already shows the title and
   * the back button: keep only the extra controls. */
  const hosted = useInSubPageHost();
  if (hosted) {
    if (!children && !subtitle && !actions) return null;
    return (
      <div className="mb-4 flex shrink-0 items-center gap-3">
        {children}
        <div className="caption min-w-0 flex-1 truncate text-muted">{subtitle}</div>
        {actions}
      </div>
    );
  }
  return (
    <div className="flex shrink-0 items-center gap-3 border-b border-hairline px-5 py-4">
      {children}
      <div className="min-w-0 flex-1">
        <div className="truncate text-[15px] font-medium text-ink">{title}</div>
        {subtitle && <div className="caption truncate text-muted">{subtitle}</div>}
      </div>
      {actions}
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
