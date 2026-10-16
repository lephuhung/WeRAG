import { useLayoutEffect, useRef, type RefObject } from "react";

/** Keep an anchored popup inside its pane, including after toolbar reflow. */
export function useContainedPopup(anchorRef: RefObject<HTMLElement | null>, placement: "above" | "below") {
  const popupRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const anchor = anchorRef.current;
    const popup = popupRef.current;
    if (!anchor || !popup) return;
    const pane = anchor.closest(".chat-compact");
    const update = () => {
      const rect = anchor.getBoundingClientRect();
      const bounds = pane?.getBoundingClientRect();
      const left = Math.max(0, bounds?.left ?? 0) + 8;
      const right = Math.min(window.innerWidth, bounds?.right ?? window.innerWidth) - 8;
      popup.style.maxWidth = `${Math.max(0, right - left)}px`;
      const width = popup.getBoundingClientRect().width;
      popup.style.right = "auto";
      popup.style.left = `${Math.max(left, Math.min(rect.right - width, right - width)) - rect.left}px`;
      const room = placement === "above" ? rect.top - Math.max(0, bounds?.top ?? 0) - 8
        : Math.min(window.innerHeight, bounds?.bottom ?? window.innerHeight) - rect.bottom - 8;
      popup.style.maxHeight = `${Math.max(0, room)}px`;
      popup.style.overflowY = "auto";
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(anchor);
    observer.observe(popup);
    if (pane) observer.observe(pane);
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => { observer.disconnect(); window.removeEventListener("resize", update); window.removeEventListener("scroll", update, true); };
  });
  return popupRef;
}
