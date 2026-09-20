"use client";

import "highlight.js/styles/github.css";
import { useMemo } from "react";
import { renderChatMarkdown } from "@/lib/markdown";

export function Markdown({ text, streaming }: { text: string; streaming?: boolean }) {
  const html = useMemo(() => renderChatMarkdown(text), [text]);
  return <div className="chat-markdown" dangerouslySetInnerHTML={{ __html: html }} data-streaming={streaming ? "1" : undefined} />;
}
