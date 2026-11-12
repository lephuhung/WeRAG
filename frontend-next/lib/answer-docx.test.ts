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

it("reads the server's file name from Content-Disposition", async () => {
  const { contentDispositionFileName } = await import("./api-client.ts");
  assert.equal(
    contentDispositionFileName(
      `attachment; filename="Tra loi - Ho so.docx"; filename*=UTF-8''Tr%E1%BA%A3%20l%E1%BB%9Di%20-%20H%E1%BB%93%20s%C6%A1.docx`,
    ),
    "Trả lời - Hồ sơ.docx",
  );
  assert.equal(contentDispositionFileName(`attachment; filename="a.docx"`), "a.docx");
  assert.equal(contentDispositionFileName(`attachment; filename*=UTF-8''%E0%A4%A; filename="b.docx"`), "b.docx");
  assert.equal(contentDispositionFileName(null), null);
  assert.equal(contentDispositionFileName("attachment"), null);
});
