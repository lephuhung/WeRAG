"use client";

import { useCallback, useEffect, useState } from "react";
import { IconRefresh } from "@/components/icons";
import {
  getBrowserConnection,
  pairBrowserConnection,
  revokeBrowserConnection,
  downloadBrowserExtension,
  type BrowserAccountStatus,
} from "@/lib/api/browser";
import { copyToClipboard } from "@/lib/clipboard";
import { useT } from "@/lib/i18n";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { FieldLabel } from "@/components/settings/field";
import { useToast } from "@/components/toast";
import { formatDateTime } from "@/lib/datetime";

export function BrowserConnectionSettings() {
  const { t, locale } = useT();
  const [status, setStatus] = useState<BrowserAccountStatus | null>(null);
  const [pairingLink, setPairingLink] = useState("");
  const [loading, setLoading] = useState(true);
  const [actionBusy, setActionBusy] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();

  const loadStatus = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await getBrowserConnection();
      setStatus(res.data ?? null);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("browser.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadStatus();
  }, [loadStatus]);

  const handlePair = async () => {
    setActionBusy(true);
    setError("");
    try {
      const res = await pairBrowserConnection(window.location.origin);
      if (res.data?.pairing_link) {
        setPairingLink(res.data.pairing_link);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("browser.pairFailed"));
    } finally {
      setActionBusy(false);
    }
  };

  const handleRevoke = async () => {
    setActionBusy(true);
    setError("");
    try {
      const res = await revokeBrowserConnection();
      setStatus(res.data ?? null);
      setPairingLink("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("browser.disconnectFailed"));
    } finally {
      setActionBusy(false);
    }
  };

  const handleDownload = async () => {
    try {
      const blob = await downloadBrowserExtension();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "browser-skill-weknora.zip";
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("browser.downloadFailed"));
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <SectionHeader
        title={t("browser.title")}
        subtitle={t("browser.subtitle")}
        actions={
          <button
            type="button"
            onClick={() => void loadStatus()}
            className="btn btn-outline btn-sm p-1.5"
            title={t("browser.refreshTip")}
            aria-label={t("browser.refreshTip")}
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        }
      />

      {error && (
        <Notice
          tone="error"
          action={
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() => void loadStatus()}
            >
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      {/* Connection Status Card */}
      <div className="rounded-xl border border-hairline bg-surface-card p-6 space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span
              className={`h-3 w-3 rounded-full ${
                status?.connected ? "animate-pulse" : "bg-surface-strong"
              }`}
              style={
                status?.connected ? { background: "var(--color-success)" } : undefined
              }
            />
            <div>
              <span className="font-semibold text-ink block text-base">
                {loading
                  ? t("browser.checking")
                  : status?.connected
                  ? t("browser.connected")
                  : t("browser.notConnected")}
              </span>
              {status?.device && (
                <span className="caption text-muted block text-xs mt-0.5">
                  {t("browser.device")}: {status.device.label || status.device.id} • {t("browser.lastSeen")}:{" "}
                  {status.device.last_seen_at
                    ? formatDateTime(status.device.last_seen_at, locale)
                    : t("browser.now")}
                </span>
              )}
            </div>
          </div>

          <div className="flex items-center gap-2">
            {status?.connected ? (
              <button
                type="button"
                disabled={actionBusy}
                onClick={() => void handleRevoke()}
                className="btn btn-danger btn-sm text-xs"
              >
                {t("browser.disconnect")}
              </button>
            ) : (
              <button
                type="button"
                disabled={actionBusy}
                onClick={() => void handlePair()}
                className="btn btn-primary btn-sm text-xs"
              >
                {actionBusy ? t("browser.generating") : t("browser.pair")}
              </button>
            )}
          </div>
        </div>

        {/* Pairing link generated */}
        {pairingLink && !status?.connected && (
          <div className="rounded-xl border border-primary/30 bg-primary/5 p-4 space-y-2 mt-4">
            <FieldLabel htmlFor="browser-pairing-link" className="caption font-semibold text-primary">
              {t("browser.pairingLink")}
            </FieldLabel>
            <div className="flex items-center gap-2">
              <input
                id="browser-pairing-link"
                type="text"
                readOnly
                aria-label={t("browser.pairingLink")}
                value={pairingLink}
                className="input text-xs font-mono select-all"
              />
              <button
                type="button"
                onClick={async () => {
                  const ok = await copyToClipboard(pairingLink);
                  if (ok) {
                    toast.success(t("browser.copied"));
                  }
                }}
                className="btn btn-primary btn-sm shrink-0 text-xs"
              >
                {t("common.copy")}
              </button>
            </div>
            <p className="caption text-muted text-[11px]">
              {t("browser.pairingHint")}
            </p>
          </div>
        )}
      </div>

      {/* Extension package download card */}
      <div className="rounded-xl border border-hairline p-6 flex items-center justify-between gap-4">
        <div>
          <span className="font-semibold text-ink block text-sm">{t("browser.download")}</span>
          <span className="caption text-muted block text-xs mt-0.5">
            {t("browser.downloadDesc")}
          </span>
        </div>
        <button
          type="button"
          onClick={() => void handleDownload()}
          className="btn btn-outline btn-sm text-xs shrink-0"
        >
          {t("browser.downloadZip")}
        </button>
      </div>
    </div>
  );
}
