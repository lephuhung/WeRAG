// Pure helpers of the document scope chip and the clarification card.
// Runs with: node --experimental-strip-types --test lib/document-scope.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  SCOPE_CLARIFICATION_TYPE,
  SCOPE_MAX_SECTIONS,
  estimatedPages,
  initialScopeChoice,
  parseDocumentScope,
  parseScopeClarification,
  scopeCardAction,
  scopeCardFromDocuments,
  scopeChipParts,
  scopeChoiceReady,
  sectionsLabel,
  takeScopeClarification,
  taskTakesSections,
} from "./document-scope.ts";

const payload = {
  display_type: SCOPE_CLARIFICATION_TYPE,
  query: "góp ý giúp",
  documents: [
    {
      id: "ws-1",
      handle: "vb1",
      file_name: "quy-che.docx",
      role: "target",
      unit: "paragraph",
      paragraphs: 320,
      runes: 41000,
      long: true,
      sections: [
        { title: "Điều 1. Phạm vi", from: 3, to: 9 },
        { title: "Điều 2. Đối tượng", from: 10, to: 20 },
        { title: "Điều 3. Nguyên tắc", from: 21, to: 40 },
      ],
    },
    { id: "ws-2", handle: "vb2", file_name: "so-lieu.pdf", role: "source", paragraphs: 12, runes: 3000, sections: [] },
  ],
  tasks: [
    { key: "format", label: "Kiểm tra thể thức" },
    { key: "spelling", label: "Kiểm tra chính tả" },
    { key: "summary", label: "Tóm tắt" },
    { key: "part", label: "Xem một phần" },
    { key: "compare", label: "Đối chiếu với nguồn" },
    { key: "other", label: "Việc khác" },
    { key: "bogus", label: "?" },
  ],
  suggested: { document_id: "ws-1", task: "summary", sections: [{ title: "Điều 2. Đối tượng", from: 10, to: 20 }] },
};

describe("parseScopeClarification", () => {
  it("reads the card from tool data", () => {
    const card = parseScopeClarification(payload);
    assert.ok(card);
    assert.equal(card.query, "góp ý giúp");
    assert.equal(card.documents.length, 2);
    assert.equal(card.documents[0].sections.length, 3);
    assert.deepEqual(
      card.tasks.map((t) => t.key),
      ["format", "spelling", "summary", "part", "compare", "other"],
      "an unknown task is dropped",
    );
    assert.equal(card.suggested.document_id, "ws-1");
  });
  it("is null for other tool data", () => {
    assert.equal(parseScopeClarification({ display_type: "grep_results" }), null);
    assert.equal(parseScopeClarification(null), null);
    assert.equal(parseScopeClarification({ display_type: SCOPE_CLARIFICATION_TYPE, documents: [] }), null);
  });
  it("falls back to the first document when the suggestion names none", () => {
    const card = parseScopeClarification({ ...payload, suggested: { document_id: "gone", sections: [] } });
    assert.equal(card?.suggested.document_id, "ws-1");
    assert.equal(card?.suggested.task, "");
  });
});

describe("takeScopeClarification", () => {
  it("lifts the card out of the timeline steps", () => {
    const steps = [
      { type: "thinking", id: "t" },
      { type: "tool", id: "ragpipe-docscope-a", tool_data: payload },
    ];
    const { card, steps: rest } = takeScopeClarification(steps);
    assert.equal(card?.documents[0].handle, "vb1");
    assert.deepEqual(rest, [{ type: "thinking", id: "t" }]);
  });
  it("leaves other steps alone", () => {
    const steps = [{ type: "tool", id: "x", tool_data: { display_type: "grep_results" } }];
    const out = takeScopeClarification(steps);
    assert.equal(out.card, null);
    assert.equal(out.steps, steps);
    assert.equal(takeScopeClarification([{ tool_data: payload }]).steps, undefined, "a card-only turn has no timeline");
  });
});

