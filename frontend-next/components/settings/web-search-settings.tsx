"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconSearch, IconTrash } from "@/components/icons";
import { Select } from "@/components/select";
import {
  listWebSearchProviders,
  createWebSearchProvider,
  deleteWebSearchProvider,
  testWebSearchProvider,
  type WebSearchProviderEntity,
} from "@/lib/api/web-search";
import { ApiError } from "@/lib/api-client";
import { useT } from "@/lib/i18n";
import { useConfirm } from "@/components/confirm-dialog";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { Loading } from "@/components/settings/loading";
import { DeniedView } from "@/components/settings/system-section";
import { FieldLabel } from "@/components/settings/field";
import { ROW_HOVER } from "@/components/settings/data-table";
import { ConsolePage } from "@/components/settings/console-page";
import { useToast } from "@/components/toast";

const SUPPORTED_PROVIDERS = [
  { id: "brave", name: "Brave Search" },
  { id: "tavily", name: "Tavily AI Search" },
  { id: "google", name: "Google Custom Search" },
  { id: "bing", name: "Bing Search" },
  { id: "duckduckgo", name: "DuckDuckGo" },
  { id: "searxng", name: "SearXNG (Self-hosted)" },
] as const;

export function WebSearchSettings() {
  const { t } = useT();
  const toast = useToast();
  const confirmAction = useConfirm();
  const [providers, setProviders] = useState<WebSearchProviderEntity[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [denied, setDenied] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [testError, setTestError] = useState("");
  const [formError, setFormError] = useState("");
  const [nameError, setNameError] = useState("");

  const [form, setForm] = useState<{
    name: string;
    provider: WebSearchProviderEntity["provider"];
    api_key: string;
    base_url: string;
    is_default: boolean;
  }>({
    name: "",
    provider: "tavily",
    api_key: "",
    base_url: "",
    is_default: false,
  });

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listWebSearchProviders();
      setProviders(res.data ?? []);
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setProviders([]);
      } else {
        setError(e instanceof Error ? e.message : t("ws.loadFailed"));
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const handleCreate = async () => {
    if (!form.name.trim()) {
      setNameError(t("common.required"));
      return;
    }
    setNameError("");
    setFormError("");
    setError("");
    setBusy(true);
    try {
      await createWebSearchProvider({
        name: form.name.trim(),
        provider: form.provider,
        parameters: {
          api_key: form.api_key || undefined,
          base_url: form.base_url || undefined,
        },
        is_default: form.is_default,
      });
      setModalOpen(false);
      setForm({
        name: "",
        provider: "tavily",
        api_key: "",
        base_url: "",
        is_default: false,
      });
      await load();
      toast.success(t("common.saved"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setModalOpen(false);
        return;
      }
      setFormError(e instanceof Error ? e.message : t("ws.addFailed"));
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (
      !(await confirmAction({
        title: t("common.delete"),
        message: t("ws.confirmRemove"),
        danger: true,
      }))
    )
      return;
    try {
      await deleteWebSearchProvider(id);
      setProviders((prev) => prev.filter((p) => p.id !== id));
      toast.success(t("common.saved"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setError(e instanceof Error ? e.message : t("ws.removeFailed"));
    }
  };

  const handleTest = async (id: string) => {
    setTestError("");
    try {
      await testWebSearchProvider(id);
      toast.success(t("ws.testOk"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setTestError(e instanceof Error ? e.message : t("ws.testFailed"));
    }
  };

  if (denied) {
    return <DeniedView />;
  }

  return (
    <ConsolePage size="form">
      <div>
        <SectionHeader
          title={t("ws.title")}
          subtitle={t("ws.subtitle")}
          actions={
            <button
              type="button"
              className="btn btn-primary btn-sm flex items-center gap-1.5"
              onClick={() => setModalOpen(true)}
            >
              <IconPlus className="h-4 w-4" /> {t("ws.add")}
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
        {testError && (
          <Notice tone="error" className="mt-4" title={t("ws.testStatus")}>
            {testError}
          </Notice>
        )}

        {/* List */}
        {loading ? (
          <Loading label={t("common.loading")} className="py-12" />
        ) : providers.length === 0 ? (
          <div className="card p-8 text-center text-muted">
            {t("ws.empty")}
          </div>
        ) : (
          <div className="mt-6 space-y-3">
            {providers.map((p) => (
              <div key={p.id} className={`card flex flex-col items-start gap-3 p-4 ${ROW_HOVER} sm:flex-row sm:items-center sm:justify-between`}>
                <div className="min-w-0 w-full sm:flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <IconSearch className="h-4 w-4 text-accent" />
                    <span className="min-w-0 break-words font-medium text-sm text-ink">{p.name}</span>
                    <span className="rounded bg-surface-strong px-2 py-0.5 text-xs font-mono text-muted uppercase">
                      {p.provider}
                    </span>
                    {p.is_default && (
                      <span className="rounded bg-accent-surface px-1.5 py-0.5 text-[10px] font-semibold text-accent">
                        {t("ws.default")}
                      </span>
                    )}
                  </div>
                  {p.parameters?.base_url && (
                    <p className="caption mt-1 break-all text-muted font-mono">{p.parameters.base_url}</p>
                  )}
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  {p.id && (
                    <button
                      type="button"
                      className="btn btn-outline btn-sm"
                      onClick={() => void handleTest(p.id!)}
                    >
                      {t("ws.test")}
                    </button>
                  )}
                  {p.id && (
                    <button
                      type="button"
                      className="btn btn-danger btn-sm p-1.5"
                      onClick={() => void handleDelete(p.id!)}
                      title={t("common.delete")}
                      aria-label={t("common.delete")}
                    >
                      <IconTrash className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Add Modal */}
        <Modal
          open={modalOpen}
          title={t("ws.addTitle")}
          onClose={() => setModalOpen(false)}
          width="w-[480px]"
        >
          <div className="space-y-4">
            {formError && (
              <Notice tone="error" className="mb-4">
                {formError}
              </Notice>
            )}
            <div>
              <FieldLabel htmlFor="ws-display-name" required>
                {t("ws.displayName")}
              </FieldLabel>
              <input
                id="ws-display-name"
                required
                aria-required="true"
                aria-invalid={nameError ? true : undefined}
                aria-describedby={nameError ? "ws-display-name-error" : undefined}
                className="input w-full"
                placeholder={t("ws.displayNamePh")}
                value={form.name}
                onChange={(e) => {
                  setForm({ ...form, name: e.target.value });
                  if (nameError) setNameError("");
                }}
              />
              {nameError && (
                <p id="ws-display-name-error" className="caption mt-1 text-error">
                  {nameError}
                </p>
              )}
            </div>

            <div>
              <FieldLabel htmlFor="ws-provider">{t("ws.providerEngine")}</FieldLabel>
              <Select
                id="ws-provider"
                className="w-full"
                value={form.provider}
                onChange={(v) =>
                  setForm({ ...form, provider: v as WebSearchProviderEntity["provider"] })
                }
                options={SUPPORTED_PROVIDERS.map((sp) => ({ value: sp.id, label: sp.name }))}
              />
            </div>

            <div>
              <FieldLabel htmlFor="ws-api-key">{t("ws.apiKey")}</FieldLabel>
              <input
                id="ws-api-key"
                type="password"
                className="input w-full font-mono text-[13px]"
                placeholder="tvly-... or key"
                value={form.api_key}
                onChange={(e) => setForm({ ...form, api_key: e.target.value })}
              />
            </div>

            {form.provider === "searxng" && (
              <div>
                <FieldLabel htmlFor="ws-base-url">{t("ws.baseUrl")}</FieldLabel>
                <input
                  id="ws-base-url"
                  className="input w-full font-mono text-[13px]"
                  placeholder="http://localhost:8080"
                  value={form.base_url}
                  onChange={(e) => setForm({ ...form, base_url: e.target.value })}
                />
              </div>
            )}

            <div className="flex items-center gap-2 pt-1">
              <input
                type="checkbox"
                id="default_search"
                checked={form.is_default}
                onChange={(e) => setForm({ ...form, is_default: e.target.checked })}
              />
              <label htmlFor="default_search" className="text-ink cursor-pointer select-none">
                {t("ws.setDefault")}
              </label>
            </div>

            <div className="flex justify-end gap-2 pt-3">
              <button
                type="button"
                className="btn btn-outline"
                onClick={() => setModalOpen(false)}
              >
                {t("common.cancel")}
              </button>
              <button
                type="button"
                className="btn btn-primary"
                disabled={!form.name.trim() || busy}
                onClick={() => void handleCreate()}
              >
                {busy ? t("memp.saving") : t("ws.addCta")}
              </button>
            </div>
          </div>
        </Modal>
      </div>
    </ConsolePage>
  );
}
