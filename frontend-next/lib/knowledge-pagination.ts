// Complete pagination over the knowledge chunk / knowledge file list
// endpoints. Both endpoints answer `{ success, data, total, page,
// page_size }` where `data` is either a bare array or `{ items }`; callers
// previously rendered only the first page (25 chunks / 100 files).
//
// fetchAllPages walks pages through a caller-supplied page fetcher until
// the accumulated items reach `total`, a partial page arrives, or an empty
// page arrives (the empty-page break is the busy-loop guard for servers
// that misreport `total`). A page failure keeps the pages that did load
// and returns the error message so the UI can surface it instead of
// silently rendering the document/KB as empty. `isCurrent` aborts the walk
// (and discards the partial walk) when the document/KB identity goes stale
// mid-pagination; `maxPages` caps runaway totals.
//
// Two integrity rules keep the walk honest:
//
// - maxPages cap: reaching the cap with `total` still unreached (or with a
//   full last page and an unknown total, where completeness cannot be
//   proven) reports `incomplete: true` with an item-count reason instead
//   of silently claiming a complete list via `error: null`.
// - OFFSET shift: the backend orders by created_at DESC with OFFSET, so an
//   insert between page fetches shifts every later page — the boundary row
//   repeats and the new row is missed. Duplicate ids (or a changed `total`
//   mid-walk) trigger one bounded fresh walk; if the list is still
//   unstable, the walk reports `incomplete: true` rather than silently
//   claiming completeness.

export interface NormalizedPage<T> {
  items: T[];
  total: number | null;
}

export function normalizePaginatedResponse<T>(res: unknown): NormalizedPage<T> {
  const r = res as {
    data?: unknown;
    total?: unknown;
  } | null;
  const data = r?.data;
  let items: T[] = [];
  if (Array.isArray(data)) items = data as T[];
  else if (data && typeof data === "object" && Array.isArray((data as { items?: unknown }).items)) {
    items = (data as { items: T[] }).items;
  }
  const topTotal = r?.total;
  const dataTotal =
    data && typeof data === "object" ? (data as { total?: unknown }).total : undefined;
  const total =
    typeof topTotal === "number" && Number.isFinite(topTotal)
      ? topTotal
      : typeof dataTotal === "number" && Number.isFinite(dataTotal)
        ? dataTotal
        : null;
  return { items, total };
}

export interface FetchAllPagesOptions<T = unknown> {
  pageSize: number;
  maxPages?: number;
  isCurrent?: () => boolean;
  /** Identity extractor for OFFSET-shift detection. Defaults to the
   * `id` / `knowledge_id` / `chunk_id` field. Items without an
   * extractable id are skipped by duplicate detection (never flagged). */
  idOf?: (item: T) => string | number | null | undefined;
  /** Bounded fresh-walk retries after an unstable (shifted) walk.
   * Defaults to 1. */
  maxRetries?: number;
}

export interface FetchAllPagesResult<T> {
  items: T[];
  total: number | null;
  pagesFetched: number;
  /** Non-null when a page failed mid-walk; `items` still holds whatever
   * loaded before the failure. */
  error: string | null;
  /** True when the walk stopped because the identity went stale. */
  aborted: boolean;
  /** True when the returned items may not be the whole list (maxPages cap
   * hit without proving completeness, or the list kept shifting under
   * OFFSET pagination). Never silently claim complete: callers must
   * surface `incompleteReason` when set. */
  incomplete: boolean;
  incompleteReason: string | null;
  /** True when at least one fresh-walk retry ran (OFFSET shift recovery). */
  retried: boolean;
}

function errorMessage(e: unknown): string {
  if (e instanceof Error && e.message) return e.message;
  return String(e ?? "request failed");
}

function defaultIdOf(item: unknown): string | number | null {
  if (item && typeof item === "object") {
    const r = item as Record<string, unknown>;
    for (const key of ["id", "knowledge_id", "chunk_id"]) {
      const v = r[key];
      if (typeof v === "string" || typeof v === "number") return v;
    }
  }
  return null;
}

/** De-duplicate by identity, keeping the first occurrence. Items without
 * an extractable id are always kept. */
function dedupeById<T>(items: T[], idOf: (item: T) => string | number | null | undefined): T[] {
  const seen = new Set<string | number>();
  const out: T[] = [];
  for (const item of items) {
    const id = idOf(item);
    if (id === null || id === undefined) {
      out.push(item);
      continue;
    }
    if (seen.has(id)) continue;
    seen.add(id);
    out.push(item);
  }
  return out;
}

interface SingleWalk<T> {
  items: T[];
  total: number | null;
  pagesFetched: number;
  error: string | null;
  aborted: boolean;
  duplicateCount: number;
  totalChanged: boolean;
  firstTotal: number | null;
  hitCap: boolean;
  lastPageFull: boolean;
}

