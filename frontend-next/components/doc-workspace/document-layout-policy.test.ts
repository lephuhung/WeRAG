// Document editor layout gate and split bounds.
// Runs with: npm run test:doc-layout (Node >= 22.6, type stripping)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { DOCUMENT_EDITOR, resolveDocumentLayout } from "./document-layout-policy.ts";
import { DEFAULT_SPLIT_BOUNDS, SPLIT_DIVIDER_PX, effectiveBounds } from "./split-pane-math.ts";

describe("document layout", () => {
  it("picks a layout per viewport", () => {
    assert.equal(resolveDocumentLayout(1920, 1080, true), "side");
    assert.equal(resolveDocumentLayout(1536, 864, true), "side");
    assert.equal(resolveDocumentLayout(1440, 800, true), "side-folded");
    assert.equal(resolveDocumentLayout(1366, 768, true), "side-folded");
    assert.equal(resolveDocumentLayout(1024, 650, true), "side-folded");
    assert.equal(resolveDocumentLayout(900, 1000, false), "stacked");
    assert.equal(resolveDocumentLayout(800, 600, false), "stacked");
    assert.equal(resolveDocumentLayout(1366, 500, true), "blocked");
    assert.equal(resolveDocumentLayout(700, 900, false), "blocked");
  });
});

describe("effective split bounds", () => {
  const minRight = DOCUMENT_EDITOR.minAssistantPx;
  it("keeps the nominal bounds on a wide pane", () => {
    assert.deepEqual(effectiveBounds(DEFAULT_SPLIT_BOUNDS, 2000, minRight), DEFAULT_SPLIT_BOUNDS);
  });
  it("lowers the ceiling so the assistant keeps its minimum", () => {
    const b = effectiveBounds(DEFAULT_SPLIT_BOUNDS, 1024, minRight);
    assert.ok(Math.abs((b.max / 100) * 1024 + minRight + SPLIT_DIVIDER_PX - 1024) < 1e-9);
    assert.ok(b.min <= b.max && b.def <= b.max);
  });
  it("never lets the floor pass the ceiling (rail unfolded at 1024)", () => {
    const b = effectiveBounds(DEFAULT_SPLIT_BOUNDS, 760, minRight);
    assert.equal(b.min, b.max);
    assert.equal(b.def, b.max);
  });
});

import { dismissSidebarFold, holdSidebarFold, resetSidebarFoldDismissal, sidebarFoldedNow } from "../../lib/sidebar-state.ts";

describe("sidebar fold", () => {
  it("counts holders so a pane swap keeps the rail folded", () => {
    const a = holdSidebarFold();
    const b = holdSidebarFold(); // the next session's pane mounts first
    a();
    assert.equal(sidebarFoldedNow(), true);
    b();
    assert.equal(sidebarFoldedNow(), false);
  });
  it("stays unfolded after the user brings the rail back, until reset", () => {
    const a = holdSidebarFold();
    dismissSidebarFold();
    assert.equal(sidebarFoldedNow(), false);
    const b = holdSidebarFold(); // a session click remounts the pane
    assert.equal(sidebarFoldedNow(), false);
    resetSidebarFoldDismissal(); // wide window
    assert.equal(sidebarFoldedNow(), true);
    a();
    b();
    a(); // a release is idempotent
    assert.equal(sidebarFoldedNow(), false);
  });
});
