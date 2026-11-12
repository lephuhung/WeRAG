"use client";

/* The panel of one amending document B in the diagram: its links to A,
 * each opening the confirm panel, and the bulk approval of every link a
 * person can approve as it is (with a confirm step stating the count). A
 * document not yet in the catalogue cannot be approved: the panel says
 * what it waits for and offers the recheck. */

import { useState } from "react";
import { Modal } from "@/components/modal";
import { Notice } from "@/components/settings/notice";
import { useToast } from "@/components/toast";
import { useT, type LocaleKey } from "@/lib/i18n";
import { userErrorMessage } from "@/lib/user-error";
import { provisionLabel } from "@/lib/legal-provision-label";
import { opColor } from "@/components/graph/legal-style";
import {
  LEGAL_LINK_BATCH_MAX,
  decideLegalLinkCandidates,
  type LegalLinkCandidate,
} from "@/lib/api/legal-link-candidates";
import { edgeState, type AmenderGroup } from "@/lib/legal-link-graph";
import { MatchBadge } from "./link-confirm-panel";

export function LinkGroupPanel({
  group,
  cands,
  onClose,
  onOpenEdge,
  onChanged,
  onRecheck,
  rechecking,
}: {
  group: AmenderGroup;
  cands: LegalLinkCandidate[];
  onClose: () => void;
  onOpenEdge: (candidateId: number) => void;
  onChanged: () => Promise<void> | void;
  onRecheck: () => void;
  rechecking: boolean;
}) {
  const { t, locale } = useT();
  const toast = useToast();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const blocked = group.kind === "blocked";
  const ids = group.approvableIds;
  const byId = new Map(cands.map((c) => [c.id, c]));
  const notFull = ids.filter((id) => byId.get(id)?.match_status !== "matched").length;
  const title = group.title || "";

  const approveAll = async () => {
    setBusy(true);
    setError("");
    try {
      let done = 0;
      const failed: string[] = [];
      for (let i = 0; i < ids.length; i += LEGAL_LINK_BATCH_MAX) {
        const res = await decideLegalLinkCandidates({ ids: ids.slice(i, i + LEGAL_LINK_BATCH_MAX), action: "approve" });
        done += res.data?.done.length ?? 0;
        for (const f of res.data?.failed ?? []) failed.push(f.error);
      }
      if (done > 0) toast.success(t("llink.bulk.done", { n: done }));
      if (failed.length > 0) setError(t("llink.bulk.failed", { n: failed.length, reason: failed[0] }));
      setConfirming(false);
      await onChanged();
      if (failed.length === 0) onClose();
    } catch (e) {
      setError(userErrorMessage(e, t, "llink.decideFailed"));
    } finally {
      setBusy(false);
    }
  };

  const sorted = [...cands].sort((a, b) => {
    const rank = (c: LegalLinkCandidate) => ({ pending: 0, blocked: 1, approved: 2, auto: 3, rejected: 4 })[edgeState(c)];
    return rank(a) - rank(b) || a.id - b.id;
  });

  return (
    <Modal
      open
      title={blocked ? t("llink.diagram.waitingFor", { doc: group.number }) : group.number}
      onClose={onClose}
      width="w-[720px]"
    >
      <div className="flex flex-col gap-4">
        {title && <p className="text-[13px] text-body">{title}</p>}
        {blocked ? (
          <Notice tone="warning">
            {t("llink.group.blockedHelp", { doc: group.number, n: group.counts.blocked })}
            {group.requestStatus && (
              <div className="mt-1">{t("llink.panel.request", { state: t(`llink.req.${group.requestStatus}` as LocaleKey) })}</div>
            )}
          </Notice>
        ) : null}

        <ul className="flex max-h-[50vh] flex-col divide-y divide-hairline overflow-y-auto rounded-[12px] border border-hairline">
          {sorted.map((c) => {
            const st = edgeState(c);
            return (
              <li key={c.id}>
                <button
                  type="button"
                  className="flex w-full items-start gap-3 px-3 py-2.5 text-left hover:bg-surface-strong focus:bg-surface-strong focus:outline-none"
                  onClick={() => onOpenEdge(c.id)}
                >
                  <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full" style={{ background: opColor(c.op) }} aria-hidden />
                  <span className="min-w-0 flex-1">
                    <span className="block text-[13px] text-ink">
                      {t("llink.group.row", {
                        target: provisionLabel(c.target_path, locale) || "—",
                        op: t(`llink.op.${c.op}` as LocaleKey).toLowerCase(),
                        source: c.source_path ? provisionLabel(c.source_path, locale) : t("llink.noPath").toLowerCase(),
                      })}
                    </span>
                    <span className="caption text-muted">{t(`llink.state.${st}` as LocaleKey)}</span>
                  </span>
                  <MatchBadge c={c} />
                </button>
              </li>
            );
          })}
        </ul>

        {error && <Notice tone="error">{error}</Notice>}

        {confirming ? (
          <div className="flex flex-col gap-3 rounded-[12px] border border-warning/40 bg-warning-surface p-3">
            <p className="text-[13px] text-ink">{t("llink.bulk.confirm", { n: ids.length, doc: group.number })}</p>
            {notFull > 0 && <p className="caption text-warning">{t("llink.bulk.notFull", { n: notFull })}</p>}
            <div className="flex justify-end gap-2">
              <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={() => setConfirming(false)}>
                {t("common.cancel")}
              </button>
              <button type="button" className="btn btn-primary btn-sm" disabled={busy} onClick={() => void approveAll()}>
                {busy ? t("common.loading") : t("llink.bulk.go", { n: ids.length })}
              </button>
            </div>
          </div>
        ) : (
          <div className="flex flex-wrap justify-end gap-2 border-t border-hairline pt-4">
            {blocked && (
              <button type="button" className="btn btn-outline btn-sm" disabled={rechecking} onClick={onRecheck}>
                {rechecking ? t("common.loading") : t("llink.recheck")}
              </button>
            )}
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>
              {t("common.close")}
            </button>
            {ids.length > 0 && (
              <button type="button" className="btn btn-primary btn-sm" onClick={() => setConfirming(true)}>
                {t("llink.bulk.button", { n: ids.length })}
              </button>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}
