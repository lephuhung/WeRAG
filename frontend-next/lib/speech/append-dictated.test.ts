// Runs with: node --experimental-strip-types --test lib/speech/append-dictated.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { appendDictated } from "./append-dictated.ts";

describe("appendDictated", () => {
  it("capitalises the first words of an empty draft", () => {
    assert.equal(appendDictated("", "điện thoại vệ tinh"), "Điện thoại vệ tinh");
  });

  it("continues a sentence in lower case with one space", () => {
    assert.equal(appendDictated("Điện thoại", "vệ tinh"), "Điện thoại vệ tinh");
    assert.equal(appendDictated("Điện thoại ", "vệ tinh"), "Điện thoại vệ tinh");
  });

  it("capitalises after sentence punctuation or a new line", () => {
    assert.equal(appendDictated("Xong.", "tiếp theo"), "Xong. Tiếp theo");
    assert.equal(appendDictated("Dòng một\n", "dòng hai"), "Dòng một\nDòng hai");
  });

  it("keeps the draft when nothing was recognised", () => {
    assert.equal(appendDictated("abc", ""), "abc");
  });
});
