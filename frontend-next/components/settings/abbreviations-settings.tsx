/* Global Vietnamese abbreviation dictionary (AIRAG port): the agent expands
 * these short forms via the resolve_abbreviation tool before searching. Any
 * member can suggest entries; a Tenant Admin (or system admin) edits,
 * activates and deletes — the server enforces the same split (PATCH/DELETE
 * are Admin+ via OwnerOrSystemAdmin, an Admin-level alias). */
"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconCheck, IconClose, IconEdit, IconPlus, IconRefresh, IconSearch, IconTrash } from "@/components/icons";
import {
  listAbbreviations,
  createAbbreviation,
  updateAbbreviation,
  deleteAbbreviation,
  type Abbreviation,
} from "@/lib/api/abbreviations";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { Notice } from "@/components/settings/notice";
import { DataTable } from "@/components/settings/data-table";
import { Pill, PillGroup } from "@/components/settings/pill";
import { SectionHeader } from "@/components/settings/section-header";
import { Pager } from "@/components/settings/pager";
import { FieldLabel } from "@/components/settings/field";
import { useToast } from "@/components/toast";

const PAGE_SIZE = 20;
type StatusFilter = "all" | "active" | "pending";

/* Lets the account-menu pending badge refresh after any mutation here. */
function notifyAbbreviationsChanged() {
  window.dispatchEvent(new Event("weknora:abbreviations-changed"));
}

