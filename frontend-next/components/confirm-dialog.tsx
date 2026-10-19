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
import { useDialogFocus } from "./use-dialog-focus";
import { createPortal } from "react-dom";
import { useT } from "@/lib/i18n";
import { HostedSubPage, useInSubPageHost } from "@/components/settings/sub-page-host";

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

/* Inside the settings modal the question opens as a sub-page (back =
 * cancel) instead of a second dialog on top — see sub-page-host. The
 * settings modal mounts its own ConfirmProvider so this branch is reached. */
function ConfirmDialog(props: { options: ConfirmOptions; onSettle: (v: boolean) => void }) {
  const hosted = useInSubPageHost();
  return hosted ? <HostedConfirm {...props} /> : <OverlayConfirm {...props} />;
}

function ConfirmButtons({
  options,
  onSettle,
  confirmRef,
}: {
  options: ConfirmOptions;
  onSettle: (v: boolean) => void;
  confirmRef?: React.Ref<HTMLButtonElement>;
}) {
  const { t } = useT();
  return (
    <div className="mt-5 flex justify-end gap-2">
      <button type="button" className="btn btn-outline" onClick={() => onSettle(false)}>
        {options.cancelLabel ?? t("common.cancel")}
      </button>
      <button
        type="button"
        ref={confirmRef}
        className={`btn ${options.danger ? "bg-red-600 text-white hover:bg-red-700" : "btn-primary"}`}
        onClick={() => onSettle(true)}
      >
        {options.confirmLabel ?? t("common.confirm")}
      </button>
    </div>
  );
}

function HostedConfirm({
  options,
  onSettle,
}: {
  options: ConfirmOptions;
  onSettle: (v: boolean) => void;
}) {
  const confirmBtnRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    confirmBtnRef.current?.focus();
  }, []);
  return (
    <HostedSubPage title={options.title} onClose={() => onSettle(false)}>
      <div role="alertdialog" aria-label={options.title}>
        <p className="text-[13.5px] leading-relaxed text-muted">{options.message}</p>
        <ConfirmButtons options={options} onSettle={onSettle} confirmRef={confirmBtnRef} />
      </div>
    </HostedSubPage>
  );
}

function OverlayConfirm({
  options,
  onSettle,
}: {
  options: ConfirmOptions;
  onSettle: (v: boolean) => void;
}) {
  const [mounted, setMounted] = useState(false);
  const confirmBtnRef = useRef<HTMLButtonElement>(null);
  useEffect(() => setMounted(true), []);

  const dialogRef = useRef<HTMLDivElement>(null);
  useDialogFocus({ open: mounted, containerRef: dialogRef, onClose: () => onSettle(false) });

  if (!mounted) return null;

  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-3 sm:p-6">
      <div className="absolute inset-0 bg-ink/30 backdrop-blur-[2px]" onClick={() => onSettle(false)} />
      <div
        ref={dialogRef}
        role="alertdialog"
        aria-modal="true"
        aria-label={options.title}
        className="card relative w-[420px] max-w-full p-5 sm:p-6"
      >
        <h2 className="title-md">{options.title}</h2>
        <p className="mt-2 text-[13.5px] leading-relaxed text-muted">{options.message}</p>
        <ConfirmButtons options={options} onSettle={onSettle} confirmRef={confirmBtnRef} />
      </div>
    </div>,
    document.body,
  );
}

export function useConfirm() {
  return useContext(ConfirmContext);
}
