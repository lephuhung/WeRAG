/* Ported from frontend/src/views/knowledge/KnowledgeBaseEditorModal.vue
 * (settings sections) and backend internal/types/knowledgebase.go
 * field shape. Covers: basic info, indexing strategy toggles, wiki config,
 * auto-tag/profile configs, and chunking. FAQ config + vector-store binding
 * + multimodal/ASR/graph extraction stay in the Vue app until ported.
 * Data flow mirrors the Vue editor: load the full KB row (the GET returns
 * every config block), edit a local draft, then PUT through
 * updateKnowledgeBase with only the blocks the user touched.
 */
"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { Toggle } from "@/components/settings/toggle";
import { useT } from "@/lib/i18n";
import {
  getKnowledgeBase,
  updateKnowledgeBase,
  type KnowledgeBaseRow,
} from "@/lib/api/knowledge";

type IndexingStrategy = {
  vector_enabled: boolean;
  keyword_enabled: boolean;
  wiki_enabled: boolean;
  graph_enabled: boolean;
};

type KBConfigDraft = {
  indexing_strategy: IndexingStrategy;
  wiki_config: {
    synthesis_model_id: string;
    max_pages_per_ingest: number;
    extraction_granularity: "focused" | "standard" | "exhaustive";
    content_instructions: string;
    extraction_instructions: string;
  };
  auto_tag_config: {
    enabled: boolean;
    model_id: string;
    max_tags: number;
  };
  profile_config: {
    enabled: boolean;
    model_id: string;
  };
  chunking_config: {
    chunk_size: number;
    chunk_overlap: number;
    strategy: string;
  };
};

type Section = "basic" | "indexing" | "wiki" | "tags" | "chunking";

const SECTIONS: { id: Section; i18nKey: "kbSettings.basic" | "kbSettings.indexing" | "kbSettings.wiki" | "kbSettings.tags" | "kbSettings.chunking" }[] = [
  { id: "basic", i18nKey: "kbSettings.basic" },
  { id: "indexing", i18nKey: "kbSettings.indexing" },
  { id: "wiki", i18nKey: "kbSettings.wiki" },
  { id: "tags", i18nKey: "kbSettings.tags" },
  { id: "chunking", i18nKey: "kbSettings.chunking" },
];

const STRATEGY_TIERS = ["", "auto", "heading", "heuristic", "recursive"];

