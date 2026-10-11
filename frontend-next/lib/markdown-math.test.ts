// KaTeX math in chat answers.
// Runs with: node --experimental-strip-types --test lib/markdown-math.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Marked } from "marked";

import { mathExtensions, preprocessMathDelimiters } from "./markdown-math.ts";

const md = new Marked({ breaks: true, gfm: true }, { extensions: mathExtensions });
const render = (s: string) => md.parse(preprocessMathDelimiters(s), { async: false }) as string;

describe("chat math", () => {
  it("renders the $\\rightarrow$ arrows Gemma writes", () => {
    const html = render("*Vấn đề là gì $\\rightarrow$ Ví dụ cụ thể $\\rightarrow$ Hậu quả.*");
    assert.ok(!html.includes("\\rightarrow"), html);
    assert.ok(!html.includes("$"), html);
    assert.equal(html.match(/class="katex"/g)?.length, 2);
    assert.ok(html.includes("→"));
  });

  it("renders display math and the \\( \\) / \\[ \\] delimiters", () => {
    assert.ok(render("$$\nx^2 + 1\n$$").includes("katex-display"));
    assert.ok(render("a \\(x_1\\) b").includes('class="katex"'));
    assert.ok(render("\\[\\frac{1}{2}\\]").includes("katex-display"));
  });

  it("leaves code literal", () => {
    const html = render("`$\\alpha$`\n\n```\n$x$\n```");
    assert.ok(!html.includes("katex"), html);
    assert.ok(html.includes("$\\alpha$"));
  });

  it("shows a bad formula instead of failing", () => {
    assert.ok(render("$\\frac{$").length > 0);
    assert.ok(render("giá $\\badcommand{x}$ này").includes("katex"));
  });

  it("keeps a lone dollar sign as text", () => {
    assert.ok(render("Giá 5$ một chiếc").includes("5$"));
  });
});
