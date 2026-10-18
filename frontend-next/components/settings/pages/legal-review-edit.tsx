"use client";

/* Hand-editing tabs of the legal-review dialog: the document's parsed text
 * (chunks: edit, enable/disable, revision history, LLM spellcheck) and its
 * display title / description. Chunk saves go through the regular chunk
 * API, which re-indexes the chunk; spellcheck only suggests — the reviewer
 * applies a fix to the draft and still presses Save. */

import { useCallback, useEffect, useState } from "react";
import { useT } from "@/lib/i18n";
import { formatDateTime } from "@/lib/datetime";
import { ApiError } from "@/lib/api-client";
import {
  getKnowledgeDetails,
  listChunkRevisions,
  listKnowledgeChunks,
  revertDocumentChunk,
  updateDocumentChunk,
  updateKnowledgeDisplay,
} from "@/lib/api/knowledge";
import { spellcheckLegalReview, type LegalSpellcheckFinding } from "@/lib/api/legal-review";

const CHUNK_PAGE_SIZE = 20;

interface ChunkRow {
  id: string;
  chunk_index: number;
  content: string;
  content_revision: number;
  is_enabled: boolean;
  index_status?: string;
}

interface ChunkRevisionRow {
  id: string;
  revision: number;
  content: string;
  is_enabled: boolean;
  edit_source?: string;
  edited_at?: string;
}

/* A spellcheck finding as shown under its chunk. */
interface Finding extends LegalSpellcheckFinding {
  key: string;
}

function errText(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback;
}

function isConflict(e: unknown): boolean {
  return (
    e instanceof ApiError &&
    (e.status === 409 || /modified by another user/i.test(e.message))
  );
}

export function LegalContentTab({ knowledgeId }: { knowledgeId: string }) {
  const { t } = useT();
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [chunks, setChunks] = useState<ChunkRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  /* Drafts of chunks in edit mode, by chunk ID. */
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [findings, setFindings] = useState<Record<string, Finding[]>>({});
  const [checked, setChecked] = useState(false);
  const [checking, setChecking] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = (await listKnowledgeChunks(knowledgeId, page, { pageSize: CHUNK_PAGE_SIZE })) as {
        data?: ChunkRow[];
        total?: number;
      };
      setChunks(res.data ?? []);
      setTotal(res.total ?? 0);
    } catch (e) {
      setError(errText(e, t("lrev.loadFailed")));
    } finally {
      setLoading(false);
    }
  }, [knowledgeId, page, t]);

  useEffect(() => {
    setDrafts({});
    setFindings({});
    setChecked(false);
    void load();
  }, [load]);

  const replaceChunk = (row: ChunkRow) =>
    setChunks((prev) => prev.map((c) => (c.id === row.id ? { ...c, ...row } : c)));

  const spellcheck = async () => {
    setChecking(true);
    setError("");
    try {
      const items = chunks
        .filter((c) => c.is_enabled || drafts[c.id] !== undefined)
        .map((c) => ({ chunk_id: c.id, content: drafts[c.id] ?? c.content }));
      const res = await spellcheckLegalReview(knowledgeId, items);
      const byChunk: Record<string, Finding[]> = {};
      (res.data ?? []).forEach((f, i) => {
        (byChunk[f.chunk_id] ??= []).push({ ...f, key: `${f.chunk_id}-${i}` });
      });
      setFindings(byChunk);
      setChecked(true);
    } catch (e) {
      setError(errText(e, t("lrev.c.spellFailed")));
    } finally {
      setChecking(false);
    }
  };

  const pages = Math.max(1, Math.ceil(total / CHUNK_PAGE_SIZE));
  const findingCount = Object.values(findings).reduce((n, l) => n + l.length, 0);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="caption text-muted">{t("lrev.c.note")}</span>
        <button
          type="button"
          className="btn btn-outline btn-sm ml-auto"
          onClick={() => void spellcheck()}
          disabled={checking || loading || chunks.length === 0}
        >
          {checking ? t("lrev.c.spellRunning") : t("lrev.c.spellPage")}
        </button>
      </div>
      {checked && !checking && (
        <p className={`caption ${findingCount ? "text-amber-700 dark:text-amber-400" : "text-emerald-700 dark:text-emerald-400"}`}>
          {findingCount ? t("lrev.c.spellFound", { count: findingCount }) : t("lrev.c.spellNone")}
        </p>
      )}
      {error && <p className="caption text-error">{error}</p>}

      <div className="flex flex-col gap-2">
        {chunks.map((c) => (
          <ChunkCard
            key={c.id}
            knowledgeId={knowledgeId}
            chunk={c}
            draft={drafts[c.id]}
            findings={findings[c.id] ?? []}
            onDraft={(v) =>
              setDrafts((d) => {
                const next = { ...d };
                if (v === undefined) delete next[c.id];
                else next[c.id] = v;
                return next;
              })
            }
            onFindings={(l) => setFindings((f) => ({ ...f, [c.id]: l }))}
            onSaved={replaceChunk}
            onReload={load}
          />
        ))}
        {chunks.length === 0 && (
          <p className="py-8 text-center text-[13px] text-muted">
            {loading ? t("usrp.loading") : t("lrev.c.empty")}
          </p>
        )}
      </div>

      {pages > 1 && (
        <div className="flex items-center justify-center gap-3">
          <button
            type="button"
            className="btn btn-outline btn-sm"
            disabled={page <= 1 || loading}
            onClick={() => setPage(page - 1)}
          >
            ‹
          </button>
          <span className="caption tabular-nums text-muted">
            {page} / {pages}
          </span>
          <button
            type="button"
            className="btn btn-outline btn-sm"
            disabled={page >= pages || loading}
            onClick={() => setPage(page + 1)}
          >
            ›
          </button>
        </div>
      )}
    </div>
  );
}