export function AbbreviationsSettings() {
  const { t } = useT();
  const auth = useAuth();

  const isSystemAdmin = auth.user?.is_system_admin === true;
  /* Approving, editing and deleting entries is SuperAdmin work; everyone
   * else can browse the dictionary and suggest new entries. */
  const canManage = isSystemAdmin;

  const [rows, setRows] = useState<Abbreviation[]>([]);
  const [total, setTotal] = useState(0);
  /* Row offset of the current page. The server lists pending rows first, so
   * approving in place shifts every later row; paging forward skips only the
   * rows that still sit ahead of the next unseen one (see nextOffset). */
  const [offset, setOffset] = useState(0);
  /* is_active of each row as fetched, and how many rows the server returned. */
  const [fetched, setFetched] = useState<{ active: Record<string, boolean>; count: number; total: number }>({
    active: {},
    count: 0,
    total: 0,
  });
  /* Bumped to refetch when the next offset equals the current one (every
   * row on the page was approved, so the unseen rows moved up to it). */
  const [reloadTick, setReloadTick] = useState(0);
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const toast = useToast();

  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Abbreviation | null>(null);
  const [formShort, setFormShort] = useState("");
  const [formFull, setFormFull] = useState("");
  const [formDesc, setFormDesc] = useState("");
  const [formActive, setFormActive] = useState(false);
  const [formProper, setFormProper] = useState(false);
  const [formBusy, setFormBusy] = useState(false);

  const [deleting, setDeleting] = useState<Abbreviation | null>(null);
  const [actionBusy, setActionBusy] = useState(false);

  /* Pending rows ticked for a bulk approve / reject, and the dictionary-wide
   * pending count behind the review banner. */
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkBusy, setBulkBusy] = useState(false);
  const [pendingTotal, setPendingTotal] = useState(0);

  const page = Math.floor(offset / PAGE_SIZE) + 1;

  /* Rows of this page that still come before the first unseen row in the
   * server order (pending first): rows that left the filter, or moved from
   * the pending block to the active block, no longer take a slot. */
  const keptAhead = rows.filter((r) => {
    if (statusFilter === "pending") return !r.is_active;
    if (statusFilter === "active") return r.is_active;
    return !(r.is_active && fetched.active[r.id] === false);
  }).length;
  const nextOffset = offset + keptAhead;
  /* Rows approved (or un-approved) here have left a status filter. */
  const leftFilter =
    statusFilter === "all"
      ? 0
      : rows.filter((r) => r.is_active !== (statusFilter === "active")).length;
  const shownTotal = Math.max(0, total - leftFilter);
  const totalPages = Math.max(page, Math.ceil(shownTotal / PAGE_SIZE));

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listAbbreviations({
        search: search || undefined,
        isActive: statusFilter === "all" ? undefined : statusFilter === "active",
        offset,
        pageSize: PAGE_SIZE,
      });
      const data = res.data ?? [];
      setRows(data);
      setTotal(res.total ?? 0);
      setFetched({
        active: Object.fromEntries(data.map((r) => [r.id, r.is_active])),
        count: data.length,
        total: res.total ?? 0,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : t("abbrev.loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reloadTick only forces a refetch
  }, [search, statusFilter, offset, reloadTick]);

  useEffect(() => {
    void load();
  }, [load]);

  const loadPendingTotal = useCallback(async () => {
    if (!canManage) return;
    try {
      const res = await listAbbreviations({ isActive: false, pageSize: 1 });
      setPendingTotal(res.total ?? 0);
    } catch {
      /* banner only */
    }
  }, [canManage]);

  useEffect(() => {
    void loadPendingTotal();
  }, [loadPendingTotal, reloadTick]);

  // A new page or filter starts with nothing ticked.
  useEffect(() => {
    setSelected(new Set());
  }, [search, statusFilter, offset, reloadTick]);

  // Debounce the free-text search; each filter change restarts at page 1.
  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(searchInput.trim());
      setOffset(0);
    }, 300);
    return () => clearTimeout(timer);
  }, [searchInput]);

  const openCreate = () => {
    setEditing(null);
    setFormShort("");
    setFormFull("");
    setFormDesc("");
    setFormActive(false);
    setFormProper(false);
    setFormOpen(true);
  };

  const openEdit = (row: Abbreviation) => {
    setEditing(row);
    setFormShort(row.short_form);
    setFormFull(row.full_form);
    setFormDesc(row.description);
    setFormActive(row.is_active);
    setFormProper(!!row.is_proper_name);
    setFormOpen(true);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const short = formShort.trim();
    // a proper name is kept as typed: its full form defaults to itself
    const full = formFull.trim() || (formProper ? short : "");
    if (!short || !full) return;
    setFormBusy(true);
    setError("");
    try {
      if (editing) {
        const res = await updateAbbreviation(editing.id, {
          short_form: short,
          full_form: full,
          description: formDesc.trim(),
          is_active: formActive,
          is_proper_name: formProper,
        });
        if (res.success) {
          setFormOpen(false);
          // Replace the row in place — reloading would reorder the page.
          const saved = res.data ?? { ...editing, short_form: short, full_form: full, description: formDesc.trim(), is_active: formActive, is_proper_name: formProper };
          setRows((rs) => rs.map((r) => (r.id === editing.id ? saved : r)));
          notifyAbbreviationsChanged();
        } else {
          setError(t("abbrev.saveFailed"));
        }
      } else {
        // POST always creates an inactive suggestion — owners flip it active
        // right below via the status toggle.
        const res = await createAbbreviation({
          short_form: short,
          full_form: full,
          description: formDesc.trim(),
          is_proper_name: formProper,
        });
        if (res.success) {
          toast.success(t("abbrev.suggestSaved"));
          setFormOpen(false);
          await load();
          notifyAbbreviationsChanged();
        } else {
          setError(t("abbrev.saveFailed"));
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("abbrev.saveFailed"));
    } finally {
      setFormBusy(false);
    }
  };

  /* Approve / un-approve in place: flip the row at once, send the PATCH and
   * put it back only if the server refuses. No reload, so the page keeps its
   * order and scroll position. */
  const handleToggleActive = async (row: Abbreviation) => {
    if (!canManage || busyIds.has(row.id)) return;
    const next = !row.is_active;
    setError("");
    setBusyIds((s) => new Set(s).add(row.id));
    setRows((rs) => rs.map((r) => (r.id === row.id ? { ...r, is_active: next } : r)));
    const revert = () =>
      setRows((rs) => rs.map((r) => (r.id === row.id ? { ...r, is_active: row.is_active } : r)));
    try {
      const res = await updateAbbreviation(row.id, { is_active: next });
      if (res.success) {
        setPendingTotal((n) => Math.max(0, n + (next ? -1 : 1)));
        setSelected((sel) => {
          const n = new Set(sel);
          n.delete(row.id);
          return n;
        });
        notifyAbbreviationsChanged();
      } else {
        revert();
        setError(t("abbrev.saveFailed"));
      }
    } catch (err) {
      revert();
      setError(err instanceof Error ? err.message : t("abbrev.saveFailed"));
    } finally {
      setBusyIds((s) => {
        const n = new Set(s);
        n.delete(row.id);
        return n;
      });
    }
  };

  /* Reject = drop the suggestion. No confirm dialog: it is a pending entry
   * nobody relies on yet, and reviewers go through hundreds of them. */
  const handleReject = async (row: Abbreviation) => {
    if (!canManage || busyIds.has(row.id)) return;
    setError("");
    setBusyIds((s) => new Set(s).add(row.id));
    try {
      const res = await deleteAbbreviation(row.id);
      if (res.success) {
        setRows((rs) => rs.filter((r) => r.id !== row.id));
        setTotal((n) => Math.max(0, n - 1));
        if (!row.is_active) setPendingTotal((n) => Math.max(0, n - 1));
        setSelected((sel) => {
          const n = new Set(sel);
          n.delete(row.id);
          return n;
        });
        notifyAbbreviationsChanged();
      } else {
        setError(t("abbrev.deleteFailed"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("abbrev.deleteFailed"));
    } finally {
      setBusyIds((s) => {
        const n = new Set(s);
        n.delete(row.id);
        return n;
      });
    }
  };

  /* Bulk approve / reject the ticked rows, then refetch the same offset:
   * the server lists pending first, so the next batch of suggestions moves
   * up into view. */
  const handleBulk = async (approve: boolean) => {
    const ids = rows.filter((r) => selected.has(r.id) && !r.is_active).map((r) => r.id);
    if (!canManage || ids.length === 0 || bulkBusy) return;
    setBulkBusy(true);
    setError("");
    const results = await Promise.allSettled(
      ids.map((id) => (approve ? updateAbbreviation(id, { is_active: true }) : deleteAbbreviation(id))),
    );
    const ok = results.filter((r) => r.status === "fulfilled" && r.value.success).length;
    const failed = ids.length - ok;
    toast.success(t("abbrev.bulkDone", { approved: approve ? ok : 0, rejected: approve ? 0 : ok }));
    if (failed > 0) setError(t("abbrev.bulkPartial", { failed }));
    setBulkBusy(false);
    setSelected(new Set());
    setReloadTick((n) => n + 1);
    notifyAbbreviationsChanged();
  };

  const pendingOnPage = rows.filter((r) => !r.is_active);
  const allPendingSelected = pendingOnPage.length > 0 && pendingOnPage.every((r) => selected.has(r.id));
  const toggleSelectPage = () =>
    setSelected(allPendingSelected ? new Set() : new Set(pendingOnPage.map((r) => r.id)));
  const toggleSelect = (id: string) =>
    setSelected((sel) => {
      const n = new Set(sel);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const selectedCount = pendingOnPage.filter((r) => selected.has(r.id)).length;

  const handleDelete = async () => {
    if (!deleting) return;
    setActionBusy(true);
    setError("");
    try {
      const res = await deleteAbbreviation(deleting.id);
      if (res.success) {
        const id = deleting.id;
        if (!deleting.is_active) setPendingTotal((n) => Math.max(0, n - 1));
        setDeleting(null);
        setRows((rs) => rs.filter((r) => r.id !== id));
        setTotal((n) => Math.max(0, n - 1));
        notifyAbbreviationsChanged();
      } else {
        setError(t("abbrev.deleteFailed"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("abbrev.deleteFailed"));
    } finally {
      setActionBusy(false);
    }
  };

  const filterChips: { id: StatusFilter; label: string }[] = [
    { id: "all", label: t("common.all") },
    { id: "active", label: t("abbrev.active") },
    { id: "pending", label: t("abbrev.pending") },
  ];

  return (
    <div className="space-y-6">
      {/* Header */}
      <SectionHeader
        title={t("abbrev.title")}
        subtitle={t("abbrev.desc")}
        actions={
          <>
            <button
              type="button"
              onClick={openCreate}
              className="btn btn-primary btn-sm flex items-center gap-1.5"
            >
              <IconPlus className="h-3.5 w-3.5" />
              <span>{t("abbrev.add")}</span>
            </button>
            <button
              type="button"
              onClick={() => void load()}
              className="btn btn-outline btn-sm flex items-center gap-1.5"
              title={t("common.refresh")}
              aria-label={t("common.refresh")}
            >
              <IconRefresh className="h-3.5 w-3.5" />
            </button>
          </>
        }
      />

      {error && (
        <Notice
          tone="error"
          action={
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() => void load()}
            >
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}

      {/* Search + status filter */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="relative w-full max-w-sm">
          <IconSearch className="absolute left-3 top-1/2 -translate-y-1/2 text-muted h-4 w-4" />
          <input
            id="abbrev-search"
            type="text"
            className="input pl-9 text-sm"
            placeholder={t("abbrev.search")}
            aria-label={t("abbrev.search")}
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
          />
        </div>
        <PillGroup ariaLabel={t("abbrev.actions")}>
          {filterChips.map((f) => (
            <Pill
              key={f.id}
              active={statusFilter === f.id}
              onClick={() => {
                setStatusFilter(f.id);
                setOffset(0);
              }}
            >
              {f.label}
            </Pill>
          ))}
        </PillGroup>
      </div>

      {canManage && pendingTotal > 0 && statusFilter !== "pending" && (
        <Notice
          tone="warning"
          action={
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() => {
                setStatusFilter("pending");
                setOffset(0);
              }}
            >
              {t("abbrev.reviewPending")}
            </button>
          }
        >
          {t("abbrev.pendingBanner", { n: pendingTotal })}
        </Notice>
      )}

      {canManage && selectedCount > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-hairline bg-surface-strong/50 px-4 py-2.5 text-sm">
          <span className="text-muted">{t("abbrev.selected", { n: selectedCount })}</span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={bulkBusy}
              onClick={() => void handleBulk(true)}
              className="btn btn-primary btn-sm flex items-center gap-1.5 disabled:opacity-50"
            >
              <IconCheck className="h-3.5 w-3.5" />
              {t("abbrev.approveSelected", { n: selectedCount })}
            </button>
            <button
              type="button"
              disabled={bulkBusy}
              onClick={() => void handleBulk(false)}
              className="btn btn-danger btn-sm flex items-center gap-1.5 disabled:opacity-50"
            >
              <IconClose className="h-3.5 w-3.5" />
              {t("abbrev.rejectSelected", { n: selectedCount })}
            </button>
          </div>
        </div>
      )}

      {/* Table */}
      <DataTable<Abbreviation>
        columns={[
          ...(canManage
            ? [
                {
                  key: "select",
                  header: (
                    <input
                      type="checkbox"
                      aria-label={t("abbrev.selectPage")}
                      title={t("abbrev.selectPage")}
                      checked={allPendingSelected}
                      disabled={pendingOnPage.length === 0}
                      onChange={toggleSelectPage}
                    />
                  ),
                  className: "w-8",
                },
              ]
            : []),
          { key: "short", header: t("abbrev.shortForm"), className: "w-32" },
          { key: "full", header: t("abbrev.fullForm"), className: "w-[22%]" },
          { key: "desc", header: t("abbrev.description") },
          { key: "actions", header: t("abbrev.actions"), align: "right" as const },
        ]}
        rows={rows}
        rowKey={(r) => r.id}
        loading={loading && rows.length === 0}
        loadingLabel={t("common.loading")}
        emptyLabel={t("abbrev.empty")}
        minWidth="min-w-[720px]"
        rowClassName={loading && rows.length > 0 ? "opacity-60 transition-opacity" : ""}
        caption={t("abbrev.title")}
        renderRow={(r) => (
          <>
            {canManage && (
              <td className="px-5 py-3.5 align-middle">
                {!r.is_active && (
                  <input
                    type="checkbox"
                    aria-label={r.short_form}
                    checked={selected.has(r.id)}
                    onChange={() => toggleSelect(r.id)}
                  />
                )}
              </td>
            )}
            <td className="px-5 py-3.5 align-middle">
              <div className="flex flex-col items-start gap-1.5">
                <div className="flex max-w-full items-center gap-1">
                  <code className="min-w-0 break-words rounded bg-surface-strong px-1.5 py-0.5 text-[12.5px] font-semibold text-ink">
                    {r.short_form}
                  </code>
                  {canManage ? (
                    <button
                      type="button"
                      onClick={() => void handleToggleActive(r)}
                      disabled={busyIds.has(r.id)}
                      title={`${r.is_active ? t("abbrev.active") : t("abbrev.pending")} — ${r.is_active ? t("abbrev.deactivate") : t("abbrev.activate")}`}
                      aria-label={`${r.is_active ? t("abbrev.active") : t("abbrev.pending")} — ${r.is_active ? t("abbrev.deactivate") : t("abbrev.activate")}`}
                      className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full transition-colors hover:bg-surface-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:opacity-50"
                    >
                      <span
                        aria-hidden="true"
                        className="h-2.5 w-2.5 rounded-full"
                        style={{
                          background: r.is_active
                            ? "var(--color-success)"
                            : "var(--color-warning)",
                        }}
                      />
                    </button>
                  ) : (
                    <span
                      title={r.is_active ? t("abbrev.active") : t("abbrev.pending")}
                      className="inline-flex h-6 w-6 shrink-0 items-center justify-center"
                    >
                      <span
                        aria-hidden="true"
                        className="h-2.5 w-2.5 rounded-full"
                        style={{
                          background: r.is_active
                            ? "var(--color-success)"
                            : "var(--color-warning)",
                        }}
                      />
                      <span className="sr-only">{r.is_active ? t("abbrev.active") : t("abbrev.pending")}</span>
                    </span>
                  )}
                </div>
              </div>
            </td>
            <td className="px-5 py-3.5 align-middle break-words text-ink">{r.full_form}</td>
            <td className="px-5 py-3.5 align-middle break-words text-muted text-xs leading-relaxed">
              <p className="line-clamp-3" title={r.description || undefined}>
                {r.description || "—"}
              </p>
            </td>
            <td className="px-5 py-3.5 align-middle text-right">
              {canManage && (
                <div className="flex flex-col items-end gap-1">
                  {!r.is_active && (
                    <div className="flex items-center justify-end gap-1 whitespace-nowrap">
                      <button
                        type="button"
                        onClick={() => void handleToggleActive(r)}
                        disabled={busyIds.has(r.id)}
                        className="btn btn-ghost btn-sm flex items-center gap-1 px-2 py-1 text-xs text-success hover:bg-success-surface disabled:opacity-50"
                        title={t("abbrev.approve")}
                      >
                        <IconCheck className="h-3.5 w-3.5" />
                        {t("abbrev.approve")}
                      </button>
                      <button
                        type="button"
                        onClick={() => void handleReject(r)}
                        disabled={busyIds.has(r.id)}
                        className="btn btn-danger btn-sm flex items-center gap-1 px-2 py-1 text-xs disabled:opacity-50"
                        title={t("abbrev.rejectHint")}
                      >
                        <IconClose className="h-3.5 w-3.5" />
                        {t("abbrev.reject")}
                      </button>
                    </div>
                  )}
                  <div className="flex items-center justify-end gap-1">
                    <button
                      type="button"
                      onClick={() => openEdit(r)}
                      className="btn btn-ghost btn-sm text-muted hover:text-ink p-1.5"
                      title={t("common.edit")}
                      aria-label={t("common.edit")}
                    >
                      <IconEdit className="h-4 w-4" />
                    </button>
                    <button
                      type="button"
                      onClick={() => setDeleting(r)}
                      className="btn btn-danger btn-sm p-1.5"
                      title={t("common.delete")}
                      aria-label={t("common.delete")}
                    >
                      <IconTrash className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              )}
            </td>
          </>
        )}
      />

      {/* Pagination */}
      {(totalPages > 1 || offset > 0) && (
        <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-muted">
          <span>{t("abbrev.total", { total: shownTotal })}</span>
          <Pager
            page={page}
            pageCount={totalPages}
            loading={loading}
            onPage={(p) => {
              /* The server lists pending rows first, so the next page starts
               * after the rows that are still ahead of the first unseen one. */
              setOffset(nextOffset + (p - page - 1) * PAGE_SIZE);
              setReloadTick((n) => n + 1);
            }}
          />
        </div>
      )}

      {/* Create / Edit modal */}
      <Modal
        open={formOpen}
        title={editing ? t("abbrev.editTitle") : t("abbrev.suggestTitle")}
        onClose={() => setFormOpen(false)}
      >
        <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4">
          <div>
            <FieldLabel htmlFor="abbrev-short-form" required>
              {t("abbrev.shortForm")}
            </FieldLabel>
            <input
              id="abbrev-short-form"
              type="text"
              className="input text-sm"
              value={formShort}
              onChange={(e) => setFormShort(e.target.value)}
              maxLength={50}
              required
              aria-required="true"
              placeholder="BMNN"
            />
          </div>
          <div>
            <FieldLabel htmlFor="abbrev-full-form" required={!formProper}>
              {t("abbrev.fullForm")}
            </FieldLabel>
            <input
              id="abbrev-full-form"
              type="text"
              className="input text-sm"
              value={formProper ? formShort : formFull}
              onChange={(e) => setFormFull(e.target.value)}
              maxLength={255}
              disabled={formProper}
              required={!formProper}
              aria-required={!formProper ? "true" : undefined}
              placeholder={formProper ? t("abbrev.properNameFullPlaceholder") : t("abbrev.fullForm")}
            />
          </div>
          <label className="flex items-start gap-2.5 text-sm text-ink">
            <input
              id="abbrev-proper-name"
              type="checkbox"
              className="mt-0.5 h-4 w-4 accent-primary"
              checked={formProper}
              onChange={(e) => setFormProper(e.target.checked)}
            />
            <span>
              {t("abbrev.properName")}
              <span className="caption block text-muted">{t("abbrev.properNameHint")}</span>
            </span>
          </label>
          <div>
            <FieldLabel htmlFor="abbrev-description">{t("abbrev.description")}</FieldLabel>
            <textarea
              id="abbrev-description"
              className="input text-sm min-h-[72px]"
              value={formDesc}
              onChange={(e) => setFormDesc(e.target.value)}
            />
          </div>
          {editing && canManage && (
            <label className="flex items-center gap-2.5 text-sm text-ink">
              <input
                type="checkbox"
                className="h-4 w-4 accent-primary"
                checked={formActive}
                onChange={(e) => setFormActive(e.target.checked)}
              />
              {t("abbrev.active")}
            </label>
          )}
          {!editing && <p className="caption text-muted">{t("abbrev.suggestNote")}</p>}
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() => setFormOpen(false)}
            >
              {t("common.cancel")}
            </button>
            <button type="submit" className="btn btn-primary btn-sm" disabled={formBusy}>
              {t("common.save")}
            </button>
          </div>
        </form>
      </Modal>

      {/* Delete confirm */}
      <Modal
        open={deleting !== null}
        title={t("abbrev.deleteTitle")}
        onClose={() => setDeleting(null)}
      >
        <div className="space-y-5">
          <p className="text-sm text-ink">
            {deleting &&
              t("abbrev.deleteConfirm", { short: deleting.short_form, full: deleting.full_form })}
          </p>
          <div className="flex justify-end gap-2">
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() => setDeleting(null)}
            >
              {t("common.cancel")}
            </button>
            <button
              type="button"
              className="btn btn-danger btn-sm"
              disabled={actionBusy}
              onClick={() => void handleDelete()}
            >
              {t("common.delete")}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
