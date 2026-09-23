"use client";

import { useEffect, useState } from "react";
import {
  listStorageBackends,
  listStorageBackendTypes,
  createStorageBackend,
  deleteStorageBackend,
  setDefaultStorageBackend,
  type StorageBackend,
} from "@/lib/api/storage-backends";
import { IconDoc, IconPlus, IconTrash } from "@/components/icons";
import { Select } from "@/components/select";

export function StorageSettings() {
  const [backends, setBackends] = useState<StorageBackend[]>([]);
  const [defaultId, setDefaultId] = useState<string | null>(null);
  const [types, setTypes] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formData, setFormData] = useState({
    name: "",
    provider: "local",
    endpoint: "",
    bucket_name: "",
    region: "",
    access_key_id: "",
    secret_access_key: "",
  });

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const [bRes, tRes] = await Promise.allSettled([
        listStorageBackends(),
        listStorageBackendTypes(),
      ]);

      if (bRes.status === "fulfilled" && bRes.value) {
        setBackends(bRes.value.data ?? []);
        setDefaultId(bRes.value.default_storage_backend_id ?? null);
      }
      if (tRes.status === "fulfilled" && tRes.value?.data) {
        setTypes(tRes.value.data);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load storage backends");
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
      setDefaultId(id);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to set default storage backend");
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Are you sure you want to delete this storage backend?")) return;
    try {
      await deleteStorageBackend(id);
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete storage backend");
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.name) return;
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
      setFormData({
        name: "",
        provider: "local",
        endpoint: "",
        bucket_name: "",
        region: "",
        access_key_id: "",
        secret_access_key: "",
      });
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create storage backend");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between border-b border-hairline pb-4">
        <div>
          <h2 className="title-md">Storage Engine Settings</h2>
          <p className="caption mt-1 text-muted">
            Configure local or object storage backends for document parsing, artifacts, and knowledge storage.
          </p>
        </div>
        <button
          onClick={() => setModalOpen(true)}
          className="btn btn-primary btn-sm flex items-center gap-1.5"
        >
          <IconPlus className="h-4 w-4" /> Add Storage Backend
        </button>
      </div>

      {error && (
        <div className="mt-4 rounded-xl bg-error/10 p-3 text-[13px] text-error">
          {error}
        </div>
      )}

      {loading ? (
        <div className="py-12 text-center text-muted">Loading storage backends…</div>
      ) : backends.length === 0 ? (
        <div className="py-12 text-center text-muted">
          <IconDoc className="mx-auto mb-2 h-8 w-8 text-muted-soft" />
          <p className="text-[14px]">No storage backends configured.</p>
        </div>
      ) : (
        <div className="mt-6 space-y-3">
          {backends.map((b) => {
            const isDefault = defaultId === b.id;
            return (
              <div
                key={b.id}
                className="flex items-center justify-between rounded-xl border border-hairline bg-surface p-4 transition-colors hover:border-hairline-strong"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-[15px] font-medium text-ink">{b.name}</span>
                    <span className="badge-pill uppercase text-[10px]">{b.provider}</span>
                    {isDefault && (
                      <span className="badge-pill bg-success/15 text-success text-[10px]">
                        Default
                      </span>
                    )}
                  </div>
                  <p className="caption mt-1 text-muted">
                    Source: {b.source} · Status: {b.status}
                    {b.config.bucket_name ? ` · Bucket: ${b.config.bucket_name}` : ""}
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  {!isDefault && (
                    <button
                      onClick={() => handleSetDefault(b.id)}
                      className="btn btn-outline btn-sm"
                    >
                      Set Default
                    </button>
                  )}
                  {b.source !== "env" && (
                    <button
                      onClick={() => handleDelete(b.id)}
                      className="rounded-lg p-1.5 text-muted hover:bg-surface-strong hover:text-error"
                      title="Delete"
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
      {modalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm">
          <div className="w-full max-w-[500px] rounded-2xl border border-hairline bg-surface-card p-6 shadow-2xl">
            <h3 className="title-sm mb-4">Add Storage Backend</h3>
            <form onSubmit={handleSubmit} className="space-y-4">
              <div>
                <label className="caption mb-1 block font-medium text-ink">Name</label>
                <input
                  required
                  className="input h-9 w-full text-[13px]"
                  placeholder="e.g. MinIO S3 Store"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                />
              </div>

              <div>
                <label className="caption mb-1 block font-medium text-ink">Provider Type</label>
                <Select
                  className="h-9 w-full text-[13px]"
                  value={formData.provider}
                  onChange={(v) => setFormData({ ...formData, provider: v })}
                  options={[
                    { value: "local", label: "Local Filesystem" },
                    { value: "s3", label: "AWS S3 / S3 Compatible" },
                    { value: "minio", label: "MinIO" },
                    { value: "oss", label: "Aliyun OSS" },
                    { value: "cos", label: "Tencent COS" },
                    ...types
                      .filter((t) => !["local", "s3", "minio", "oss", "cos"].includes(t))
                      .map((t) => ({ value: t, label: t.toUpperCase() })),
                  ]}
                />
              </div>

              {formData.provider !== "local" && (
                <>
                  <div>
                    <label className="caption mb-1 block font-medium text-ink">Endpoint URL</label>
                    <input
                      className="input h-9 w-full text-[13px]"
                      placeholder="https://s3.amazonaws.com or http://minio:9000"
                      value={formData.endpoint}
                      onChange={(e) => setFormData({ ...formData, endpoint: e.target.value })}
                    />
                  </div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div>
                      <label className="caption mb-1 block font-medium text-ink">Bucket Name</label>
                      <input
                        className="input h-9 w-full text-[13px]"
                        placeholder="my-bucket"
                        value={formData.bucket_name}
                        onChange={(e) => setFormData({ ...formData, bucket_name: e.target.value })}
                      />
                    </div>
                    <div>
                      <label className="caption mb-1 block font-medium text-ink">Region</label>
                      <input
                        className="input h-9 w-full text-[13px]"
                        placeholder="us-east-1"
                        value={formData.region}
                        onChange={(e) => setFormData({ ...formData, region: e.target.value })}
                      />
                    </div>
                  </div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div>
                      <label className="caption mb-1 block font-medium text-ink">Access Key ID</label>
                      <input
                        className="input h-9 w-full text-[13px]"
                        type="password"
                        value={formData.access_key_id}
                        onChange={(e) => setFormData({ ...formData, access_key_id: e.target.value })}
                      />
                    </div>
                    <div>
                      <label className="caption mb-1 block font-medium text-ink">Secret Access Key</label>
                      <input
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
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={submitting}
                  className="btn btn-primary btn-sm"
                >
                  {submitting ? "Saving…" : "Save"}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
