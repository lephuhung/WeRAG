"use client";

import { useCallback, useEffect, useState } from "react";
import { Toggle } from "@/components/settings/toggle";
import { ParserRulesEditor } from "@/components/knowledge/parser-rules-editor";
import {
  getSystemSetting,
  getSystemParseDefaults,
  updateSystemSetting,
  resetSystemSetting,
  PARSE_DEFAULTS_SETTING_KEY,
  type SystemParseDefaults,
} from "@/lib/api/system";
import { listModels, type ModelConfig } from "@/lib/api/models";
import {
  applyOverrides,
  buildProcessOverrides,
  createDefaultUIState,
  type ParseUIState,
} from "@/lib/parse-settings";
import { useT, type LocaleKey } from "@/lib/i18n";
import { Select } from "@/components/select";
import { SegmentedPills } from "@/components/knowledge/segmented-pills";

/* SystemAdmin editor for the platform-wide parse defaults stored under the
 * system_settings key "knowledge.parse_defaults". While `enabled` is on,
 * every ingestion path (upload, reparse, datasource import) resolves against
 * these values and per-upload process_config is ignored — workspace admins
 * never see the parse-settings dialog. The editor mirrors the dialog's
 * sections so the same mental model applies. Rendered full-page at
 * /platform/system/engines/parse-defaults and inside a modal from the
 * knowledge-bases page — callers must gate on is_system_admin. */
