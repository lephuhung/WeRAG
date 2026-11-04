// Which surfaces fit on which screen.
// Runs with: npm run test:large-view (Node >= 22.6, type stripping)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { LARGE_VIEW, fitsLargeView, largeViewQuery, type LargeViewId } from "./large-view-policy.ts";

const IDS = Object.keys(LARGE_VIEW) as LargeViewId[];

describe("large-view gate", () => {
  it("passes exactly at the threshold, on both sides", () => {
    for (const id of IDS) {
      const view = LARGE_VIEW[id];
      assert.equal(fitsLargeView(id, view.minWidth, view.minHeight), true, id);
      assert.equal(fitsLargeView(id, view.minWidth - 1, view.minHeight), false, id);
      assert.equal(fitsLargeView(id, view.minWidth, view.minHeight - 1), false, id);
    }
  });

  it("lets the graph through on the screens it can serve", () => {
    assert.equal(fitsLargeView("graph", 1920, 1080), true);
    assert.equal(fitsLargeView("graph", 1024, 768), true);
    // phones and tablets in portrait stay on the notice
    assert.equal(fitsLargeView("graph", 390, 844), false);
    assert.equal(fitsLargeView("graph", 768, 1024), false);
    assert.equal(fitsLargeView("graph", 820, 1180), false);
  });

  it("keeps the processing trace on laptops, not on phones", () => {
    assert.equal(fitsLargeView("timeline", 1024, 768), true);
    assert.equal(fitsLargeView("timeline", 768, 1024), true);
    assert.equal(fitsLargeView("timeline", 390, 844), false);
    assert.equal(fitsLargeView("timeline", 844, 390), false);
  });

  it("keeps every media query and size label in step with its numbers", () => {
    for (const id of IDS) {
      const view = LARGE_VIEW[id];
      assert.equal(
        largeViewQuery(id),
        `(min-width: ${view.minWidth}px) and (min-height: ${view.minHeight}px)`,
        id,
      );
      assert.equal(view.sizeLabel, `${view.minWidth} × ${view.minHeight} px`, id);
    }
  });
});