export async function fetchAllPages<T>(
  fetchPage: (page: number, pageSize: number) => Promise<unknown>,
  opts: FetchAllPagesOptions<T>,
): Promise<FetchAllPagesResult<T>> {
  const pageSize = Math.max(1, Math.floor(opts.pageSize));
  const maxPages = Math.max(1, Math.floor(opts.maxPages ?? 200));
  const isCurrent = opts.isCurrent ?? (() => true);
  const idOf = opts.idOf ?? (defaultIdOf as (item: T) => string | number | null | undefined);
  const maxRetries = Math.max(0, Math.floor(opts.maxRetries ?? 1));

  const walkOnce = async (): Promise<SingleWalk<T>> => {
    const items: T[] = [];
    const seenIds = new Set<string | number>();
    let duplicateCount = 0;
    let total: number | null = null;
    let firstTotal: number | null = null;
    let totalChanged = false;
    let pagesFetched = 0;
    let page = 1;
    let hitCap = false;
    let lastPageFull = false;

    for (;;) {
      if (!isCurrent()) return { items: [], total, pagesFetched, error: null, aborted: true, duplicateCount, totalChanged, firstTotal, hitCap: false, lastPageFull: false };
      let res: unknown;
      try {
        res = await fetchPage(page, pageSize);
      } catch (e) {
        return { items, total, pagesFetched, error: errorMessage(e), aborted: false, duplicateCount, totalChanged, firstTotal, hitCap: false, lastPageFull: false };
      }
      if (!isCurrent()) return { items: [], total, pagesFetched, error: null, aborted: true, duplicateCount, totalChanged, firstTotal, hitCap: false, lastPageFull: false };
      const norm = normalizePaginatedResponse<T>(res);
      if (norm.total !== null) {
        if (firstTotal === null) firstTotal = norm.total;
        else if (norm.total !== firstTotal) totalChanged = true;
        total = norm.total;
      }
      pagesFetched += 1;
      lastPageFull = norm.items.length === pageSize;
      // Empty page: nothing more to walk, even if `total` claims otherwise.
      if (norm.items.length === 0) break;
      for (const item of norm.items) {
        const id = idOf(item);
        if (id !== null && id !== undefined) {
          if (seenIds.has(id)) duplicateCount += 1;
          else seenIds.add(id);
        }
        items.push(item);
      }
      // Partial page or accumulated total reached: this was the last page.
      // A non-positive total alongside real items is contradictory — ignore
      // it rather than truncating the walk on the first page.
      if (norm.items.length < pageSize) break;
      if (total !== null && total > 0 && items.length >= total) break;
      page += 1;
      if (page > maxPages) {
        hitCap = true;
        break;
      }
    }

    return { items, total, pagesFetched, error: null, aborted: false, duplicateCount, totalChanged, firstTotal, hitCap, lastPageFull };
  };

  let retried = false;
  for (let attempt = 0; ; attempt += 1) {
    const w = await walkOnce();
    if (w.error !== null || w.aborted) {
      return { items: w.items, total: w.total, pagesFetched: w.pagesFetched, error: w.error, aborted: w.aborted, incomplete: false, incompleteReason: null, retried };
    }
    const shifted = w.duplicateCount > 0 || w.totalChanged;
    if (shifted && attempt < maxRetries && isCurrent()) {
      // Bounded recovery: one fresh walk from page 1 against the now-settled
      // list. A concurrent insert that has already landed reads cleanly.
      retried = true;
      continue;
    }
    if (shifted) {
      const unique = dedupeById(w.items, idOf).length;
      const causes: string[] = [];
      if (w.duplicateCount > 0) {
        causes.push(
          `saw ${w.duplicateCount} duplicate ${w.duplicateCount === 1 ? "entry" : "entries"}`,
        );
      }
      if (w.totalChanged) causes.push(`total changed mid-walk (first saw ${w.firstTotal}, ended at ${w.total})`);
      return {
        items: dedupeById(w.items, idOf),
        total: w.total,
        pagesFetched: w.pagesFetched,
        error: null,
        aborted: false,
        incomplete: true,
        incompleteReason:
          `The list changed while loading (${causes.join("; ")}); ` +
          `showing ${unique} unique of ${w.items.length} loaded items — the list may be incomplete.`,
        retried,
      };
    }
    if (w.hitCap) {
      const provablyComplete =
        w.total !== null && w.total > 0 ? w.items.length >= w.total : !w.lastPageFull;
      if (!provablyComplete) {
        const count =
          w.total !== null && w.total > 0
            ? `${w.items.length} of ${w.total}`
            : `${w.items.length}`;
        const suffix =
          w.total !== null && w.total > 0
            ? "the list is incomplete"
            : "the list may be incomplete";
        return {
          items: w.items,
          total: w.total,
          pagesFetched: w.pagesFetched,
          error: null,
          aborted: false,
          incomplete: true,
          incompleteReason:
            `Stopped after ${maxPages} pages with ${count} items loaded; ${suffix}.`,
          retried,
        };
      }
    }
    return { items: w.items, total: w.total, pagesFetched: w.pagesFetched, error: null, aborted: false, incomplete: false, incompleteReason: null, retried };
  }
}
