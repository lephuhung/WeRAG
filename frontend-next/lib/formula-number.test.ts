// Example numbers of the domain pack formula editor.
// Runs with: node --experimental-strip-types --test lib/formula-number.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { parseFormulaNumber } from "./formula-number.ts";

describe("parseFormulaNumber", () => {
  it("reads certain Vietnamese and English writings", () => {
    const cases: [string, number][] = [
      ["1.000.000", 1000000],
      ["500.000", 500000],
      ["0,5", 0.5],
      ["1,5", 1.5],
      ["1.234,5", 1234.5],
      ["1,234.5", 1234.5],
      ["1,234,567", 1234567],
      ["0.075", 0.075],
      ["8000000000", 8000000000],
      ["-30.000.000", -30000000],
      [" 46666666.67 ", 46666666.67],
      ["12,3456", 12.3456],
      ["1,50", 1.5],
      ["0,015", 0.015],
    ];
    for (const [text, want] of cases) assert.equal(parseFormulaNumber(text), want, text);
  });

  it("flags one comma before three digits as ambiguous", () => {
    for (const text of ["1,500", "500,000", "-2,000", "12,345"]) {
      assert.equal(parseFormulaNumber(text), "ambiguous", text);
    }
  });

  it("refuses junk", () => {
    for (const text of ["", "  ", "abc", "1.2.3", "1,2,3", "10%", "0x10", "Infinity"]) {
      assert.equal(parseFormulaNumber(text), null, text);
    }
  });
});
