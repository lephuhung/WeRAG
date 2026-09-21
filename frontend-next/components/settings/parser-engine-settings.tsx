"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { IconDocReader, IconParserEngine, IconRefresh } from "@/components/icons";
import { getParserEngines, type ParserEngineInfo } from "@/lib/api/system";
import { useAuth } from "@/lib/auth";

export function ParserEngineSettings() {
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;

  const [engines, setEngines] = useState<ParserEngineInfo[]>([]);
  const [docreaderAddr, setDocreaderAddr] = useState("");
  const [docreaderConnected, setDocreaderConnected] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await getParserEngines();
      setEngines(res.data ?? []);
      setDocreaderAddr(res.docreader_addr || "");
      setDocreaderConnected(res.connected ?? null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load parser engines");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">Parser Engines</h2>
          <p className="caption text-muted mt-1">
            Engines responsible for extracting text, tables, OCR, and formulas from uploaded documents.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {isSystemAdmin && (
            <Link
              href="/platform/system/services"
              className="btn btn-primary btn-sm flex items-center gap-1.5"
            >
              <span>Manage in System Services</span>
            </Link>
          )}
          <button
            type="button"
            onClick={() => void loadData()}
            className="btn btn-outline btn-sm p-1.5"
            title="Refresh engines"
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400">
          {error}
        </div>
      )}

      {/* Admin Notice */}
      <div className="rounded-xl border border-hairline bg-surface-card p-4 text-sm text-muted">
        Document parsing services (DocReader, MinerU, PaddleOCR) are cluster-wide infrastructure
        components.{" "}
        {isSystemAdmin ? (
          <span>
            To configure endpoints, credentials, and OCR toggles, visit{" "}
            <Link href="/platform/system/services" className="font-semibold text-brand underline">
              System Administration &rarr; Services
            </Link>
            .
          </span>
        ) : (
          <span>Contact a system administrator to configure additional parsing backends.</span>
        )}
      </div>

      {/* DocReader Card */}
      <div className="card p-5 space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 border border-blue-500/20">
              <IconDocReader className="h-5 w-5" />
            </div>
            <div>
              <span className="font-semibold text-ink block text-base">DocReader Core Service</span>
              <span className="caption text-muted block text-xs">
                Primary parser gateway for standard document formats (PDF, DOCX, XLSX, TXT, MD, HTML).
              </span>
            </div>
          </div>

          <span
            className={`badge-pill text-xs ${
              docreaderConnected === true
                ? "bg-emerald-500/10 text-emerald-700 border border-emerald-500/20"
                : docreaderConnected === false
                ? "bg-rose-500/10 text-rose-700 border border-rose-500/20"
                : ""
            }`}
          >
            {docreaderConnected === true ? "Connected" : "Disconnected"}
          </span>
        </div>

        {docreaderAddr && (
          <div className="border-t border-hairline pt-2">
            <span className="caption text-muted text-xs font-mono">
              Gateway endpoint: {docreaderAddr}
            </span>
          </div>
        )}
      </div>

      {/* Engine List */}
      <div className="space-y-3">
        <h3 className="text-sm font-semibold text-ink">Supported Specialized Engines</h3>
        {loading ? (
          <p className="caption text-muted text-center py-6">Checking engine availability…</p>
        ) : engines.length === 0 ? (
          <div className="rounded-xl border border-hairline py-8 text-center text-sm text-muted">
            No specialized parser engines detected. Standard parser fallback is active.
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {engines.map((eng) => (
              <div key={eng.Name} className="card p-4 space-y-2">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <IconParserEngine className="h-4 w-4 text-brand" />
                    <span className="font-medium text-ink text-sm">{eng.Name}</span>
                  </div>
                  <span
                    className={`badge-pill text-[11px] ${
                      eng.Available
                        ? "bg-emerald-500/10 text-emerald-700 border border-emerald-500/20"
                        : "bg-stone-500/10 text-stone-600 border border-stone-500/20"
                    }`}
                  >
                    {eng.Available ? "Available" : "Unavailable"}
                  </span>
                </div>
                {eng.Description && (
                  <p className="caption text-muted text-xs line-clamp-2">{eng.Description}</p>
                )}
                {eng.FileTypes && eng.FileTypes.length > 0 && (
                  <div className="flex flex-wrap gap-1 pt-1">
                    {eng.FileTypes.map((ft) => (
                      <span
                        key={ft}
                        className="rounded px-1.5 py-0.5 text-[10px] uppercase font-mono bg-surface-strong text-muted"
                      >
                        {ft}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
