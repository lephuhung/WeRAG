/* The panel shown with an answer about a legal document the knowledge base
 * has no full text of: the catalogue rows it matched, whether a request
 * went to the administrators, why a request was turned down, or the
 * document that replaced an expired one. The answer text itself is the
 * message under it, so the card does not repeat it. */
"use client";

import { useT } from "@/lib/i18n";
import {
  showIssuedOn,
  statusInForce,
  type LegalRequestCard,
  type LegalRequestCardDocument,
} from "@/lib/legal-request-card";

const HEADING = {
  requested: "legalRequestCard.heading.requested",
  rejected: "legalRequestCard.heading.rejected",
  expired: "legalRequestCard.heading.expired",
} as const;

export function LegalRequestCardView({ card }: { card: LegalRequestCard }) {
  const { t } = useT();
  return (
    <div className="mb-2 mt-1 w-full min-w-0 rounded-lg2 border border-hairline bg-surface-strong/30 p-3">
      <p className="text-[12.5px] font-semibold text-ink">{t(HEADING[card.mode])}</p>
      {card.documents.length > 0 && (
        <ul className="mt-1 flex flex-col gap-0.5">
          {card.documents.map((d, i) => (
            <DocumentRow key={`${i}-${d.id || d.documentNumber}`} doc={d} />
          ))}
        </ul>
      )}
      {card.mode === "requested" && (
        <p className="caption mt-1 text-muted-soft">
          {t("legalRequestCard.sent")}
          {card.requestCount > 1 && ` ${t("legalRequestCard.nth", { n: card.requestCount })}`}
        </p>
      )}
      {card.mode === "rejected" && card.decisionNote && (
        <p className="caption mt-1 text-body">
          <span className="text-muted-soft">{t("legalRequestCard.reason")}: </span>
          {card.decisionNote}
        </p>
      )}
      {card.mode === "expired" && card.replacement && (
        <div className="mt-1.5 border-t border-hairline pt-1.5">
          <p className="caption text-muted-soft">{t("legalRequestCard.replacement")}</p>
          <ul className="flex flex-col gap-0.5">
            <DocumentRow doc={card.replacement} />
          </ul>
        </div>
      )}
    </div>
  );
}

function DocumentRow({ doc }: { doc: LegalRequestCardDocument }) {
  const { t } = useT();
  const label = [doc.docType, doc.documentNumber].filter(Boolean).join(" ");
  const meta = [doc.issuer, doc.issuedOn ? showIssuedOn(doc.issuedOn) : ""].filter(Boolean).join(" · ");
  return (
    <li className="rounded-md2 px-1 py-1 text-[13px] text-body">
      <span className="font-semibold text-ink">{label || doc.title}</span>
      {doc.status && (
        <span
          className={`badge-pill ml-2 inline-block text-[10.5px] ${
            statusInForce(doc.status) ? "badge-success" : "badge-neutral"
          }`}
        >
          {doc.status}
        </span>
      )}
      {label && doc.title && <span className="block text-[12.5px] text-body">{doc.title}</span>}
      {(meta || doc.url) && (
        <span className="block text-[11.5px] text-muted-soft">
          {meta}
          {doc.url && (
            <>
              {meta && " · "}
              <a href={doc.url} target="_blank" rel="noopener noreferrer" className="text-accent hover:underline">
                {t("legalRequestCard.viewOnVbpl")}
              </a>
            </>
          )}
        </span>
      )}
    </li>
  );
}
