"use client";

import { useEffect, useRef, useState } from "react";
import { IconCheck, IconChevronDown } from "@/components/icons";

/* Styled replacement for the native <select>: same .input trigger look,
 * but the option list renders as a card dropdown instead of the OS popup so
 * selects look identical across browsers/platforms. Fixed-positioned (works
 * inside modals and scroll containers) and closes on outside click, Escape,
 * scroll or resize. */
export interface SelectOption {
  value: string;
  label: React.ReactNode;
  disabled?: boolean;
}

const MENU_MAX_H = 260;
const MENU_GAP = 4;

export function Select({
  value,
  onChange,
  options,
  placeholder = "",
  disabled,
  className = "",
  menuClassName = "",
}: {
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  menuClassName?: string;
}) {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; left: number; width: number; up: boolean } | null>(null);
  const [highlight, setHighlight] = useState(-1);

  const selected = options.find((o) => o.value === value);

  const openMenu = () => {
    if (disabled) return;
    const rect = triggerRef.current?.getBoundingClientRect();
    if (!rect) return;
    const estH = Math.min(MENU_MAX_H, options.length * 34 + 12);
    const up = rect.bottom + MENU_GAP + estH > window.innerHeight && rect.top > estH + MENU_GAP;
    setPos({ top: up ? rect.top - MENU_GAP : rect.bottom + MENU_GAP, left: rect.left, width: rect.width, up });
    setHighlight(Math.max(0, options.findIndex((o) => o.value === value)));
    setOpen(true);
  };

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (menuRef.current?.contains(e.target as Node) || triggerRef.current?.contains(e.target as Node))
        return;
      setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    const close = () => setOpen(false);
    document.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      document.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [open]);

  const pick = (v: string) => {
    onChange(v);
    setOpen(false);
    triggerRef.current?.focus();
  };

  const onTriggerKey = (e: React.KeyboardEvent) => {
    if (!open && (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter" || e.key === " ")) {
      e.preventDefault();
      openMenu();
      return;
    }
    if (!open) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const dir = e.key === "ArrowDown" ? 1 : -1;
      setHighlight((h) => {
        let i = h;
        for (let n = 0; n < options.length; n++) {
          i = (i + dir + options.length) % options.length;
          if (!options[i]?.disabled) break;
        }
        return i;
      });
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      const opt = options[highlight];
      if (opt && !opt.disabled) pick(opt.value);
    }
  };

  /* Keep the highlighted option in view while arrowing. */
  useEffect(() => {
    if (!open || highlight < 0) return;
    menuRef.current
      ?.querySelector(`[data-index="${highlight}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [highlight, open]);

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        role="combobox"
        aria-expanded={open}
        aria-haspopup="listbox"
        disabled={disabled}
        onClick={() => (open ? setOpen(false) : openMenu())}
        onKeyDown={onTriggerKey}
        className={`input flex cursor-pointer items-center justify-between gap-2 text-left disabled:cursor-not-allowed disabled:opacity-50 ${className}`}
      >
        <span className={`min-w-0 truncate ${selected ? "" : "text-muted-soft"}`}>
          {selected ? selected.label : placeholder}
        </span>
        <IconChevronDown
          className={`h-4 w-4 shrink-0 text-muted-soft transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>

      {open && pos && (
        <div
          ref={menuRef}
          role="listbox"
          className={`card fixed z-[80] overflow-y-auto p-1 shadow-[0_8px_24px_rgba(0,0,0,0.10)] ${menuClassName}`}
          style={{
            left: pos.left,
            width: pos.width,
            maxHeight: MENU_MAX_H,
            ...(pos.up ? { bottom: window.innerHeight - pos.top } : { top: pos.top }),
          }}
        >
          {options.length === 0 && (
            <div className="px-3 py-2 text-[13px] text-muted-soft">{placeholder || "—"}</div>
          )}
          {options.map((o, i) => {
            const isSel = o.value === value;
            return (
              <button
                key={o.value || `__empty-${i}`}
                type="button"
                role="option"
                aria-selected={isSel}
                data-index={i}
                disabled={o.disabled}
                onMouseEnter={() => setHighlight(i)}
                onClick={() => pick(o.value)}
                className={`flex w-full items-center justify-between gap-2 rounded-[8px] px-3 py-1.5 text-left text-[13px] transition-colors disabled:opacity-50 ${
                  i === highlight ? "bg-surface-strong" : ""
                } ${isSel ? "font-medium text-ink" : "text-body"}`}
              >
                <span className="min-w-0 truncate">{o.label}</span>
                {isSel && <IconCheck className="h-3.5 w-3.5 shrink-0 text-ink" />}
              </button>
            );
          })}
        </div>
      )}
    </>
  );
}
