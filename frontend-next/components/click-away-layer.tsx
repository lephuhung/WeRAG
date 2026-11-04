"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

/* The invisible layer that closes a popup when the user clicks anywhere else.
 *
 * Portalled to <body> rather than rendered in place. The composer card is a
 * container-query context (`@container` → `container-type: inline-size`), and
 * a containment context becomes the containing block of the `fixed`
 * descendants inside it — the layer would then cover only the composer, so a
 * click on the page around it would no longer close the menu. A click-away
 * layer belongs to the viewport anyway, so it lives on <body>.
 *
 * Only rendered while a menu is open, but the mount guard keeps the first
 * render (and any server render) free of `document`.
 */
export function ClickAwayLayer({ onClose }: { onClose: () => void }) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  if (!mounted) return null;
  return createPortal(<div className="fixed inset-0 z-40" onClick={onClose} aria-hidden />, document.body);
}
