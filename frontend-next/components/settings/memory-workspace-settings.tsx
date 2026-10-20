"use client";

import { useCallback, useEffect, useState } from "react";
import { Toggle } from "@/components/settings/toggle";
import { IconRefresh } from "@/components/icons";
import { Select } from "@/components/select";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Loading } from "@/components/settings/loading";
import { FieldLabel } from "@/components/settings/field";
import { useToast } from "@/components/toast";
import {
  getTenantMemoryConfig,
  updateTenantMemoryConfig,
  type MemoryConfig,
} from "@/lib/api/memory";
import { listModels, type ModelConfig } from "@/lib/api/models";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";

const DEFAULT_CONFIG: MemoryConfig = {
  enabled: false,
  write_mode: "auto",
  extract_model_id: "",
  max_items: 200,
  extract_delay_seconds: 15,
  extract_min_interval_seconds: 300,
  extract_instructions: "",
  interest_threshold: 3,
  retrieval_conditioning: true,
  embedding_model_id: "",
  vector_recall: false,
};

export function MemoryWorkspaceSettings() {
  const { t } = useT();
  const auth = useAuth();
  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  /* Tenant KV config (PUT /tenants/kv/:key) is Admin+ on the backend,
   * so all Tenant Admins may edit — not just legacy owners. */
  const canEdit = currentRole === "owner" || currentRole === "admin" || isSystemAdmin;

  const [config, setConfig] = useState<MemoryConfig>(DEFAULT_CONFIG);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();

  const loadData = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [cfgRes, mdlRes] = await Promise.all([
        getTenantMemoryConfig(),
        listModels().catch(() => []),
      ]);
      if (cfgRes.success && cfgRes.data) {
        setConfig(cfgRes.data);
      }
      setModels(mdlRes);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memw.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const handleSave = async (updated?: Partial<MemoryConfig>) => {
    const payload = { ...config, ...(updated || {}) };
    setSaving(true);
    setError("");
    try {
      const res = await updateTenantMemoryConfig(payload);
      if (res.success && res.data) {
        setConfig(res.data);
        toast.success(t("memw.saved"));
      } else {
        setError(t("memw.saveFailed"));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memw.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const chatModels = models.filter((m) => m.type === "KnowledgeQA");
  const embeddingModels = models.filter((m) => m.type === "Embedding");

  return (
    <div className="space-y-8">
      {/* Header */}
      <SectionHeader
        title={t("memw.title")}
        subtitle={t("memw.subtitle")}
        actions={
          <>
            {canEdit && (
              <button
                type="button"
                onClick={() => void handleSave()}
                disabled={saving}
                className="btn btn-primary btn-sm"
              >
                {saving ? t("memp.saving") : t("memp.saveChanges")}
              </button>
            )}
            <button
              type="button"
              onClick={() => void loadData()}
              className="btn btn-outline btn-sm p-1.5"
              title={t("common.refresh")}
              aria-label={t("common.refresh")}
            >
              <IconRefresh className="h-3.5 w-3.5" />
            </button>
          </>
        }
      />

      {loading && <Loading />}

      {error && (
        <Notice
          tone="error"
          action={
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() => void loadData()}
            >
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      {/* Main Switch */}
      <div className="flex items-center justify-between rounded-xl border border-hairline bg-surface-card p-5">
        <div>
          <span className="text-sm font-semibold text-ink block">{t("memw.enable")}</span>
          <span className="caption text-muted block mt-0.5">
            {t("memw.enableDesc")}
          </span>
        </div>
        <Toggle
          label={t("memw.enable")}
          checked={config.enabled}
          disabled={!canEdit}
          onChange={(checked) => {
            setConfig({ ...config, enabled: checked });
            void handleSave({ enabled: checked });
          }}
        />
      </div>

      {config.enabled && (
        <div className="space-y-6 pt-2">
          {/* Write mode */}
          <div className="rounded-xl border border-hairline p-5 space-y-3">
            <div className="flex items-center justify-between">
              <div>
                <span className="text-sm font-medium text-ink block">{t("memw.mode")}</span>
                <span className="caption text-muted block mt-0.5">
                  {t("memw.modeDesc")}
                </span>
              </div>
              <PillGroup ariaLabel={t("memw.mode")}>
                <Pill
                  active={config.write_mode === "auto"}
                  disabled={!canEdit}
                  onClick={() => setConfig({ ...config, write_mode: "auto" })}
                >
                  {t("memw.modeAuto")}
                </Pill>
                <Pill
                  active={config.write_mode === "explicit_only"}
                  disabled={!canEdit}
                  onClick={() => setConfig({ ...config, write_mode: "explicit_only" })}
                >
                  {t("memw.modeExplicit")}
                </Pill>
              </PillGroup>
            </div>
          </div>

          {/* Model selection */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="rounded-xl border border-hairline p-5 space-y-2">
              <FieldLabel htmlFor="memw-extract-model" className="text-sm font-medium text-ink">
                {t("memw.extractModel")}
              </FieldLabel>
              <span className="caption text-muted block mt-0.5 mb-2">
                {t("memw.extractModelDesc")}
              </span>
              <Select
                id="memw-extract-model"
                disabled={!canEdit}
                className="text-sm"
                value={config.extract_model_id}
                onChange={(v) => setConfig({ ...config, extract_model_id: v })}
                options={[
                  { value: "", label: t("memw.defaultModel") },
                  ...chatModels.map((m) => ({
                    value: m.id ?? "",
                    label: `${m.display_name || m.name} (${m.parameters?.provider || "remote"})`,
                  })),
                ]}
              />
            </div>

            <div className="rounded-xl border border-hairline p-5 space-y-2">
              <FieldLabel htmlFor="memw-embedding-model" className="text-sm font-medium text-ink">
                {t("memw.embeddingModel")}
              </FieldLabel>
              <span className="caption text-muted block mt-0.5 mb-2">
                {t("memw.embeddingModelDesc")}
              </span>
              <Select
                id="memw-embedding-model"
                disabled={!canEdit || !config.vector_recall}
                className="text-sm"
                value={config.embedding_model_id}
                onChange={(v) => setConfig({ ...config, embedding_model_id: v })}
                options={[
                  { value: "", label: t("memw.embeddingNone") },
                  ...embeddingModels.map((m) => ({
                    value: m.id ?? "",
                    label: m.display_name || m.name,
                  })),
                ]}
              />
            </div>
          </div>

          {/* Vector Recall and Retrieval Conditioning */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="flex items-center justify-between rounded-xl border border-hairline p-5">
              <div>
                <span className="text-sm font-medium text-ink block">{t("memw.vectorRecall")}</span>
                <span className="caption text-muted block mt-0.5">
                  {t("memw.vectorRecallDesc")}
                </span>
              </div>
              <Toggle
                label={t("memw.vectorRecall")}
                checked={config.vector_recall}
                disabled={!canEdit}
                onChange={(checked) => setConfig({ ...config, vector_recall: checked })}
              />
            </div>

            <div className="flex items-center justify-between rounded-xl border border-hairline p-5">
              <div>
                <span className="text-sm font-medium text-ink block">{t("memw.retrieval")}</span>
                <span className="caption text-muted block mt-0.5">
                  {t("memw.retrievalDesc")}
                </span>
              </div>
              <Toggle
                label={t("memw.retrieval")}
                checked={config.retrieval_conditioning}
                disabled={!canEdit}
                onChange={(checked) => setConfig({ ...config, retrieval_conditioning: checked })}
              />
            </div>
          </div>

          {/* Timing and Limits */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="rounded-xl border border-hairline p-5 space-y-1.5">
              <FieldLabel htmlFor="memw-delay" className="text-sm font-medium text-ink">
                {t("memw.delay")}
              </FieldLabel>
              <span className="caption text-muted block text-xs">
                {t("memw.delayDesc")}
              </span>
              <input
                id="memw-delay"
                type="number"
                min={5}
                max={3600}
                disabled={!canEdit}
                className="input mt-2"
                value={config.extract_delay_seconds}
                onChange={(e) =>
                  setConfig({ ...config, extract_delay_seconds: Number(e.target.value) })
                }
              />
            </div>

            <div className="rounded-xl border border-hairline p-5 space-y-1.5">
              <FieldLabel htmlFor="memw-min-interval" className="text-sm font-medium text-ink">
                {t("memw.minInterval")}
              </FieldLabel>
              <span className="caption text-muted block text-xs">
                {t("memw.minIntervalDesc")}
              </span>
              <input
                id="memw-min-interval"
                type="number"
                min={0}
                max={86400}
                disabled={!canEdit}
                className="input mt-2"
                value={config.extract_min_interval_seconds}
                onChange={(e) =>
                  setConfig({ ...config, extract_min_interval_seconds: Number(e.target.value) })
                }
              />
            </div>

            <div className="rounded-xl border border-hairline p-5 space-y-1.5">
              <FieldLabel htmlFor="memw-max-items" className="text-sm font-medium text-ink">
                {t("memw.maxItems")}
              </FieldLabel>
              <span className="caption text-muted block text-xs">
                {t("memw.maxItemsDesc")}
              </span>
              <input
                id="memw-max-items"
                type="number"
                min={10}
                max={2000}
                disabled={!canEdit}
                className="input mt-2"
                value={config.max_items}
                onChange={(e) => setConfig({ ...config, max_items: Number(e.target.value) })}
              />
            </div>
          </div>

          {/* Custom instructions */}
          <div className="rounded-xl border border-hairline p-5 space-y-2">
            <div>
              <FieldLabel htmlFor="memw-guidelines" className="text-sm font-medium text-ink">
                {t("memw.guidelines")}
              </FieldLabel>
              <span className="caption text-muted block text-xs mb-2">
                {t("memw.guidelinesDesc")}
              </span>
              <textarea
                id="memw-guidelines"
                rows={3}
                disabled={!canEdit}
                placeholder={t("memw.guidelinesPh")}
                className="input resize-none text-sm font-mono"
                value={config.extract_instructions}
                onChange={(e) => setConfig({ ...config, extract_instructions: e.target.value })}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
