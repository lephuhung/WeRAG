"use client";

import { useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";

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

  if (!open || !position) return null;
  return createPortal(
    <div ref={menuRef} data-dialog-portal className="card fixed z-[80] overflow-y-auto p-1.5 shadow-soft-drop" style={position}>
      {children}
    </div>, document.body,
  );
}
