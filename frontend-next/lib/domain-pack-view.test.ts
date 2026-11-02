// Pure helpers of the SuperAdmin domain pack pages.
// Runs with: node --experimental-strip-types --test lib/domain-pack-view.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  documentTitle,
  formatFormulaValue,
  heldInKB,
  packOriginName,
  reconcileRequests,
  visibleDocuments,
} from "./domain-pack-view.ts";
import type { DomainPackDocumentView, DomainPackReconcileResult } from "./api/domain-packs-admin.ts";

const row = (over: Partial<DomainPackDocumentView["documents"][number]> = {}) => ({
  id: "x", document_number: "59/2020/QH14", title: "", doc_type: "", issuer: "", status: "Còn hiệu lực",
  held_in_kb: false, ...over,
});

describe("domain pack view", () => {
  it("reads the pack origin of a request", () => {
    assert.equal(packOriginName({ origin: "pack:opc-vn-business-law" }), "opc-vn-business-law");
    assert.equal(packOriginName({ origin: "chat" }), "");
    assert.equal(packOriginName(null), "");
    assert.equal(packOriginName({ portal: {} }), "");
  });

  it("splits reconcile requests into created and existing", () => {
    const result: DomainPackReconcileResult = {
      pack: "p", version: "1.0.0",
      found: [
        { number: "1/2020/QH14", used_for: "", document: { ...row({ title: "Luật A" }), request: { id: "r1", status: "requested", created: true } } },
        { number: "2/2020/QH14", used_for: "", document: row() },
      ],
      missing: [{ number: "3/2020/QH14", used_for: "", title: "Luật C", request: { id: "r2", status: "rejected", created: false } }],
      ambiguous: [{ number: "4/2020/QH14", used_for: "", documents: [{ ...row(), request: { id: "r3", status: "requested", created: true } }, row()] }],
      errors: [],
    };
    const { created, existing } = reconcileRequests(result);
    assert.deepEqual(created.map((r) => r.request.id), ["r1", "r3"]);
    assert.equal(created[0].title, "Luật A");
    assert.deepEqual(existing.map((r) => [r.number, r.request.status]), [["3/2020/QH14", "rejected"]]);
  });

  it("filters, titles and held state of documents", () => {
    const docs: DomainPackDocumentView[] = [
      { number: "a", used_for: "", articles: [], cited_in: [], formulas: [], lookup: "found", documents: [row({ title: "Từ danh mục", held_in_kb: true })], warning: false },
      { number: "b", title: "Từ gói", used_for: "", articles: [], cited_in: [], formulas: [], lookup: "missing", documents: [], warning: true },
    ];
    assert.deepEqual(visibleDocuments(docs, true).map((d) => d.number), ["b"]);
    assert.equal(visibleDocuments(docs, false).length, 2);
    assert.equal(documentTitle(docs[0]), "Từ danh mục");
    assert.equal(documentTitle(docs[1]), "Từ gói");
    assert.equal(heldInKB(docs[0].documents), true);
    assert.equal(heldInKB(docs[1].documents), false);
  });

  it("formats example values the Vietnamese way", () => {
    assert.equal(formatFormulaValue(1200000000), "1.200.000.000");
    assert.equal(formatFormulaValue(-30000000), "-30.000.000");
    assert.equal(formatFormulaValue(0.075), "0,075");
    assert.equal(formatFormulaValue(1.5), "1,5");
    assert.equal(formatFormulaValue(0), "0");
  });
});
