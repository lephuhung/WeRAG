// Runs with: node --experimental-strip-types --test lib/format-findings.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { findingCheckOp, findingNavigation, findingSelectOp, numberFormatReportSections, parseFormatFindings } from "./format-findings.ts";

const anchor = { text: "Đề nghị xem xét hồ sơ.", occurrence: 1 };

describe("parseFormatFindings", () => {
  it("keeps one formatParagraph on the finding's anchor", () => {
    const [f] = parseFormatFindings([
      { id: "a", check_id: "noi_dung.font", title: "Phông", anchor, ops: [{ op: "formatParagraph", anchor, font: "Times New Roman", sizePt: 13 }] },
    ]);
    assert.equal(f.ops.length, 1);
    assert.deepEqual(findingCheckOp(f.ops[0]), { op: "checkFormat", anchor, font: "Times New Roman", sizePt: 13 });
  });
  it("drops ops on another paragraph, several ops, duplicates and malformed rows", () => {
    const list = parseFormatFindings([
      { id: "a", title: "x", anchor, ops: [{ op: "formatParagraph", anchor: { text: "khác" }, bold: true }] },
      { id: "b", title: "y", anchor, ops: [{ op: "formatParagraph", anchor, bold: true }, { op: "formatParagraph", anchor, italic: true }] },
      { id: "b", title: "dup" },
      { title: "no id" },
      { id: "c", title: "z", ops: [{ op: "formatParagraph", anchor, bold: true }] },
    ]);
    assert.deepEqual(list.map((f) => [f.id, f.ops.length]), [["a", 0], ["b", 0], ["c", 0]]);
  });
});

describe("findingSelectOp", () => {
  it("selects the text in the anchor paragraph", () => {
    const [f] = parseFormatFindings([{ id: "a", title: "x", anchor, text: "Đề nghị", text_occurrence: 2 }]);
    assert.deepEqual(findingSelectOp(f), { op: "select", anchor, text: "Đề nghị", textOccurrence: 2 });
  });
  it("goes to the document start for a missing header component only", () => {
    const [q, other] = parseFormatFindings([
      { id: "q", check_id: "component.quoc_hieu.present", title: "Thiếu Quốc hiệu" },
      { id: "o", check_id: "page.margins", title: "Lề" },
    ]);
    assert.equal(findingNavigation(q), "document-start");
    assert.deepEqual(findingSelectOp(q), { op: "select", anchor: { atStart: true } });
    assert.equal(findingSelectOp(other), null);
  });
});

describe("numberFormatReportSections", () => {
  it("numbers the top-level sections, not fenced lines", () => {
    const out = numberFormatReportSections("## Kết luận\nok\n```\n## Kết luận\n```\n## Đề xuất:\n- a");
    assert.equal(out, "## 1. Kết luận\nok\n```\n## Kết luận\n```\n## 2. Đề xuất\n- a");
  });
});
