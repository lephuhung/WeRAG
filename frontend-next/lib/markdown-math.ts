import katex from "katex";
import type { TokenizerAndRendererExtension } from "marked";

/* KaTeX math in chat answers — port of the Vue renderer's
 * marked-katex-extension({ nonStandard: true }) plus its
 * preprocessMathDelimiters. Models write $\rightarrow$, $$…$$, \(…\) and
 * \[…\]; without this they showed up as raw TeX. Code spans and fences are
 * tokenized by marked first, so math inside them stays literal. KaTeX
 * escapes its input and runs with trust off; a formula it cannot parse is
 * shown in red (throwOnError false) instead of breaking the answer.
 */

const INLINE_RE = /^(\${1,2})(?!\$)((?:\\.|[^\\\n])*?(?:\\.|[^\\\n$]))\1/;
const BLOCK_RE = /^(\${1,2})\n((?:\\[\s\S]|[^\\])+?)\n\1(?:\n|$)/;

type MathToken = { type: string; raw: string; text: string; displayMode: boolean };

function renderMath(text: string, displayMode: boolean): string {
  return katex.renderToString(text, { displayMode, throwOnError: false, output: "html" });
}

export const mathExtensions: TokenizerAndRendererExtension[] = [
  {
    name: "blockKatex",
    level: "block",
    start: (src: string) => src.match(/^\$\$?\n/m)?.index,
    tokenizer(src: string) {
      const m = BLOCK_RE.exec(src);
      if (!m) return undefined;
      return { type: "blockKatex", raw: m[0], text: m[2].trim(), displayMode: m[1].length === 2 } as MathToken;
    },
    renderer: (token) => `<div class="math-block">${renderMath(token.text as string, token.displayMode as boolean)}</div>\n`,
  },
  {
    name: "inlineKatex",
    level: "inline",
    start: (src: string) => {
      const i = src.indexOf("$");
      return i < 0 ? undefined : i;
    },
    tokenizer(src: string) {
      const m = INLINE_RE.exec(src);
      if (!m) return undefined;
      return { type: "inlineKatex", raw: m[0], text: m[2].trim(), displayMode: m[1].length === 2 } as MathToken;
    },
    renderer: (token) => renderMath(token.text as string, token.displayMode as boolean),
  },
];

/** \[…\] → $$…$$ and \(…\) → $…$ (Vue preprocessMathDelimiters). */
export function preprocessMathDelimiters(text: string): string {
  return text
    .replace(/\\\[([\s\S]*?)\\\]/g, (_, body: string) => `$$${body}$$`)
    .replace(/\\\(([\s\S]*?)\\\)/g, (_, body: string) => `$${body}$`);
}
