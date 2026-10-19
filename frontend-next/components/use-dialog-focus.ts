"use client";

import { useEffect, useRef, type RefObject } from "react";

const dialogs: HTMLElement[] = [];
const focusable = 'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Only actual overlays register; hosted sub-pages remain in their parent's scope. */
export function useDialogFocus({ open, containerRef, onClose }: {
  open: boolean;
  containerRef: RefObject<HTMLElement | null>;
  onClose?: () => void;
}): void {
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const container = containerRef.current;
    if (!open || !container) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogs.push(container);
    const top = () => dialogs.at(-1) === container;
    const portals = () => [...document.querySelectorAll<HTMLElement>('[data-dialog-portal], [role="listbox"]')]
      .filter(el => container.contains(el) && el.getClientRects().length && !el.closest('[inert], [aria-hidden="true"]'));
    const elements = () => [...container.querySelectorAll<HTMLElement>(focusable)]
      .filter(el => el.tabIndex >= 0 && el.getClientRects().length && !el.closest('[inert], [aria-hidden="true"]'));
    const focusFirst = () => (elements()[0] ?? container).focus({ preventScroll: true });
    // Make the no-controls state focusable without adding a tab stop.
    const previousTabIndex = container.getAttribute("tabindex");
    container.tabIndex = -1;
    if (top()) focusFirst();
    const keydown = (event: KeyboardEvent) => {
      if (!top() || event.defaultPrevented) return;
      if (event.key === "Escape") {
        // The select/menu owns the first Escape; its own handler restores focus.
        if (portals().length) return;
        if (!closeRef.current) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        closeRef.current();
      } else if (event.key === "Tab") {
        const list = elements();
        const first = list[0];
        const last = list.at(-1);
        const active = document.activeElement;
        if (!first || !last) { event.preventDefault(); container.focus(); return; }
        if (!list.includes(active as HTMLElement) || (event.shiftKey ? active === first : active === last)) {
          event.preventDefault();
          (event.shiftKey ? last : first).focus();
        }
      }
    };
    const focusin = (event: FocusEvent) => {
      if (!top()) return;
      const target = event.target as Node;
      if (!container.contains(target) && !portals().some(el => el.contains(target))) focusFirst();
    };
    document.addEventListener("keydown", keydown, true);
    document.addEventListener("focusin", focusin);
    return () => {
      const wasTop = top();
      const index = dialogs.lastIndexOf(container);
      if (index >= 0) dialogs.splice(index, 1);
      document.removeEventListener("keydown", keydown, true);
      document.removeEventListener("focusin", focusin);
      if (previousTabIndex === null) container.removeAttribute("tabindex");
      else container.setAttribute("tabindex", previousTabIndex);
      if (wasTop) queueMicrotask(() => {
        const parent = dialogs.at(-1);
        if (previous?.isConnected && !previous.closest('[inert], [aria-hidden="true"]') && (!parent || parent.contains(previous))) previous.focus({ preventScroll: true });
        else if (parent) parent.focus({ preventScroll: true });
        else {
          const pageTargets = [...document.querySelectorAll<HTMLElement>(focusable)].filter(el =>
            !container.contains(el) && el.tabIndex >= 0 && el.getClientRects().length &&
            !el.closest('[inert], [aria-hidden="true"], [role="dialog"], [role="alertdialog"]'));
          const fallback = pageTargets.find(el => el.closest("main")) ?? pageTargets[0];
          fallback?.focus({ preventScroll: true });
        }
      });
    };
  }, [open, containerRef]);
}
