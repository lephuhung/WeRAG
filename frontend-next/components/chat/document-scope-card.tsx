/* The scope clarification card (document assistant): the backend answers a
 * generic request about a long document with this card instead of reading
 * it blindly. The user picks a task, the document when several and, for a
 * summary or one part, the sections; confirming stores a user scope and
 * re-sends the question (see scopeCardAction). The scope chip above the
 * composer opens the same card to change the scope.
 */
"use client";

import { useMemo, useState } from "react";
import { useT } from "@/lib/i18n";
import {
  SCOPE_MAX_SECTIONS,
  estimatedPages,
  initialScopeChoice,
  scopeCardAction,
  scopeChoiceReady,
  taskTakesSections,
  type ScopeCardAction,
  type ScopeCardChoice,
  type ScopeCardTask,
  type ScopeClarification,
} from "@/lib/document-scope";

const TASK_KEYS: Record<ScopeCardTask, Parameters<ReturnType<typeof useT>["t"]>[0]> = {
  format: "docws.scopeCard.task.format",
  spelling: "docws.scopeCard.task.spelling",
  summary: "docws.scopeCard.task.summary",
  part: "docws.scopeCard.task.part",
  compare: "docws.scopeCard.task.compare",
  other: "docws.scopeCard.task.other",
};

export function DocumentScopeCard({
  card,
  disabled = false,
  onAction,
  onCancel,
}: {
  card: ScopeClarification;
  /** A turn is running: the card can be read but not confirmed. */
  disabled?: boolean;
  /** Runs the confirmed action; a rejection keeps the card open with an error. */
  onAction: (action: ScopeCardAction) => Promise<void> | void;
  /** Shown when opened from the scope chip. */
  onCancel?: () => void;
}) {
  const { t } = useT();
  const [choice, setChoice] = useState<ScopeCardChoice>(() => initialScopeChoice(card));
  const [working, setWorking] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const doc = card.documents.find((d) => d.id === choice.documentId) ?? card.documents[0];
  const status = useMemo(() => {
    const long = card.documents.filter((d) => d.long);
    if (long.length === 0) return t("docws.scopeCard.changeTitle");
    return long
      .map((d) => {
        const name = d.handle ? `${d.handle} · ${d.file_name}` : d.file_name;
        const pages = estimatedPages(d.runes);
        return d.role === "source"
          ? t("docws.scopeCard.statusSource", { name, pages })
          : t("docws.scopeCard.status", { name, paragraphs: d.paragraphs, pages });
      })
      .join(" ");
  }, [card.documents, t]);

  const pickTask = (task: ScopeCardTask) => {
    setError(null);
    setChoice((c) => ({ ...c, task }));
  };
  const pickDocument = (documentId: string) => {
    setError(null);
    setChoice((c) => (c.documentId === documentId ? c : { ...c, documentId, sections: [] }));
  };
  const toggleSection = (i: number) =>
    setChoice((c) => {
      if (c.sections.includes(i)) return { ...c, sections: c.sections.filter((x) => x !== i) };
      if (c.sections.length >= SCOPE_MAX_SECTIONS) return c;
      return { ...c, sections: [...c.sections, i] };
    });

  const confirm = async () => {
    const action = scopeCardAction(card, choice);
    if (!action) return;
    setWorking(true);
    setError(null);
    try {
      await onAction(action);
      setDone(action.kind !== "compose");
    } catch (e) {
      setError(`${t("docws.scopeCard.failed")}: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setWorking(false);
    }
  };

  if (done) {
    return <p className="caption mt-2 text-muted-soft">{t("docws.scopeCard.done")}</p>;
  }

  const showSections = taskTakesSections(choice.task);
  const ready = scopeChoiceReady(card, choice);
  const chip = (active: boolean) =>
    `rounded-full border px-3 py-1 text-[12.5px] transition-colors ${
      active ? "border-ink bg-surface-strong text-ink" : "border-hairline bg-surface-card text-body hover:border-ink hover:text-ink"
    } disabled:cursor-default disabled:opacity-60`;

  return (
    <div className="mt-3 w-full min-w-0 rounded-[10px] border border-hairline bg-surface-card p-3">
      <p className="text-[13px] leading-relaxed text-body">{status}</p>

      <div className="mt-2.5 flex flex-wrap gap-1.5" role="radiogroup" aria-label={t("docws.scopeCard.taskLabel")}>
        {card.tasks.map((task) => (
          <button
            key={task.key}
            type="button"
            role="radio"
            aria-checked={choice.task === task.key}
            disabled={working}
            onClick={() => pickTask(task.key)}
            className={chip(choice.task === task.key)}
          >
            {t(TASK_KEYS[task.key])}
          </button>
        ))}
      </div>

      {card.documents.length > 1 && choice.task !== "other" && (
        <div className="mt-2.5 flex min-w-0 flex-wrap items-center gap-1.5">
          <span className="caption-uppercase shrink-0 text-muted-soft">{t("docws.scopeCard.document")}</span>
          {card.documents.map((d) => (
            <button
              key={d.id}
              type="button"
              disabled={working}
              onClick={() => pickDocument(d.id)}
              title={d.file_name}
              className={`${chip(d.id === doc.id)} max-w-[240px] truncate`}
            >
              {d.handle ? `${d.handle} · ${d.file_name}` : d.file_name}
            </button>
          ))}
        </div>
      )}

      {showSections && (
        <div className="mt-2.5">
          <div className="flex items-baseline justify-between gap-2">
            <span className="caption-uppercase text-muted-soft">{t("docws.scopeCard.sections")}</span>
            <span className="caption text-muted-soft">
              {choice.task === "part"
                ? t("docws.scopeCard.sectionsMax", { n: SCOPE_MAX_SECTIONS })
                : t("docws.scopeCard.sectionsHintWhole")}
            </span>
          </div>
          {doc.sections.length === 0 ? (
            <p className="caption mt-1 text-muted-soft">{t("docws.scopeCard.noSections")}</p>
          ) : (
            <ul className="mt-1 max-h-48 overflow-y-auto pr-1">
              {doc.sections.map((s, i) => (
                <li key={`${s.from}-${s.to}-${i}`}>
                  <label className="flex cursor-pointer items-start gap-2 py-0.5 text-[12.5px] text-body">
                    <input
                      type="checkbox"
                      className="mt-0.5"
                      checked={choice.sections.includes(i)}
                      disabled={working || (!choice.sections.includes(i) && choice.sections.length >= SCOPE_MAX_SECTIONS)}
                      onChange={() => toggleSection(i)}
                    />
                    <span className="min-w-0 flex-1">
                      {s.title || t("docws.scopeCard.untitled")}
                      <span className="ml-1 text-muted-soft">
                        [{s.from}–{s.to}]
                      </span>
                    </span>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {error && <p className="caption mt-2 text-error">{error}</p>}

      <div className="mt-3 flex items-center justify-end gap-2">
        {onCancel && (
          <button type="button" className="btn btn-outline btn-sm" disabled={working} onClick={onCancel}>
            {t("docws.scopeCard.cancel")}
          </button>
        )}
        <button
          type="button"
          className="btn btn-primary btn-sm"
          disabled={!ready || working || disabled}
          onClick={() => void confirm()}
        >
          {t("docws.scopeCard.confirm")}
        </button>
      </div>
    </div>
  );
}
