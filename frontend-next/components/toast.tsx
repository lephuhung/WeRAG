"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

type ToastKind = "success" | "error" | "info";
type ToastItem = {
  id: number;
  kind: ToastKind;
  message: string;
  onClick?: () => void;
  durationMs?: number;
};

type Toast = {
  success: (message: string, onClick?: () => void, durationMs?: number) => void;
  error: (message: string, onClick?: () => void, durationMs?: number) => void;
  info: (message: string, onClick?: () => void, durationMs?: number) => void;
};

const ToastContext = createContext<Toast>({
  success: () => {},
  error: () => {},
  info: () => {},
});

const AUTO_DISMISS_MS = 5000;

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const [mounted, setMounted] = useState(false);
  const nextId = useRef(1);

  useEffect(() => setMounted(true), []);

  const push = useCallback((kind: ToastKind, message: string, onClick?: () => void, durationMs?: number) => {
    if (!message) return;
    const id = nextId.current++;
    setItems((prev) => [...prev.slice(-4), { id, kind, message, onClick, durationMs }]);
  }, []);

  const dismiss = useCallback((id: number) => {
    setItems((prev) => prev.filter((x) => x.id !== id));
  }, []);

  const api = useMemo<Toast>(
    () => ({
      success: (m, onClick, durationMs) => push("success", m, onClick, durationMs),
      error: (m, onClick, durationMs) => push("error", m, onClick, durationMs),
      info: (m, onClick, durationMs) => push("info", m, onClick, durationMs),
    }),
    [push],
  );

  const accent: Record<ToastKind, string> = {
    success: "bg-emerald-500",
    error: "bg-red-500",
    info: "bg-sky-500",
  };

  return (
    <ToastContext.Provider value={api}>
      {children}
      {mounted &&
        items.length > 0 &&
        createPortal(
          <div
            aria-live="polite"
            className="pointer-events-none fixed right-4 top-4 z-[70] flex w-[min(360px,calc(100vw-2rem))] flex-col gap-2"
          >
            {items.map((item) => (
              <ToastRow key={item.id} item={item} accent={accent[item.kind]} onDismiss={dismiss} />
            ))}
          </div>,
          document.body,
        )}
    </ToastContext.Provider>
  );
}

function ToastRow({
  item,
  accent,
  onDismiss,
}: {
  item: ToastItem;
  accent: string;
  onDismiss: (id: number) => void;
}) {
  const [leaving, setLeaving] = useState(false);
  const duration = item.durationMs ?? AUTO_DISMISS_MS;

  useEffect(() => {
    const hide = setTimeout(() => setLeaving(true), duration);
    const remove = setTimeout(() => onDismiss(item.id), duration + 200);
    return () => {
      clearTimeout(hide);
      clearTimeout(remove);
    };
  }, [item.id, item.durationMs, onDismiss, duration]);

  return (
    <div
      role="status"
      onClick={item.onClick}
      className={`card pointer-events-auto flex items-start gap-2.5 py-2.5 pl-3 pr-1.5 shadow-lg transition-all duration-200 ${
        leaving ? "-translate-y-1 opacity-0" : "opacity-100"
      } ${item.onClick ? "cursor-pointer" : ""}`}
    >
      <span className={`mt-0.5 h-4 w-1 shrink-0 rounded-full ${accent}`} aria-hidden />
      <p className="min-w-0 flex-1 text-[13px] leading-snug break-words text-ink">{item.message}</p>
      <button
        type="button"
        aria-label="Close"
        onClick={(e) => {
          e.stopPropagation();
          onDismiss(item.id);
        }}
        className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-strong hover:text-ink"
      >
        <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </div>
  );
}

export function useToast() {
  return useContext(ToastContext);
}
