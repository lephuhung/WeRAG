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
import { useT } from "@/lib/i18n";

export type ConfirmOptions = {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** danger renders the confirm button in red (destructive actions). */
  danger?: boolean;
};

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<ConfirmFn>(async () => false);

export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [opts, setOpts] = useState<ConfirmOptions | null>(null);
  const resolveRef = useRef<((v: boolean) => void) | null>(null);

  const confirm = useCallback<ConfirmFn>((next) => {
    return new Promise<boolean>((resolve) => {
      /* A second confirm() while one is open resolves the first as cancelled —
       * native window.confirm() blocks instead, which we don't want. */
      resolveRef.current?.(false);
      resolveRef.current = resolve;
      setOpts(next);
    });
  }, []);

  const settle = useCallback((v: boolean) => {
    resolveRef.current?.(v);
    resolveRef.current = null;
    setOpts(null);
  }, []);

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {opts && <ConfirmDialog options={opts} onSettle={settle} />}
    </ConfirmContext.Provider>
  );
}

function ConfirmDialog({
  options,
  onSettle,
}: {
  options: ConfirmOptions;
  onSettle: (v: boolean) => void;
}) {
  const { t } = useT();
  const [mounted, setMounted] = useState(false);
  const confirmBtnRef = useRef<HTMLButtonElement>(null);
  useEffect(() => setMounted(true), []);

  useEffect(() => {
    confirmBtnRef.current?.focus();
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onSettle(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onSettle]);

  if (!mounted) return null;

  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-3 sm:p-6">
      <div className="absolute inset-0 bg-ink/30 backdrop-blur-[2px]" onClick={() => onSettle(false)} />
      <div
        role="alertdialog"
        aria-modal="true"
        aria-label={options.title}
        className="card relative w-[420px] max-w-full p-5 sm:p-6"
      >
        <h2 className="title-md">{options.title}</h2>
        <p className="mt-2 text-[13.5px] leading-relaxed text-muted">{options.message}</p>
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" className="btn btn-outline" onClick={() => onSettle(false)}>
            {options.cancelLabel ?? t("common.cancel")}
          </button>
          <button
            type="button"
            ref={confirmBtnRef}
            className={`btn ${options.danger ? "bg-red-600 text-white hover:bg-red-700" : "btn-primary"}`}
            onClick={() => onSettle(true)}
          >
            {options.confirmLabel ?? t("common.confirm")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

export function useConfirm() {
  return useContext(ConfirmContext);
}
