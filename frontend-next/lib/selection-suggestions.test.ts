// Quick actions for a highlighted passage.
// Runs with: node --experimental-strip-types --test lib/selection-suggestions.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { selectionSuggestions } from "./selection-suggestions.ts";

describe("selectionSuggestions", () => {
  it("offers nothing for an empty selection", () => {
    assert.deepEqual(selectionSuggestions("  \n "), []);
  });

  it("keeps a word or two to rewrite, spelling and a legal lookup", () => {
    assert.deepEqual(selectionSuggestions("Nghị định 30"), ["rewrite", "spelling", "legal"]);
    assert.deepEqual(selectionSuggestions("cải cách"), ["rewrite", "spelling"]);
  });

  it("names the diagram after what the passage looks like", () => {
    const steps = "Quy trình giải quyết:\n1. Tiếp nhận hồ sơ\n2. Thẩm định hồ sơ\n3. Trả kết quả cho công dân";
    assert.equal(selectionSuggestions(steps)[1], "diagramProcess");
    const org =
      "Sở Tài chính có Ban Giám đốc và các phòng chuyên môn; Chi cục Tài chính doanh nghiệp là đơn vị trực thuộc Sở.";
    assert.ok(selectionSuggestions(org).includes("diagramOrg"));
    const dates = "Năm 2021 xây dựng đề án, năm 2023 thí điểm, năm 2025 nhân rộng trên toàn tỉnh.";
    assert.ok(selectionSuggestions(dates).includes("diagramTimeline"));
  });

  it("offers a generic diagram, shortening and an illustration for a long passage", () => {
    const long = Array.from({ length: 50 }, (_, i) => `từ${i}`).join(" ");
    const got = selectionSuggestions(long);
    assert.deepEqual(got, ["rewrite", "diagram", "shorten", "spelling", "illustration"]);
  });

  it("caps the list", () => {
    const text =
      "Theo Nghị định 30/2020, quy trình tiếp nhận tại các phòng trực thuộc Sở năm 2021, 2022, 2023 gồm nhiều bước " +
      Array.from({ length: 40 }, () => "nội dung").join(" ");
    assert.equal(selectionSuggestions(text).length, 5);
  });
});
