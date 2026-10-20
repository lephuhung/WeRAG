"use client";

/* Parser engines (SuperAdmin): DocReader connection, the engines it and the
 * backend expose, and the PaddleOCR-VL / OpenAI-compatible OCR endpoint
 * (senocr-vi on the dev stack). The save lands on the configuration profile
 * and is copied to every workspace on it. Which engine parses which file
 * type is set in the parse defaults on the knowledge base page, not here. */

import { useEffect, useState } from "react";
import {
  checkParserEngines,
  getParserEngineConfig,
  getParserEngines,
  reconnectDocReader,
  updateParserEngineConfig,
  type ParserEngineConfig,
  type ParserEngineInfo,
} from "@/lib/api/system";
import { ApiError } from "@/lib/api-client";
import { IconRefresh } from "@/components/icons";
import { Select } from "@/components/select";
import { Toggle } from "@/components/settings/toggle";
import { useT } from "@/lib/i18n";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { Loading } from "@/components/settings/loading";
import { DeniedView } from "@/components/settings/system-section";
import { FieldLabel } from "@/components/settings/field";
import { ConsolePage } from "@/components/settings/console-page";
import { useToast } from "@/components/toast";

type Probe = { ok: boolean; message: string } | null;

function StatusDot({ ok }: { ok: boolean }) {
  return <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${ok ? "bg-success" : "bg-warning"}`} />;
}

function ProbeNote({ probe }: { probe: Probe }) {
  if (!probe) return null;
  return (
    <p className={`caption mt-2 ${probe.ok ? "text-success" : "text-error"}`}>{probe.message}</p>
  );
}

/* The fields this page edits. The whole loaded config goes back on save, so
 * fields the page does not show (vllm_xargs, legacy keys) keep their
 * values; the dirty check compares only what the user can change here. */
const EDITABLE_KEYS = [
  "paddleocr_vl_endpoint",
  "paddleocr_vl_api",
  "paddleocr_vl_model",
  "paddleocr_vl_api_key",
  "paddleocr_vl_prompt",
  "paddleocr_vl_repetition_penalty",
  "paddleocr_vl_use_seal_recognition",
  "paddleocr_vl_use_chart_recognition",
] as const;

/* The two toggles fall back to a default when the backend stored nothing;
 * the dirty check compares the values the user actually sees, so the same
 * fallback applies here. */
const TOGGLE_DEFAULTS: Partial<Record<(typeof EDITABLE_KEYS)[number], boolean>> = {
  paddleocr_vl_use_seal_recognition: true,
  paddleocr_vl_use_chart_recognition: false,
};

export function ParserEngineSettings() {
  const { t } = useT();
  const toast = useToast();
  const [engines, setEngines] = useState<ParserEngineInfo[]>([]);
  const [docreader, setDocreader] = useState({ addr: "", transport: "", connected: false });
  const [addr, setAddr] = useState("");
  const [config, setConfig] = useState<ParserEngineConfig>({});
  const [loadedConfig, setLoadedConfig] = useState<ParserEngineConfig>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [denied, setDenied] = useState(false);
  const [saving, setSaving] = useState(false);
  const [reconnecting, setReconnecting] = useState(false);
  const [docreaderProbe, setDocreaderProbe] = useState<Probe>(null);
  const [checking, setChecking] = useState(false);
  const [ocrProbe, setOcrProbe] = useState<Probe>(null);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const [list, cfg] = await Promise.all([getParserEngines(), getParserEngineConfig()]);
      setEngines(list.data ?? []);
      setDocreader({
        addr: list.docreader_addr ?? "",
        transport: list.docreader_transport ?? "",
        connected: list.connected !== false,
      });
      setAddr(list.docreader_addr ?? "");
      setConfig(cfg.data ?? {});
      setLoadedConfig(cfg.data ?? {});
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setEngines([]);
      } else {
        setError(e instanceof Error ? e.message : t("peng.loadFailed"));
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const patch = (p: Partial<ParserEngineConfig>) => {
    setConfig((c) => ({ ...c, ...p }));
  };

  const dirty = EDITABLE_KEYS.some((k) => {
    const fallback = TOGGLE_DEFAULTS[k];
    if (fallback !== undefined) {
      return (config[k] ?? fallback) !== (loadedConfig[k] ?? fallback);
    }
    return String(config[k] ?? "") !== String(loadedConfig[k] ?? "");
  });

  const handleReconnect = async () => {
    setReconnecting(true);
    setDocreaderProbe(null);
    try {
      const res = await reconnectDocReader(addr.trim());
      setDocreaderProbe(
        res.connected ? { ok: true, message: t("eng.docreaderOk") } : { ok: false, message: res.msg || t("eng.docreaderFail") },
      );
      if (res.connected) await load();
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setDocreaderProbe({ ok: false, message: e instanceof Error ? e.message : t("eng.docreaderFail") });
    } finally {
      setReconnecting(false);
    }
  };

  /* Probes with the typed values; nothing is saved. */
  const handleCheck = async () => {
    setChecking(true);
    setOcrProbe(null);
    try {
      const res = await checkParserEngines(config);
      const ocr = (res.data ?? []).find((e) => e.Name === "paddleocr_vl");
      setOcrProbe(
        ocr?.Available
          ? { ok: true, message: t("eng.probeOk") }
          : { ok: false, message: ocr?.UnavailableReason || t("eng.probeFail") },
      );
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setOcrProbe({ ok: false, message: e instanceof Error ? e.message : t("eng.testFail") });
    } finally {
      setChecking(false);
    }
  };

  /* The whole loaded config goes back, so fields this page does not show
   * (vllm_xargs, legacy keys) keep their values. */
  const handleSave = async () => {
    setSaving(true);
    setError("");
    try {
      const res = await updateParserEngineConfig(config);
      const saved = res.data ?? config;
      setConfig(saved);
      setLoadedConfig(saved);
      toast.success(t("common.saved"));
      await load();
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setError(e instanceof Error ? e.message : t("eng.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  if (denied) {
    return <DeniedView />;
  }

  if (loading && engines.length === 0) {
    return <Loading label={t("peng.loading")} className="py-12" />;
  }

  return (
    <ConsolePage size="form">
      <div>
        <SectionHeader
          title={t("peng.title")}
          subtitle={t("peng.subtitle")}
          actions={
            <button
              type="button"
              className="btn btn-outline btn-sm flex items-center gap-1.5"
              onClick={() => void load()}
              disabled={loading}
            >
              <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} /> {t("common.refresh")}
            </button>
          }
        />

        {error && (
          <Notice
            tone="error"
            className="mt-4"
            action={
              <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
                {t("common.retry")}
              </button>
            }
          >
            {error}
          </Notice>
        )}

        {/* DocReader */}
        <section className="mt-6 rounded-xl border border-hairline bg-surface-card p-4">
          <div className="flex items-center gap-2">
            <StatusDot ok={docreader.connected} />
            <h3 className="text-[15px] font-medium text-ink">DocReader</h3>
            {docreader.transport && <span className="badge-pill uppercase text-[10px]">{docreader.transport}</span>}
            <span className="caption text-muted">
              {docreader.connected ? t("peng.connected") : t("peng.disconnected")}
            </span>
          </div>
          <div className="mt-3">
            <FieldLabel htmlFor="peng-docreader-addr">{t("eng.docreaderAddr")}</FieldLabel>
            <div className="flex gap-2">
              <input
                id="peng-docreader-addr"
                className="input h-9 w-full text-[13px]"
                placeholder="docreader:50051"
                value={addr}
                onChange={(e) => setAddr(e.target.value)}
              />
              <button
                type="button"
                className="btn btn-outline btn-sm shrink-0"
                onClick={() => void handleReconnect()}
                disabled={reconnecting}
              >
                {reconnecting ? t("eng.checking") : t("peng.reconnect")}
              </button>
            </div>
          </div>
          <p className="caption mt-1 text-muted">{t("peng.docreaderHint")}</p>
          <ProbeNote probe={docreaderProbe} />
        </section>

        {/* Engines */}
        <section className="mt-4 rounded-xl border border-hairline bg-surface-card p-4">
          <h3 className="text-[15px] font-medium text-ink">{t("peng.enginesTitle")}</h3>
          <p className="caption mt-1 text-muted">{t("peng.enginesHint")}</p>
          <div className="mt-3 divide-y divide-hairline">
            {engines.map((e) => (
              <div key={e.Name} className="py-2.5">
                <div className="flex items-center gap-2">
                  <StatusDot ok={e.Available !== false} />
                  <span className="font-mono text-[13px] text-ink">{e.Name}</span>
                  <span className="caption text-muted">
                    {e.Available === false ? t("eng.stDegraded") : t("eng.stHealthy")}
                  </span>
                </div>
                {e.Description && <p className="caption mt-0.5 text-muted">{e.Description}</p>}
                {e.Available === false && e.UnavailableReason && (
                  <p className="caption mt-0.5 text-error">{e.UnavailableReason}</p>
                )}
                {(e.FileTypes ?? []).length > 0 && (
                  <div className="mt-1 flex flex-wrap gap-1">
                    {e.FileTypes.map((ft) => (
                      <span key={ft} className="badge-pill text-[10px]">
                        .{ft}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            ))}
            {engines.length === 0 && <p className="caption py-2 text-muted">{t("ps.parserNoEngines")}</p>}
          </div>
        </section>

        {/* PaddleOCR-VL / OpenAI-compatible OCR */}
        <section className="mt-4 rounded-xl border border-hairline bg-surface-card p-4">
          <h3 className="text-[15px] font-medium text-ink">{t("peng.ocrTitle")}</h3>
          <p className="caption mt-1 text-muted">{t("peng.ocrHint")}</p>

          <div className="mt-3 space-y-3">
            <div>
              <FieldLabel htmlFor="peng-ocr-endpoint">{t("peng.endpoint")}</FieldLabel>
              <input
                id="peng-ocr-endpoint"
                className="input h-9 w-full text-[13px]"
                placeholder="http://hrag-vllm-ocr:8001/v1"
                value={config.paddleocr_vl_endpoint ?? ""}
                onChange={(e) => patch({ paddleocr_vl_endpoint: e.target.value })}
              />
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <FieldLabel htmlFor="peng-ocr-api">{t("peng.api")}</FieldLabel>
                <Select
                  id="peng-ocr-api"
                  className="h-9 w-full text-[13px]"
                  value={config.paddleocr_vl_api ?? ""}
                  onChange={(v) => patch({ paddleocr_vl_api: v })}
                  options={[
                    { value: "", label: t("peng.apiAuto") },
                    { value: "openai", label: t("peng.apiOpenAI") },
                    { value: "layout", label: t("peng.apiLayout") },
                  ]}
                />
              </div>
              <div>
                <FieldLabel htmlFor="peng-ocr-model">{t("eng.model")}</FieldLabel>
                <input
                  id="peng-ocr-model"
                  className="input h-9 w-full text-[13px]"
                  placeholder="senocr-vi"
                  value={config.paddleocr_vl_model ?? ""}
                  onChange={(e) => patch({ paddleocr_vl_model: e.target.value })}
                />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <FieldLabel htmlFor="peng-ocr-api-key">{t("eng.apiKey")}</FieldLabel>
                <input
                  id="peng-ocr-api-key"
                  className="input h-9 w-full text-[13px]"
                  type="password"
                  autoComplete="off"
                  placeholder={t("peng.optional")}
                  value={config.paddleocr_vl_api_key ?? ""}
                  onChange={(e) => patch({ paddleocr_vl_api_key: e.target.value })}
                />
              </div>
              <div>
                <FieldLabel htmlFor="peng-ocr-rep-penalty">{t("peng.repPenalty")}</FieldLabel>
                <input
                  id="peng-ocr-rep-penalty"
                  className="input h-9 w-full text-[13px]"
                  inputMode="decimal"
                  placeholder="1.05"
                  value={config.paddleocr_vl_repetition_penalty ?? ""}
                  onChange={(e) => patch({ paddleocr_vl_repetition_penalty: e.target.value })}
                />
                <p className="caption mt-1 text-muted">{t("peng.repPenaltyHint")}</p>
              </div>
            </div>
            <div>
              <FieldLabel htmlFor="peng-ocr-prompt">{t("peng.prompt")}</FieldLabel>
              <input
                id="peng-ocr-prompt"
                className="input h-9 w-full text-[13px]"
                placeholder={t("peng.promptPh")}
                value={config.paddleocr_vl_prompt ?? ""}
                onChange={(e) => patch({ paddleocr_vl_prompt: e.target.value })}
              />
            </div>
            <div className="flex flex-wrap gap-6">
              <div className="flex items-center gap-2 text-[13px] text-ink">
                <Toggle
                  size="sm"
                  checked={config.paddleocr_vl_use_seal_recognition ?? true}
                  onChange={(v) => patch({ paddleocr_vl_use_seal_recognition: v })}
                  label={t("eng.seal")}
                />
                {t("eng.seal")}
              </div>
              <div className="flex items-center gap-2 text-[13px] text-ink">
                <Toggle
                  size="sm"
                  checked={config.paddleocr_vl_use_chart_recognition ?? false}
                  onChange={(v) => patch({ paddleocr_vl_use_chart_recognition: v })}
                  label={t("eng.chart")}
                />
                {t("eng.chart")}
              </div>
            </div>
          </div>
          <ProbeNote probe={ocrProbe} />
        </section>

        <div className="mt-6 flex items-center justify-end gap-2 border-t border-hairline pt-4">
          <button
            type="button"
            className="btn btn-outline btn-sm"
            onClick={() => void handleCheck()}
            disabled={checking}
          >
            {checking ? t("eng.checking") : t("eng.testConn")}
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            onClick={() => void handleSave()}
            disabled={!dirty || saving}
          >
            {saving ? t("memp.saving") : t("eng.saveConfig")}
          </button>
        </div>
      </div>
    </ConsolePage>
  );
}
