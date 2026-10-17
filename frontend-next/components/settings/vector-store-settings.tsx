"use client";

import { useEffect, useState } from "react";
import {
  listVectorStores,
  deleteVectorStore,
  testVectorStoreById,
  type VectorStoreEntity,
} from "@/lib/api/vector-stores";
import { IconBook, IconPulse, IconTrash } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { useConfirm } from "@/components/confirm-dialog";

export function VectorStoreSettings() {
  const { t } = useT();
  const confirmAction = useConfirm();
  const [stores, setStores] = useState<VectorStoreEntity[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [testingId, setTestingId] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, { ok: boolean; msg: string }>>({});

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listVectorStores();
      if (res?.data) setStores(res.data);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("vstore.loadFailed"));
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
      setTestResults((prev) => ({ ...prev, [id]: { ok: true, msg: t("vstore.testOk") } }));
    } catch (err) {
      setTestResults((prev) => ({
        ...prev,
        [id]: { ok: false, msg: err instanceof Error ? err.message : t("vstore.testFail") },
      }));
    } finally {
      setTestingId(null);
    }
  };

  const handleDelete = async (id: string) => {
    if (
      !(await confirmAction({
        title: t("common.delete"),
        message: t("vstore.confirmDelete"),
        danger: true,
      }))
    )
      return;
    try {
      await deleteVectorStore(id);
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("vstore.deleteFailed"));
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between border-b border-hairline pb-4">
        <div>
          <h2 className="title-md">{t("vstore.title")}</h2>
          <p className="caption mt-1 text-muted">
            {t("vstore.subtitle")}
          </p>
        </div>
      </div>

      {error && (
        <div className="mt-4 rounded-xl bg-error/10 p-3 text-[13px] text-error">
          {error}
        </div>
      )}

      {loading ? (
        <div className="py-12 text-center text-muted">{t("vstore.loading")}</div>
      ) : stores.length === 0 ? (
        <div className="py-12 text-center text-muted">
          <IconBook className="mx-auto mb-2 h-8 w-8 text-muted-soft" />
          <p className="text-[14px]">{t("vstore.empty")}</p>
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
                        {testingId === s.id ? t("vstore.testing") : t("vstore.test")}
                      </button>
                    )}
                    {!s.readonly && s.id && (
                      <button
                        onClick={() => handleDelete(s.id!)}
                        className="rounded-lg p-1.5 text-muted hover:bg-surface-strong hover:text-error"
                        title={t("common.delete")}
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

    </div>
  );
}
