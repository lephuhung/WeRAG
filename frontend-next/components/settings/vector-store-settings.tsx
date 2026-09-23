"use client";

import { useEffect, useState } from "react";
import {
  listVectorStores,
  listVectorStoreTypes,
  createVectorStore,
  deleteVectorStore,
  testVectorStoreById,
  type VectorStoreEntity,
  type VectorStoreTypeInfo,
} from "@/lib/api/vector-stores";
import { IconBook, IconPlus, IconPulse, IconTrash } from "@/components/icons";
import { Select } from "@/components/select";

export function VectorStoreSettings() {
  const [stores, setStores] = useState<VectorStoreEntity[]>([]);
  const [types, setTypes] = useState<VectorStoreTypeInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [testingId, setTestingId] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, { ok: boolean; msg: string }>>({});

  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formData, setFormData] = useState({
    name: "",
    engine_type: "pgvector",
    host: "",
    port: "",
    database: "",
    user: "",
    password: "",
  });

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const [sRes, tRes] = await Promise.allSettled([
        listVectorStores(),
        listVectorStoreTypes(),
      ]);

      if (sRes.status === "fulfilled" && sRes.value?.data) {
        setStores(sRes.value.data);
      }
      if (tRes.status === "fulfilled" && tRes.value) {
        setTypes(tRes.value);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load vector stores");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  const handleTest = async (id: string) => {
    setTestingId(id);
    try {
      await testVectorStoreById(id);
      setTestResults((prev) => ({ ...prev, [id]: { ok: true, msg: "Connection successful" } }));
    } catch (err) {
      setTestResults((prev) => ({
        ...prev,
        [id]: { ok: false, msg: err instanceof Error ? err.message : "Connection failed" },
      }));
    } finally {
      setTestingId(null);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Are you sure you want to delete this vector store?")) return;
    try {
      await deleteVectorStore(id);
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete vector store");
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.name) return;
    setSubmitting(true);
    try {
      await createVectorStore({
        name: formData.name,
        engine_type: formData.engine_type,
        connection_config: {
          host: formData.host || undefined,
          port: formData.port ? Number(formData.port) : undefined,
          database: formData.database || undefined,
          user: formData.user || undefined,
          password: formData.password || undefined,
        },
        index_config: {},
      });
      setModalOpen(false);
      setFormData({
        name: "",
        engine_type: "pgvector",
        host: "",
        port: "",
        database: "",
        user: "",
        password: "",
      });
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create vector store");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between border-b border-hairline pb-4">
        <div>
          <h2 className="title-md">Vector Store Engines</h2>
          <p className="caption mt-1 text-muted">
            Configure vector databases used for dense retrieval, embedding indexes, and semantic search.
          </p>
        </div>
        <button
          onClick={() => setModalOpen(true)}
          className="btn btn-primary btn-sm flex items-center gap-1.5"
        >
          <IconPlus className="h-4 w-4" /> Add Vector Store
        </button>
      </div>

      {error && (
        <div className="mt-4 rounded-xl bg-error/10 p-3 text-[13px] text-error">
          {error}
        </div>
      )}

      {loading ? (
        <div className="py-12 text-center text-muted">Loading vector stores…</div>
      ) : stores.length === 0 ? (
        <div className="py-12 text-center text-muted">
          <IconBook className="mx-auto mb-2 h-8 w-8 text-muted-soft" />
          <p className="text-[14px]">No vector stores configured.</p>
        </div>
      ) : (
        <div className="mt-6 space-y-3">
          {stores.map((s) => {
            const sid = s.id || s.name;
            const res = testResults[sid];
            return (
              <div
                key={sid}
                className="flex flex-col gap-2 rounded-xl border border-hairline bg-surface p-4 transition-colors hover:border-hairline-strong"
              >
                <div className="flex items-center justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-[15px] font-medium text-ink">{s.name}</span>
                      <span className="badge-pill uppercase text-[10px]">{s.engine_type}</span>
                      {s.source && (
                        <span className="badge-pill text-[10px] text-muted">
                          {s.source}
                        </span>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    {s.id && (
                      <button
                        onClick={() => handleTest(s.id!)}
                        disabled={testingId === s.id}
                        className="btn btn-outline btn-sm flex items-center gap-1"
                      >
                        <IconPulse className={`h-3.5 w-3.5 ${testingId === s.id ? "animate-spin" : ""}`} />
                        {testingId === s.id ? "Testing…" : "Test"}
                      </button>
                    )}
                    {!s.readonly && s.id && (
                      <button
                        onClick={() => handleDelete(s.id!)}
                        className="rounded-lg p-1.5 text-muted hover:bg-surface-strong hover:text-error"
                        title="Delete"
                      >
                        <IconTrash className="h-4 w-4" />
                      </button>
                    )}
                  </div>
                </div>

                {res && (
                  <div
                    className={`rounded-lg px-3 py-1.5 text-[12px] font-medium ${
                      res.ok ? "bg-success/15 text-success" : "bg-error/15 text-error"
                    }`}
                  >
                    {res.msg}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* Add Vector Store Modal */}
      {modalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm">
          <div className="w-full max-w-[500px] rounded-2xl border border-hairline bg-surface-card p-6 shadow-2xl">
            <h3 className="title-sm mb-4">Add Vector Store Engine</h3>
            <form onSubmit={handleSubmit} className="space-y-4">
              <div>
                <label className="caption mb-1 block font-medium text-ink">Name</label>
                <input
                  required
                  className="input h-9 w-full text-[13px]"
                  placeholder="e.g. Production pgvector"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                />
              </div>

              <div>
                <label className="caption mb-1 block font-medium text-ink">Engine Type</label>
                <Select
                  className="h-9 w-full text-[13px]"
                  value={formData.engine_type}
                  onChange={(v) => setFormData({ ...formData, engine_type: v })}
                  options={[
                    { value: "pgvector", label: "PGVector (PostgreSQL)" },
                    { value: "milvus", label: "Milvus" },
                    { value: "qdrant", label: "Qdrant" },
                    { value: "chroma", label: "Chroma" },
                    { value: "elasticsearch", label: "Elasticsearch" },
                    { value: "opensearch", label: "OpenSearch" },
                    ...types
                      .filter(
                        (t) =>
                          !["pgvector", "milvus", "qdrant", "chroma", "elasticsearch", "opensearch"].includes(t.type),
                      )
                      .map((t) => ({ value: t.type, label: t.display_name || t.type })),
                  ]}
                />
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="sm:col-span-2">
                  <label className="caption mb-1 block font-medium text-ink">Host</label>
                  <input
                    className="input h-9 w-full text-[13px]"
                    placeholder="localhost or 127.0.0.1"
                    value={formData.host}
                    onChange={(e) => setFormData({ ...formData, host: e.target.value })}
                  />
                </div>
                <div>
                  <label className="caption mb-1 block font-medium text-ink">Port</label>
                  <input
                    className="input h-9 w-full text-[13px]"
                    placeholder="5432"
                    value={formData.port}
                    onChange={(e) => setFormData({ ...formData, port: e.target.value })}
                  />
                </div>
              </div>

              <div>
                <label className="caption mb-1 block font-medium text-ink">Database Name</label>
                <input
                  className="input h-9 w-full text-[13px]"
                  placeholder="werag_vectors"
                  value={formData.database}
                  onChange={(e) => setFormData({ ...formData, database: e.target.value })}
                />
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label className="caption mb-1 block font-medium text-ink">User</label>
                  <input
                    className="input h-9 w-full text-[13px]"
                    placeholder="postgres"
                    value={formData.user}
                    onChange={(e) => setFormData({ ...formData, user: e.target.value })}
                  />
                </div>
                <div>
                  <label className="caption mb-1 block font-medium text-ink">Password</label>
                  <input
                    className="input h-9 w-full text-[13px]"
                    type="password"
                    value={formData.password}
                    onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                  />
                </div>
              </div>

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
