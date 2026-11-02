"use client";

import { useEffect, useRef, useState } from "react";
import { useDialogFocus } from "./use-dialog-focus";
import { createPortal } from "react-dom";
import { useT } from "@/lib/i18n";
import { HostedSubPage, useInSubPageHost } from "@/components/settings/sub-page-host";

/**
 * Overlay z-index scale — single source of truth for the whole app.
 *
 *   z-40  backdrop        dimmed layer behind a drawer or modal
 *   z-45  drawer          slide-panel drawer (above its backdrop, below modals)
 *   z-50  modal           modal cards (modal, doc preview, parse settings)
 *   z-60  popover/menu    transient layer above a modal: menus, popovers and
 *                         secondary dialogs stacked on top of an open modal
 *                         (confirm dialog)
 *   z-70  toast           toasts
 *   z-90  palette         command palette (top-most layer)
 *
 * Tailwind v4 has no `--z-*` theme namespace, so the scale is expressed with
 * plain numeric utilities (z-40 / z-45 / z-50 / z-60 / z-70 / z-90). Do not
 * invent values between them (no z-[55], no z-[60] arbitrary values): a layer
 * either owns a slot or does not exist.
 */

type ModalProps = {
  open: boolean;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  width?: string;
};

/* `width` is a Tailwind width class ("w-[460px]"); it is applied from the
 * `sm` breakpoint up. Below `sm` the dialog is a full-screen sheet. */
function cssLength(width: string): string {
  const match = /^w-\[(.+)\]$/.exec(width.trim());
  return match ? match[1] : width.trim();
}

/* Inside the settings modal a Modal never stacks a second dialog: it opens
 * as a sub-page of the modal (title + back button), see sub-page-host. */
export function Modal(props: ModalProps) {
  const hosted = useInSubPageHost();
  if (hosted) {
    return props.open ? (
      <HostedSubPage title={props.title} onClose={props.onClose}>
        {props.children}
      </HostedSubPage>
    ) : null;
  }
  return <OverlayModal {...props} />;
}

function OverlayModal({
  open,
  title,
  onClose,
  children,
  width = "w-[460px]",
}: ModalProps) {
  /* Portal to <body>: modals nested inside SlidePanel (or any other
   * transformed/animated container) would otherwise be trapped by the
   * ancestor's transform — `position: fixed` resolves against that box,
   * squashing the dialog into the panel and breaking the Select menu's
   * viewport coordinates inside it. */
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  const { t } = useT();
  const dialogRef = useRef<HTMLDivElement>(null);
  useDialogFocus({ open: open && mounted, containerRef: dialogRef, onClose });

  if (!open || !mounted) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-0 sm:p-6">
      <div className="absolute inset-0 z-40 bg-scrim/40 backdrop-blur-sm" onClick={onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        style={{ "--modal-w": cssLength(width) } as React.CSSProperties}
        className="card relative z-50 h-full w-full max-w-none overflow-y-auto rounded-none p-4 sm:h-auto sm:max-h-[calc(100dvh-3rem)] sm:w-[var(--modal-w)] sm:max-w-[calc(100vw-3rem)] sm:rounded-[16px] sm:p-6"
      >
        <div className="mb-5 flex items-center justify-between">
          <h2 className="title-md">{title}</h2>
          <button
            onClick={onClose}
            aria-label={t("common.close")}
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
