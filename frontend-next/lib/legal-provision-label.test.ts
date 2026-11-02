import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { provisionLabel } from "./legal-provision-label.ts";

describe("provisionLabel", () => {
  it("reads a điểm in Vietnamese order", () =>
    assert.equal(provisionLabel("dieu:5/khoan:2/diem:a", "vi"), "điểm a khoản 2 Điều 5"));
  it("reads a Điều alone", () => assert.equal(provisionLabel("dieu:12", "vi"), "Điều 12"));
  it("marks an appendix", () => assert.equal(provisionLabel("phuluc/dieu:3", "vi"), "Điều 3 (phụ lục)"));
  it("uses article notation in English", () => assert.equal(provisionLabel("dieu:5/khoan:2/diem:a", "en"), "Art. 5(2)(a)"));
  it("is empty for no path", () => assert.equal(provisionLabel("", "vi"), ""));
});
