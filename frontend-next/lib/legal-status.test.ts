import { test } from "node:test";
import assert from "node:assert/strict";
import {
  formatLegalDate,
  knowledgeDocHref,
  legalDocShortLabel,
  legalLookupHref,
  legalStatusBadgeClass,
  legalStatusKindFromText,
  legalStatusLabelKey,
  legalStatusTextBadgeClass,
} from "./legal-status.ts";

test("status kinds pick a tone and a label; anything else is unknown", () => {
  assert.equal(legalStatusBadgeClass("in_force"), "badge-success");
  assert.equal(legalStatusBadgeClass("expired"), "badge-error");
  assert.equal(legalStatusBadgeClass("partly_expired"), "badge-warning");
  assert.equal(legalStatusBadgeClass("not_yet_in_force"), "badge-neutral");
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

test("status wordings map to the server's kinds", () => {
  assert.equal(legalStatusKindFromText("Còn hiệu lực"), "in_force");
  assert.equal(legalStatusKindFromText(" còn  hiệu lực "), "in_force");
  assert.equal(legalStatusKindFromText("Hết hiệu lực toàn bộ"), "expired");
  assert.equal(legalStatusKindFromText("Hết hiệu lực một phần"), "partly_expired");
  assert.equal(legalStatusKindFromText("Chưa có hiệu lực"), "not_yet_in_force");
  assert.equal(legalStatusKindFromText("Ngưng hiệu lực một phần"), "suspended");
  assert.equal(legalStatusKindFromText("Đình chỉ, tạm ngưng hiệu lực"), "suspended");
  assert.equal(legalStatusKindFromText("Chưa xác định"), "unknown");
  assert.equal(legalStatusKindFromText(null), "unknown");
});

test("the admin pages keep their tones for the portal's wordings", () => {
  // the tones the three admin copies gave before they were shared
  const before: Record<string, string> = {
    "Còn hiệu lực": "badge-success",
    "Hết hiệu lực toàn bộ": "badge-error",
    "Hết hiệu lực một phần": "badge-warning",
    "Ngưng hiệu lực": "badge-error",
    "Ngưng hiệu lực một phần": "badge-error",
    "Chưa có hiệu lực": "badge-neutral",
    "Chưa xác định": "badge-neutral",
    "": "badge-neutral",
  };
  for (const [status, tone] of Object.entries(before)) {
    assert.equal(legalStatusTextBadgeClass(status), tone, status);
  }
});

test("wordings whose tone changed on purpose when the copies were shared", () => {
  // [wording, tone before (admin copies), tone now] — now matching the
  // server's classification (types.ClassifyLegalStatus)
  const changed: [string, string, string][] = [
    ["Hết hiệu lực", "badge-warning", "badge-error"],
    ["Hết hiệu lực toàn bộ (theo vbpl)", "badge-warning", "badge-error"],
    ["Tạm ngưng hiệu lực", "badge-neutral", "badge-error"],
    ["Đình chỉ, tạm ngưng hiệu lực", "badge-neutral", "badge-error"],
  ];
  for (const [status, before, now] of changed) {
    assert.notEqual(before, now, status);
    assert.equal(legalStatusTextBadgeClass(status), now, status);
  }
  // the lookup page used amber for "not yet in force"; neutral everywhere now
  assert.equal(legalStatusBadgeClass("not_yet_in_force"), "badge-neutral");
});
