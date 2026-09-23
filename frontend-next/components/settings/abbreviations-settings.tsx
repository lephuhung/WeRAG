/* Global Vietnamese abbreviation dictionary (AIRAG port): the agent expands
 * these short forms via the resolve_abbreviation tool before searching. Any
 * member can suggest entries; a workspace owner (or system admin) edits,
 * activates and deletes — the server enforces the same split. */
"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconEdit, IconPlus, IconRefresh, IconSearch, IconTrash } from "@/components/icons";
import {
  listAbbreviations,
  createAbbreviation,
  updateAbbreviation,
  deleteAbbreviation,
  type Abbreviation,
} from "@/lib/api/abbreviations";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";

const PAGE_SIZE = 20;
type StatusFilter = "all" | "active" | "pending";

function fmtDate(v?: string): string {
  if (!v) return "-";
  try {
    return new Date(v).toLocaleDateString();
  } catch {
    return v;
  }
}

export function AbbreviationsSettings() {
  const { t } = useT();
  const auth = useAuth();

  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const canManage = currentRole === "owner" || isSystemAdmin;

  const [rows, setRows] = useState<Abbreviation[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Abbreviation | null>(null);
  const [formShort, setFormShort] = useState("");
  const [formFull, setFormFull] = useState("");
  const [formDesc, setFormDesc] = useState("");
  const [formActive, setFormActive] = useState(false);
  const [formBusy, setFormBusy] = useState(false);

  const [deleting, setDeleting] = useState<Abbreviation | null>(null);
  const [actionBusy, setActionBusy] = useState(false);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listAbbreviations({
        search: search || undefined,
        isActive: statusFilter === "all" ? undefined : statusFilter === "active",
        page,
        pageSize: PAGE_SIZE,
      });
      setRows(res.data ?? []);
      setTotal(res.total ?? 0);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load abbreviations");
    } finally {
      setLoading(false);
    }
  }, [search, statusFilter, page]);

  useEffect(() => {
    void load();
  }, [load]);

  // Debounce the free-text search; each filter change restarts at page 1.
  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(searchInput.trim());
      setPage(1);
    }, 300);
    return () => clearTimeout(timer);
  }, [searchInput]);

  const openCreate = () => {
    setEditing(null);
    setFormShort("");
    setFormFull("");
    setFormDesc("");
    setFormActive(false);
    setFormOpen(true);
  };

  const openEdit = (row: Abbreviation) => {
    setEditing(row);
    setFormShort(row.short_form);
    setFormFull(row.full_form);
    setFormDesc(row.description);
    setFormActive(row.is_active);
    setFormOpen(true);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const short = formShort.trim();
    const full = formFull.trim();
    if (!short || !full) return;
    setFormBusy(true);
    setError("");
    setSuccess("");
    try {
      if (editing) {
        const res = await updateAbbreviation(editing.id, {
          short_form: short,
          full_form: full,
          description: formDesc.trim(),
          is_active: formActive,
        });
        if (res.success) {
          setFormOpen(false);
          await load();
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
        });
        if (res.success) {
          setSuccess(t("abbrev.suggestSaved"));
          setFormOpen(false);
          await load();
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

  const handleToggleActive = async (row: Abbreviation) => {
    if (!canManage) return;
    setError("");
    setSuccess("");
    try {
      const res = await updateAbbreviation(row.id, { is_active: !row.is_active });
      if (res.success) {
        await load();
      } else {
        setError(t("abbrev.saveFailed"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("abbrev.saveFailed"));
    }
  };

  const handleDelete = async () => {
    if (!deleting) return;
    setActionBusy(true);
    setError("");
    try {
      const res = await deleteAbbreviation(deleting.id);
      if (res.success) {
        setDeleting(null);
        await load();
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
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">{t("abbrev.title")}</h2>
          <p className="caption text-muted mt-1 max-w-2xl">{t("abbrev.desc")}</p>
        </div>
        <div className="flex items-center gap-2">
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
            title={t("common.loading")}
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

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

      {/* Search + status filter */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="relative w-full max-w-sm">
          <IconSearch className="absolute left-3 top-1/2 -translate-y-1/2 text-muted h-4 w-4" />
          <input
            type="text"
            className="input pl-9 text-sm"
            placeholder={t("abbrev.search")}
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
          />
        </div>
        <div className="flex items-center gap-1.5">
          {filterChips.map((f) => (
            <button
              key={f.id}
              type="button"
              onClick={() => {
                setStatusFilter(f.id);
                setPage(1);
              }}
              className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                statusFilter === f.id
                  ? "bg-ink text-white dark:bg-white dark:text-ink"
                  : "text-muted hover:bg-surface-strong hover:text-ink"
              }`}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {/* Table */}
      <div className="overflow-hidden rounded-xl border border-hairline">
        <table className="w-full text-left border-collapse text-sm">
          <thead>
            <tr className="border-b border-hairline bg-surface-strong/50 text-xs font-semibold text-muted">
              <th className="py-3 px-4 w-36">{t("abbrev.shortForm")}</th>
              <th className="py-3 px-4">{t("abbrev.fullForm")}</th>
              <th className="py-3 px-4">{t("abbrev.description")}</th>
              <th className="py-3 px-4 w-36">{t("abbrev.status")}</th>
              <th className="py-3 px-4 w-28">{t("abbrev.createdAt")}</th>
              <th className="py-3 px-4 w-24 text-right">{t("abbrev.actions")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-hairline">
            {loading ? (
              <tr>
                <td colSpan={6} className="py-12 text-center text-muted">
                  {t("common.loading")}
                </td>
              </tr>
            ) : rows.length === 0 ? (
              <tr>
                <td colSpan={6} className="py-12 text-center text-muted">
                  {t("abbrev.empty")}
                </td>
              </tr>
            ) : (
              rows.map((r) => (
                <tr key={r.id} className="hover:bg-surface-strong/30 transition-colors">
                  <td className="py-3 px-4">
                    <code className="rounded bg-surface-strong px-1.5 py-0.5 text-[12.5px] font-semibold text-ink">
                      {r.short_form}
                    </code>
                  </td>
                  <td className="py-3 px-4 text-ink">{r.full_form}</td>
                  <td className="py-3 px-4 text-muted text-xs">{r.description || "—"}</td>
                  <td className="py-3 px-4">
                    {canManage ? (
                      <button
                        type="button"
                        onClick={() => void handleToggleActive(r)}
                        title={r.is_active ? t("abbrev.deactivate") : t("abbrev.activate")}
                        className={`badge-pill text-[11px] font-semibold cursor-pointer ${
                          r.is_active
                            ? "bg-emerald-500/10 text-emerald-600 border border-emerald-500/20"
                            : "bg-amber-500/10 text-amber-600 border border-amber-500/20"
                        }`}
                      >
                        {r.is_active ? t("abbrev.active") : t("abbrev.pending")}
                      </button>
                    ) : (
                      <span
                        className={`badge-pill text-[11px] font-semibold ${
                          r.is_active
                            ? "bg-emerald-500/10 text-emerald-600 border border-emerald-500/20"
                            : "bg-amber-500/10 text-amber-600 border border-amber-500/20"
                        }`}
                      >
                        {r.is_active ? t("abbrev.active") : t("abbrev.pending")}
                      </span>
                    )}
                  </td>
                  <td className="py-3 px-4 text-muted text-xs">{fmtDate(r.created_at)}</td>
                  <td className="py-3 px-4 text-right">
                    {canManage && (
                      <div className="flex items-center justify-end gap-1">
                        <button
                          type="button"
                          onClick={() => openEdit(r)}
                          className="btn btn-ghost btn-sm text-muted hover:text-ink p-1.5"
                          title={t("common.edit")}
                        >
                          <IconEdit className="h-4 w-4" />
                        </button>
                        <button
                          type="button"
                          onClick={() => setDeleting(r)}
                          className="btn btn-ghost btn-sm text-muted hover:text-rose-600 p-1.5"
                          title={t("common.delete")}
                        >
                          <IconTrash className="h-4 w-4" />
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between text-xs text-muted">
          <span>{t("abbrev.total", { total })}</span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              className="btn btn-outline btn-sm text-xs py-1 disabled:opacity-50"
            >
              ‹
            </button>
            <span>
              {page} / {totalPages}
            </span>
            <button
              type="button"
              disabled={page >= totalPages}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              className="btn btn-outline btn-sm text-xs py-1 disabled:opacity-50"
            >
              ›
            </button>
          </div>
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
            <label className="caption mb-1.5 block text-muted">{t("abbrev.shortForm")}</label>
            <input
              type="text"
              className="input text-sm"
              value={formShort}
              onChange={(e) => setFormShort(e.target.value)}
              maxLength={50}
              required
              placeholder="BMNN"
            />
          </div>
          <div>
            <label className="caption mb-1.5 block text-muted">{t("abbrev.fullForm")}</label>
            <input
              type="text"
              className="input text-sm"
              value={formFull}
              onChange={(e) => setFormFull(e.target.value)}
              maxLength={255}
              required
              placeholder={t("abbrev.fullForm")}
            />
          </div>
          <div>
            <label className="caption mb-1.5 block text-muted">{t("abbrev.description")}</label>
            <textarea
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
              className="btn btn-primary btn-sm bg-rose-600 border-rose-600 hover:bg-rose-700"
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
