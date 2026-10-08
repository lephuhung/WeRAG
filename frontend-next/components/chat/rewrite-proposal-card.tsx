/* The rewrite proposal card (document assistant): rewrite_paragraphs on a
 * highlighted passage proposes one or more versions instead of changing the
 * document. The card shows the original passage and each version; "Thay vào
 * văn bản" asks the parent to snapshot and apply that version in the editor
 * (the outcome comes back through `outcome`), "Bỏ qua" folds the card.
 * Only the newest proposal of a document is `active`; older ones are
 * read-only. The applied version survives reloads via the parent's ledger.
 */
"use client";

import { useEffect, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import {
  proposalBatchId,
  proposalOutcomeApplied,
  type OpsFailure,
  type RewriteProposal,
  type RewriteVariant,
} from "@/lib/api/document-ops";

/** The editor's report on one applied batch (see use-editor-ops). */
export type ProposalOutcome =
  | { kind: "result"; batchId: string; applied: number; failed: OpsFailure[] }
  | { kind: "timeout"; batchId: string };

/** No outcome after this long (editor not ready, document reloaded): give up. */
const OUTCOME_TIMEOUT_MS = 45_000;

export function RewriteProposalCard({
  proposal,
  appliedVariantId,
  active,
  disabled = false,
  outcomeFor,
  onApply,
  onApplied,
}: {
  proposal: RewriteProposal;
  /** The version the user applied earlier (message state / ledger). */
  appliedVariantId?: string;
  /** The newest proposal of its document: the only one that can apply. */
  active: boolean;
  /** A turn is running: readable, not applicable. */
  disabled?: boolean;
  /** The editor's outcome for a version's batch id, once it arrived. */
  outcomeFor: (batchId: string) => ProposalOutcome | undefined;
  /** Snapshots and sends the version to the editor; rejects when it could not. */
  onApply: (proposal: RewriteProposal, variant: RewriteVariant) => Promise<void>;
  /** The version landed in the document. */
  onApplied: (proposal: RewriteProposal, variantId: string) => void;
}) {
  const { t } = useT();
  const [pending, setPending] = useState<string | null>(null);
  const [failedIds, setFailedIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState(false);
  const startedRef = useRef(0);

  const pendingOutcome = pending ? outcomeFor(proposalBatchId(proposal.batchId, pending)) : undefined;
  useEffect(() => {
    if (!pending || !pendingOutcome) return;
    if (proposalOutcomeApplied(pendingOutcome)) {
      onApplied(proposal, pending);
    } else {
      setFailedIds((ids) => [...ids, pending]);
      setError(
        pendingOutcome.kind === "timeout" ? t("docws.proposal.timeout") : t("docws.proposal.notFound"),
      );
    }
    setPending(null);
  }, [pending, pendingOutcome, proposal, onApplied, t]);

  // Fallback when the editor never reports (document replaced, tab closed).
  useEffect(() => {
    if (!pending) return;
    const started = startedRef.current;
    const timer = setTimeout(() => {
      if (startedRef.current !== started) return;
      setFailedIds((ids) => [...ids, pending]);
      setError(t("docws.proposal.timeout"));
      setPending(null);
    }, OUTCOME_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [pending, t]);

  const apply = async (variant: RewriteVariant) => {
    setError(null);
    startedRef.current += 1;
    setPending(variant.id);
    try {
      await onApply(proposal, variant);
    } catch (e) {
      setPending(null);
      setError(`${t("docws.proposal.failed")}: ${e instanceof Error ? e.message : String(e)}`);
    }
  };

  const applied = appliedVariantId ? proposal.variants.find((v) => v.id === appliedVariantId) : undefined;
  const live = active && !applied && !dismissed;
  const original = proposal.variants[0]?.old || proposal.selectionText;

  if (dismissed) {
    return <p className="caption mt-2 text-muted-soft">{t("docws.proposal.dismissed")}</p>;
  }

  return (
    <div className="mt-3 w-full min-w-0 rounded-[10px] border border-hairline bg-surface-card p-3">
      <div className="flex items-baseline justify-between gap-2">
        <span className="caption-uppercase text-muted-soft">{t("docws.proposal.title")}</span>
        {proposal.document && (
          <span className="caption min-w-0 truncate text-muted-soft" title={proposal.document}>
            {proposal.document}
          </span>
        )}
      </div>

      {original && (
        <div className="mt-2">
          <span className="caption text-muted-soft">{t("docws.proposal.original")}</span>
          <p className="mt-0.5 whitespace-pre-wrap rounded-md bg-surface-strong px-2.5 py-1.5 text-[13px] leading-relaxed text-body">
            {original}
          </p>
        </div>
      )}

      <ul className="mt-2 space-y-2">
        {proposal.variants.map((v) => {
          const isApplied = applied?.id === v.id;
          return (
            <li
              key={v.id}
              className={`rounded-md border px-2.5 py-2 ${isApplied ? "border-ink" : "border-hairline"}`}
            >
              <span className="caption text-muted-soft">{v.label}</span>
              <p className="mt-0.5 whitespace-pre-wrap text-[13px] leading-relaxed text-ink">{v.new}</p>
              {isApplied && (
                <p className="caption mt-1.5 text-muted-soft">{t("docws.proposal.applied")}</p>
              )}
              {live && (
                <div className="mt-1.5 flex justify-end">
                  <button
                    type="button"
                    className="btn btn-primary btn-sm"
                    disabled={disabled || pending !== null || failedIds.includes(v.id)}
                    onClick={() => void apply(v)}
                  >
                    {pending === v.id ? t("docws.proposal.applying") : t("docws.proposal.apply")}
                  </button>
                </div>
              )}
            </li>
          );
        })}
      </ul>

      {error && <p className="caption mt-2 text-error">{error}</p>}
      {!active && !applied && <p className="caption mt-2 text-muted-soft">{t("docws.proposal.inactive")}</p>}

      {live && (
        <div className="mt-2 flex justify-end">
          <button
            type="button"
            className="btn btn-outline btn-sm"
            disabled={pending !== null}
            onClick={() => setDismissed(true)}
          >
            {t("docws.proposal.dismiss")}
          </button>
        </div>
      )}
    </div>
  );
}
