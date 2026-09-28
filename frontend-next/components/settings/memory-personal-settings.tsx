"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconRefresh, IconTrash } from "@/components/icons";
import { Select } from "@/components/select";
import {
  getMemorySettings,
  updateMemoryEnabled,
  listMemoryItems,
  confirmMemoryItem,
  rejectMemoryItem,
  createMemoryItem,
  updateMemoryItem,
  deleteMemoryItem,
  clearMemoryItems,
  exportMemoryItems,
  consolidateMemory,
  listMemoryTopics,
  deleteMemoryTopic,
  promoteMemoryTopic,
  listMemoryDocuments,
  deleteMemoryDocument,
  type MemoryItem,
  type MemorySettings,
  type MemoryStatus,
  type MemoryKind,
  type MemoryTopic,
  type MemoryDoc,
} from "@/lib/api/memory";
import { Toggle } from "@/components/settings/toggle";
import { useT } from "@/lib/i18n";

export function MemoryPersonalSettings() {
  const { t } = useT();

  const KINDS: { id: MemoryKind; label: string; desc: string }[] = [
    { id: "profile", label: t("memp.kProfile"), desc: t("memp.kProfileDesc") },
    { id: "preference", label: t("memp.kPreference"), desc: t("memp.kPreferenceDesc") },
    { id: "fact", label: t("memp.kFact"), desc: t("memp.kFactDesc") },
    { id: "task", label: t("memp.kTask"), desc: t("memp.kTaskDesc") },
    { id: "interest", label: t("memp.kInterest"), desc: t("memp.kInterestDesc") },
  ];
  const [settings, setSettings] = useState<MemorySettings | null>(null);
  const [items, setItems] = useState<MemoryItem[]>([]);
  const [topics, setTopics] = useState<MemoryTopic[]>([]);
  const [docs, setDocs] = useState<MemoryDoc[]>([]);

  const [mainTab, setMainTab] = useState<"items" | "topics" | "docs">("items");
  const [statusFilter, setStatusFilter] = useState<MemoryStatus>("active");

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [busyAction, setBusyAction] = useState(false);

  // Add modal
  const [addModalOpen, setAddModalOpen] = useState(false);
  const [draftKind, setDraftKind] = useState<MemoryKind>("preference");
  const [draftContent, setDraftContent] = useState("");
  const [draftImportance, setDraftImportance] = useState(3);
  const [savingItem, setSavingItem] = useState(false);

  // Edit modal
  const [editingItem, setEditingItem] = useState<MemoryItem | null>(null);
  const [editContent, setEditContent] = useState("");
  const [editImportance, setEditImportance] = useState(3);

  // Clear all confirmation
  const [confirmClearOpen, setConfirmClearOpen] = useState(false);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [sRes, iRes] = await Promise.all([
        getMemorySettings(),
        listMemoryItems({ status: statusFilter, limit: 100 }),
      ]);
      if (sRes.success && sRes.data) setSettings(sRes.data);
      if (iRes.success && iRes.data) setItems(iRes.data);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [statusFilter]);

  const loadTopics = async () => {
    try {
      const res = await listMemoryTopics({ limit: 100 });
      if (res.success && res.data) setTopics(res.data);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.loadTopicsFailed"));
    }
  };

  const loadDocs = async () => {
    try {
      const res = await listMemoryDocuments({ limit: 100 });
      if (res.success && res.data) setDocs(res.data);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.loadDocsFailed"));
    }
  };

  useEffect(() => {
    if (mainTab === "items") void loadData();
    else if (mainTab === "topics") void loadTopics();
    else if (mainTab === "docs") void loadDocs();
  }, [mainTab, loadData]);

  // Toggle user memory
  const handleToggle = async (checked: boolean) => {
    setError("");
    try {
      const res = await updateMemoryEnabled(checked);
      if (res.success && res.data) {
        setSettings(res.data);
        setSuccess(t(checked ? "memp.enabledMsg" : "memp.disabledMsg"));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.updateFailed"));
    }
  };

  // Confirm item
  const handleConfirm = async (id: string) => {
    try {
      await confirmMemoryItem(id);
      setSuccess(t("memp.approved"));
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.confirmFailed"));
    }
  };

  // Reject item
  const handleReject = async (id: string) => {
    try {
      await rejectMemoryItem(id);
      setSuccess(t("memp.rejected"));
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.rejectFailed"));
    }
  };

  // Delete item
  const handleDelete = async (id: string) => {
    try {
      await deleteMemoryItem(id);
      setSuccess(t("memp.deleted"));
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.deleteFailed"));
    }
  };

  // Consolidate
  const handleConsolidate = async () => {
    setBusyAction(true);
    setError("");
    try {
      const res = await consolidateMemory();
      if (res.success && res.data) {
        setSuccess(
          t("memp.consolidateDone", {
            a: res.data.reviewed,
            b: res.data.merged,
            c: res.data.demoted,
          }),
        );
        await loadData();
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.consolidateFailed"));
    } finally {
      setBusyAction(false);
    }
  };

  // Export
  const handleExport = async () => {
    try {
      const res = await exportMemoryItems();
      if (res.success && res.data) {
        const json = JSON.stringify(res.data, null, 2);
        const blob = new Blob([json], { type: "application/json" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `memory-export-${new Date().toISOString().slice(0, 10)}.json`;
        a.click();
        URL.revokeObjectURL(url);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.exportFailed"));
    }
  };

  // Clear all
  const handleClearAll = async () => {
    setBusyAction(true);
    setError("");
    try {
      const res = await clearMemoryItems();
      if (res.success) {
        setSuccess(t("memp.cleared", { n: res.removed }));
        setConfirmClearOpen(false);
        await loadData();
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.clearFailed"));
    } finally {
      setBusyAction(false);
    }
  };

  // Add item
  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!draftContent.trim()) return;
    setSavingItem(true);
    setError("");
    try {
      const res = await createMemoryItem({
        kind: draftKind,
        content: draftContent.trim(),
        importance: draftImportance,
      });
      if (res.success) {
        setSuccess(t("memp.created"));
        setAddModalOpen(false);
        setDraftContent("");
        await loadData();
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.createFailed"));
    } finally {
      setSavingItem(false);
    }
  };

  // Save edit item
  const handleSaveEdit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingItem || !editContent.trim()) return;
    setSavingItem(true);
    setError("");
    try {
      const res = await updateMemoryItem(editingItem.id, {
        content: editContent.trim(),
        importance: editImportance,
      });
      if (res.success) {
        setSuccess(t("memp.updated"));
        setEditingItem(null);
        await loadData();
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.updateItemFailed"));
    } finally {
      setSavingItem(false);
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">{t("memp.title")}</h2>
          <p className="caption text-muted mt-1">
            {t("memp.subtitle")}
          </p>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setAddModalOpen(true)}
            className="btn btn-primary btn-sm flex items-center gap-1.5"
          >
            <IconPlus className="h-3.5 w-3.5" />
            <span>{t("memp.add")}</span>
          </button>
          <button
            type="button"
            onClick={() => void handleConsolidate()}
            disabled={busyAction}
            className="btn btn-outline btn-sm text-xs"
            title={t("memp.consolidateTip")}
          >
            {busyAction ? t("memp.consolidating") : t("memp.consolidate")}
          </button>
          <button
            type="button"
            onClick={() => void handleExport()}
            className="btn btn-outline btn-sm text-xs"
            title={t("memp.exportTip")}
          >
            {t("memp.export")}
          </button>
          <button
            type="button"
            onClick={() => void loadData()}
            className="btn btn-outline btn-sm p-1.5"
            title={t("common.refresh")}
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* Workspace disabled banner if applicable */}
      {settings && !settings.workspace_enabled && (
        <div className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-400">
          <strong>{t("memp.workspaceDisabled")}</strong> {t("memp.workspaceDisabledBody")}
        </div>
      )}

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400">
          {error}
        </div>
      )}
      {success && (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/30 dark:text-emerald-400">
          {success}
        </div>
      )}

      {/* Switch Row */}
      <div className="flex items-center justify-between rounded-xl border border-hairline bg-surface-card p-5">
        <div>
          <span className="text-sm font-semibold text-ink block">{t("memp.enableTitle")}</span>
          <span className="caption text-muted block mt-0.5">
            {t("memp.enableDesc")}
          </span>
        </div>
        <Toggle
          checked={settings?.user_enabled ?? true}
          disabled={!settings || !settings.workspace_enabled}
          onChange={(v) => void handleToggle(v)}
        />
      </div>

      {/* Main navigation tabs: Items | Topics | Documents */}
      <div className="flex items-center justify-between border-b border-hairline pb-2">
        <div className="flex gap-4">
          <button
            type="button"
            onClick={() => setMainTab("items")}
            className={`text-sm font-medium pb-2 border-b-2 -mb-2.5 transition-colors ${
              mainTab === "items"
                ? "border-brand text-brand font-semibold"
                : "border-transparent text-muted hover:text-ink"
            }`}
          >
            {t("memp.tabItems")} ({items.length})
          </button>
          <button
            type="button"
            onClick={() => setMainTab("topics")}
            className={`text-sm font-medium pb-2 border-b-2 -mb-2.5 transition-colors ${
              mainTab === "topics"
                ? "border-brand text-brand font-semibold"
                : "border-transparent text-muted hover:text-ink"
            }`}
          >
            {t("memp.tabTopics")}
          </button>
          <button
            type="button"
            onClick={() => setMainTab("docs")}
            className={`text-sm font-medium pb-2 border-b-2 -mb-2.5 transition-colors ${
              mainTab === "docs"
                ? "border-brand text-brand font-semibold"
                : "border-transparent text-muted hover:text-ink"
            }`}
          >
            {t("memp.tabDocs")}
          </button>
        </div>

        {mainTab === "items" && (
          <div className="flex items-center gap-1.5">
            {(["active", "pending", "superseded", "archived"] as MemoryStatus[]).map((st) => (
              <button
                key={st}
                type="button"
                onClick={() => setStatusFilter(st)}
                className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                  statusFilter === st
                    ? "bg-ink text-white dark:bg-white dark:text-ink"
                    : "text-muted hover:bg-surface-strong hover:text-ink"
                }`}
              >
                {t(`memp.st.${st}`)}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Tab: Items */}
      {mainTab === "items" && (
        <div className="space-y-3">
          {loading ? (
            <p className="caption text-muted text-center py-12">{t("memp.loading")}</p>
          ) : items.length === 0 ? (
            <div className="rounded-xl border border-hairline py-16 text-center text-sm text-muted">
              {t("memp.emptyItems")}
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3">
              {items.map((it) => (
                <div
                  key={it.id}
                  className="card p-4 hover:border-ink/20 transition-all flex flex-col justify-between gap-3"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="badge-pill uppercase text-[10.5px] font-semibold bg-brand/10 text-brand">
                        {it.kind}
                      </span>
                      {it.topic && (
                        <span className="badge-pill text-[10.5px] text-muted font-medium">
                          #{it.topic}
                        </span>
                      )}
                      <span className="caption text-muted text-[11px]">
                        {t("memp.importance")}: {it.importance}/5
                      </span>
                      {it.origin && (
                        <span className="caption text-muted-soft text-[11px]">
                          ({it.origin})
                        </span>
                      )}
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1 shrink-0">
                      {it.status === "pending" && (
                        <>
                          <button
                            type="button"
                            onClick={() => void handleConfirm(it.id)}
                            className="btn btn-primary btn-sm text-xs py-1"
                          >
                            {t("memp.approve")}
                          </button>
                          <button
                            type="button"
                            onClick={() => void handleReject(it.id)}
                            className="btn btn-outline btn-sm text-xs py-1 text-rose-600"
                          >
                            {t("memp.reject")}
                          </button>
                        </>
                      )}
                      <button
                        type="button"
                        onClick={() => {
                          setEditingItem(it);
                          setEditContent(it.content);
                          setEditImportance(it.importance);
                        }}
                        className="btn btn-ghost btn-sm text-xs py-1"
                      >
                        {t("common.edit")}
                      </button>
                      <button
                        type="button"
                        onClick={() => void handleDelete(it.id)}
                        className="btn btn-ghost btn-sm p-1 text-muted hover:text-rose-600"
                        title={t("common.delete")}
                      >
                        <IconTrash className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </div>

                  <p className="text-sm text-ink leading-relaxed whitespace-pre-wrap">
                    {it.content}
                  </p>

                  <div className="flex items-center justify-between text-[11px] text-muted border-t border-hairline pt-2 mt-1">
                    <span>
                      {t("memp.createdAt")}: {it.created_at ? new Date(it.created_at).toLocaleDateString() : "—"}
                    </span>
                    <span>
                      {it.use_count > 0
                        ? t("memp.recalledTimes", { n: it.use_count })
                        : t("memp.notRecalled")}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}

          {items.length > 0 && (
            <div className="flex justify-end pt-4">
              <button
                type="button"
                onClick={() => setConfirmClearOpen(true)}
                className="btn btn-outline btn-sm text-xs text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/30"
              >
                {t("memp.clearAll")}
              </button>
            </div>
          )}
        </div>
      )}

      {/* Tab: Topics */}
      {mainTab === "topics" && (
        <div className="space-y-3">
          {topics.length === 0 ? (
            <div className="rounded-xl border border-hairline py-16 text-center text-sm text-muted">
              {t("memp.emptyTopics")}
            </div>
          ) : (
            <div className="overflow-hidden rounded-xl border border-hairline">
              <table className="w-full text-left border-collapse text-sm">
                <thead>
                  <tr className="border-b border-hairline bg-surface-strong/50 text-xs font-semibold text-muted">
                    <th className="py-3 px-4">{t("memp.colTopic")}</th>
                    <th className="py-3 px-4">{t("memp.colHits")}</th>
                    <th className="py-3 px-4">{t("memp.colLastSeen")}</th>
                    <th className="py-3 px-4 text-right">{t("mem.colActions")}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-hairline">
                  {topics.map((tp) => (
                    <tr key={tp.id} className="hover:bg-surface-strong/30 transition-colors">
                      <td className="py-3 px-4 font-medium text-ink">#{tp.topic}</td>
                      <td className="py-3 px-4 text-muted text-xs">
                        {t("memp.mentions", { n: tp.hits })}
                      </td>
                      <td className="py-3 px-4 text-muted text-xs">
                        {tp.last_seen_at ? new Date(tp.last_seen_at).toLocaleDateString() : "—"}
                      </td>
                      <td className="py-3 px-4 text-right space-x-1">
                        <button
                          type="button"
                          onClick={async () => {
                            await promoteMemoryTopic(tp.id);
                            await loadTopics();
                            setSuccess(t("memp.promoted", { topic: tp.topic }));
                          }}
                          className="btn btn-outline btn-sm text-xs py-1"
                        >
                          {t("memp.promote")}
                        </button>
                        <button
                          type="button"
                          onClick={async () => {
                            await deleteMemoryTopic(tp.id);
                            await loadTopics();
                          }}
                          className="btn btn-ghost btn-sm p-1.5 text-muted hover:text-rose-600"
                        >
                          <IconTrash className="h-3.5 w-3.5" />
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Tab: Documents */}
      {mainTab === "docs" && (
        <div className="space-y-3">
          {docs.length === 0 ? (
            <div className="rounded-xl border border-hairline py-16 text-center text-sm text-muted">
              {t("memp.emptyDocs")}
            </div>
          ) : (
            <div className="divide-y divide-hairline rounded-xl border border-hairline">
              {docs.map((dc) => (
                <div key={dc.id} className="flex items-center justify-between p-4 text-sm">
                  <div>
                    <span className="font-medium text-ink block">{dc.title}</span>
                    <span className="caption text-muted block text-xs">
                      KB ID: {dc.knowledge_base_id} • {t("memp.colHits")}: {dc.hits}
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={async () => {
                      await deleteMemoryDocument(dc.id);
                      await loadDocs();
                    }}
                    className="btn btn-ghost btn-sm p-1.5 text-muted hover:text-rose-600"
                  >
                    <IconTrash className="h-3.5 w-3.5" />
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Add Memory Modal */}
      <Modal open={addModalOpen} title={t("memp.addTitle")} onClose={() => setAddModalOpen(false)}>
        <form onSubmit={handleCreate} className="space-y-4">
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("memp.category")}</span>
            <Select
              value={draftKind}
              onChange={(v) => setDraftKind(v as MemoryKind)}
              options={KINDS.map((k) => ({ value: k.id, label: `${k.label} — ${k.desc}` }))}
            />
          </label>

          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("memp.content")}</span>
            <textarea
              required
              rows={4}
              placeholder={t("memp.contentPh")}
              className="input resize-none"
              value={draftContent}
              onChange={(e) => setDraftContent(e.target.value)}
            />
          </label>

          <label className="block">
            <span className="caption mb-1.5 block text-muted">
              {t("memp.importanceRange")}
            </span>
            <div className="flex items-center gap-3">
              <input
                type="range"
                min={1}
                max={5}
                value={draftImportance}
                onChange={(e) => setDraftImportance(Number(e.target.value))}
                className="w-full accent-brand"
              />
              <span className="font-semibold text-sm w-4">{draftImportance}</span>
            </div>
          </label>

          <div className="flex justify-end gap-3 pt-3">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setAddModalOpen(false)}
            >
              {t("common.cancel")}
            </button>
            <button
              type="submit"
              disabled={savingItem || !draftContent.trim()}
              className="btn btn-primary"
            >
              {savingItem ? t("memp.saving") : t("memp.saveMemory")}
            </button>
          </div>
        </form>
      </Modal>

      {/* Edit Memory Modal */}
      <Modal
        open={editingItem !== null}
        title={t("memp.editTitle")}
        onClose={() => setEditingItem(null)}
      >
        <form onSubmit={handleSaveEdit} className="space-y-4">
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("memp.contentLabel")}</span>
            <textarea
              required
              rows={4}
              className="input resize-none"
              value={editContent}
              onChange={(e) => setEditContent(e.target.value)}
            />
          </label>

          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("memp.importance")}</span>
            <div className="flex items-center gap-3">
              <input
                type="range"
                min={1}
                max={5}
                value={editImportance}
                onChange={(e) => setEditImportance(Number(e.target.value))}
                className="w-full accent-brand"
              />
              <span className="font-semibold text-sm w-4">{editImportance}</span>
            </div>
          </label>

          <div className="flex justify-end gap-3 pt-3">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setEditingItem(null)}
            >
              {t("common.cancel")}
            </button>
            <button
              type="submit"
              disabled={savingItem || !editContent.trim()}
              className="btn btn-primary"
            >
              {savingItem ? t("memp.saving") : t("memp.saveChanges")}
            </button>
          </div>
        </form>
      </Modal>

      {/* Clear All Modal */}
      <Modal
        open={confirmClearOpen}
        title={t("memp.clearTitle")}
        onClose={() => setConfirmClearOpen(false)}
      >
        <div className="space-y-4">
          <p className="body-sm text-body">
            {t("memp.clearBody")}
          </p>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setConfirmClearOpen(false)}
            >
              {t("common.cancel")}
            </button>
            <button
              type="button"
              disabled={busyAction}
              onClick={() => void handleClearAll()}
              className="btn btn-primary bg-rose-600 hover:bg-rose-700 text-white"
            >
              {busyAction ? t("memp.clearing") : t("memp.clearCta")}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
