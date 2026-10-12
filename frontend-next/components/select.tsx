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
  /* Text matched by a searchable select; defaults to the label when it is a
   * string. */
  keywords?: string;
}

/* foldText lower-cases and strips Vietnamese diacritics so "hue" finds
 * "Huế" and "da nang" finds "Đà Nẵng". */
function foldText(s: string): string {
  return s
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "D")
    .toLowerCase();
}

const MENU_MAX_H = 260;
const MENU_GAP = 4;

export function Select({
  value,
  onChange,
  options: allOptions,
  placeholder = "",
  disabled,
  className = "",
  menuClassName = "",
  searchable = false,
  searchPlaceholder = "",
}: {
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  menuClassName?: string;
  /* Show a filter box atop the list; typing on the closed trigger opens it. */
  searchable?: boolean;
  searchPlaceholder?: string;
}) {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const folded = foldText(query.trim());
  const options = !searchable || !folded
    ? allOptions
    : allOptions.filter((o) =>
        foldText(o.keywords ?? (typeof o.label === "string" ? o.label : String(o.value))).includes(folded)
      );
  const [pos, setPos] = useState<{ top: number; left: number; width: number; up: boolean } | null>(null);
  const [highlight, setHighlight] = useState(-1);

  const selected = allOptions.find((o) => o.value === value);

  const openMenu = (seed = "") => {
    if (disabled) return;
    const rect = triggerRef.current?.getBoundingClientRect();
    if (!rect) return;
    setQuery(seed);
    const estH = Math.min(MENU_MAX_H, allOptions.length * 34 + 12 + (searchable ? 44 : 0));
    const up = rect.bottom + MENU_GAP + estH > window.innerHeight && rect.top > estH + MENU_GAP;
    setPos({ top: up ? rect.top - MENU_GAP : rect.bottom + MENU_GAP, left: rect.left, width: rect.width, up });
    setHighlight(seed ? 0 : Math.max(0, allOptions.findIndex((o) => o.value === value)));
    setOpen(true);
  };

  /* Focus the filter box as the menu opens. */
  useEffect(() => {
    if (open && searchable) searchRef.current?.focus();
  }, [open, searchable]);

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
    // Scrolling the list itself must not close it.
    const close = (e: Event) => {
      if (e.type === "scroll" && menuRef.current?.contains(e.target as Node)) return;
      setOpen(false);
    };
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
    if (!open && searchable && e.key.length === 1 && e.key !== " " && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      openMenu(e.key);
      return;
    }
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
    } else if (e.key === "Enter" || (e.key === " " && !searchable)) {
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
          {searchable && (
            <div className="sticky top-0 z-10 -mx-1 -mt-1 mb-1 border-b border-hairline bg-surface-card p-1.5">
              <input
                ref={searchRef}
                className="input h-8 w-full text-[13px]"
                value={query}
                placeholder={searchPlaceholder}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setHighlight(0);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    setOpen(false);
                    triggerRef.current?.focus();
                    return;
                  }
                  onTriggerKey(e);
                }}
              />
            </div>
          )}
          {options.length === 0 && (
            <div className="px-3 py-2 text-[13px] text-muted-soft">{searchable && query ? "—" : placeholder || "—"}</div>
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
