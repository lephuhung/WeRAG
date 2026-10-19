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
import { IconRefresh } from "@/components/icons";
import { Select } from "@/components/select";
import { Toggle } from "@/components/settings/toggle";
import { useT } from "@/lib/i18n";

type Probe = { ok: boolean; message: string } | null;

function StatusDot({ ok }: { ok: boolean }) {
  return <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${ok ? "bg-emerald-500" : "bg-amber-500"}`} />;
}

function ProbeNote({ probe }: { probe: Probe }) {
  if (!probe) return null;
  return (
    <p className={`caption mt-2 ${probe.ok ? "text-success" : "text-error"}`}>{probe.message}</p>
  );
}

export function ParserEngineSettings() {
  const { t } = useT();
  const [engines, setEngines] = useState<ParserEngineInfo[]>([]);
  const [docreader, setDocreader] = useState({ addr: "", transport: "", connected: false });
  const [addr, setAddr] = useState("");
  const [config, setConfig] = useState<ParserEngineConfig>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
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
    } catch (e) {
      setError(e instanceof Error ? e.message : t("peng.loadFailed"));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const patch = (p: Partial<ParserEngineConfig>) => {
    setSaved(false);
    setConfig((c) => ({ ...c, ...p }));
  };

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
      setConfig(res.data ?? config);
      setSaved(true);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("eng.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  if (loading && engines.length === 0) {
    return <div className="py-12 text-center text-muted">{t("peng.loading")}</div>;
  }

  return (
    <div>
      <div className="flex items-center justify-between border-b border-hairline pb-4">
        <div>
          <h2 className="title-md">{t("peng.title")}</h2>
          <p className="caption mt-1 text-muted">{t("peng.subtitle")}</p>
        </div>
        <button className="btn btn-outline btn-sm flex items-center gap-1.5" onClick={() => void load()} disabled={loading}>
          <IconRefresh className="h-3.5 w-3.5" /> {t("common.refresh")}
        </button>
      </div>

      {error && <div className="mt-4 rounded-xl bg-error/10 p-3 text-[13px] text-error">{error}</div>}

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
        <label className="caption mb-1 mt-3 block font-medium text-ink">{t("eng.docreaderAddr")}</label>
        <div className="flex gap-2">
          <input
            className="input h-9 w-full text-[13px]"
            placeholder="docreader:50051"
            value={addr}
            onChange={(e) => setAddr(e.target.value)}
          />
          <button className="btn btn-outline btn-sm shrink-0" onClick={() => void handleReconnect()} disabled={reconnecting}>
            {reconnecting ? t("eng.checking") : t("peng.reconnect")}
          </button>
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
            <label className="caption mb-1 block font-medium text-ink">{t("peng.endpoint")}</label>
            <input
              className="input h-9 w-full text-[13px]"
              placeholder="http://hrag-vllm-ocr:8001/v1"
              value={config.paddleocr_vl_endpoint ?? ""}
              onChange={(e) => patch({ paddleocr_vl_endpoint: e.target.value })}
            />
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="caption mb-1 block font-medium text-ink">{t("peng.api")}</label>
              <Select
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
              <label className="caption mb-1 block font-medium text-ink">{t("eng.model")}</label>
              <input
                className="input h-9 w-full text-[13px]"
                placeholder="senocr-vi"
                value={config.paddleocr_vl_model ?? ""}
                onChange={(e) => patch({ paddleocr_vl_model: e.target.value })}
              />
            </div>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="caption mb-1 block font-medium text-ink">{t("eng.apiKey")}</label>
              <input
                className="input h-9 w-full text-[13px]"
                type="password"
                autoComplete="off"
                placeholder={t("peng.optional")}
                value={config.paddleocr_vl_api_key ?? ""}
                onChange={(e) => patch({ paddleocr_vl_api_key: e.target.value })}
              />
            </div>
            <div>
              <label className="caption mb-1 block font-medium text-ink">{t("peng.repPenalty")}</label>
              <input
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
            <label className="caption mb-1 block font-medium text-ink">{t("peng.prompt")}</label>
            <input
              className="input h-9 w-full text-[13px]"
              placeholder={t("peng.promptPh")}
              value={config.paddleocr_vl_prompt ?? ""}
              onChange={(e) => patch({ paddleocr_vl_prompt: e.target.value })}
            />
          </div>
          <div className="flex flex-wrap gap-6">
            <label className="flex items-center gap-2 text-[13px] text-ink">
              <Toggle
                size="sm"
                checked={config.paddleocr_vl_use_seal_recognition ?? true}
                onChange={(v) => patch({ paddleocr_vl_use_seal_recognition: v })}
                label={t("eng.seal")}
              />
              {t("eng.seal")}
            </label>
            <label className="flex items-center gap-2 text-[13px] text-ink">
              <Toggle
                size="sm"
                checked={config.paddleocr_vl_use_chart_recognition ?? false}
                onChange={(v) => patch({ paddleocr_vl_use_chart_recognition: v })}
                label={t("eng.chart")}
              />
              {t("eng.chart")}
            </label>
          </div>
        </div>
        <ProbeNote probe={ocrProbe} />
      </section>

      <div className="mt-6 flex items-center justify-end gap-2 border-t border-hairline pt-4">
        {saved && <span className="caption mr-auto text-success">{t("peng.saved")}</span>}
        <button className="btn btn-outline btn-sm" onClick={() => void handleCheck()} disabled={checking}>
          {checking ? t("eng.checking") : t("eng.testConn")}
        </button>
        <button className="btn btn-primary btn-sm" onClick={() => void handleSave()} disabled={saving}>
          {saving ? t("memp.saving") : t("eng.saveConfig")}
        </button>
      </div>
    </div>
  );
}
