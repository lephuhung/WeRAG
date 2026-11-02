"use client";

import { useEffect, useState } from "react";
import {
  listVectorStores,
  deleteVectorStore,
  testVectorStoreById,
  type VectorStoreEntity,
} from "@/lib/api/vector-stores";
import { ApiError } from "@/lib/api-client";
import { IconBook, IconPulse, IconTrash } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { useConfirm } from "@/components/confirm-dialog";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { Loading } from "@/components/settings/loading";
import { DeniedView } from "@/components/settings/system-section";
import { ROW_HOVER } from "@/components/settings/data-table";
import { ConsolePage } from "@/components/settings/console-page";
import { useToast } from "@/components/toast";

export function VectorStoreSettings() {
  const { t } = useT();
  const toast = useToast();
  const confirmAction = useConfirm();
  const [stores, setStores] = useState<VectorStoreEntity[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [denied, setDenied] = useState(false);
  const [testingId, setTestingId] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, string>>({});

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listVectorStores();
      if (res?.data) setStores(res.data);
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setStores([]);
      } else {
        setError(e instanceof Error ? e.message : t("vstore.loadFailed"));
      }
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
      toast.success(t("vstore.testOk"));
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setDenied(true);
        return;
      }
      setTestResults((prev) => ({
        ...prev,
        [id]: err instanceof Error ? err.message : t("vstore.testFail"),
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
      toast.success(t("common.saved"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        return;
      }
      setError(e instanceof Error ? e.message : t("vstore.deleteFailed"));
    }
  };

  if (denied) {
    return <DeniedView />;
  }

  return (
    <ConsolePage size="form">
      <div>
        <SectionHeader title={t("vstore.title")} subtitle={t("vstore.subtitle")} />

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
          <Loading label={t("vstore.loading")} className="py-12" />
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
                  className={`flex flex-col gap-2 rounded-xl border border-hairline bg-surface-card p-4 ${ROW_HOVER}`}
                >
                  <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <div className="min-w-0 w-full sm:flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="min-w-0 break-words text-[15px] font-medium text-ink">{s.name}</span>
                        <span className="badge-pill uppercase text-[10px]">{s.engine_type}</span>
                        {s.source && (
                          <span className="badge-pill text-[10px] text-muted">
                            {s.source}
                          </span>
                        )}
                      </div>
                    </div>

                    <div className="flex shrink-0 flex-wrap items-center gap-2">
                      {s.id && (
                        <button
                          type="button"
                          onClick={() => void handleTest(s.id!)}
                          disabled={testingId === s.id}
                          className="btn btn-outline btn-sm flex items-center gap-1"
                        >
                          <IconPulse className={`h-3.5 w-3.5 ${testingId === s.id ? "animate-spin" : ""}`} />
                          {testingId === s.id ? t("vstore.testing") : t("vstore.test")}
                        </button>
                      )}
                      {!s.readonly && s.id && (
                        <button
                          type="button"
                          onClick={() => void handleDelete(s.id!)}
                          className="btn btn-danger btn-sm p-1.5"
                          title={t("common.delete")}
                          aria-label={t("common.delete")}
                        >
                          <IconTrash className="h-4 w-4" />
                        </button>
                      )}
                    </div>
                  </div>

                  {res && <Notice tone="error">{res}</Notice>}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </ConsolePage>
  );
}
