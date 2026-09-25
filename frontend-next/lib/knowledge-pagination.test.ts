// B-phase2b(2) RED: DocPanel shows only the first chunk page and KbDetail
// shows only the first 100 files. fetchAllPages must walk every page via a
// mocked paginated API helper, honouring total/page_size, both data shapes,
// partial/empty pages (no busy-loop), mid-way errors (surfaced, not silent),
// and stale identity aborts.
// Runs with: node --experimental-strip-types --test lib/knowledge-pagination.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  fetchAllPages,
  normalizePaginatedResponse,
} from "./knowledge-pagination.ts";

function pageResponse(items: number[], total?: number) {
  return {
    success: true,
    data: items.map((i) => ({ id: `c${i}`, content: `t${i}` })),
    ...(total === undefined ? {} : { total }),
  };
}

describe("normalizePaginatedResponse", () => {
  it("reads the array data shape with a top-level total", () => {
    const norm = normalizePaginatedResponse(pageResponse([1, 2], 2));
    assert.equal(norm.items.length, 2);
    assert.equal(norm.total, 2);
  });

  it("reads the { items } data shape", () => {
    const norm = normalizePaginatedResponse<{ id: string }>({
      success: true,
      data: { items: [{ id: "a" }], total: 1 },
      total: 1,
    });
    assert.equal(norm.items.length, 1);
    assert.equal(norm.total, 1);
  });

  it("falls back to an empty page for unknown shapes", () => {
    assert.deepEqual(normalizePaginatedResponse(null).items, []);
    assert.deepEqual(normalizePaginatedResponse({ success: true }).items, []);
    assert.equal(normalizePaginatedResponse(null).total, null);
  });
});

describe("fetchAllPages", () => {
  it("paginates to completion honouring total (mocked API helper)", async () => {
    const calls: number[] = [];
    const out = await fetchAllPages<{ id: string }>(async (page) => {
      calls.push(page);
      if (page === 3) return pageResponse([5], 5);
      const start = (page - 1) * 2 + 1;
      return pageResponse([start, start + 1], 5);
    }, { pageSize: 2 });
    assert.equal(out.error, null);
    assert.equal(out.aborted, false);
    assert.deepEqual(calls, [1, 2, 3], "stops once accumulated items reach total");
    assert.deepEqual(
      out.items.map((c) => c.id),
      ["c1", "c2", "c3", "c4", "c5"],
    );
    assert.equal(out.total, 5);
  });

  it("stops on a partial page even without a total", async () => {
    const calls: number[] = [];
    const out = await fetchAllPages(async (page) => {
      calls.push(page);
      if (page === 1) return pageResponse([1, 2, 3]);
      return pageResponse([4]);
    }, { pageSize: 3 });
    assert.deepEqual(calls, [1, 2]);
    assert.equal(out.items.length, 4);
  });

  it("stops on an empty page without busy-looping", async () => {
    const calls: number[] = [];
    const out = await fetchAllPages(async (page) => {
      calls.push(page);
      return pageResponse([], 999);
    }, { pageSize: 25 });
    assert.deepEqual(calls, [1], "one empty page ends the walk even when total lies");
    assert.equal(out.items.length, 0);
  });

  it("surfaces a mid-way page error with the partial items (no silent empty)", async () => {
    const out = await fetchAllPages<{ id: string }>(async (page) => {
      if (page === 1) return pageResponse([1, 2], 4);
      throw new Error("boom p2");
    }, { pageSize: 2 });
    assert.equal(out.items.length, 2, "keeps the pages that did load");
    assert.match(out.error ?? "", /boom p2/);
    assert.equal(out.aborted, false);
  });

  it("surfaces a first-page error instead of pretending the document is empty", async () => {
    const out = await fetchAllPages(async () => {
      throw new Error("offline");
    }, { pageSize: 25 });
    assert.equal(out.items.length, 0);
    assert.match(out.error ?? "", /offline/);
  });

  it("aborts without further calls once the identity goes stale", async () => {
    const calls: number[] = [];
    let current = true;
    const out = await fetchAllPages<{ id: string }>(async (page) => {
      calls.push(page);
      if (page === 1) {
        current = false; // doc/KB switched while page 1 was resolving
        return pageResponse([1, 2], 6);
      }
      return pageResponse([3, 4], 6);
    }, { pageSize: 2, isCurrent: () => current });
    assert.equal(out.aborted, true);
    assert.deepEqual(calls, [1], "no page 2 fetch after the identity went stale");
    assert.equal(out.items.length, 0, "stale page 1 items are not handed to the new identity");
  });

  it("caps runaway totals with maxPages", async () => {
    const calls: number[] = [];
    const out = await fetchAllPages(async (page) => {
      calls.push(page);
      return pageResponse([page * 2 - 1, page * 2], 1_000_000);
    }, { pageSize: 2, maxPages: 3 });
    assert.deepEqual(calls, [1, 2, 3]);
    assert.equal(out.items.length, 6);
  });
});

