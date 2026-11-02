"use client";

import { useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useDialogFocus } from "@/components/use-dialog-focus";

/** Escapes the editor's clipping ancestors and stays inside the viewport. */
export function DocumentAddMenu({ anchorRef, open, onClose, children }: {
  anchorRef: RefObject<HTMLButtonElement | null>;
  open: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  const [position, setPosition] = useState<{ left: number; top: number; width: number; maxHeight: number } | null>(null);

  /* The menu is portalled out of the editor's DOM, so it manages its own
   * focus: the focus moves into the menu, stays inside it and returns to
   * the anchor when the menu closes. */
  useDialogFocus({ open, containerRef: menuRef, onClose: closeRef.current });

  useLayoutEffect(() => {
    if (!open || !anchorRef.current) return;
    const anchor = anchorRef.current.getBoundingClientRect();
    const width = Math.max(0, Math.min(320, window.innerWidth - 16));
    const top = Math.min(anchor.bottom + 4, Math.max(8, window.innerHeight - 120));
    setPosition({ left: Math.max(8, Math.min(anchor.left, window.innerWidth - width - 8)), top, width, maxHeight: Math.max(0, window.innerHeight - top - 8) });
    const outside = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node) && !anchorRef.current?.contains(event.target as Node)) closeRef.current();
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopImmediatePropagation();
      closeRef.current();
      anchorRef.current?.focus();
    };
    const close = (event: Event) => {
      if (event.type === "scroll" && menuRef.current?.contains(event.target as Node)) return;
      closeRef.current();
    };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape, true);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", escape, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [open, anchorRef]);

  if (!open) return null;
  /* The menu stays mounted while it is open (the position is measured in a
   * layout effect, before the first paint) so the focus can move into it
   * as soon as it appears. */
  return createPortal(
    <div
      ref={menuRef}
      data-dialog-portal
      className="fixed z-[80] overflow-y-auto rounded-md2 border border-hairline bg-surface-card p-1.5 shadow-soft-drop"
      style={position ?? { left: 0, top: 0, width: 0, maxHeight: 0 }}
    >
      {children}
    </div>, document.body,
  );
}
