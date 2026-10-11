/* The illustration card (document assistant): draw_diagram and
 * generate_illustration propose a picture instead of changing the document.
 * A diagram is drawn here from its Mermaid source; an image is the artifact
 * the image model produced (also shown in the answer). "Chèn vào văn bản"
 * asks the parent to snapshot and insert it after the anchor paragraph (the
 * outcome comes back through `outcomeFor`); "Tải PNG" saves it; "Bỏ qua"
 * folds the card. Nothing reaches the document without the click.
 */
"use client";

import { useEffect, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import { proposalBatchId, proposalOutcomeApplied, type Illustration } from "@/lib/api/document-ops";
import { downloadArtifact, type ArtifactMeta } from "@/lib/api/chat";
import {
  blobToRaster,
  downloadDataUrl,
  illustrationFileName,
  renderMermaidSvg,
  svgToPng,
  type RasterImage,
} from "@/lib/illustration-render";
import type { ProposalOutcome } from "@/components/chat/rewrite-proposal-card";

/** No outcome after this long (editor not ready, document reloaded): give up. */
const OUTCOME_TIMEOUT_MS = 45_000;

type Picture = { state: "loading" } | { state: "ready"; raster: RasterImage } | { state: "error"; message: string };

export function IllustrationCard({
  illustration,
  applied,
  canInsert,
  disabled = false,
  artifacts,
  imageContext,
  outcomeFor,
  onInsert,
  onInserted,
}: {
  illustration: Illustration;
  /** Inserted earlier (message state / ledger). */
  applied?: boolean;
  /** The target document is open in an editor tab. */
  canInsert: boolean;
  /** A turn is running: viewable, not insertable. */
  disabled?: boolean;
  /** The message's artifacts: an image illustration is one of them. */
  artifacts?: ArtifactMeta[];
  imageContext: { sessionId: string; messageId: string } | null;
  outcomeFor: (batchId: string) => ProposalOutcome | undefined;
  /** Snapshots and sends the picture to the editor; rejects when it could not. */
  onInsert: (illustration: Illustration, picture: RasterImage) => Promise<void>;
  onInserted: (illustration: Illustration) => void;
}) {
  const { t } = useT();
  const [picture, setPicture] = useState<Picture>({ state: "loading" });
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const startedRef = useRef(0);

  const isDiagram = illustration.kind === "diagram";
  const artifact =
    illustration.kind === "image"
      ? artifacts?.find((a) => a.content_hash && a.content_hash === illustration.imageSha256)
      : undefined;
  const artifactIndex = artifact?.index;

  // Draw the diagram / fetch the image once.
  useEffect(() => {
    let alive = true;
    const done = (p: Picture) => alive && setPicture(p);
    if (isDiagram) {
      renderMermaidSvg(illustration.mermaid ?? "")
        .then((svg) => svgToPng(svg))
        .then((raster) => done({ state: "ready", raster }))
        .catch((e) => done({ state: "error", message: e instanceof Error ? e.message : String(e) }));
    } else if (imageContext && artifactIndex !== undefined) {
      downloadArtifact(imageContext.sessionId, imageContext.messageId, artifactIndex)
        .then(blobToRaster)
        .then((raster) => done({ state: "ready", raster }))
        .catch((e) => done({ state: "error", message: e instanceof Error ? e.message : String(e) }));
    } else {
      done({ state: "error", message: t("docws.illustration.imageMissing") });
    }
    return () => {
      alive = false;
    };
  }, [isDiagram, illustration.mermaid, imageContext?.sessionId, imageContext?.messageId, artifactIndex, t]);

  const batchId = proposalBatchId(illustration.batchId, "v1");
  const outcome = pending ? outcomeFor(batchId) : undefined;
  useEffect(() => {
    if (!pending || !outcome) return;
    if (proposalOutcomeApplied(outcome)) {
      onInserted(illustration);
    } else {
      setFailed(true);
      setError(outcome.kind === "timeout" ? t("docws.illustration.timeout") : t("docws.illustration.notInserted"));
    }
    setPending(false);
  }, [pending, outcome, illustration, onInserted, t]);

  useEffect(() => {
    if (!pending) return;
    const started = startedRef.current;
    const timer = setTimeout(() => {
      if (startedRef.current !== started) return;
      setFailed(true);
      setError(t("docws.illustration.timeout"));
      setPending(false);
    }, OUTCOME_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [pending, t]);

  if (dismissed) {
    return <p className="caption mt-2 text-muted-soft">{t("docws.illustration.dismissed")}</p>;
  }

  const ready = picture.state === "ready" ? picture.raster : null;
  const insertable = !applied && canInsert && illustration.anchor !== null;

  const insert = async () => {
    if (!ready) return;
    setError(null);
    startedRef.current += 1;
    setPending(true);
    try {
      await onInsert(illustration, ready);
    } catch (e) {
      setPending(false);
      setError(`${t("docws.illustration.failed")}: ${e instanceof Error ? e.message : String(e)}`);
    }
  };

  const title = illustration.title || t(isDiagram ? "docws.illustration.diagram" : "docws.illustration.image");

  return (
    <div className="mt-3 w-full min-w-0 rounded-[10px] border border-hairline bg-surface-card p-3">
      <div className="flex items-baseline justify-between gap-2">
        <span className="caption-uppercase text-muted-soft">
          {t(isDiagram ? "docws.illustration.diagram" : "docws.illustration.image")}
        </span>
        {illustration.document && (
          <span className="caption min-w-0 truncate text-muted-soft" title={illustration.document}>
            {illustration.document}
          </span>
        )}
      </div>
      <p className="mt-1 text-[13px] font-medium text-ink">{title}</p>

      {isDiagram && (
        <div className="mt-2 flex min-h-[80px] items-center justify-center overflow-auto rounded-md border border-hairline bg-white p-2">
          {picture.state === "loading" && <span className="caption text-muted-soft">{t("docws.illustration.drawing")}</span>}
          {picture.state === "error" && (
            <div className="w-full">
              <p className="caption text-error">{t("docws.illustration.syntaxError")}</p>
              <pre className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap text-[11px] text-muted-soft">{picture.message}</pre>
            </div>
          )}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          {ready && <img src={ready.dataUrl} alt={title} className="max-h-[420px] max-w-full object-contain" />}
        </div>
      )}
      {!isDiagram && picture.state === "error" && (
        <p className="caption mt-2 text-muted-soft">{picture.message}</p>
      )}

      {illustration.caption && (
        <p className="caption mt-1.5 text-center italic text-muted-soft">{illustration.caption}</p>
      )}

      {applied ? (
        <p className="caption mt-2 text-muted-soft">{t("docws.illustration.inserted")}</p>
      ) : (
        <>
          {!canInsert && <p className="caption mt-2 text-muted-soft">{t("docws.illustration.noEditor")}</p>}
          {canInsert && illustration.anchor === null && (
            <p className="caption mt-2 text-muted-soft">{t("docws.illustration.noPlace")}</p>
          )}
          {canInsert && illustration.anchor && (
            <p className="caption mt-2 text-muted-soft" title={illustration.anchor.text}>
              {t("docws.illustration.after")} “{illustration.anchor.text.slice(0, 80)}
              {illustration.anchor.text.length > 80 ? "…" : ""}”
            </p>
          )}
        </>
      )}
      {error && <p className="caption mt-1.5 text-error">{error}</p>}

      <div className="mt-2 flex flex-wrap justify-end gap-2">
        {!applied && (
          <button
            type="button"
            className="btn btn-outline btn-sm"
            disabled={pending}
            onClick={() => setDismissed(true)}
          >
            {t("docws.proposal.dismiss")}
          </button>
        )}
        <button
          type="button"
          className="btn btn-outline btn-sm"
          disabled={!ready}
          onClick={() => ready && downloadDataUrl(ready.dataUrl, illustrationFileName(title))}
        >
          {t("docws.illustration.download")}
        </button>
        {insertable && (
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={disabled || pending || failed || !ready}
            onClick={() => void insert()}
          >
            {pending ? t("docws.illustration.inserting") : t("docws.illustration.insert")}
          </button>
        )}
      </div>
    </div>
  );
}
