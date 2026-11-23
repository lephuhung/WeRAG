"use client";

import { LegalNetwork } from "@/components/graph/legal-network";
import { LargeViewOnly } from "@/components/large-view-only";

/* The knowledge graph of a KB is its documents linked by the fixed legal
 * relations (thay thế, bãi bỏ, sửa đổi, căn cứ, …) read from the legal
 * catalogue and from the documents' own texts, drawn as one network of
 * bubbles; no model extracts anything. (The LLM entity graph, the wiki
 * link map and the one-document focus view were dropped on 2026-11-18/21.)
 *
 * The tab is gated: a dragged and zoomed map needs room (the thresholds
 * and the notice live in lib/large-view-policy.ts +
 * components/large-view-only.tsx). Skipping the body also skips its fetch. */
export function KnowledgeGraph({
  kbId,
  onOpenKnowledge,
}: {
  kbId: string;
  onOpenKnowledge?: (knowledgeId: string) => void;
}) {
  return (
    <LargeViewOnly id="graph">
      <div className="flex min-h-0 flex-1 flex-col">
        <LegalNetwork kbId={kbId} onOpenKnowledge={onOpenKnowledge} />
      </div>
    </LargeViewOnly>
  );
}
