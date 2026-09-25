import { marked } from "marked";
import DOMPurify from "dompurify";
import hljs from "highlight.js";
import { ARTIFACT_TRANSPARENT_PIXEL } from "@/lib/artifact-images";
import type { ArtifactMeta } from "@/lib/api/chat";

/* Minimal port of frontend/src/utils/chatMarkdownRenderer.ts +
 * security.ts sanitizeMarkdownHTML: GFM tables/breaks, fenced code with
 * highlight.js, DOMPurify allowlist. Mermaid/KaTeX/citation pills are
 * intentionally out of scope for this pass — raw blocks degrade to code.
 */

marked.setOptions({ breaks: true, gfm: true });

const renderer = new marked.Renderer();
renderer.code = ({ text, lang }: { text?: string; lang?: string }) => {
  const language = (lang ?? "").trim().split(/\s+/)[0];
  let highlighted: string;
  try {
    highlighted =
      language && hljs.getLanguage(language)
        ? hljs.highlight(text ?? "", { language }).value
        : hljs.highlightAuto(text ?? "").value;
  } catch {
    highlighted = escapeHtml(text ?? "");
  }
  const cls = language ? ` class="language-${escapeHtml(language)}"` : "";
  return `<pre><code${cls}>${highlighted}</code></pre>`;
};
renderer.image = ({ href, title, text }: { href?: string; title?: string | null; text?: string }) => {
  const src = (href ?? "").trim();
  const alt = escapeHtml(text ?? "");
  const titleAttr = title ? ` title="${escapeHtml(title)}"` : "";
  // Artifact images carry a data-artifact-index the chat panel hydrates with
  // an authenticated blob URL (see lib/artifact-images.ts). The transparent
  // pixel keeps layout stable until hydration swaps the real bytes in.
  const idx = artifactImageIndex(src);
  if (idx !== null) {
    return `<img src="${ARTIFACT_TRANSPARENT_PIXEL}" alt="${alt}"${titleAttr} class="markdown-image artifact-ref-image" data-artifact-index="${idx}" data-img-loading="1">`;
  }
  return `<img src="${escapeHtml(src)}" alt="${alt}"${titleAttr} class="markdown-image" loading="lazy">`;
};

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}
/* Resolve a Markdown image destination against this message's artifacts.
 * Returns the artifact index when the destination names an image artifact,
 * null otherwise (ordinary images render as-is). The active artifact list is
 * installed per render by the chat panel — see setActiveArtifacts. */
let activeArtifacts: ArtifactMeta[] | null = null;

export function setActiveArtifacts(artifacts: ArtifactMeta[] | null) {
  activeArtifacts = artifacts;
}

const RESOURCE_HANDLE_RE = /^resource:\/\/([A-Za-z0-9_-]{22})$/;

const IMAGE_EXTENSIONS: Record<string, true> = {
  png: true,
  jpg: true,
  jpeg: true,
  gif: true,
  webp: true,
  bmp: true,
  avif: true,
};

function artifactImageIndex(src: string): number | null {
  if (!src || !activeArtifacts?.length) return null;
  const handleMatch = src.match(RESOURCE_HANDLE_RE);
  const found = handleMatch
    ? activeArtifacts.find((a) => (a.handle || "").trim() === handleMatch[0])
    : activeArtifacts.find((a) => (a.file_name || "").trim() === src.split("/").pop()?.trim());
  if (!found) return null;
  const ext = (found.file_name || "").trim().toLowerCase().split(".").pop() ?? "";
  const isImage = ext ? IMAGE_EXTENSIONS[ext] === true : (found.file_type || "").toLowerCase().startsWith("image/");
  if (!isImage) return null;
  return found.index;
}

export function renderChatMarkdown(raw: string): string {
  if (!raw.trim()) return "";
  // Strip the <think>…</think> reasoning envelope the same way the Vue
  // pipeline does (useChatStreamHandler splits thinkContent from content).
  const withoutThink = raw.includes("<think>")
    ? raw.replace(/<think>[\s\S]*?(<\/think>|$)/, "").trim()
    : raw;

  // Pre-process wiki links [[slug|name]] to custom HTML tags (matches Vue renderMarkdown)
  const withWikiLinks = withoutThink.replace(/\[\[([^\]\n]+)\]\]/g, (_, inner: string) => {
    const pipeIdx = inner.indexOf("|");
    const slug = pipeIdx > 0 ? inner.substring(0, pipeIdx).trim() : inner.trim();
    const display =
      pipeIdx > 0
        ? inner.substring(pipeIdx + 1).trim()
        : (slug.split("/").length > 1 ? slug.split("/").slice(1).join("/") : slug);
    return `<a href="#" class="wiki-content-link" data-slug="${escapeHtml(slug)}">${escapeHtml(display)}</a>`;
  });

  const html = marked.parse(withWikiLinks, { renderer, breaks: true, async: false }) as string;
  return DOMPurify.sanitize(html, {
    ADD_TAGS: ["pre", "code", "span", "table", "thead", "tbody", "tr", "th", "td", "img"],
    ADD_ATTR: ["class", "colspan", "rowspan", "data-slug", "src", "alt", "title", "loading", "data-artifact-index", "data-img-loading"],
  });
}