function ChunkCard({
  knowledgeId,
  chunk,
  draft,
  findings,
  onDraft,
  onFindings,
  onSaved,
  onReload,
}: {
  knowledgeId: string;
  chunk: ChunkRow;
  draft: string | undefined;
  findings: Finding[];
  onDraft: (v: string | undefined) => void;
  onFindings: (l: Finding[]) => void;
  onSaved: (row: ChunkRow) => void;
  /* Reloads the page after a conflict so the reviewer sees the latest text. */
  onReload: () => Promise<void>;
}) {
  const { t, locale } = useT();
  const editing = draft !== undefined;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [history, setHistory] = useState<ChunkRevisionRow[] | null>(null);
  const text = draft ?? chunk.content;

  const save = async (patch: { content?: string; is_enabled?: boolean }) => {
    setBusy(true);
    setError("");
    try {
      const res = (await updateDocumentChunk(knowledgeId, chunk.id, {
        ...patch,
        expected_revision: chunk.content_revision,
      })) as { data?: ChunkRow };
      if (res.data) onSaved(res.data);
      if (patch.content !== undefined) onDraft(undefined);
      if (history) setHistory(null);
    } catch (e) {
      if (isConflict(e)) {
        setError(t("lrev.c.conflict"));
        await onReload();
      } else {
        setError(errText(e, t("lrev.c.saveFailed")));
      }
    } finally {
      setBusy(false);
    }
  };

  const toggleHistory = async () => {
    if (history) {
      setHistory(null);
      return;
    }
    setError("");
    try {
      const res = (await listChunkRevisions(knowledgeId, chunk.id)) as { data?: ChunkRevisionRow[] };
      setHistory(res.data ?? []);
    } catch (e) {
      setError(errText(e, t("lrev.loadFailed")));
    }
  };

  const revert = async (revision: number) => {
    setBusy(true);
    setError("");
    try {
      const res = (await revertDocumentChunk(knowledgeId, chunk.id, revision, chunk.content_revision)) as {
        data?: ChunkRow;
      };
      if (res.data) onSaved(res.data);
      onDraft(undefined);
      setHistory(null);
    } catch (e) {
      if (isConflict(e)) {
        setError(t("lrev.c.conflict"));
        await onReload();
      } else {
        setError(errText(e, t("lrev.c.saveFailed")));
      }
    } finally {
      setBusy(false);
    }
  };

  /* Replace the first occurrence of a finding in the draft (edit mode
   * opens with it); the reviewer still saves. */
  const applyOne = (base: string, f: Finding): string | null => {
    const i = base.indexOf(f.wrong);
    if (i < 0) return null;
    return base.slice(0, i) + f.correct + base.slice(i + f.wrong.length);
  };
  const apply = (f: Finding) => {
    const next = applyOne(text, f);
    if (next === null) return;
    onDraft(next);
    onFindings(findings.filter((x) => x.key !== f.key));
  };
  const applyAll = () => {
    let next = text;
    const left: Finding[] = [];
    for (const f of findings) {
      const r = applyOne(next, f);
      if (r === null) left.push(f);
      else next = r;
    }
    onDraft(next);
    onFindings(left);
  };

  return (
    <div
      className={`rounded-[12px] border p-3 ${
        chunk.is_enabled ? "border-hairline" : "border-dashed border-hairline bg-surface-strong/30"
      }`}
    >
      <div className="mb-1.5 flex flex-wrap items-center gap-2">
        <span className="caption font-mono text-muted">#{chunk.chunk_index}</span>
        {chunk.content_revision > 0 && (
          <span className="caption text-muted">{t("lrev.c.revision", { n: chunk.content_revision })}</span>
        )}
        {!chunk.is_enabled && <span className="caption text-amber-700 dark:text-amber-400">{t("lrev.c.disabled")}</span>}
        {chunk.index_status === "processing" && (
          <span className="caption text-muted">{t("lrev.c.reindexing")}</span>
        )}
        {chunk.index_status === "failed" && <span className="caption text-error">{t("lrev.c.indexFailed")}</span>}
        <div className="ml-auto flex flex-wrap gap-1.5">
          {!editing && (
            <button type="button" className="btn btn-outline btn-sm py-0.5 text-[12px]" onClick={() => onDraft(chunk.content)} disabled={busy}>
              {t("lrev.c.edit")}
            </button>
          )}
          <button
            type="button"
            className="btn btn-outline btn-sm py-0.5 text-[12px]"
            onClick={() => void save({ is_enabled: !chunk.is_enabled })}
            disabled={busy || editing}
            title={chunk.is_enabled ? t("lrev.c.disableHint") : undefined}
          >
            {chunk.is_enabled ? t("lrev.c.disable") : t("lrev.c.enable")}
          </button>
          <button type="button" className="btn btn-ghost btn-sm py-0.5 text-[12px]" onClick={() => void toggleHistory()} disabled={busy}>
            {history ? t("lrev.c.hideHistory") : t("lrev.c.history")}
          </button>
        </div>
      </div>

      {editing ? (
        <div className="flex flex-col gap-2">
          <textarea
            className="input min-h-[160px] w-full resize-y py-2 text-[13px] leading-relaxed"
            value={draft}
            onChange={(e) => onDraft(e.target.value)}
          />
          <div className="flex justify-end gap-2">
            <button type="button" className="btn btn-ghost btn-sm" onClick={() => onDraft(undefined)} disabled={busy}>
              {t("common.cancel")}
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={() => void save({ content: draft })}
              disabled={busy || draft === chunk.content || !draft?.trim()}
            >
              {busy ? "…" : t("lrev.c.save")}
            </button>
          </div>
        </div>
      ) : (
        <p className={`whitespace-pre-wrap break-words text-[13px] leading-relaxed ${chunk.is_enabled ? "text-body" : "text-muted"}`}>
          {chunk.content.trim()}
        </p>
      )}

      {findings.length > 0 && (
        <div className="mt-2 rounded-[10px] bg-amber-500/5 p-2">
          <div className="mb-1 flex items-center gap-2">
            <span className="caption text-amber-700 dark:text-amber-400">{t("lrev.c.findings", { count: findings.length })}</span>
            <button type="button" className="btn btn-outline btn-sm ml-auto py-0.5 text-[12px]" onClick={applyAll}>
              {t("lrev.c.applyAll")}
            </button>
          </div>
          <ul className="flex flex-col gap-1">
            {findings.map((f) => {
              const stale = !text.includes(f.wrong);
              return (
                <li key={f.key} className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px]">
                  <span className={stale ? "text-muted line-through" : "text-red-700 dark:text-red-400"}>“{f.wrong}”</span>
                  <span className="text-muted">→</span>
                  <span className="font-medium text-emerald-700 dark:text-emerald-400">“{f.correct}”</span>
                  {f.reason && <span className="text-muted">· {f.reason}</span>}
                  {stale && <span className="caption text-muted">({t("lrev.c.stale")})</span>}
                  <span className="ml-auto flex gap-1">
                    <button
                      type="button"
                      className="btn btn-outline btn-sm py-0 text-[11px]"
                      onClick={() => apply(f)}
                      disabled={stale}
                    >
                      {t("lrev.c.fix")}
                    </button>
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm py-0 text-[11px]"
                      onClick={() => onFindings(findings.filter((x) => x.key !== f.key))}
                    >
                      {t("lrev.c.dismiss")}
                    </button>
                  </span>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {history && (
        <div className="mt-2 rounded-[10px] border border-hairline p-2">
          {history.length === 0 ? (
            <p className="caption text-muted">{t("lrev.c.noHistory")}</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {[...history]
                .sort((a, b) => b.revision - a.revision)
                .map((r) => (
                  <li key={r.id} className="flex flex-col gap-1">
                    <div className="flex items-center gap-2">
                      <span className="caption text-muted">
                        {t("lrev.c.revision", { n: r.revision })}
                        {r.edited_at ? ` · ${formatDateTime(r.edited_at, locale)}` : ""}
                        {r.revision === chunk.content_revision ? ` · ${t("lrev.c.current")}` : ""}
                      </span>
                      {r.revision !== chunk.content_revision && (
                        <button
                          type="button"
                          className="btn btn-outline btn-sm ml-auto py-0 text-[11px]"
                          onClick={() => void revert(r.revision)}
                          disabled={busy}
                        >
                          {t("lrev.c.restore")}
                        </button>
                      )}
                    </div>
                    <p className="line-clamp-3 whitespace-pre-wrap text-[12px] text-muted">{r.content}</p>
                  </li>
                ))}
            </ul>
          )}
        </div>
      )}

      {error && <p className="caption mt-1.5 text-error">{error}</p>}
    </div>
  );
}

export function LegalDisplayTab({ knowledgeId, initialTitle }: { knowledgeId: string; initialTitle: string }) {
  const { t } = useT();
  const [title, setTitle] = useState(initialTitle);
  const [description, setDescription] = useState("");
  const [orig, setOrig] = useState({ title: initialTitle, description: "" });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const res = (await getKnowledgeDetails(knowledgeId)) as { data?: { title?: string; description?: string } };
        if (!alive) return;
        const next = { title: res.data?.title ?? initialTitle, description: res.data?.description ?? "" };
        setTitle(next.title);
        setDescription(next.description);
        setOrig(next);
      } catch (e) {
        if (alive) setError(errText(e, t("lrev.loadFailed")));
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [knowledgeId, initialTitle, t]);

  const save = async () => {
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const patch: { title?: string; description?: string } = {};
      if (title.trim() !== orig.title) patch.title = title.trim();
      if (description !== orig.description) patch.description = description;
      await updateKnowledgeDisplay(knowledgeId, patch);
      setOrig({ title: title.trim(), description });
      setNotice(t("lrev.d.saved"));
    } catch (e) {
      setError(errText(e, t("lrev.c.saveFailed")));
    } finally {
      setSaving(false);
    }
  };

  const dirty = title.trim() !== orig.title || description !== orig.description;

  return (
    <div className="flex flex-col gap-3">
      <span className="caption text-muted">{t("lrev.d.note")}</span>
      <label className="flex flex-col gap-1">
        <span className="caption text-muted">{t("lrev.d.title")}</span>
        <input className="input h-9 text-[13px]" value={title} onChange={(e) => setTitle(e.target.value)} disabled={loading} />
      </label>
      <label className="flex flex-col gap-1">
        <span className="caption text-muted">{t("lrev.d.description")}</span>
        <textarea
          className="input min-h-[180px] w-full resize-y py-2 text-[13px] leading-relaxed"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          disabled={loading}
        />
      </label>
      {notice && <p className="caption text-emerald-700 dark:text-emerald-400">{notice}</p>}
      {error && <p className="caption text-error">{error}</p>}
      <div className="flex justify-end">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => void save()}
          disabled={saving || loading || !dirty || !title.trim()}
        >
          {saving ? "…" : t("lrev.d.save")}
        </button>
      </div>
    </div>
  );
}
