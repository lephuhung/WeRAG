import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { csvField, toCSV } from "./csv.ts";

describe("csvField", () => {
  it("neutralises text that a spreadsheet would run as a formula", () => {
    assert.equal(csvField("=HYPERLINK(\"http://x\",\"a\")"), `"'=HYPERLINK(""http://x"",""a"")"`);
    assert.equal(csvField("+1+1"), "'+1+1");
    assert.equal(csvField("-2+3"), "'-2+3");
    assert.equal(csvField("@SUM(A1)"), "'@SUM(A1)");
    assert.equal(csvField("\t=1"), "'\t=1");
    assert.equal(csvField("\r=1"), `"'\r=1"`);
  });

  it("leaves numbers and ordinary text alone", () => {
    assert.equal(csvField(-5), "-5");
    assert.equal(csvField(1234567), "1234567");
    assert.equal(csvField("Nguyễn Văn A"), "Nguyễn Văn A");
    assert.equal(csvField("a@b.vn"), "a@b.vn");
    assert.equal(csvField(""), "");
  });

  it("quotes separators, quotes and line breaks", () => {
    assert.equal(csvField("Sở Tư pháp, Huế"), `"Sở Tư pháp, Huế"`);
    assert.equal(csvField('say "hi"'), `"say ""hi"""`);
    assert.equal(csvField("a\nb"), `"a\nb"`);
  });
});

describe("toCSV", () => {
  it("adds a BOM and CRLF line ends", () => {
    assert.equal(toCSV([["a", 1], ["=b", -2]]), "\uFEFFa,1\r\n'=b,-2");
  });
});
