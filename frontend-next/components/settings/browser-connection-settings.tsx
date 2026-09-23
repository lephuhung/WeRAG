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

export function BrowserConnectionSettings() {
  const [status, setStatus] = useState<BrowserAccountStatus | null>(null);
  const [pairingLink, setPairingLink] = useState("");
  const [loading, setLoading] = useState(true);
  const [actionBusy, setActionBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

  const loadStatus = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await getBrowserConnection();
      setStatus(res.data ?? null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load browser connection status");
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
      setError(e instanceof Error ? e.message : "Failed to generate pairing link");
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
      setError(e instanceof Error ? e.message : "Failed to disconnect browser");
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
      setError(e instanceof Error ? e.message : "Failed to download browser extension bundle");
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">Browser Extension Connection</h2>
          <p className="caption text-muted mt-1">
            Pair your local browser extension to allow agents to interact with web pages directly.
          </p>
        </div>

        <button
          type="button"
          onClick={() => void loadStatus()}
          className="btn btn-outline btn-sm p-1.5"
          title="Refresh status"
        >
          <IconRefresh className="h-3.5 w-3.5" />
        </button>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400">
          {error}
        </div>
      )}

      {/* Connection Status Card */}
      <div className="rounded-xl border border-hairline bg-surface-card p-6 space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span
              className={`h-3 w-3 rounded-full ${
                status?.connected ? "bg-emerald-500 animate-pulse" : "bg-stone-300 dark:bg-stone-600"
              }`}
            />
            <div>
              <span className="font-semibold text-ink block text-base">
                {loading
                  ? "Checking connection…"
                  : status?.connected
                  ? "Browser Connected"
                  : "Not Connected"}
              </span>
              {status?.device && (
                <span className="caption text-muted block text-xs mt-0.5">
                  Device: {status.device.label || status.device.id} • Last seen:{" "}
                  {status.device.last_seen_at
                    ? new Date(status.device.last_seen_at).toLocaleTimeString()
                    : "now"}
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
                className="btn btn-outline btn-sm text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/30 text-xs"
              >
                Disconnect
              </button>
            ) : (
              <button
                type="button"
                disabled={actionBusy}
                onClick={() => void handlePair()}
                className="btn btn-primary btn-sm text-xs"
              >
                {actionBusy ? "Generating…" : "Pair New Browser"}
              </button>
            )}
          </div>
        </div>

        {/* Pairing link generated */}
        {pairingLink && !status?.connected && (
          <div className="rounded-xl border border-brand/30 bg-brand/5 p-4 space-y-2 mt-4">
            <span className="caption font-semibold text-brand block">
              Single-use Pairing Link (Expires in 5 minutes):
            </span>
            <div className="flex items-center gap-2">
              <input
                type="text"
                readOnly
                value={pairingLink}
                className="input text-xs font-mono select-all"
              />
              <button
                type="button"
                onClick={async () => {
                  const ok = await copyToClipboard(pairingLink);
                  if (ok) {
                    setCopied(true);
                    setTimeout(() => setCopied(false), 2000);
                  }
                }}
                className="btn btn-primary btn-sm shrink-0 text-xs"
              >
                {copied ? "Copied!" : "Copy"}
              </button>
            </div>
            <p className="caption text-muted text-[11px]">
              Open this link in the browser containing your WeRAG extension to complete pairing.
            </p>
          </div>
        )}
      </div>

      {/* Extension package download card */}
      <div className="rounded-xl border border-hairline p-6 flex items-center justify-between gap-4">
        <div>
          <span className="font-semibold text-ink block text-sm">Download Chrome / Edge Extension</span>
          <span className="caption text-muted block text-xs mt-0.5">
            Install the extension bundle unpacked in Developer Mode to enable local browsing capabilities.
          </span>
        </div>
        <button
          type="button"
          onClick={() => void handleDownload()}
          className="btn btn-outline btn-sm text-xs shrink-0"
        >
          Download .zip
        </button>
      </div>
    </div>
  );
}