describe("card choice and action", () => {
  const card = parseScopeClarification(payload)!;
  it("starts from the suggestion", () => {
    const choice = initialScopeChoice(card);
    assert.deepEqual(choice, { task: "summary", documentId: "ws-1", sections: [1] });
    assert.ok(scopeChoiceReady(card, choice));
  });
  it("summary stores a user scope with the ticked sections and re-sends the question", () => {
    const action = scopeCardAction(card, { task: "summary", documentId: "ws-1", sections: [2, 0, 2] });
    assert.deepEqual(action, {
      kind: "scope",
      scope: {
        document_ids: ["ws-1"],
        sections: [
          { document_id: "ws-1", from: 3, to: 9, title: "Điều 1. Phạm vi" },
          { document_id: "ws-1", from: 21, to: 40, title: "Điều 3. Nguyên tắc" },
        ],
        task: "summary",
        set_by: "user",
      },
      resend: "góp ý giúp",
    });
  });
  it("one part needs a section and is a lookup", () => {
    assert.equal(scopeChoiceReady(card, { task: "part", documentId: "ws-1", sections: [] }), false);
    assert.equal(scopeCardAction(card, { task: "part", documentId: "ws-1", sections: [] }), null);
    const action = scopeCardAction(card, { task: "part", documentId: "ws-1", sections: [1] });
    assert.equal(action?.kind === "scope" && action.scope.task, "lookup");
  });
  it("compare scopes every document, the chosen first", () => {
    const action = scopeCardAction(card, { task: "compare", documentId: "ws-2", sections: [] });
    assert.ok(action?.kind === "scope");
    assert.deepEqual(action.scope.document_ids, ["ws-2", "ws-1"]);
    assert.equal(action.scope.task, "compare");
  });
  it("format and spelling send a request naming the document; other hands the composer back", () => {
    assert.deepEqual(scopeCardAction(card, { task: "format", documentId: "ws-1", sections: [] }), {
      kind: "message",
      text: "Kiểm tra thể thức của vb1",
    });
    assert.deepEqual(scopeCardAction(card, { task: "spelling", documentId: "ws-1", sections: [] }), {
      kind: "message",
      text: "Kiểm tra chính tả của vb1",
    });
    assert.deepEqual(scopeCardAction(card, { task: "other", documentId: "ws-1", sections: [] }), { kind: "compose" });
  });
  it("no task picked yet: not ready", () => {
    assert.equal(scopeChoiceReady(card, { task: "", documentId: "ws-1", sections: [] }), false);
    assert.equal(scopeCardAction(card, { task: "", documentId: "ws-1", sections: [] }), null);
  });
  it("caps the sections of one scope", () => {
    const many = {
      ...card,
      documents: [{ ...card.documents[0], sections: Array.from({ length: 20 }, (_, i) => ({ title: `Điều ${i + 1}`, from: i, to: i })) }],
    };
    const action = scopeCardAction(many, { task: "part", documentId: "ws-1", sections: Array.from({ length: 20 }, (_, i) => i) });
    assert.equal(action?.kind === "scope" && action.scope.sections.length, SCOPE_MAX_SECTIONS);
  });
  it("shows the section list for summary, part and compare", () => {
    assert.ok(taskTakesSections("summary") && taskTakesSections("part") && taskTakesSections("compare"));
    assert.ok(!taskTakesSections("format") && !taskTakesSections("other") && !taskTakesSections(""));
  });
});

describe("scope chip", () => {
  const docs = [
    { id: "ws-1", handle: "vb1", file_name: "quy-che.docx" },
    { id: "ws-2", handle: "vb2", file_name: "so-lieu.pdf" },
  ];
  it("labels runs of one kind of section", () => {
    assert.equal(sectionsLabel([{ title: "Điều 3. A", from: 1, to: 2 }, { title: "Điều 5. C", from: 5, to: 6 }, { title: "Điều 4. B", from: 3, to: 4 }]), "Điều 3–5");
    assert.equal(sectionsLabel([{ title: "Điều 3. A", from: 1, to: 2 }, { title: "Điều 7. C", from: 5, to: 6 }]), "Điều 3, 7");
    assert.equal(sectionsLabel([{ title: "Chương II. X", from: 1, to: 9 }]), "Chương II");
    assert.equal(sectionsLabel([{ title: "Căn cứ ban hành", from: 0, to: 2 }, { title: "Điều 1. A", from: 3, to: 4 }]), "Căn cứ ban hành +1");
    assert.equal(sectionsLabel([]), "");
  });
  it("reads the scope of GET /documents", () => {
    const scope = parseDocumentScope({
      document_ids: ["ws-1", "gone"],
      sections: [{ document_id: "ws-1", from: 21, to: 40, title: "Điều 3. Nguyên tắc" }, { document_id: "ws-1" }],
      task: "compare",
      set_by: "router",
    });
    assert.ok(scope);
    assert.equal(scope.sections?.length, 1);
    const parts = scopeChipParts(scope, docs);
    assert.deepEqual(parts, { documents: "vb1", sections: "Điều 3", task: "compare", byUser: false });
    assert.equal(parseDocumentScope(null), null);
    assert.equal(parseDocumentScope({ document_ids: [] }), null);
  });
  it("hides when the scope names no open document", () => {
    assert.equal(scopeChipParts({ document_ids: ["gone"], set_by: "user" }, docs), null);
    assert.equal(scopeChipParts(null, docs), null);
  });
  it("opens a card from the session documents with the current scope suggested", () => {
    const card = scopeCardFromDocuments(
      [
        { id: "ws-1", handle: "vb1", file_name: "quy-che.docx", profile: { sections: [{ title: "Điều 1", from: 0, to: 4 }] } },
        { id: "ws-2", handle: "vb2", file_name: "so-lieu.pdf", role: "source" },
      ],
      { document_ids: ["ws-1"], sections: [{ document_id: "ws-1", from: 0, to: 4 }], task: "lookup", set_by: "user" },
      [
        { key: "summary", label: "" },
        { key: "part", label: "" },
        { key: "compare", label: "" },
      ],
    );
    assert.ok(card);
    assert.equal(card.query, "", "nothing is re-sent after changing the scope from the chip");
    assert.deepEqual(initialScopeChoice(card), { task: "part", documentId: "ws-1", sections: [0] });
    const one = scopeCardFromDocuments([{ id: "ws-1", file_name: "a.docx" }], null, [{ key: "compare", label: "" }, { key: "part", label: "" }]);
    assert.deepEqual(one?.tasks.map((t) => t.key), ["part"], "compare needs another document");
  });
  it("estimates pages", () => {
    assert.equal(estimatedPages(0), 1);
    assert.equal(estimatedPages(41000), 21);
  });
});
