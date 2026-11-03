/* Provision paths of the legal provision links ("dieu:10/khoan:2/diem:a")
 * — parsed, worded and sorted the way a Vietnamese reader numbers them. */

export interface ProvisionPath {
  article: string;
  clause: string;
  point: string;
}

/** The Vietnamese alphabet order of điểm letters (no f, j, w, z). */
const POINT_ORDER = "a ă â b c d đ e ê g h i k l m n o ô ơ p q r s t u ư v x y".split(" ");

/** parsePath reads "dieu:10/khoan:2/diem:a"; null for "" or an unknown shape. */
export function parsePath(path: string): ProvisionPath | null {
  if (!path) return null;
  const p: ProvisionPath = { article: "", clause: "", point: "" };
  for (const part of path.split("/")) {
    const [k, v] = part.split(":");
    if (!v) return null;
    if (k === "dieu") p.article = v;
    else if (k === "khoan") p.clause = v;
    else if (k === "diem") p.point = v;
    else return null;
  }
  return p.article ? p : null;
}

type Words = { article: string; clause: string; point: string };
const VI: Words = { article: "Điều", clause: "khoản", point: "điểm" };
const EN: Words = { article: "Article", clause: "clause", point: "point" };

/** pathLabel words a path: "Điều 10 khoản 2 điểm a" (vi) / "Article 10, clause 2, point a" (en).
 * withArticle false drops the article ("Khoản 2 điểm a"), for rows under an article heading. */
export function pathLabel(path: string, locale: string, withArticle = true): string {
  const p = parsePath(path);
  if (!p) return "";
  const w = locale === "vi" ? VI : EN;
  const sep = locale === "vi" ? " " : ", ";
  const parts: string[] = [];
  if (withArticle) parts.push(`${w.article} ${p.article}`);
  if (p.clause) parts.push(`${w.clause} ${p.clause}`);
  if (p.point) parts.push(`${w.point} ${p.point}`);
  const s = parts.join(sep);
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** The sort key of one number: "10a" → [10, "a"]. */
function numKey(v: string): [number, string] {
  const m = /^(\d+)(.*)$/.exec(v);
  return m ? [parseInt(m[1], 10), m[2]] : [Number.MAX_SAFE_INTEGER, v];
}

function compareNum(a: string, b: string): number {
  if (a === b) return 0;
  if (!a) return -1; // the whole article before its clauses
  if (!b) return 1;
  const [na, sa] = numKey(a);
  const [nb, sb] = numKey(b);
  return na !== nb ? na - nb : sa.localeCompare(sb, "vi");
}

function comparePoint(a: string, b: string): number {
  if (a === b) return 0;
  if (!a) return -1;
  if (!b) return 1;
  const ia = POINT_ORDER.indexOf(a.toLowerCase());
  const ib = POINT_ORDER.indexOf(b.toLowerCase());
  if (ia >= 0 && ib >= 0) return ia - ib;
  return a.localeCompare(b, "vi");
}

/** comparePaths orders paths numerically: Điều 2 < Điều 10, the article
 * before its clauses, đ after d; unknown paths last. */
export function comparePaths(a: string, b: string): number {
  const pa = parsePath(a);
  const pb = parsePath(b);
  if (!pa || !pb) return pa ? -1 : pb ? 1 : 0;
  return (
    compareNum(pa.article, pb.article) || compareNum(pa.clause, pb.clause) || comparePoint(pa.point, pb.point)
  );
}

/** compareArticles orders article numbers ("2" < "10" < "10a"). */
export function compareArticles(a: string, b: string): number {
  return compareNum(a, b);
}