describe("fetchAllPages maxPages honesty", () => {
  it("flags incomplete with item counts when total exceeds the cap", async () => {
    const out = await fetchAllPages<{ id: string }>(async (page) => {
      return pageResponse([page * 2 - 1, page * 2], 1_000_000);
    }, { pageSize: 2, maxPages: 3 });
    assert.equal(out.error, null);
    assert.equal(out.aborted, false);
    assert.equal(out.incomplete, true, "must not silently claim complete at the cap");
    assert.match(out.incompleteReason ?? "", /3 pages/);
    assert.match(out.incompleteReason ?? "", /6 of 1000000/);
    assert.equal(out.items.length, 6);
  });

  it("flags incomplete when the cap ends on a full page with unknown total", async () => {
    const out = await fetchAllPages<{ id: string }>(async (page) => {
      // No total at all: every page is full, so completeness is unprovable.
      return { success: true, data: [{ id: `a${page}x` }, { id: `a${page}y` }] };
    }, { pageSize: 2, maxPages: 3 });
    assert.equal(out.error, null);
    assert.equal(out.incomplete, true, "a full page at the cap cannot claim complete");
    assert.match(out.incompleteReason ?? "", /unknown total|may be incomplete/);
  });

  it("stays complete for stable walks that finish before the cap", async () => {
    const out = await fetchAllPages<{ id: string }>(async (page) => {
      if (page === 3) return pageResponse([5], 5);
      const start = (page - 1) * 2 + 1;
      return pageResponse([start, start + 1], 5);
    }, { pageSize: 2 });
    assert.equal(out.incomplete, false);
    assert.equal(out.incompleteReason, null);
    assert.equal(out.retried, false);
    assert.equal(out.items.length, 5);
  });
});

describe("fetchAllPages OFFSET-shift recovery", () => {
  // Simulates the backend's created_at DESC + OFFSET paging over a 101-file
  // KB: rows are ids newest-first, page N is slice((N-1)*size, N*size).
  function offsetBackend(rows: string[], pageSize: number) {
    return async (page: number) => ({
      success: true,
      data: rows.slice((page - 1) * pageSize, page * pageSize).map((id) => ({ id })),
      total: rows.length,
    });
  }

  it("recovers from a concurrent insert with one fresh walk (no dupes, nothing missed)", async () => {
    const rows = Array.from({ length: 101 }, (_, i) => `f${100 - i}`); // f100 newest
    let calls = 0;
    const fetchPage = async (page: number, size: number) => {
      calls += 1;
      const res = await offsetBackend(rows, size)(page);
      if (calls === 1) {
        // Concurrent upload lands after page 1 was read: shifts page 2 by one.
        rows.unshift("f101");
      }
      return res;
    };
    const out = await fetchAllPages<{ id: string }>(fetchPage, { pageSize: 50 });
    assert.equal(out.error, null);
    assert.equal(out.aborted, false);
    assert.equal(out.retried, true, "the shifted walk must trigger a bounded retry");
    assert.equal(out.incomplete, false, "a settled retry claims complete honestly");
    assert.equal(out.items.length, 102, "new row present, boundary row not duplicated");
    assert.deepEqual(
      new Set(out.items.map((d) => d.id)).size,
      102,
      "no duplicate ids after recovery",
    );
    assert.ok(out.items.some((d) => d.id === "f101"), "the inserted row is not missed");
  });

  it("reports incomplete instead of silently claiming complete when the list never settles", async () => {
    const rows = Array.from({ length: 30 }, (_, i) => `g${29 - i}`);
    const fetchPage = async (page: number, size: number) => {
      // Every page read races another insert: no walk can ever be stable.
      rows.unshift(`live-${page}-${rows.length}`);
      return offsetBackend(rows, size)(page);
    };
    const out = await fetchAllPages<{ id: string }>(fetchPage, {
      pageSize: 10,
      maxRetries: 1,
    });
    assert.equal(out.error, null);
    assert.equal(out.aborted, false);
    assert.equal(out.retried, true);
    assert.equal(out.incomplete, true, "an unstable list must never claim complete");
    assert.match(out.incompleteReason ?? "", /changed while loading/);
    assert.deepEqual(
      new Set(out.items.map((d) => String(d.id))).size,
      out.items.length,
      "returned items are de-duplicated",
    );
  });

  it("leaves stable array/items shapes untouched (no retry, no warning)", async () => {
    const arrayPages = await fetchAllPages<{ id: string }>(async (page) => {
      if (page > 2) return pageResponse([], 4);
      const start = (page - 1) * 2 + 1;
      return pageResponse([start, start + 1], 4);
    }, { pageSize: 2 });
    assert.equal(arrayPages.retried, false);
    assert.equal(arrayPages.incomplete, false);
    assert.equal(arrayPages.items.length, 4);

    const itemsShape = await fetchAllPages<{ id: string }>(async () => ({
      success: true,
      data: { items: [{ id: "a" }, { id: "b" }], total: 2 },
      total: 2,
    }), { pageSize: 25 });
    assert.equal(itemsShape.retried, false);
    assert.equal(itemsShape.incomplete, false);
    assert.equal(itemsShape.items.length, 2);
  });
});
