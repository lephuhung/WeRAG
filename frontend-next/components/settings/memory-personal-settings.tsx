"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconRefresh, IconTrash } from "@/components/icons";
import { Notice } from "@/components/settings/notice";
import { DataTable } from "@/components/settings/data-table";
import { Pill, PillGroup } from "@/components/settings/pill";
import { SectionHeader } from "@/components/settings/section-header";
import { TabStrip } from "@/components/settings/tab-strip";
import { Loading } from "@/components/settings/loading";
import { FieldLabel } from "@/components/settings/field";
import { useToast } from "@/components/toast";
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
import { formatDate, vnDayKey } from "@/lib/datetime";

export function MemoryPersonalSettings() {
  const { t } = useT();
  const toast = useToast();

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
        toast.success(t(checked ? "memp.enabledMsg" : "memp.disabledMsg"));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.updateFailed"));
    }
  };

  // Confirm item
  const handleConfirm = async (id: string) => {
    try {
      await confirmMemoryItem(id);
      toast.success(t("memp.approved"));
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.confirmFailed"));
    }
  };

  // Reject item
  const handleReject = async (id: string) => {
    try {
      await rejectMemoryItem(id);
      toast.success(t("memp.rejected"));
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("memp.rejectFailed"));
    }
  };

  // Delete item
  const handleDelete = async (id: string) => {
    try {
      await deleteMemoryItem(id);
      toast.success(t("memp.deleted"));
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
        toast.success(
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
        a.download = `memory-export-${vnDayKey(new Date())}.json`;
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
        toast.success(t("memp.cleared", { n: res.removed }));
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
        toast.success(t("memp.created"));
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
        toast.success(t("memp.updated"));
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
      <SectionHeader
        title={t("memp.title")}
        subtitle={t("memp.subtitle")}
        actions={
          <>
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
              aria-label={t("common.refresh")}
            >
              <IconRefresh className="h-3.5 w-3.5" />
            </button>
          </>
        }
      />

      {/* Workspace disabled banner if applicable */}
      {settings && !settings.workspace_enabled && (
        <Notice tone="warning" title={t("memp.workspaceDisabled")}>
          {t("memp.workspaceDisabledBody")}
        </Notice>
      )}

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

      {/* Switch Row */}
      <div className="flex items-center justify-between rounded-xl border border-hairline bg-surface-card p-5">
        <div>
          <span className="text-sm font-semibold text-ink block">{t("memp.enableTitle")}</span>
          <span className="caption text-muted block mt-0.5">
            {t("memp.enableDesc")}
          </span>
        </div>
        <Toggle
          label={t("memp.enableTitle")}
          checked={settings?.user_enabled ?? true}
          disabled={!settings || !settings.workspace_enabled}
          onChange={(v) => void handleToggle(v)}
        />
      </div>

      {/* Main navigation tabs: Items | Topics | Documents */}
      <div className="space-y-3">
        <TabStrip
          ariaLabel={t("memp.title")}
          value={mainTab}
          onChange={setMainTab}
          tabs={[
            { id: "items", label: t("memp.tabItems"), badge: items.length },
            { id: "topics", label: t("memp.tabTopics") },
            { id: "docs", label: t("memp.tabDocs") },
          ]}
        />

        {mainTab === "items" && (
          <PillGroup ariaLabel={t("memp.tabItems")}>
            {(["active", "pending", "superseded", "archived"] as MemoryStatus[]).map((st) => (
              <Pill key={st} active={statusFilter === st} onClick={() => setStatusFilter(st)}>
                {t(`memp.st.${st}`)}
              </Pill>
            ))}
          </PillGroup>
        )}
      </div>

      {/* Tab: Items */}
      {mainTab === "items" && (
        <div
          id="tabpanel-items"
          role="tabpanel"
          aria-labelledby="tab-items"
          className="space-y-3"
        >
          {loading ? (
            <Loading label={t("memp.loading")} />
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
                      <span className="badge-pill badge-accent uppercase text-[10.5px] font-semibold">
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
                            className="btn btn-danger btn-sm text-xs py-1"
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
                        className="btn btn-danger btn-sm p-1"
                        title={t("common.delete")}
                        aria-label={t("common.delete")}
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
                      {t("memp.createdAt")}: {it.created_at ? formatDate(it.created_at) : "—"}
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
                className="btn btn-danger btn-sm text-xs"
              >
                {t("memp.clearAll")}
              </button>
            </div>
          )}
        </div>
      )}

      {/* Tab: Topics */}
      {mainTab === "topics" && (
        <div
          id="tabpanel-topics"
          role="tabpanel"
          aria-labelledby="tab-topics"
          className="space-y-3"
        >
          <DataTable<MemoryTopic>
            columns={[
              { key: "topic", header: t("memp.colTopic") },
              { key: "hits", header: t("memp.colHits") },
              { key: "lastSeen", header: t("memp.colLastSeen") },
              { key: "actions", header: t("mem.colActions"), align: "right" },
            ]}
            rows={topics}
            rowKey={(tp) => String(tp.id)}
            emptyLabel={t("memp.emptyTopics")}
            minWidth="min-w-[640px]"
            caption={t("memp.tabTopics")}
            renderRow={(tp) => (
              <>
                <td className="px-5 py-3.5 align-middle font-medium text-ink">#{tp.topic}</td>
                <td className="px-5 py-3.5 align-middle text-muted text-xs whitespace-nowrap">
                  {t("memp.mentions", { n: tp.hits })}
                </td>
                <td className="px-5 py-3.5 align-middle text-muted text-xs whitespace-nowrap">
                  {tp.last_seen_at ? formatDate(tp.last_seen_at) : "—"}
                </td>
                <td className="px-5 py-3.5 align-middle text-right">
                  <div className="flex items-center justify-end gap-1">
                    <button
                      type="button"
                      onClick={async () => {
                        await promoteMemoryTopic(tp.id);
                        await loadTopics();
                        toast.success(t("memp.promoted", { topic: tp.topic }));
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
                      className="btn btn-danger btn-sm p-1.5"
                      title={t("common.delete")}
                      aria-label={t("common.delete")}
                    >
                      <IconTrash className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </td>
              </>
            )}
          />
        </div>
      )}

      {/* Tab: Documents */}
      {mainTab === "docs" && (
        <div
          id="tabpanel-docs"
          role="tabpanel"
          aria-labelledby="tab-docs"
          className="space-y-3"
        >
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
                    className="btn btn-danger btn-sm p-1.5"
                    title={t("common.delete")}
                    aria-label={t("common.delete")}
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
          <div>
            <FieldLabel htmlFor="memp-category">{t("memp.category")}</FieldLabel>
            <Select
              id="memp-category"
              value={draftKind}
              onChange={(v) => setDraftKind(v as MemoryKind)}
              options={KINDS.map((k) => ({ value: k.id, label: `${k.label} — ${k.desc}` }))}
            />
          </div>

          <div>
            <FieldLabel htmlFor="memp-content" required>
              {t("memp.content")}
            </FieldLabel>
            <textarea
              id="memp-content"
              required
              aria-required="true"
              rows={4}
              placeholder={t("memp.contentPh")}
              className="input resize-none"
              value={draftContent}
              onChange={(e) => setDraftContent(e.target.value)}
            />
          </div>

          <div>
            <FieldLabel htmlFor="memp-importance">{t("memp.importanceRange")}</FieldLabel>
            <div className="flex items-center gap-3">
              <input
                id="memp-importance"
                type="range"
                min={1}
                max={5}
                value={draftImportance}
                onChange={(e) => setDraftImportance(Number(e.target.value))}
                className="w-full accent-brand"
              />
              <span className="font-semibold text-sm w-4">{draftImportance}</span>
            </div>
          </div>

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
          <div>
            <FieldLabel htmlFor="memp-edit-content" required>
              {t("memp.contentLabel")}
            </FieldLabel>
            <textarea
              id="memp-edit-content"
              required
              aria-required="true"
              rows={4}
              className="input resize-none"
              value={editContent}
              onChange={(e) => setEditContent(e.target.value)}
            />
          </div>

          <div>
            <FieldLabel htmlFor="memp-edit-importance">{t("memp.importance")}</FieldLabel>
            <div className="flex items-center gap-3">
              <input
                id="memp-edit-importance"
                type="range"
                min={1}
                max={5}
                value={editImportance}
                onChange={(e) => setEditImportance(Number(e.target.value))}
                className="w-full accent-brand"
              />
              <span className="font-semibold text-sm w-4">{editImportance}</span>
            </div>
          </div>

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
              className="btn btn-danger"
            >
              {busyAction ? t("memp.clearing") : t("memp.clearCta")}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
