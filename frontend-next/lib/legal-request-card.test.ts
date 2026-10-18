// Runs with: node --experimental-strip-types --test lib/legal-request-card.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { parseLegalRequestCard, showIssuedOn, statusInForce, takeLegalRequestCard } from "./legal-request-card.ts";

const requested = {
  display_type: "legal_document_request",
  mode: "requested",
  reference: "53/2022/NĐ-CP",
  doc_type: "Nghị định",
  documents: [
    { id: "155366", document_number: "53/2022/NĐ-CP", title: "Quy định chi tiết…", doc_type: "Nghị định",
      issuer: "Chính phủ", status: "Còn hiệu lực", issued_on: "2022-08-15", url: "https://vbpl.vn/x" },
    { foo: "no number or title" },
  ],
  request_count: 3,
};

test("requested card keeps rows with a number and the request count", () => {
  const c = parseLegalRequestCard(requested);
  assert.ok(c);
  assert.equal(c.mode, "requested");
  assert.equal(c.documents.length, 1);
  assert.equal(c.documents[0].documentNumber, "53/2022/NĐ-CP");
  assert.equal(c.requestCount, 3);
  assert.equal(c.replacement, null);
});

test("replacement only in expired mode; other display types are ignored", () => {
  const rep = { id: "1", document_number: "10/2024/NĐ-CP", title: "x" };
  assert.equal(parseLegalRequestCard({ ...requested, replacement: rep })?.replacement, null);
  assert.equal(parseLegalRequestCard({ ...requested, mode: "expired", replacement: rep })?.replacement?.documentNumber, "10/2024/NĐ-CP");
  assert.equal(parseLegalRequestCard({ ...requested, display_type: "legal_candidates" }), null);
  assert.equal(parseLegalRequestCard({ ...requested, documents: [] })?.documents.length, 0);
});

test("takeLegalRequestCard pulls the card out of the steps", () => {
  const { card, steps } = takeLegalRequestCard([{ tool_data: requested }, { tool_data: { x: 1 } }]);
  assert.equal(card?.reference, "53/2022/NĐ-CP");
  assert.equal(steps?.length, 1);
});

test("date and status helpers", () => {
  assert.equal(showIssuedOn("2022-08-15"), "15/08/2022");
  assert.equal(showIssuedOn("2022-08-15T00:00:00Z"), "15/08/2022");
  assert.ok(statusInForce("Còn hiệu lực"));
  assert.ok(!statusInForce("Hết hiệu lực toàn bộ"));
});
