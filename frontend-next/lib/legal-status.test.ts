import { test } from "node:test";
import assert from "node:assert/strict";
import {
  formatLegalDate,
  knowledgeDocHref,
  legalDocShortLabel,
  legalLookupHref,
  legalStatusBadgeClass,
  legalStatusLabelKey,
} from "./legal-status.ts";

test("status kinds pick a tone and a label; anything else is unknown", () => {
  assert.equal(legalStatusBadgeClass("in_force"), "badge-success");
  assert.equal(legalStatusBadgeClass("expired"), "badge-error");
  assert.equal(legalStatusBadgeClass("partly_expired"), "badge-warning");
  assert.equal(legalStatusBadgeClass("bogus"), "badge-neutral");
  assert.equal(legalStatusLabelKey("not_yet_in_force"), "llookup.status.not_yet_in_force");
  assert.equal(legalStatusLabelKey(""), "llookup.status.unknown");
  assert.equal(legalStatusLabelKey(undefined), "llookup.status.unknown");
});

test("catalogue dates are calendar days, shown dd/mm/yyyy", () => {
  assert.equal(formatLegalDate("2024-08-01"), "01/08/2024");
  assert.equal(formatLegalDate("2024-08-01T00:00:00Z"), "01/08/2024");
  assert.equal(formatLegalDate(""), "");
  assert.equal(formatLegalDate(null), "");
  assert.equal(formatLegalDate("01/08/2024"), "");
});

test("lookup and document links", () => {
  assert.equal(legalLookupHref(" 30/2020/NĐ-CP "), "/platform/legal-lookup?q=30%2F2020%2FN%C4%90-CP");
  assert.equal(legalLookupHref(""), "/platform/legal-lookup");
  assert.equal(knowledgeDocHref("kb 1", "k/2"), "/platform/knowledge-bases/kb%201?knowledge_id=k%2F2");
  assert.equal(legalDocShortLabel({ doc_type: "Luật", document_number: "31/2024/QH15", title: "Luật Đất đai" }), "Luật 31/2024/QH15");
  assert.equal(legalDocShortLabel({ doc_type: "Luật", document_number: " ", title: "Luật Đất đai" }), "Luật Đất đai");
});
