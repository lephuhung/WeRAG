import { marked } from "marked";
import DOMPurify from "dompurify";
import hljs from "highlight.js";

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

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
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
    ADD_TAGS: ["pre", "code", "span", "table", "thead", "tbody", "tr", "th", "td"],
    ADD_ATTR: ["class", "colspan", "rowspan", "data-slug"],
  });
}
