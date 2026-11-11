// Runs with: node --experimental-strip-types --test lib/answer-docx.test.ts
import { it } from "node:test";
import assert from "node:assert/strict";

import { answerDocxFileName } from "./answer-docx.ts";

it("names the file after the question, like the server", () => {
  assert.equal(answerDocxFileName("Hồ sơ cấp phép gồm những gì?"), "Trả lời - Hồ sơ cấp phép gồm những gì.docx");
  assert.equal(answerDocxFileName('Đường/đi: "x" <y>|z*'), "Trả lời - Đường đi x y z.docx");
  assert.equal(answerDocxFileName("   "), "Trả lời WeRAG.docx");
  assert.equal(answerDocxFileName(undefined), "Trả lời WeRAG.docx");
  assert.equal(
    answerDocxFileName("Nghị định quy định chi tiết ".repeat(10)),
    "Trả lời - Nghị định quy định chi tiết Nghị định quy định chi tiết.docx",
  );
});
