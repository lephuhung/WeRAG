"use client";

import { useEffect, useMemo, useState } from "react";
import { Toggle } from "@/components/settings/toggle";
import { IconClose, IconDoc } from "@/components/icons";
import { ParserRulesEditor } from "@/components/knowledge/parser-rules-editor";
import {
  listKnowledgeTags,
  type KnowledgeBaseRow,
  type KnowledgeProcessOverrides,
} from "@/lib/api/knowledge";
import { listModels, type ModelConfig } from "@/lib/api/models";
import {
  applyOverrides,
  AUDIO_EXTENSIONS,
  buildProcessOverrides,
  fileExt,
  IMAGE_EXTENSIONS,
  initFromKb,
  type ParseUIState,
} from "@/lib/parse-settings";
import { useT, type LocaleKey } from "@/lib/i18n";

/* Port of frontend/src/views/knowledge/components/UploadConfirmDialog.vue,
 * reduced to the two modes this app uses: "file" (batch upload) and
 * "reparse" (rebuild one document). URL/manual/graph-edit flows are not
 * ported; graph/extract config is passed through from the KB defaults. */

type SectionKey = "tags" | "parser" | "chunking" | "multimodal" | "asr" | "summary" | "question";

export interface ReparseSource {
  fileName: string;
  fileType?: string;
  overrides?: KnowledgeProcessOverrides | null;
}

export interface ParseSettingsResult {
  processConfig: KnowledgeProcessOverrides;
  tagIds: string[];
  /** Remaining files in file mode (rows can be removed inside the dialog). */
  files: File[];
}

