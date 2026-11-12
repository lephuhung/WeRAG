"use client";

/* The confirm panel of one pushed link: A's provision | the change | B's
 * provision, side by side with their wording, then Duyệt / Từ chối, or
 * Sửa to correct the amending document or a provision first. The
 * correction picks the document from the catalogue by số hiệu or title and
 * the provision from the held copy's list; only a document whose structure
 * is unknown asks for the provision as typed text. */

import { useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { Notice } from "@/components/settings/notice";
import { FieldLabel } from "@/components/settings/field";
import { useT, type LocaleKey } from "@/lib/i18n";
import { userErrorMessage } from "@/lib/user-error";
import { provisionLabel } from "@/lib/legal-provision-label";
import { opColor } from "@/components/graph/legal-style";
import { searchLegalCatalog, type LegalDocument } from "@/lib/api/legal-review";
import {
  decideLegalLinkCandidate,
  listLegalHeldProvisions,
  type LegalHeldProvisionList,
  type LegalLinkCandidate,
} from "@/lib/api/legal-link-candidates";
import { edgeState, isApprovable, parseProvisionInput } from "@/lib/legal-link-graph";
import { MentionsBox, ProvisionTextBox, docName } from "./provision-text";

function day(v: string | null | undefined): string {
  return v ? v.slice(0, 10).split("-").reverse().join("/") : "";
}

/** The reviewer-task wording of a candidate's check result. */
export function matchTone(s: string): string {
  switch (s) {
    case "matched":
      return "badge-success";
    case "duplicate":
      return "badge-info";
    case "doc_only":
      return "badge-warning";
    default:
      return "badge-error";
  }
}

export function MatchBadge({ c }: { c: LegalLinkCandidate }) {
  const { t } = useT();
  return (
    <span
      className={`badge-pill inline-block w-fit text-[10.5px] ${matchTone(c.match_status)}`}
      title={c.match_reason ? `${t("llink.techDetail")}: ${c.match_reason}` : undefined}
    >
      {t(`llink.match.${c.match_status}` as LocaleKey)}
    </span>
  );
}

export function LinkConfirmPanel({
  c,
  targetHeld,
  position,
  startEditing = false,
  onClose,
  onDecided,
  onSkip,
}: {
  c: LegalLinkCandidate;
  targetHeld: boolean;
  /** "3 / 12": where this link is among those to decide. */
  position?: string;
  startEditing?: boolean;
  onClose: () => void;
  onDecided: (action: "approve" | "reject") => void;
  onSkip?: () => void;
}) {
  const { t, locale } = useT();
  const [editing, setEditing] = useState(startEditing);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    setEditing(startEditing);
    setError("");
  }, [c.id, startEditing]);

  const state = edgeState(c);
  const pending = c.review_status === "pending";
  const approvable = isApprovable(c);
  const targetName = docName(c.target, c.target_doc_id);
  const amenderName = docName(c.amender, c.amender_doc_number);

  const decide = async (action: "approve" | "reject") => {
    setBusy(true);
    setError("");
    try {
      await decideLegalLinkCandidate(c.id, { action });
      onDecided(action);
    } catch (e) {
      setError(userErrorMessage(e, t, "llink.decideFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open title={editing ? t("llink.dialog.title") : t("llink.panel.title")} onClose={onClose} width="w-[1040px]">
      {editing ? (
        <CorrectionForm
          c={c}
          onCancel={() => (startEditing ? onClose() : setEditing(false))}
          onDone={() => onDecided("approve")}
        />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-2">
            <span
              className="badge-pill inline-block text-[11px] font-semibold"
              style={{ color: opColor(c.op), borderColor: opColor(c.op), borderWidth: 1 }}
            >
              {t(`llink.op.${c.op}` as LocaleKey)}
            </span>
            <MatchBadge c={c} />
            {!pending && (
              <span className="caption text-muted">
                {t(`llink.state.${state}` as LocaleKey)}
                {c.reviewed_by ? ` · ${c.reviewed_by === "auto:matched" ? t("llink.autoReviewer") : c.reviewed_by}` : ""}
              </span>
            )}
            {position && <span className="caption ml-auto tabular-nums text-muted">{position}</span>}
          </div>

          <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_220px_minmax(0,1fr)]">
            <section className="flex min-w-0 flex-col gap-2 rounded-[12px] border border-hairline p-3">
              <div className="caption text-muted">{t("llink.col.target")}</div>
              <div className="min-w-0">
                <div className="text-[13.5px] font-semibold text-ink">{provisionLabel(c.target_path, locale) || "—"}</div>
                <div className="caption line-clamp-2 text-body" title={c.target?.title}>
                  {targetName}
                  {c.target?.title ? ` — ${c.target.title}` : ""}
                </div>
              </div>
              <ProvisionTextBox docId={c.target_doc_id} path={c.target_path} docLabel={targetName} />
              {!targetHeld && <p className="caption text-muted">{t("llink.panel.targetFromSource")}</p>}
            </section>

            <section className="flex flex-col gap-2 rounded-[12px] border border-hairline bg-canvas-soft p-3">
              <div className="caption text-muted">{t("llink.panel.relation")}</div>
              <div className="text-[13px] text-ink">
                {t("llink.panel.sentence", {
                  target: provisionLabel(c.target_path, locale) || "—",
                  op: t(`llink.op.${c.op}` as LocaleKey).toLowerCase(),
                })}
              </div>
              {(c.eff_from || c.eff_to) && (
                <div className="caption tabular-nums text-body">
                  {c.eff_from && t("llink.effFrom", { day: day(c.eff_from) })}
                  {c.eff_to && ` · ${t("llink.effTo", { day: day(c.eff_to) })}`}
                </div>
              )}
              {c.note && (
                <div>
                  <div className="caption font-medium text-muted">{t("llink.col.note")}</div>
                  <div className="caption mt-0.5 max-h-40 overflow-y-auto whitespace-pre-wrap text-body">{c.note}</div>
                </div>
              )}
              {c.other_links.length > 0 && (
                <div className="caption text-body">
                  {t("llink.others", { n: c.other_links.length })}:{" "}
                  {c.other_links
                    .slice(0, 3)
                    .map((l) => t(`llink.op.${l.op}` as LocaleKey))
                    .join(", ")}
                </div>
              )}
              <a href={c.feed_url} target="_blank" rel="noreferrer" className="caption mt-auto text-link hover:underline">
                {t("llink.text.sourcePage")}
              </a>
            </section>

            <section className="flex min-w-0 flex-col gap-2 rounded-[12px] border border-hairline p-3">
              <div className="caption text-muted">{t("llink.col.amender")}</div>
              <div className="min-w-0">
                <div className="text-[13.5px] font-semibold text-ink">
                  {c.source_path ? provisionLabel(c.source_path, locale) : t("llink.noPath")}
                </div>
                <div className="caption line-clamp-2 text-body" title={c.amender?.title}>
                  {amenderName}
                  {c.amender?.title ? ` — ${c.amender.title}` : ""}
                </div>
              </div>
              <ProvisionTextBox
                docId={c.amender_doc_id}
                path={c.source_path}
                docLabel={amenderName}
                missingNumber={c.amender_doc_number}
              />
              {!c.source_path && c.amender_doc_id && (
                <MentionsBox candidateId={c.id} targetLabel={provisionLabel(c.target_path, locale)} />
              )}
              {state === "blocked" && c.amender_request && (
                <p className="caption text-muted">
                  {t("llink.panel.request", { state: t(`llink.req.${c.amender_request.status}` as LocaleKey) })}
                </p>
              )}
            </section>
          </div>

          {error && <Notice tone="error">{error}</Notice>}

          <div className="flex flex-wrap items-center justify-end gap-2 border-t border-hairline pt-4">
            {pending && !approvable && <span className="caption mr-auto text-warning">{t("llink.needFix")}</span>}
            {onSkip && pending && (
              <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={onSkip}>
                {t("llink.panel.skip")}
              </button>
            )}
            {pending ? (
              <>
                <button type="button" className="btn btn-danger btn-sm" disabled={busy} onClick={() => void decide("reject")}>
                  {t("llink.reject")}
                </button>
                <button type="button" className="btn btn-outline btn-sm" disabled={busy} onClick={() => setEditing(true)}>
                  {t("llink.edit")}
                </button>
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  disabled={busy || !approvable}
                  title={approvable ? undefined : t("llink.needFix")}
                  onClick={() => void decide("approve")}
                >
                  {busy ? t("common.loading") : t("llink.approve")}
                </button>
              </>
            ) : (
              <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>
                {t("common.close")}
              </button>
            )}
          </div>
        </div>
      )}
    </Modal>
  );
}

/* Pick a provision of a document: from its held copy's list, or typed when
 * its structure is unknown. */
function ProvisionPicker({
  id,
  docId,
  value,
  onChange,
}: {
  id: string;
  docId: string;
  value: string;
  onChange: (path: string, valid: boolean) => void;
}) {
  const { t, locale } = useT();
  const [list, setList] = useState<LegalHeldProvisionList | null>(null);
  const [failed, setFailed] = useState(false);
  const [typed, setTyped] = useState(value ? provisionLabel(value, "vi") : "");

  useEffect(() => {
    let alive = true;
    setList(null);
    setFailed(false);
    if (!docId) return;
    listLegalHeldProvisions(docId).then(
      (r) => alive && setList(r.data),
      () => alive && setFailed(true),
    );
    return () => {
      alive = false;
    };
  }, [docId]);

  if (!docId) return <p className="caption text-muted">{t("llink.f.chooseDocFirst")}</p>;
  if (!list && !failed) return <p className="caption text-muted">{t("common.loading")}</p>;

  if (list && list.held && list.provisions.length > 0) {
    const known = list.provisions.some((p) => p.path === value);
    return (
      <select id={id} className="input h-9 text-[13px]" value={value} onChange={(e) => onChange(e.target.value, true)}>
        {!value && <option value="">{t("llink.f.pick")}</option>}
        {value && !known && <option value={value}>{t("llink.f.notInHeld", { prov: provisionLabel(value, locale) })}</option>}
        {list.provisions.map((p) => (
          <option key={p.path} value={p.path}>
            {provisionLabel(p.path, locale)}
          </option>
        ))}
      </select>
    );
  }
  const parsed = parseProvisionInput(typed);
  return (
    <div className="flex flex-col gap-1">
      <input
        id={id}
        className="input h-9 text-[13px]"
        value={typed}
        placeholder={t("llink.f.typedPlaceholder")}
        onChange={(e) => {
          setTyped(e.target.value);
          const p = parseProvisionInput(e.target.value);
          onChange(p ?? "", !!p || !e.target.value.trim());
        }}
      />
      <span className={`caption ${typed && !parsed ? "text-error" : "text-muted-soft"}`}>
        {typed && !parsed
          ? t("llink.f.typedInvalid")
          : parsed
            ? t("llink.f.typedPreview", { prov: provisionLabel(parsed, locale) })
            : t("llink.f.structureUnknown")}
      </span>
    </div>
  );
}

/* Correct the amending document or a position, then approve. */
function CorrectionForm({ c, onCancel, onDone }: { c: LegalLinkCandidate; onCancel: () => void; onDone: () => void }) {
  const { t } = useT();
  const [amender, setAmender] = useState<{ id: string; label: string } | null>(
    c.amender_doc_id ? { id: c.amender_doc_id, label: docName(c.amender, c.amender_doc_number) } : null,
  );
  const [query, setQuery] = useState(c.amender_doc_id ? "" : c.amender_doc_number);
  const [results, setResults] = useState<LegalDocument[]>([]);
  const [searching, setSearching] = useState(false);
  const [sourcePath, setSourcePath] = useState(c.source_path);
  const [sourceOK, setSourceOK] = useState(true);
  const [targetPath, setTargetPath] = useState(c.target_path);
  const [targetOK, setTargetOK] = useState(true);
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setResults([]);
      return;
    }
    let alive = true;
    const timer = setTimeout(() => {
      setSearching(true);
      searchLegalCatalog(q)
        .then((r) => alive && setResults((r.data ?? []).filter((d) => d.id !== c.target_doc_id).slice(0, 8)))
        .catch(() => alive && setResults([]))
        .finally(() => alive && setSearching(false));
    }, 300);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [query, c.target_doc_id]);

  const changed = useMemo(
    () => (amender?.id ?? "") !== c.amender_doc_id || sourcePath !== c.source_path || targetPath !== c.target_path,
    [amender, sourcePath, targetPath, c],
  );
  const canSave = !!amender && !!targetPath && sourceOK && targetOK;

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      await decideLegalLinkCandidate(c.id, {
        action: "approve",
        amender_doc_id: amender && amender.id !== c.amender_doc_id ? amender.id : undefined,
        source_path: sourcePath && sourcePath !== c.source_path ? sourcePath : undefined,
        target_path: targetPath && targetPath !== c.target_path ? targetPath : undefined,
        note: note.trim() || undefined,
      });
      onDone();
    } catch (e) {
      setError(userErrorMessage(e, t, "llink.decideFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {c.note && (
        <div className="rounded-[12px] border border-hairline bg-canvas-soft p-3">
          <div className="caption font-medium text-muted">{t("llink.col.note")}</div>
          <div className="caption mt-0.5 whitespace-pre-wrap text-body">{c.note}</div>
        </div>
      )}
      <div className="flex flex-col gap-1">
        <FieldLabel htmlFor="llink-amender-search">{t("llink.f.amender")}</FieldLabel>
        {amender && (
          <div className="flex items-center gap-2 rounded-[10px] border border-hairline px-3 py-2">
            <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{amender.label}</span>
            <button type="button" className="btn btn-ghost btn-sm h-7 px-2 text-[12px]" onClick={() => setAmender(null)}>
              {t("llink.f.change")}
            </button>
          </div>
        )}
        {!amender && (
          <>
            <input
              id="llink-amender-search"
              className="input h-9 text-[13px]"
              value={query}
              placeholder={t("llink.f.searchPlaceholder")}
              onChange={(e) => setQuery(e.target.value)}
              autoComplete="off"
            />
            <span className="caption text-muted-soft">{t("llink.f.amenderHint")}</span>
            {searching && <span className="caption text-muted">{t("common.loading")}</span>}
            {results.length > 0 && (
              <ul className="flex max-h-56 flex-col overflow-y-auto rounded-[10px] border border-hairline" role="listbox">
                {results.map((d) => (
                  <li key={d.id}>
                    <button
                      type="button"
                      role="option"
                      aria-selected={false}
                      className="w-full px-3 py-2 text-left hover:bg-surface-strong focus:bg-surface-strong focus:outline-none"
                      onClick={() => {
                        // the provision stays as pushed until another is picked
                        // (the server keeps a path it is not sent)
                        setAmender({ id: d.id, label: docName(d, d.document_number) });
                      }}
                    >
                      <div className="text-[13px] font-medium text-ink">{docName(d, d.document_number)}</div>
                      <div className="caption line-clamp-1 text-body">
                        {d.title}
                        {d.issued_on ? ` · ${day(d.issued_on)}` : ""}
                      </div>
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {!searching && query.trim().length >= 2 && results.length === 0 && (
              <span className="caption text-muted">{t("llink.f.noResults")}</span>
            )}
          </>
        )}
      </div>
      <div className="flex flex-col gap-1">
        <FieldLabel htmlFor="llink-source-path">{t("llink.f.sourcePath")}</FieldLabel>
        <ProvisionPicker
          key={amender?.id ?? ""}
          id="llink-source-path"
          docId={amender?.id ?? ""}
          value={sourcePath}
          onChange={(p, ok) => {
            setSourcePath(p);
            setSourceOK(ok);
          }}
        />
      </div>
      <div className="flex flex-col gap-1">
        <FieldLabel htmlFor="llink-target-path">{t("llink.f.targetPath")}</FieldLabel>
        <ProvisionPicker
          id="llink-target-path"
          docId={c.target_doc_id}
          value={targetPath}
          onChange={(p, ok) => {
            setTargetPath(p);
            setTargetOK(ok);
          }}
        />
      </div>
      <div className="flex flex-col gap-1">
        <FieldLabel htmlFor="llink-note">{t("llink.f.note")}</FieldLabel>
        <textarea
          id="llink-note"
          className="input min-h-[64px] w-full resize-y py-2 text-[13px] leading-relaxed"
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
      </div>
      {error && <Notice tone="error">{error}</Notice>}
      <div className="flex justify-end gap-2 border-t border-hairline pt-4">
        <button className="btn btn-ghost btn-sm" onClick={onCancel} disabled={saving}>
          {t("common.cancel")}
        </button>
        <button className="btn btn-primary btn-sm" onClick={() => void save()} disabled={saving || !canSave}>
          {saving ? t("common.loading") : changed ? t("llink.saveApprove") : t("llink.approve")}
        </button>
      </div>
    </div>
  );
}