export function KbSettingsModal({
  kbId,
  open,
  onClose,
  onSaved,
}: {
  kbId: string;
  open: boolean;
  onClose: () => void;
  onSaved?: (row: KnowledgeBaseRow) => void;
}) {
  const { t } = useT();

  const [section, setSection] = useState<Section>("basic");
  const [kb, setKb] = useState<KnowledgeBaseRow | null>(null);
  const [draft, setDraft] = useState<KBConfigDraft | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [loading, setLoading] = useState(open);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (!open) return;
    let alive = true;
    setLoading(true);
    setError("");
    try {
      getKnowledgeBase(kbId)
        .then((row) => {
          if (!alive) return;
          if (!row) {
            setError("Knowledge base not found");
            setLoading(false);
            return;
          }
          setKb(row);
          setName(row.name);
          setDescription(row.description ?? "");
          /* Backend GET returns every config block; mirror the defaults
           * used by the Vue editor's loadKBData. */
          const chunking = (row as { chunking_config?: Record<string, unknown> }).chunking_config ?? {};
          const wiki = (row as { wiki_config?: Record<string, unknown> }).wiki_config ?? {};
          setDraft({
            indexing_strategy: {
              vector_enabled: true,
              keyword_enabled: true,
              wiki_enabled: false,
              graph_enabled: false,
              ...((row as { indexing_strategy?: IndexingStrategy }).indexing_strategy ?? {}),
            },
            wiki_config: {
              synthesis_model_id: String(wiki.synthesis_model_id ?? ""),
              max_pages_per_ingest: Number(wiki.max_pages_per_ingest ?? 0),
              extraction_granularity: (["focused", "standard", "exhaustive"].includes(String(wiki.extraction_granularity))
                ? String(wiki.extraction_granularity)
                : "standard") as "focused" | "standard" | "exhaustive",
              content_instructions: String(wiki.content_instructions ?? ""),
              extraction_instructions: String(wiki.extraction_instructions ?? ""),
            },
            auto_tag_config: {
              enabled: Boolean((row as { auto_tag_config?: { enabled?: boolean } }).auto_tag_config?.enabled),
              model_id: String((row as { auto_tag_config?: { model_id?: string } }).auto_tag_config?.model_id ?? ""),
              max_tags: Number((row as { auto_tag_config?: { max_tags?: number } }).auto_tag_config?.max_tags ?? 3),
            },
            profile_config: {
              enabled: Boolean((row as { profile_config?: { enabled?: boolean } }).profile_config?.enabled),
              model_id: String((row as { profile_config?: { model_id?: string } }).profile_config?.model_id ?? ""),
            },
            chunking_config: {
              chunk_size: Number(chunking.chunk_size ?? 512),
              chunk_overlap: Number(chunking.chunk_overlap ?? 80),
              strategy: String(chunking.strategy ?? ""),
            },
          });
          setLoading(false);
        })
        .catch((e: unknown) => {
          if (alive) {
            setError(e instanceof Error ? e.message : "Failed to load settings");
            setLoading(false);
          }
        });
    } finally {
      /* no-op: async cleanup handled above */
    }
    return () => {
      alive = false;
    };
  }, [kbId, open]);

  const patch = <K extends keyof KBConfigDraft>(key: K, value: KBConfigDraft[K]) => {
    setDraft((d) => (d ? { ...d, [key]: value } : d));
    setDirty(true);
  };

  const save = async () => {
    if (!draft) return;
    setSaving(true);
    setError("");
    try {
      await updateKnowledgeBase(kbId, {
        name: name.trim() || kb?.name || "",
        description,
        config: {
          indexing_strategy: draft.indexing_strategy,
          wiki_config: draft.wiki_config,
          auto_tag_config: draft.auto_tag_config,
          profile_config: draft.profile_config,
          chunking_config: draft.chunking_config,
        },
      });
      const fresh = await getKnowledgeBase(kbId);
      if (fresh) {
        setKb(fresh);
        onSaved?.(fresh);
      }
      setDirty(false);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open={open} title={t("kbSettings.title")} onClose={onClose} width="w-[880px]">
      {loading ? (
        <p className="caption text-muted">{t("kbSettings.loading")}</p>
      ) : draft ? (
        <div className="flex min-h-[420px] gap-6">
          {/* section nav */}
          <div className="w-[170px] shrink-0 border-r border-hairline pr-4">
            {SECTIONS.map((s) => (
              <button
                key={s.id}
                onClick={() => setSection(s.id)}
                className={`nav-item mb-0.5 ${section === s.id ? "active" : ""}`}
              >
                {t(s.i18nKey)}
              </button>
            ))}
          </div>

          {/* panel */}
          <div className="min-w-0 flex-1 overflow-y-auto pr-1">
            {error && <p className="caption mb-3 text-error">{error}</p>}

            {section === "basic" && (
              <div className="flex flex-col gap-4">
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("kbSettings.name")}</span>
                  <input className="input" value={name} onChange={(e) => { setName(e.target.value); setDirty(true); }} />
                </label>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("kbSettings.description")}</span>
                  <textarea
                    className="input h-auto min-h-[80px] resize-y"
                    value={description}
                    onChange={(e) => { setDescription(e.target.value); setDirty(true); }}
                  />
                </label>
                <div className="caption text-muted">
                  {t("kbSettings.visibilityNote")}: <span className="text-ink">{kb?.visibility ?? "tenant"}</span>
                </div>
              </div>
            )}

            {section === "indexing" && (
              <div className="flex flex-col divide-y divide-hairline">
                {(
                  [
                    ["vector_enabled", "kbSettings.idxVector"],
                    ["keyword_enabled", "kbSettings.idxKeyword"],
                    ["wiki_enabled", "kbSettings.idxWiki"],
                    ["graph_enabled", "kbSettings.idxGraph"],
                  ] as const
                ).map(([field, labelKey]) => (
                  <ToggleRow
                    key={field}
                    label={t(labelKey)}
                    checked={draft.indexing_strategy[field]}
                    onChange={(v) => patch("indexing_strategy", { ...draft.indexing_strategy, [field]: v })}
                  />
                ))}
              </div>
            )}

            {section === "wiki" && (
              <div className="flex flex-col gap-4">
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("kbSettings.wikiGranularity")}</span>
                  <select
                    className="input w-[280px]"
                    value={draft.wiki_config.extraction_granularity}
                    onChange={(e) =>
                      patch("wiki_config", { ...draft.wiki_config, extraction_granularity: e.target.value as "focused" | "standard" | "exhaustive" })
                    }
                  >
                    {(["focused", "standard", "exhaustive"] as const).map((g) => (
                      <option key={g} value={g}>
                        {g}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("kbSettings.wikiMaxPages")}</span>
                  <input
                    className="input w-[160px]"
                    type="number"
                    min={0}
                    value={draft.wiki_config.max_pages_per_ingest}
                    onChange={(e) => patch("wiki_config", { ...draft.wiki_config, max_pages_per_ingest: Number(e.target.value) })}
                  />
                </label>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("kbSettings.wikiContentInstructions")}</span>
                  <textarea
                    className="input h-auto min-h-[72px] resize-y"
                    value={draft.wiki_config.content_instructions}
                    onChange={(e) => patch("wiki_config", { ...draft.wiki_config, content_instructions: e.target.value })}
                  />
                </label>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("kbSettings.wikiExtractionInstructions")}</span>
                  <textarea
                    className="input h-auto min-h-[72px] resize-y"
                    value={draft.wiki_config.extraction_instructions}
                    onChange={(e) => patch("wiki_config", { ...draft.wiki_config, extraction_instructions: e.target.value })}
                  />
                </label>
              </div>
            )}

            {section === "tags" && (
              <div className="flex flex-col divide-y divide-hairline">
                <ToggleRow
                  label={t("kbSettings.autoTagEnabled")}
                  checked={draft.auto_tag_config.enabled}
                  onChange={(v) => patch("auto_tag_config", { ...draft.auto_tag_config, enabled: v })}
                />
                {draft.auto_tag_config.enabled && (
                  <label className="flex items-center justify-between gap-8 py-4">
                    <span className="title-sm">{t("kbSettings.autoTagMaxTags")}</span>
                    <input
                      className="input w-[120px]"
                      type="number"
                      min={1}
                      max={10}
                      value={draft.auto_tag_config.max_tags}
                      onChange={(e) => patch("auto_tag_config", { ...draft.auto_tag_config, max_tags: Number(e.target.value) })}
                    />
                  </label>
                )}
                <ToggleRow
                  label={t("kbSettings.profileEnabled")}
                  checked={draft.profile_config.enabled}
                  onChange={(v) => patch("profile_config", { ...draft.profile_config, enabled: v })}
                />
              </div>
            )}

            {section === "chunking" && (
              <div className="flex flex-col gap-4">
                <div className="flex gap-4">
                  <label className="block flex-1">
                    <span className="caption mb-1.5 block text-muted">{t("kbSettings.chunkSize")}</span>
                    <input
                      className="input"
                      type="number"
                      min={50}
                      value={draft.chunking_config.chunk_size}
                      onChange={(e) => patch("chunking_config", { ...draft.chunking_config, chunk_size: Number(e.target.value) })}
                    />
                  </label>
                  <label className="block flex-1">
                    <span className="caption mb-1.5 block text-muted">{t("kbSettings.chunkOverlap")}</span>
                    <input
                      className="input"
                      type="number"
                      min={0}
                      value={draft.chunking_config.chunk_overlap}
                      onChange={(e) => patch("chunking_config", { ...draft.chunking_config, chunk_overlap: Number(e.target.value) })}
                    />
                  </label>
                </div>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">{t("kbSettings.chunkStrategy")}</span>
                  <select
                    className="input w-[280px]"
                    value={draft.chunking_config.strategy}
                    onChange={(e) => patch("chunking_config", { ...draft.chunking_config, strategy: e.target.value })}
                  >
                    {STRATEGY_TIERS.map((s) => (
                      <option key={s} value={s}>
                        {s || t("kbSettings.chunkStrategyLegacy")}
                      </option>
                    ))}
                  </select>
                  <p className="caption mt-1 text-muted-soft">{t("kbSettings.chunkStrategyDesc")}</p>
                </label>
              </div>
            )}
          </div>
        </div>
      ) : (
        <p className="caption text-muted">{error || t("kbSettings.loading")}</p>
      )}

      {/* footer */}
      <div className="mt-5 flex items-center justify-end gap-2 border-t border-hairline pt-4">
        {error && <span className="caption mr-auto text-error">{error}</span>}
        <button className="btn btn-outline btn-sm" onClick={onClose} disabled={saving}>
          {t("common.cancel")}
        </button>
        <button
          className="btn btn-primary btn-sm"
          disabled={saving || !draft}
          onClick={() => void save()}
        >
          {saving ? "…" : t("common.save")}
        </button>
      </div>
    </Modal>
  );
}

function ToggleRow({ label, checked, onChange }: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-8 py-4">
      <span className="title-sm">{label}</span>
      <Toggle checked={checked} onChange={onChange} label={label} />
    </div>
  );
}