export function ParseSettingsDialog({
  open,
  mode,
  kb,
  files,
  reparse,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  mode: "file" | "reparse";
  kb: KnowledgeBaseRow | null;
  files?: File[];
  reparse?: ReparseSource | null;
  onCancel: () => void;
  onConfirm: (result: ParseSettingsResult) => void;
}) {
  const { t } = useT();
  const [state, setState] = useState<ParseUIState | null>(null);
  const [localFiles, setLocalFiles] = useState<File[]>([]);
  const [tagIds, setTagIds] = useState<string[]>([]);
  const [tags, setTags] = useState<{ id: string; name: string }[] | null>(null);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [section, setSection] = useState<SectionKey>("tags");
  const [moreOpen, setMoreOpen] = useState(false);
  const [sepDraft, setSepDraft] = useState("");

  useEffect(() => {
    if (!open) return;
    const base = initFromKb(kb);
    const next = mode === "reparse" ? applyOverrides(base, reparse?.overrides) : base;
    setState(next);
    setLocalFiles(mode === "file" ? [...(files ?? [])] : []);
    setTagIds([]);
    /* Same landing-section logic as the Vue dialog: reparse opens on the
     * parser tab; batches missing VLM/ASR setup land on the failing tab. */
    const exts = new Set(
      (mode === "file" ? (files ?? []) : [])
        .map((f) => fileExt(f.name))
        .filter(Boolean),
    );
    if (mode === "reparse" && reparse?.fileType) exts.add(reparse.fileType.toLowerCase());
    const needsMm =
      [...exts].some((x) => IMAGE_EXTENSIONS.includes(x)) &&
      (!next.multimodal.enabled || !next.multimodal.vllmModelId);
    const needsAsr =
      [...exts].some((x) => AUDIO_EXTENSIONS.includes(x)) &&
      (!next.asr.enabled || !next.asr.modelId);
    setSection(mode === "reparse" ? "parser" : needsMm ? "multimodal" : needsAsr ? "asr" : "tags");
    setMoreOpen(false);
    setSepDraft("");
    listModels()
      .then(setModels)
      .catch(() => setModels([]));
    if (mode !== "reparse" && kb?.id) {
      setTags(null);
      listKnowledgeTags(kb.id, { page: 1, page_size: 1000 })
        .then((res) => {
          const rows =
            (res as { data?: { data?: { id: unknown; name: unknown }[] } })?.data?.data ?? [];
          setTags(rows.map((x) => ({ id: String(x.id), name: String(x.name ?? "") })));
        })
        .catch(() => setTags([]));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onCancel();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onCancel]);

  const batchExts = useMemo(() => {
    const set = new Set<string>();
    if (mode === "reparse" && reparse?.fileType) set.add(reparse.fileType.toLowerCase());
    for (const f of localFiles) {
      const ext = fileExt(f.name);
      if (ext) set.add(ext);
    }
    return [...set];
  }, [mode, reparse, localFiles]);

  const hasImages = batchExts.some((x) => IMAGE_EXTENSIONS.includes(x));
  const hasAudio = batchExts.some((x) => AUDIO_EXTENSIONS.includes(x));
  const hasPdf = batchExts.includes("pdf");

  const mmMissing = !!state?.multimodal.enabled && !state.multimodal.vllmModelId;
  const asrMissing = !!state?.asr.enabled && !state.asr.modelId;
  const mmIssue = hasImages ? !state?.multimodal.enabled || !state.multimodal.vllmModelId : mmMissing;
  const asrIssue = hasAudio ? !state?.asr.enabled || !state.asr.modelId : asrMissing;

  const canConfirm =
    !!state &&
    (mode !== "file" || localFiles.length > 0) &&
    (!hasImages || (!!state.multimodal.enabled && !!state.multimodal.vllmModelId)) &&
    (!hasAudio || (!!state.asr.enabled && !!state.asr.modelId)) &&
    !mmMissing &&
    !asrMissing;

  const patch = (fn: (s: ParseUIState) => ParseUIState) =>
    setState((prev) => (prev ? fn({ ...prev }) : prev));

  if (!open) return null;

  const navItems: { key: SectionKey; label: string; status: string; warn?: boolean }[] = [];
  if (mode !== "reparse") {
    navItems.push({
      key: "tags",
      label: t("ps.tabTags"),
      status: tagIds.length ? t("ps.tagsCount", { count: tagIds.length }) : t("common.off"),
    });
  }
  navItems.push(
    {
      key: "parser",
      label: t("ps.tabParser"),
      status: state?.pdfForceScanned && hasPdf ? t("ps.parserForceScanned") : t("ps.parserDefault"),
    },
    {
      key: "chunking",
      label: t("ps.tabChunking"),
      status: t("ps.chunkingStatus", { size: state?.chunking.chunkSize ?? 0 }),
    },
    {
      key: "multimodal",
      label: t("ps.tabMultimodal"),
      status: mmIssue
        ? t("ps.statusNeedsSetup")
        : state?.multimodal.enabled
          ? modelName(state.multimodal.vllmModelId)
          : t("common.off"),
      warn: mmIssue,
    },
    {
      key: "asr",
      label: t("ps.tabAsr"),
      status: asrIssue
        ? t("ps.statusNeedsSetup")
        : state?.asr.enabled
          ? modelName(state.asr.modelId)
          : t("common.off"),
      warn: asrIssue,
    },
    {
      key: "summary",
      label: t("ps.tabSummary"),
      status: state?.summaryEnabled ? t("common.on") : t("common.off"),
    },
    {
      key: "question",
      label: t("ps.tabQuestion"),
      status: state?.question.enabled
        ? t("ps.questionCount", { count: state.question.questionCount })
        : t("common.off"),
    },
  );

  function modelName(id: string): string {
    if (!id) return t("ps.notSet");
    return models.find((m) => m.id === id)?.name ?? id;
  }

  const modelSelect = (
    type: "VLLM" | "ASR",
    value: string,
    error: boolean,
    onSel: (v: string) => void,
  ) => (
    <select
      className={`input w-[280px] ${error ? "border-error" : ""}`}
      value={value}
      onChange={(e) => onSel(e.target.value)}
    >
      <option value="">{t("ps.modelPlaceholder")}</option>
      {models
        .filter((m) => m.type === type)
        .map((m) => (
          <option key={m.id} value={m.id}>
            {m.name}
          </option>
        ))}
    </select>
  );

  const row = (label: string, desc: string | undefined, control: React.ReactNode) => (
    <div className="flex items-center justify-between gap-6 py-3">
      <div className="min-w-0">
        <div className="text-[13px] font-medium text-ink">{label}</div>
        {desc && <p className="caption mt-0.5 text-muted">{desc}</p>}
      </div>
      <div className="shrink-0">{control}</div>
    </div>
  );

  const confirm = () => {
    if (!state || !canConfirm) return;
    onConfirm({
      processConfig: buildProcessOverrides(state),
      tagIds,
      files: localFiles,
    });
  };

  const title = mode === "reparse" ? t("ps.titleReparse") : t("ps.title");

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-6">
      <div className="absolute inset-0 bg-ink/20" onClick={onCancel} />
      <div
        role="dialog"
        aria-label={title}
        className="card relative flex h-[600px] w-[920px] max-w-full flex-col overflow-hidden p-0"
      >
        <button
          onClick={onCancel}
          aria-label={t("common.close")}
          className="absolute right-4 top-4 z-10 flex h-8 w-8 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-strong hover:text-ink"
        >
          <IconClose className="h-4 w-4" />
        </button>

        <div className="flex min-h-0 flex-1">
          {/* left: files / source */}
          <aside className="flex w-[260px] shrink-0 flex-col border-r border-hairline bg-surface">
            <div className="border-b border-hairline px-4 py-3">
              <h2 className="title-sm text-ink">{title}</h2>
              {mode === "file" && (
                <p className="caption mt-0.5 text-muted">
                  {t("ps.fileCount", { count: localFiles.length })}
                </p>
              )}
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-2">
              {mode === "reparse" && reparse && (
                <div className="rounded-[10px] bg-surface-card p-3">
                  <p className="truncate text-[13px] font-medium text-ink" title={reparse.fileName}>
                    {reparse.fileName}
                  </p>
                  <p className="caption mt-1 text-muted">{t("ps.reparseHint")}</p>
                </div>
              )}
              {mode === "file" &&
                localFiles.map((f, i) => (
                  <div
                    key={`${f.name}-${i}`}
                    className="group flex items-center gap-2 rounded-[8px] px-2 py-1.5 hover:bg-surface-strong"
                  >
                    <IconDoc className="h-4 w-4 shrink-0 text-muted" />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[12.5px] text-ink" title={f.name}>
                        {f.name}
                      </p>
                    </div>
                    <button
                      type="button"
                      aria-label={t("common.remove")}
                      className="hidden shrink-0 text-muted-soft hover:text-error group-hover:block"
                      onClick={() => setLocalFiles((prev) => prev.filter((_, j) => j !== i))}
                    >
                      <IconClose className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
            </div>
          </aside>

          {/* middle: section nav */}
          <nav className="w-[200px] shrink-0 overflow-y-auto border-r border-hairline p-2">
            <p className="px-2 pb-2 pt-1 text-[11px] font-medium uppercase tracking-wide text-muted-soft">
              {t("ps.parseConfig")}
            </p>
            {navItems.map((item) => (
              <button
                key={item.key}
                type="button"
                onClick={() => setSection(item.key)}
                className={`flex w-full flex-col rounded-[8px] px-3 py-2 text-left transition-colors ${
                  section === item.key ? "bg-surface-strong" : "hover:bg-surface-strong/60"
                }`}
              >
                <span className="text-[13px] font-medium text-ink">{item.label}</span>
                <span
                  className={`caption mt-0.5 truncate ${
                    item.warn ? "text-error" : "text-muted-soft"
                  }`}
                >
                  {item.status}
                </span>
              </button>
            ))}
          </nav>

          {/* right: config panel */}
          <div className="min-w-0 flex-1 overflow-y-auto p-5">
            {!state ? (
              <p className="caption text-muted">{t("common.loading")}</p>
            ) : (
              <>
                {section === "tags" && (
                  <div>
                    <h3 className="title-sm text-ink">{t("ps.tabTags")}</h3>
                    <p className="caption mt-1 text-muted">{t("ps.tagsDesc")}</p>
                    <div className="mt-4 flex flex-wrap gap-2">
                      {tags === null && <p className="caption text-muted">{t("common.loading")}</p>}
                      {tags?.length === 0 && <p className="caption text-muted">{t("ps.tagsEmpty")}</p>}
                      {tags?.map((tag) => {
                        const on = tagIds.includes(tag.id);
                        return (
                          <button
                            key={tag.id}
                            type="button"
                            onClick={() =>
                              setTagIds((prev) =>
                                on ? prev.filter((x) => x !== tag.id) : [...prev, tag.id],
                              )
                            }
                            className={`rounded-full border px-3 py-1 text-[12.5px] transition-colors ${
                              on
                                ? "border-primary bg-primary/10 text-ink"
                                : "border-hairline text-muted hover:border-ink/30"
                            }`}
                          >
                            {tag.name}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                )}

                {section === "parser" && (
                  <div>
                    <h3 className="title-sm text-ink">{t("ps.tabParser")}</h3>
                    <p className="caption mt-1 text-muted">{t("ps.parserDesc")}</p>
                    <div className="mt-3">
                      <ParserRulesEditor
                        rules={state.chunking.parserEngineRules}
                        relevantExtensions={batchExts}
                        onChange={(rules) =>
                          patch((s) => ({ ...s, chunking: { ...s.chunking, parserEngineRules: rules } }))
                        }
                      />
                    </div>
                    {hasPdf && (
                      <div className="mt-4 border-t border-hairline pt-4">
                        <div className="text-[13px] font-medium text-ink">{t("ps.pdfScanLabel")}</div>
                        <p className="caption mt-0.5 text-muted">{t("ps.pdfScanDesc")}</p>
                        <div className="mt-2 flex gap-4">
                          {(["auto", "scanned"] as const).map((v) => (
                            <label key={v} className="flex items-center gap-2 text-[13px] text-ink">
                              <input
                                type="radio"
                                name="pdf-scan-mode"
                                checked={state.pdfForceScanned === (v === "scanned")}
                                onChange={() => patch((s) => ({ ...s, pdfForceScanned: v === "scanned" }))}
                              />
                              {v === "auto" ? t("ps.pdfScanAuto") : t("ps.pdfScanForce")}
                            </label>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )}

                {section === "chunking" && (
                  <div>
                    <h3 className="title-sm text-ink">{t("ps.tabChunking")}</h3>
                    {row(
                      t("ps.chunkStrategy"),
                      undefined,
                      <select
                        className="input w-[280px]"
                        value={state.chunking.strategy}
                        onChange={(e) =>
                          patch((s) => ({
                            ...s,
                            chunking: { ...s.chunking, strategy: e.target.value },
                          }))
                        }
                      >
                        {["auto", "heading", "heuristic", "legacy"].map((v) => (
                          <option key={v} value={v}>
                            {t(`ps.strategy.${v}` as LocaleKey)}
                          </option>
                        ))}
                      </select>,
                    )}
                    {row(
                      t("ps.chunkSize"),
                      undefined,
                      <input
                        className="input w-[160px]"
                        type="number"
                        min={100}
                        max={4000}
                        step={50}
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
                        className="input w-[160px]"
                        type="number"
                        min={0}
                        max={500}
                        step={20}
                        value={state.chunking.chunkOverlap}
                        onChange={(e) =>
                          patch((s) => ({
                            ...s,
                            chunking: { ...s.chunking, chunkOverlap: Number(e.target.value) },
                          }))
                        }
                      />,
                    )}
                    <button
                      type="button"
                      className="caption mt-2 text-muted hover:text-ink"
                      onClick={() => setMoreOpen((v) => !v)}
                    >
                      {moreOpen ? "▾" : "▸"} {t("ps.moreOptions")}
                    </button>
                    {moreOpen && (
                      <div className="mt-2 border-t border-hairline">
                        <div className="py-3">
                          <div className="text-[13px] font-medium text-ink">{t("ps.separators")}</div>
                          <div className="mt-2 flex flex-wrap items-center gap-1.5">
                            {state.chunking.separators.map((sep, i) => (
                              <span
                                key={i}
                                className="inline-flex items-center gap-1 rounded-full bg-surface-strong px-2 py-0.5 text-[11.5px] text-ink"
                              >
                                {sep === "\n\n" ? "\\n\\n" : sep === "\n" ? "\\n" : sep === " " ? "␣" : sep}
                                <button
                                  type="button"
                                  className="text-muted-soft hover:text-error"
                                  onClick={() =>
                                    patch((s) => ({
                                      ...s,
                                      chunking: {
                                        ...s.chunking,
                                        separators: s.chunking.separators.filter((_, j) => j !== i),
                                      },
                                    }))
                                  }
                                >
                                  <IconClose className="h-3 w-3" />
                                </button>
                              </span>
                            ))}
                            <input
                              className="input h-7 w-[140px] text-[12px]"
                              placeholder={t("ps.separatorAdd")}
                              value={sepDraft}
                              onChange={(e) => setSepDraft(e.target.value)}
                              onKeyDown={(e) => {
                                if (e.key === "Enter" && sepDraft) {
                                  patch((s) => ({
                                    ...s,
                                    chunking: {
                                      ...s.chunking,
                                      separators: [...s.chunking.separators, sepDraft],
                                    },
                                  }));
                                  setSepDraft("");
                                }
                              }}
                            />
                          </div>
                        </div>
                        {row(
                          t("ps.tokenLimit"),
                          undefined,
                          <input
                            className="input w-[160px]"
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
                        <div className="flex items-center justify-between gap-6 py-3">
                          <div className="text-[13px] font-medium text-ink">{t("ps.languages")}</div>
                          <div className="flex gap-3">
                            {["de", "en", "zh"].map((lang) => (
                              <label key={lang} className="flex items-center gap-1.5 text-[12.5px] text-ink">
                                <input
                                  type="checkbox"
                                  checked={state.chunking.languages.includes(lang)}
                                  onChange={(e) =>
                                    patch((s) => ({
                                      ...s,
                                      chunking: {
                                        ...s.chunking,
                                        languages: e.target.checked
                                          ? [...s.chunking.languages, lang]
                                          : s.chunking.languages.filter((x) => x !== lang),
                                      },
                                    }))
                                  }
                                />
                                {lang}
                              </label>
                            ))}
                          </div>
                        </div>
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
                                className="input w-[160px]"
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
                                className="input w-[160px]"
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
                    )}
                  </div>
                )}

                {section === "multimodal" && (
                  <div>
                    <h3 className="title-sm text-ink">{t("ps.tabMultimodal")}</h3>
                    <p className="caption mt-1 text-muted">{t("ps.multimodalDesc")}</p>
                    {mmIssue && (
                      <p className="caption mt-3 rounded-[8px] bg-surface-strong px-3 py-2 text-error">
                        {t("ps.multimodalSetupHint")}
                      </p>
                    )}
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
                          <select
                            className="input w-[280px]"
                            value={state.multimodal.descriptionLanguage}
                            onChange={(e) =>
                              patch((s) => ({
                                ...s,
                                multimodal: { ...s.multimodal, descriptionLanguage: e.target.value },
                              }))
                            }
                          >
                            <option value="">{t("ps.descLangAuto")}</option>
                            <option value="Chinese">中文</option>
                            <option value="English">English</option>
                            <option value="Korean">한국어</option>
                            <option value="Russian">Русский</option>
                          </select>,
                        )}
                        <div className="py-3">
                          <div className="text-[13px] font-medium text-ink">
                            {t("ps.customInstrLabel")}
                          </div>
                          <textarea
                            className="input mt-2 h-auto min-h-[72px] w-full resize-y"
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
                )}

                {section === "asr" && (
                  <div>
                    <h3 className="title-sm text-ink">{t("ps.tabAsr")}</h3>
                    <p className="caption mt-1 text-muted">{t("ps.asrDesc")}</p>
                    {asrIssue && (
                      <p className="caption mt-3 rounded-[8px] bg-surface-strong px-3 py-2 text-error">
                        {t("ps.asrSetupHint")}
                      </p>
                    )}
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
                            className="input w-[280px]"
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
                )}

                {section === "summary" && (
                  <div>
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
                  </div>
                )}

                {section === "question" && (
                  <div>
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
                          onChange={(v) =>
                            patch((s) => ({ ...s, question: { ...s.question, enabled: v } }))
                          }
                        />
                      </div>,
                    )}
                    {state.question.enabled && (
                      <div className="py-3">
                        <div className="text-[13px] font-medium text-ink">
                          {t("ps.questionInstrLabel")}
                        </div>
                        <textarea
                          className="input mt-2 h-auto min-h-[72px] w-full resize-y"
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
                  </div>
                )}
              </>
            )}
          </div>
        </div>

        <footer className="flex items-center justify-end gap-2 border-t border-hairline px-5 py-3">
          <button type="button" className="btn btn-outline btn-sm" onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={!canConfirm}
            onClick={confirm}
          >
            {mode === "reparse" ? t("ps.confirmReparse") : t("ps.confirm")}
          </button>
        </footer>
      </div>
    </div>
  );
}
