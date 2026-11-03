// Provision path wording and order of the legal graph's comparison panel.
// Runs with: npm run test:legal-paths (Node >= 22.6, type stripping)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { comparePaths, parsePath, pathLabel } from "./legal-paths.ts";

describe("legal provision paths", () => {
  it("parses and words paths in Vietnamese and English", () => {
    assert.deepEqual(parsePath("dieu:10/khoan:2/diem:đ"), { article: "10", clause: "2", point: "đ" });
    assert.equal(parsePath(""), null);
    assert.equal(parsePath("phuluc/x"), null);
    assert.equal(pathLabel("dieu:10/khoan:2/diem:a", "vi"), "Điều 10 khoản 2 điểm a");
    assert.equal(pathLabel("dieu:10/khoan:2/diem:a", "vi", false), "Khoản 2 điểm a");
    assert.equal(pathLabel("dieu:7", "en"), "Article 7");
    assert.equal(pathLabel("dieu:7/khoan:3", "en", false), "Clause 3");
    assert.equal(pathLabel("", "vi"), "");
  });

  it("sorts numerically, the article first, đ after d, unknown last", () => {
    const paths = ["dieu:10", "", "dieu:2/khoan:10", "dieu:2/khoan:2/diem:đ", "dieu:2/khoan:2/diem:d", "dieu:2", "dieu:2/khoan:2", "dieu:10a"];
    assert.deepEqual([...paths].sort(comparePaths), [
      "dieu:2", "dieu:2/khoan:2", "dieu:2/khoan:2/diem:d", "dieu:2/khoan:2/diem:đ", "dieu:2/khoan:10", "dieu:10", "dieu:10a", "",
    ]);
  });
});
