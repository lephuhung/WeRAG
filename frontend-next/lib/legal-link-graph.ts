/* The provision-link diagram of one amended document A on the SuperAdmin
 * link review page (admin-legal-links.tsx): A's provisions in the left
 * column, the amending documents B on the right — a header per document
 * with its provisions under it — and one edge per pushed candidate. Pure
 * data and layout, tested with `npm run test:legal-link-graph`; the
 * component only draws what this returns. Same idea as the TVPL
 * extension's link graph (src/lib/graph.ts), at provision level. */

import { comparePaths } from "./legal-paths.ts";

/** The candidate fields the diagram reads (a LegalLinkCandidate fits). */
export interface LinkGraphCandidate {
  id: number;
  target_path: string;
  op: string;
  amender_doc_id: string;
  amender_doc_number: string;
  source_path: string;
  review_status: string;
  match_status: string;
  amender?: { document_number: string; title: string } | null;
  amender_request?: { status: string } | null;
}

/** What a reviewer can do with an edge. */
export type EdgeState = "pending" | "blocked" | "approved" | "rejected" | "auto";

export interface TargetNode {
  id: string;
  path: string;
  y: number;
  edgeIds: string[];
  /** Pending edges a person can decide now. */
  approvable: number;
}

export interface SourceNode {
  id: string;
  groupId: string;
  /** "" = the source named no provision of B. */
  path: string;
  y: number;
  edgeIds: string[];
  approvable: number;
}

export interface AmenderGroup {
  id: string;
  /** "doc": B is in the catalogue; "blocked": only its number is known. */
  kind: "doc" | "blocked";
  amenderDocId: string;
  number: string;
  title: string;
  requestStatus: string;
  y: number;
  height: number;
  collapsed: boolean;
  sources: SourceNode[];
  edgeIds: string[];
  /** Ids of the pending candidates a person can approve as they are. */
  approvableIds: number[];
  counts: Record<EdgeState, number>;
}

export interface GraphEdge {
  id: string;
  candidateId: number;
  from: string;
  /** A source node, or the group header when the group is collapsed. */
  to: string;
  op: string;
  state: EdgeState;
  approvable: boolean;
}

export interface LinkGraph {
  targets: TargetNode[];
  groups: AmenderGroup[];
  edges: GraphEdge[];
  /** Height of the drawing (both columns). */
  height: number;
  /** Candidates left out (rejected while hidden). */
  hidden: number;
}

export const LAYOUT = {
  top: 12,
  row: 34,
  nodeHeight: 26,
  header: 46,
  headerHeight: 38,
  groupGap: 14,
  bottom: 12,
} as const;

/** The reviewer's view of a candidate. */
export function edgeState(c: Pick<LinkGraphCandidate, "review_status" | "amender_doc_id">): EdgeState {
  switch (c.review_status) {
    case "approved":
      return "approved";
    case "rejected":
      return "rejected";
    case "auto":
      return "auto";
  }
  return c.amender_doc_id ? "pending" : "blocked";
}

/** Whether a person can approve the candidate as it is (no correction). */
export function isApprovable(c: Pick<LinkGraphCandidate, "review_status" | "amender_doc_id" | "target_path">): boolean {
  return c.review_status === "pending" && c.amender_doc_id !== "" && c.target_path !== "";
}

/** The catalogue number key the server uses (CatalogNumberKey, in short):
 * upper case, no spaces, Đ → D, dashes folded. */
export function numberKey(n: string): string {
  return (n ?? "")
    .normalize("NFC")
    .replace(/[‐-―−]/g, "-")
    .replace(/\s+/g, "")
    .toUpperCase()
    .replace(/Đ|Ð/g, "D");
}

function groupKey(c: LinkGraphCandidate): string {
  return c.amender_doc_id ? `d:${c.amender_doc_id}` : `n:${numberKey(c.amender_doc_number)}`;
}

const STATES: EdgeState[] = ["pending", "blocked", "approved", "rejected", "auto"];

function emptyCounts(): Record<EdgeState, number> {
  return Object.fromEntries(STATES.map((s) => [s, 0])) as Record<EdgeState, number>;
}

function comparePathsBlankLast(a: string, b: string): number {
  if (a === b) return 0;
  if (!a) return 1;
  if (!b) return -1;
  return comparePaths(a, b) || a.localeCompare(b);
}

/**
 * buildLinkGraph lays the candidates of ONE document A out. Groups with
 * links to decide come first, then the documents still missing from the
 * catalogue, then the decided ones; collapsedGroups (ids) fold a group to
 * its header — undefined folds every group with nothing pending.
 */
