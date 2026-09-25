"use client";

import "highlight.js/styles/github.css";
import { memo, useDeferredValue, useEffect, useMemo, useRef } from "react";
import { renderChatMarkdown, setActiveArtifacts } from "@/lib/markdown";
import { downloadArtifact, type ArtifactMeta } from "@/lib/api/chat";
import { hydrateArtifactImages } from "@/lib/artifact-images";

export const Markdown = memo(function Markdown({
  text,
  streaming,
  artifacts,
  imageContext,
}: {
  text: string;
  streaming?: boolean;
  /** This message's artifacts — artifact image destinations resolve here. */
  artifacts?: ArtifactMeta[] | null;
  /** Session/message identity for authenticated artifact downloads. */
  imageContext?: { sessionId: string; messageId: string } | null;
}) {
  // Defer the source so rapid stream chunks coalesce into a single parse
  // instead of re-running marked + DOMPurify + highlight.js per token.
  // memo() additionally skips reconciling past messages entirely — their
  // text/streaming props don't change between chunks.
  const deferred = useDeferredValue(text);
  const rootRef = useRef<HTMLDivElement>(null);
  const html = useMemo(() => {
    setActiveArtifacts(artifacts ?? null);
    try {
      return renderChatMarkdown(deferred);
    } finally {
      setActiveArtifacts(null);
    }
  }, [deferred, artifacts]);
  // Swap artifact-image placeholders for authenticated blob URLs after each
  // render — same hydrate-after-paint contract as the Vue panel.
  useEffect(() => {
    if (!imageContext) return;
    void hydrateArtifactImages(rootRef.current, imageContext, downloadArtifact);
  }, [html, imageContext]);
  return <div ref={rootRef} className="chat-markdown" dangerouslySetInnerHTML={{ __html: html }} data-streaming={streaming ? "1" : undefined} />;
});
