"use client";

import { useEffect, useState } from "react";
import {
  listStorageBackends,
  createStorageBackend,
  deleteStorageBackend,
  setDefaultStorageBackend,
  type StorageBackend,
} from "@/lib/api/storage-backends";
import { ApiError } from "@/lib/api-client";
import { IconDoc, IconPlus, IconTrash } from "@/components/icons";
import { Select } from "@/components/select";
import { useT } from "@/lib/i18n";
import { useConfirm } from "@/components/confirm-dialog";
import { Modal } from "@/components/modal";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { Loading } from "@/components/settings/loading";
import { DeniedView } from "@/components/settings/system-section";
import { FieldLabel } from "@/components/settings/field";
import { ROW_HOVER } from "@/components/settings/data-table";
import { ConsolePage } from "@/components/settings/console-page";
import { useToast } from "@/components/toast";

const EMPTY_FORM = {
  name: "",
  provider: "local",
  endpoint: "",
  bucket_name: "",
  region: "",
  access_key_id: "",
  secret_access_key: "",
};

export function StorageSettings() {
  const { t } = useT();
  const toast = useToast();
  const confirmAction = useConfirm();
  const [backends, setBackends] = useState<StorageBackend[]>([]);
  const [defaultId, setDefaultId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [denied, setDenied] = useState(false);

  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formData, setFormData] = useState(EMPTY_FORM);
  const [nameError, setNameError] = useState("");
  const [formError, setFormError] = useState("");

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listStorageBackends();
      setBackends(res?.data ?? []);
      setDefaultId(res?.default_storage_backend_id ?? null);
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setBackends([]);
      } else {
        setError(e instanceof Error ? e.message : t("stor.loadFailed"));
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  const handleSetDefault = async (id: string) => {
    try {
      await setDefaultStorageBackend(id);
      /* Re-read: the list marks the effective default (a workspace on its
       * legacy env alias follows the configuration profile's default). */
      await loadData();
      toast.success(t("common.saved"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setError(e instanceof Error ? e.message : t("stor.setDefaultFailed"));
    }
  };

  const handleDelete = async (id: string) => {
    if (
      !(await confirmAction({
        title: t("common.delete"),
        message: t("stor.confirmDelete"),
        danger: true,
      }))
    )
      return;
    try {
      await deleteStorageBackend(id);
      await loadData();
      toast.success(t("common.saved"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setError(e instanceof Error ? e.message : t("stor.deleteFailed"));
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.name.trim()) {
      setNameError(t("common.required"));
      return;
    }
    setNameError("");
    setFormError("");
    setSubmitting(true);
    try {
      await createStorageBackend({
        name: formData.name,
        provider: formData.provider,
        config: {
          endpoint: formData.endpoint || undefined,
          bucket_name: formData.bucket_name || undefined,
          region: formData.region || undefined,
          access_key_id: formData.access_key_id || undefined,
          secret_access_key: formData.secret_access_key || undefined,
        },
      });
      setModalOpen(false);
      setFormData({ ...EMPTY_FORM });
      await loadData();
      toast.success(t("common.saved"));
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setDenied(true);
        setModalOpen(false);
        return;
      }
      setFormError(err instanceof Error ? err.message : t("stor.createFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  if (denied) {
    return <DeniedView />;
  }

  return (
    <ConsolePage size="form">
      <div>
        <SectionHeader
          title={t("stor.title")}
          subtitle={t("stor.subtitle")}
          actions={
            <button
              type="button"
              onClick={() => setModalOpen(true)}
              className="btn btn-primary btn-sm flex shrink-0 items-center gap-1.5"
            >
              <IconPlus className="h-4 w-4" /> {t("stor.add")}
            </button>
          }
        />

        {error && (
          <Notice
            tone="error"
            className="mt-4"
            action={
              <button type="button" className="btn btn-outline btn-sm" onClick={() => void loadData()}>
                {t("common.retry")}
              </button>
            }
          >
            {error}
          </Notice>
        )}

        {loading ? (
          <Loading label={t("stor.loading")} className="py-12" />
        ) : backends.length === 0 ? (
          <div className="py-12 text-center text-muted">
            <IconDoc className="mx-auto mb-2 h-8 w-8 text-muted-soft" />
            <p className="text-[14px]">{t("stor.empty")}</p>
          </div>
        ) : (
          <div className="mt-6 space-y-3">
            {backends.map((b) => {
              const isDefault = defaultId === b.id;
              return (
                <div
                  key={b.id}
                  className={`flex items-center justify-between rounded-xl border border-hairline bg-surface-card p-4 ${ROW_HOVER}`}
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-[15px] font-medium text-ink">{b.name}</span>
                      <span className="badge-pill uppercase text-[10px]">{b.provider}</span>
                      {isDefault && (
                        <span className="badge-pill badge-success uppercase text-[10px]">
                          {t("stor.default")}
                        </span>
                      )}
                    </div>
                    <p className="caption mt-1 text-muted">
                      {t("stor.source")}: {b.source} · {t("stor.status")}: {b.status}
                      {b.config.bucket_name ? ` · ${t("stor.bucket")}: ${b.config.bucket_name}` : ""}
                    </p>
                  </div>

                  <div className="flex items-center gap-2">
                    {/* Legacy env aliases cannot be the profile default. */}
                    {!isDefault && !b.legacy_alias && (
                      <button
                        type="button"
                        onClick={() => void handleSetDefault(b.id)}
                        className="btn btn-outline btn-sm"
                      >
                        {t("stor.setDefault")}
                      </button>
                    )}
                    {b.source !== "env" && (
                      <button
                        type="button"
                        onClick={() => void handleDelete(b.id)}
                        className="btn btn-danger btn-sm p-1.5"
                        title={t("common.delete")}
                        aria-label={t("common.delete")}
                      >
                        <IconTrash className="h-4 w-4" />
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {/* Add Storage Modal */}
        <Modal open={modalOpen} title={t("stor.addTitle")} onClose={() => setModalOpen(false)} width="w-[500px]">
          <form onSubmit={handleSubmit} className="space-y-4">
            {formError && (
              <Notice tone="error" className="mb-4">
                {formError}
              </Notice>
            )}
            <div>
              <FieldLabel htmlFor="storage-name" required>
                {t("stor.name")}
              </FieldLabel>
              <input
                id="storage-name"
                required
                aria-required="true"
                aria-invalid={nameError ? true : undefined}
                aria-describedby={nameError ? "storage-name-error" : undefined}
                className="input h-9 w-full text-[13px]"
                placeholder={t("stor.namePh")}
                value={formData.name}
                onChange={(e) => {
                  setFormData({ ...formData, name: e.target.value });
                  if (nameError) setNameError("");
                }}
              />
              {nameError && (
                <p id="storage-name-error" className="caption mt-1 text-error">
                  {nameError}
                </p>
              )}
            </div>

            <div>
              <FieldLabel htmlFor="storage-provider">{t("stor.provider")}</FieldLabel>
              <Select
                id="storage-provider"
                className="h-9 w-full text-[13px]"
                value={formData.provider}
                onChange={(v) => setFormData({ ...formData, provider: v })}
                options={[
                  { value: "local", label: t("stor.pLocal") },
                  { value: "minio", label: "MinIO" },
                ]}
              />
            </div>

            {formData.provider !== "local" && (
              <>
                <div>
                  <FieldLabel htmlFor="storage-endpoint">{t("stor.endpoint")}</FieldLabel>
                  <input
                    id="storage-endpoint"
                    className="input h-9 w-full text-[13px]"
                    placeholder="http://minio:9000"
                    value={formData.endpoint}
                    onChange={(e) => setFormData({ ...formData, endpoint: e.target.value })}
                  />
                </div>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div>
                    <FieldLabel htmlFor="storage-bucket">{t("stor.bucketName")}</FieldLabel>
                    <input
                      id="storage-bucket"
                      className="input h-9 w-full text-[13px]"
                      placeholder="my-bucket"
                      value={formData.bucket_name}
                      onChange={(e) => setFormData({ ...formData, bucket_name: e.target.value })}
                    />
                  </div>
                  <div>
                    <FieldLabel htmlFor="storage-region">{t("stor.region")}</FieldLabel>
                    <input
                      id="storage-region"
                      className="input h-9 w-full text-[13px]"
                      placeholder="us-east-1"
                      value={formData.region}
                      onChange={(e) => setFormData({ ...formData, region: e.target.value })}
                    />
                  </div>
                </div>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div>
                    <FieldLabel htmlFor="storage-access-key">{t("stor.accessKey")}</FieldLabel>
                    <input
                      id="storage-access-key"
                      className="input h-9 w-full text-[13px]"
                      type="password"
                      value={formData.access_key_id}
                      onChange={(e) => setFormData({ ...formData, access_key_id: e.target.value })}
                    />
                  </div>
                  <div>
                    <FieldLabel htmlFor="storage-secret-key">{t("stor.secretKey")}</FieldLabel>
                    <input
                      id="storage-secret-key"
                      className="input h-9 w-full text-[13px]"
                      type="password"
                      value={formData.secret_access_key}
                      onChange={(e) => setFormData({ ...formData, secret_access_key: e.target.value })}
                    />
                  </div>
                </div>
              </>
            )}

            <div className="mt-6 flex justify-end gap-2 border-t border-hairline pt-4">
              <button
                type="button"
                onClick={() => setModalOpen(false)}
                className="btn btn-outline btn-sm"
              >
                {t("common.cancel")}
              </button>
              <button
                type="submit"
                disabled={submitting}
                className="btn btn-primary btn-sm"
              >
                {submitting ? t("memp.saving") : t("common.save")}
              </button>
            </div>
          </form>
        </Modal>
      </div>
    </ConsolePage>
  );
}