export function buildLinkGraph(
  cands: LinkGraphCandidate[],
  opts: { showRejected?: boolean; collapsedGroups?: ReadonlySet<string> } = {},
): LinkGraph {
  const visible = cands.filter((c) => opts.showRejected || c.review_status !== "rejected");
  const targets = new Map<string, TargetNode>();
  const groups = new Map<string, AmenderGroup>();
  const sources = new Map<string, SourceNode>();
  const edges: GraphEdge[] = [];

  for (const c of visible) {
    const state = edgeState(c);
    const approvable = isApprovable(c);
    const tid = `t:${c.target_path}`;
    let t = targets.get(tid);
    if (!t) {
      t = { id: tid, path: c.target_path, y: 0, edgeIds: [], approvable: 0 };
      targets.set(tid, t);
    }
    const gid = `g:${groupKey(c)}`;
    let g = groups.get(gid);
    if (!g) {
      g = {
        id: gid,
        kind: c.amender_doc_id ? "doc" : "blocked",
        amenderDocId: c.amender_doc_id,
        number: c.amender?.document_number || c.amender_doc_number,
        title: c.amender?.title ?? "",
        requestStatus: c.amender_request?.status ?? "",
        y: 0,
        height: 0,
        collapsed: false,
        sources: [],
        edgeIds: [],
        approvableIds: [],
        counts: emptyCounts(),
      };
      groups.set(gid, g);
    }
    const sid = `s:${groupKey(c)}|${c.source_path}`;
    let s = sources.get(sid);
    if (!s) {
      s = { id: sid, groupId: gid, path: c.source_path, y: 0, edgeIds: [], approvable: 0 };
      sources.set(sid, s);
      g.sources.push(s);
    }
    const e: GraphEdge = { id: `e:${c.id}`, candidateId: c.id, from: tid, to: sid, op: c.op, state, approvable };
    edges.push(e);
    t.edgeIds.push(e.id);
    s.edgeIds.push(e.id);
    g.edgeIds.push(e.id);
    g.counts[state] += 1;
    if (approvable) {
      t.approvable += 1;
      s.approvable += 1;
      g.approvableIds.push(c.id);
    }
  }

  const rank = (g: AmenderGroup) => (g.counts.pending > 0 ? 0 : g.counts.blocked > 0 ? 1 : 2);
  const groupList = [...groups.values()].sort(
    (a, b) => rank(a) - rank(b) || b.counts.pending - a.counts.pending || a.number.localeCompare(b.number, "vi"),
  );
  const targetList = [...targets.values()].sort((a, b) => comparePathsBlankLast(a.path, b.path));

  // left column
  let y = LAYOUT.top;
  for (const t of targetList) {
    t.y = y;
    y += LAYOUT.row;
  }
  const leftHeight = y;

  // right column
  y = LAYOUT.top;
  for (const g of groupList) {
    g.sources.sort((a, b) => comparePathsBlankLast(a.path, b.path));
    g.collapsed = opts.collapsedGroups ? opts.collapsedGroups.has(g.id) : g.counts.pending === 0 && g.counts.blocked === 0;
    g.y = y;
    y += LAYOUT.header;
    if (!g.collapsed) {
      for (const s of g.sources) {
        s.y = y;
        y += LAYOUT.row;
      }
    }
    g.height = y - g.y;
    y += LAYOUT.groupGap;
  }
  const rightHeight = groupList.length ? y - LAYOUT.groupGap : y;

  for (const e of edges) {
    const s = sources.get(e.to);
    const g = s ? groups.get(s.groupId) : undefined;
    if (g?.collapsed) e.to = g.id;
  }

  return {
    targets: targetList,
    groups: groupList,
    edges,
    height: Math.max(leftHeight, rightHeight, LAYOUT.top + LAYOUT.row) + LAYOUT.bottom,
    hidden: cands.length - visible.length,
  };
}

/** The edges touching a node (a target, a source or a whole group). */
export function edgesOf(graph: LinkGraph, nodeId: string): Set<string> {
  const t = graph.targets.find((n) => n.id === nodeId);
  if (t) return new Set(t.edgeIds);
  for (const g of graph.groups) {
    if (g.id === nodeId) return new Set(g.edgeIds);
    const s = g.sources.find((n) => n.id === nodeId);
    if (s) return new Set(s.edgeIds);
  }
  const e = graph.edges.find((x) => x.id === nodeId);
  return e ? new Set([e.id]) : new Set();
}

/** The next pending edge a person can decide after edgeId, in drawing order
 * (A's provisions top to bottom); null when none is left. */
export function nextApprovable(graph: LinkGraph, afterCandidateId: number | null, skip: ReadonlySet<number> = new Set()): GraphEdge | null {
  const order = new Map(graph.targets.map((t, i) => [t.id, i]));
  const list = graph.edges
    .filter((e) => e.state === "pending" && !skip.has(e.candidateId))
    .sort((a, b) => (order.get(a.from) ?? 0) - (order.get(b.from) ?? 0) || a.candidateId - b.candidateId);
  if (!list.length) return null;
  if (afterCandidateId == null) return list[0];
  const i = list.findIndex((e) => e.candidateId === afterCandidateId);
  if (i < 0) return list[0];
  return list[i + 1] ?? list.find((e) => e.candidateId !== afterCandidateId) ?? null;
}

/**
 * parseProvisionInput reads a provision a reviewer types when the document's
 * structure is unknown: "Điều 5 khoản 2 điểm a", "khoản 2 Điều 5", "điểm a
 * khoản 2 Điều 5" or the stored form "dieu:5/khoan:2/diem:a". null when no
 * Điều is named.
 */
export function parseProvisionInput(text: string): string | null {
  const s = (text ?? "").normalize("NFC").trim().toLowerCase();
  if (!s) return null;
  if (/^dieu:[0-9a-zđ]+(\/khoan:[0-9a-zđ]+)?(\/diem:[a-zđ0-9]+)?$/.test(s)) return s;
  const article = /điều\s*([0-9]+[a-zđ]?)/.exec(s)?.[1];
  if (!article) return null;
  const clause = /khoản\s*([0-9]+[a-zđ]?)/.exec(s)?.[1];
  const point = /điểm\s*([a-zđ][0-9]*)/.exec(s)?.[1];
  let path = `dieu:${article}`;
  if (clause) path += `/khoan:${clause}`;
  if (point) path += `/diem:${point}`;
  return path;
}