export function ParseDefaultsEditor() {
  const { t } = useT();
  const [enabled, setEnabled] = useState(false);
  const [state, setState] = useState<ParseUIState | null>(null);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");

  const loadDefaults = useCallback(async () => {
    try {
      const def = await getSystemParseDefaults();
      if (
        def &&
        typeof def === "object" &&
        ("enabled" in def || "vlm_config" in def || "chunking_config" in def)
      ) {
        setEnabled(def.enabled === true);
        setState(applyOverrides(createDefaultUIState(), def));
        return;
      }
    } catch {
      // Fall through to system setting
    }

    try {
      const item = await getSystemSetting(PARSE_DEFAULTS_SETTING_KEY);
      let rawVal: any = (item as any)?.data?.value ?? (item as any)?.value ?? item;
      if (typeof rawVal === "string") {
        try {
          rawVal = JSON.parse(rawVal);
        } catch {}
      }
      if (rawVal && typeof rawVal === "object") {
        if ("data" in rawVal && (rawVal as any).data) {
          rawVal = (rawVal as any).data;
        }
        if ("value" in rawVal) {
          let inner = (rawVal as any).value;
          if (typeof inner === "string") {
            try {
              inner = JSON.parse(inner);
            } catch {}
          }
          if (inner && typeof inner === "object") rawVal = inner;
        }
      }
      const val = (rawVal ?? {}) as SystemParseDefaults;
      setEnabled(val.enabled === true);
      setState(applyOverrides(createDefaultUIState(), val));
    } catch {
      setState(createDefaultUIState());
    }
  }, []);

  useEffect(() => {
    void loadDefaults();
    listModels()
      .then((rows) => setModels(rows ?? []))
      .catch(() => setModels([]));
  }, [loadDefaults]);

  const patch = (fn: (s: ParseUIState) => ParseUIState) =>
    setState((prev) => (prev ? fn({ ...prev }) : prev));

  /* Include all system-level models, including custom configured VLM/ASR models
   * as well as vision-capable models (matching Vue's filterModelsByType). */
  const selectableModels = models;

  const mmMissing = !!state?.multimodal.enabled && !state.multimodal.vllmModelId;
  const asrMissing = !!state?.asr.enabled && !state.asr.modelId;
  const canSave = !!state && !mmMissing && !asrMissing && !saving;

  const save = async () => {
    if (!state || !canSave) return;
    setSaving(true);
    setSaved(false);
    setError("");
    const overrides = buildProcessOverrides(state);
    const value: SystemParseDefaults = {
      enabled,
      ...overrides,
    };
    try {
      await updateSystemSetting(PARSE_DEFAULTS_SETTING_KEY, value);
      setSaved(true);
      setTimeout(() => setSaved(false), 3500);
      setState(applyOverrides(createDefaultUIState(), value));
    } catch (e) {
      setError(e instanceof Error ? e.message : t("pd.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const reset = async () => {
    setSaving(true);
    setSaved(false);
    setError("");
    try {
      await resetSystemSetting(PARSE_DEFAULTS_SETTING_KEY);
      setEnabled(false);
      setState(createDefaultUIState());
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("pd.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const row = (label: string, desc: string | undefined, control: React.ReactNode) => (
    <div className="flex items-center justify-between gap-4 py-2">
      <div className="min-w-0">
        <div className="text-[13px] font-medium text-ink">{label}</div>
        {desc && <div className="caption mt-0.5 text-muted">{desc}</div>}
      </div>
      <div className="shrink-0">{control}</div>
    </div>
  );

  const modelSelect = (
    type: "VLLM" | "ASR",
    value: string,
    invalid: boolean,
    onSel: (v: string) => void,
  ) => {
    const available = selectableModels.filter((m) => {
      if (type === "VLLM") {
        return (
          m.type === "VLLM" ||
          (m.type === "KnowledgeQA" && m.parameters?.supports_vision === true)
        );
      }
      return m.type === type;
    });

    return (
      <Select
        className={`w-full sm:w-[280px] ${invalid ? "border-error" : ""}`}
        value={value}
        onChange={onSel}
        placeholder={t("ps.modelPlaceholder")}
        options={[
          { value: "", label: t("ps.modelPlaceholder") },
          ...available
            .filter((m): m is ModelConfig & { id: string } => Boolean(m.id))
            .map((m) => ({
              value: m.id,
              label: `${m.display_name?.trim() || m.name}${m.is_builtin ? " (built-in)" : ""}`,
            })),
          ...(value && !available.some((m) => m.id === value)
            ? [{ value, label: `${value} (Current)` }]
            : []),
        ]}
      />
    );
  };

  if (!state) {
    return <p className="caption text-muted">{t("ps.parserLoading")}</p>;
  }

  return (
    <div>
      <section className="card mb-4 p-4">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="min-w-0">
            <h2 className="title-sm text-ink">{t("pd.title")}</h2>
            <p className="caption mt-1 text-muted">{t("pd.desc")}</p>
          </div>
          <div className="flex items-center gap-4 shrink-0">
            <div className="flex items-center gap-2">
              <span className="caption font-medium text-ink">{t("pd.enableLabel")}</span>
              <Toggle checked={enabled} onChange={setEnabled} label={t("pd.enableLabel")} />
            </div>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={() => void save()}
              disabled={!canSave}
            >
              {saving ? t("pd.saving") : t("common.save")}
            </button>
          </div>
        </div>
        {saved && (
          <div className="mt-3 rounded-[8px] bg-emerald-500/10 border border-emerald-500/20 px-3 py-2 text-[13px] text-emerald-600 font-medium">
            ✓ {t("pd.saved")}
          </div>
        )}
        {error && (
          <div className="mt-3 rounded-[8px] bg-rose-500/10 border border-rose-500/20 px-3 py-2 text-[13px] text-rose-600 font-medium">
            ⚠ {error}
          </div>
        )}
        {enabled && (
          <p className="caption mt-3 rounded-[8px] bg-surface-strong px-3 py-2 text-muted">
            {t("pd.lockedHint")}
          </p>
        )}
      </section>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <section className="card p-4 xl:col-span-3">
        <h3 className="title-sm text-ink">{t("ps.tabParser")}</h3>
        <p className="caption mt-1 text-muted">{t("ps.parserDesc")}</p>
        <div className="mt-2">
          <ParserRulesEditor
            rules={state.chunking.parserEngineRules}
            relevantExtensions={[]}
            columns={3}
            onChange={(rules) =>
              patch((s) => ({
                ...s,
                chunking: { ...s.chunking, parserEngineRules: rules },
              }))
            }
          />
        </div>
        {row(
          t("ps.pdfScanLabel"),
          t("ps.pdfScanDesc"),
          <SegmentedPills
            value={state.pdfForceScanned ? "force" : "auto"}
            onChange={(v) => patch((s) => ({ ...s, pdfForceScanned: v === "force" }))}
            options={[
              { value: "auto", label: t("ps.pdfScanAuto") },
              { value: "force", label: t("ps.pdfScanForce") },
            ]}
          />,
        )}
      </section>

      <section className="card p-4">
        <h3 className="title-sm text-ink">{t("ps.tabChunking")}</h3>
        <div className="divide-y divide-hairline">
          {row(
            t("ps.chunkStrategy"),
            undefined,
            <Select
              className="w-full sm:w-[280px]"
              value={state.chunking.strategy}
              onChange={(v) =>
                patch((s) => ({ ...s, chunking: { ...s.chunking, strategy: v } }))
              }
              options={["auto", "heading", "heuristic", "legacy"].map((v) => ({
                value: v,
                label: t(`ps.strategy.${v}` as LocaleKey),
              }))}
            />,
          )}
          {row(
            t("ps.chunkSize"),
            undefined,
            <input
              className="input w-full sm:w-[160px]"
              type="number"
              min={64}
              max={8192}
              step={64}
              value={state.chunking.chunkSize}
              onChange={(e) =>
                patch((s) => ({
                  ...s,
                  chunking: { ...s.chunking, chunkSize: Number(e.target.value) },
                }))
              }
            />,
          )}
          {row(
            t("ps.chunkOverlap"),
            undefined,
            <input
              className="input w-full sm:w-[160px]"
              type="number"
              min={0}
              max={1024}
              step={16}
              value={state.chunking.chunkOverlap}
              onChange={(e) =>
                patch((s) => ({
                  ...s,
                  chunking: { ...s.chunking, chunkOverlap: Number(e.target.value) },
                }))
              }
            />,
          )}
          {row(
            t("ps.tokenLimit"),
            undefined,
            <input
              className="input w-full sm:w-[160px]"
              type="number"
              min={0}
              max={8192}
              step={64}
              value={state.chunking.tokenLimit}
              onChange={(e) =>
                patch((s) => ({
                  ...s,
                  chunking: { ...s.chunking, tokenLimit: Number(e.target.value) },
                }))
              }
            />,
          )}
          {row(
            t("ps.parentChild"),
            t("ps.parentChildDesc"),
            <Toggle
              checked={state.chunking.enableParentChild}
              onChange={(v) =>
                patch((s) => ({
                  ...s,
                  chunking: { ...s.chunking, enableParentChild: v },
                }))
              }
            />,
          )}
          {state.chunking.enableParentChild && (
            <>
              {row(
                t("ps.parentChunkSize"),
                undefined,
                <input
                  className="input w-full sm:w-[160px]"
                  type="number"
                  min={512}
                  max={8192}
                  step={64}
                  value={state.chunking.parentChunkSize}
                  onChange={(e) =>
                    patch((s) => ({
                      ...s,
                      chunking: { ...s.chunking, parentChunkSize: Number(e.target.value) },
                    }))
                  }
                />,
              )}
              {row(
                t("ps.childChunkSize"),
                undefined,
                <input
                  className="input w-full sm:w-[160px]"
                  type="number"
                  min={64}
                  max={2048}
                  step={32}
                  value={state.chunking.childChunkSize}
                  onChange={(e) =>
                    patch((s) => ({
                      ...s,
                      chunking: { ...s.chunking, childChunkSize: Number(e.target.value) },
                    }))
                  }
                />,
              )}
            </>
          )}
        </div>
      </section>

      <section className="card p-4">
        <h3 className="title-sm text-ink">{t("ps.tabMultimodal")}</h3>
        <p className="caption mt-1 text-muted">{t("ps.multimodalDesc")}</p>
        <div className="divide-y divide-hairline">
          {row(
            t("ps.multimodalLabel"),
            t("ps.multimodalHint"),
            <Toggle
              checked={state.multimodal.enabled}
              onChange={(v) =>
                patch((s) => ({ ...s, multimodal: { ...s.multimodal, enabled: v } }))
              }
            />,
          )}
          {state.multimodal.enabled && (
            <>
              {row(
                t("ps.vllmLabel"),
                t("ps.vllmDesc"),
                modelSelect("VLLM", state.multimodal.vllmModelId, mmMissing, (v) =>
                  patch((s) => ({ ...s, multimodal: { ...s.multimodal, vllmModelId: v } })),
                ),
              )}
              {row(
                t("ps.descLangLabel"),
                t("ps.descLangDesc"),
                <Select
                  className="w-full sm:w-[280px]"
                  value={state.multimodal.descriptionLanguage}
                  onChange={(v) =>
                    patch((s) => ({
                      ...s,
                      multimodal: { ...s.multimodal, descriptionLanguage: v },
                    }))
                  }
                  options={[
                    { value: "", label: t("ps.descLangAuto") },
                    { value: "Vietnamese", label: "Tiếng Việt" },
                    { value: "Chinese", label: "中文" },
                    { value: "English", label: "English" },
                    { value: "Korean", label: "한국어" },
                    { value: "Russian", label: "Русский" },
                  ]}
                />,
              )}
              <div className="py-3">
                <div className="text-[13px] font-medium text-ink">{t("ps.customInstrLabel")}</div>
                <textarea
                  className="input mt-2 h-auto min-h-[56px] w-full resize-y"
                  placeholder={t("ps.customInstrPlaceholder")}
                  value={state.multimodal.customInstructions}
                  onChange={(e) =>
                    patch((s) => ({
                      ...s,
                      multimodal: { ...s.multimodal, customInstructions: e.target.value },
                    }))
                  }
                />
              </div>
            </>
          )}
        </div>
        {mmMissing && (
          <p className="caption mt-2 rounded-[8px] bg-surface-strong px-3 py-2 text-error">
            {t("pd.modelRequired")}
          </p>
        )}
      </section>

      {/* Column 3: ASR + summary + question stack — keeps the three narrow
       * sections in one column so the grid stays three cells wide. */}
      <div className="flex flex-col gap-4">
      <section className="card p-4">
        <h3 className="title-sm text-ink">{t("ps.tabAsr")}</h3>
        <p className="caption mt-1 text-muted">{t("ps.asrDesc")}</p>
        <div className="divide-y divide-hairline">
          {row(
            t("ps.asrLabel"),
            t("ps.asrHint"),
            <Toggle
              checked={state.asr.enabled}
              onChange={(v) => patch((s) => ({ ...s, asr: { ...s.asr, enabled: v } }))}
            />,
          )}
          {state.asr.enabled && (
            <>
              {row(
                t("ps.asrModelLabel"),
                undefined,
                modelSelect("ASR", state.asr.modelId, asrMissing, (v) =>
                  patch((s) => ({ ...s, asr: { ...s.asr, modelId: v } })),
                ),
              )}
              {row(
                t("ps.asrLangLabel"),
                t("ps.asrLangDesc"),
                <input
                  className="input w-full sm:w-[280px]"
                  value={state.asr.language}
                  placeholder={t("ps.asrLangPlaceholder")}
                  onChange={(e) =>
                    patch((s) => ({ ...s, asr: { ...s.asr, language: e.target.value } }))
                  }
                />,
              )}
            </>
          )}
        </div>
        {asrMissing && (
          <p className="caption mt-2 rounded-[8px] bg-surface-strong px-3 py-2 text-error">
            {t("pd.modelRequired")}
          </p>
        )}
      </section>

      <section className="card p-4">
        <h3 className="title-sm text-ink">{t("ps.tabSummary")}</h3>
        <p className="caption mt-1 text-muted">{t("ps.summaryDesc")}</p>
        {row(
          t("ps.summaryLabel"),
          t("ps.summaryHint"),
          <Toggle
            checked={state.summaryEnabled}
            onChange={(v) => patch((s) => ({ ...s, summaryEnabled: v }))}
          />,
        )}
      </section>

      <section className="card p-4">
        <h3 className="title-sm text-ink">{t("ps.tabQuestion")}</h3>
        <p className="caption mt-1 text-muted">{t("ps.questionDesc")}</p>
        {row(
          t("ps.questionLabel"),
          t("ps.questionHint"),
          <div className="flex items-center gap-3">
            {state.question.enabled && (
              <input
                className="input w-[88px]"
                type="number"
                min={1}
                max={10}
                value={state.question.questionCount}
                onChange={(e) =>
                  patch((s) => ({
                    ...s,
                    question: { ...s.question, questionCount: Number(e.target.value) },
                  }))
                }
              />
            )}
            <Toggle
              checked={state.question.enabled}
              onChange={(v) => patch((s) => ({ ...s, question: { ...s.question, enabled: v } }))}
            />
          </div>,
        )}
        {state.question.enabled && (
          <div className="py-3">
            <div className="text-[13px] font-medium text-ink">{t("ps.questionInstrLabel")}</div>
            <textarea
              className="input mt-2 h-auto min-h-[56px] w-full resize-y"
              value={state.question.customInstructions}
              onChange={(e) =>
                patch((s) => ({
                  ...s,
                  question: { ...s.question, customInstructions: e.target.value },
                }))
              }
            />
          </div>
        )}
      </section>
      </div>

      </div>

      <div className="mt-4 flex items-center justify-between gap-3 pb-2">
        <div>
          {saved && <span className="caption text-success">{t("pd.saved")}</span>}
          {error && <span className="caption text-error">{error}</span>}
        </div>
        <div className="flex gap-2">
          <button type="button" className="btn btn-outline btn-sm" onClick={reset} disabled={saving}>
            {t("pd.reset")}
          </button>
          <button type="button" className="btn btn-primary btn-sm" onClick={save} disabled={!canSave}>
            {saving ? t("pd.saving") : t("common.save")}
          </button>
        </div>
      </div>
    </div>
  );
}
