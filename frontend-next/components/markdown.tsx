"use client";

import "highlight.js/styles/github.css";
import { memo, useDeferredValue, useMemo } from "react";
import { renderChatMarkdown } from "@/lib/markdown";

export const Markdown = memo(function Markdown({
  text,
  streaming,
}: {
  text: string;
  streaming?: boolean;
}) {
  // Defer the source so rapid stream chunks coalesce into a single parse
  // instead of re-running marked + DOMPurify + highlight.js per token.
  // memo() additionally skips reconciling past messages entirely — their
  // text/streaming props don't change between chunks.
  const deferred = useDeferredValue(text);
  const html = useMemo(() => renderChatMarkdown(deferred), [deferred]);
  return <div className="chat-markdown" dangerouslySetInnerHTML={{ __html: html }} data-streaming={streaming ? "1" : undefined} />;
});
