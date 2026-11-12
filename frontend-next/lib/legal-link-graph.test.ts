// The provision-link diagram model of the SuperAdmin link review.
// Runs with: npm run test:legal-link-graph (Node >= 22.6, type stripping)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  LAYOUT,
  buildLinkGraph,
  edgeState,
  edgesOf,
  isApprovable,
  nextApprovable,
  numberKey,
  amenderChips,
  defaultFocus,
  groupIdOf,
  parseProvisionInput,
  type LinkGraphCandidate,
} from "./legal-link-graph.ts";

let seq = 0;
function cand(p: Partial<LinkGraphCandidate>): LinkGraphCandidate {
  seq += 1;
  return {
    id: seq,
    target_path: "dieu:1",
    op: "sua_doi",
    amender_doc_id: "nd15",
    amender_doc_number: "15/2026/NĐ-CP",
    source_path: "dieu:1/khoan:1",
    review_status: "pending",
    match_status: "matched",
    ...p,
  };
}

describe("edge state", () => {
  it("tells what a reviewer can do", () => {
    assert.equal(edgeState({ review_status: "pending", amender_doc_id: "x" }), "pending");
    assert.equal(edgeState({ review_status: "pending", amender_doc_id: "" }), "blocked");
    assert.equal(edgeState({ review_status: "approved", amender_doc_id: "" }), "approved");
    assert.equal(edgeState({ review_status: "auto", amender_doc_id: "x" }), "auto");
    assert.equal(isApprovable({ review_status: "pending", amender_doc_id: "x", target_path: "" }), false);
    assert.equal(isApprovable({ review_status: "pending", amender_doc_id: "x", target_path: "dieu:2" }), true);
    assert.equal(isApprovable({ review_status: "approved", amender_doc_id: "x", target_path: "dieu:2" }), false);
  });
});

describe("buildLinkGraph", () => {
  const list = [
    cand({ target_path: "dieu:10/khoan:2", source_path: "dieu:1/khoan:3" }),
    cand({ target_path: "dieu:2", source_path: "dieu:1/khoan:1" }),
    cand({ target_path: "dieu:2", source_path: "" }),
    cand({ target_path: "dieu:3", amender_doc_id: "", amender_doc_number: "20/2026/NQ-CP", source_path: "dieu:4",
      amender_request: { status: "requested" } }),
    cand({ target_path: "dieu:3", amender_doc_id: "", amender_doc_number: "20/2026/NQ–CP ", source_path: "dieu:5" }),
    cand({ target_path: "dieu:5", amender_doc_id: "nd9", amender_doc_number: "9/2020/NĐ-CP", review_status: "approved" }),
    cand({ target_path: "dieu:6", review_status: "rejected" }),
  ];

  it("sorts A's provisions numerically and groups B by document", () => {
    const g = buildLinkGraph(list);
    assert.deepEqual(
      g.targets.map((t) => t.path),
      ["dieu:2", "dieu:3", "dieu:5", "dieu:6", "dieu:10/khoan:2"],
      "everything shown, rejected included",
    );
    assert.equal(g.hidden, 0);
    assert.deepEqual(
      g.groups.map((x) => [x.kind, x.number]),
      [
        ["doc", "15/2026/NĐ-CP"],
        ["blocked", "20/2026/NQ-CP"],
        ["doc", "9/2020/NĐ-CP"],
      ],
      "to decide first, then waiting for a document, then decided",
    );
    const nd15 = g.groups[0];
    assert.deepEqual(
      nd15.sources.map((s) => s.path),
      ["dieu:1/khoan:1", "dieu:1/khoan:3", ""],
      "provisions sorted, the unnamed one last",
    );
    assert.equal(nd15.approvableIds.length, 3);
    assert.equal(nd15.counts.rejected, 1);
    const blocked = g.groups[1];
    assert.equal(blocked.counts.blocked, 2, "one group for the number, dashes and spaces folded");
    assert.equal(blocked.requestStatus, "requested");
    assert.equal(blocked.approvableIds.length, 0, "a document missing from the catalogue is never approvable");
  });

  it("folds decided groups to their header and points their edges at it", () => {
    const g = buildLinkGraph(list);
    const decided = g.groups[2];
    assert.equal(decided.collapsed, true);
    assert.equal(decided.height, LAYOUT.header);
    const e = g.edges.find((x) => x.state === "approved");
    assert.equal(e?.to, decided.id);

    const open = buildLinkGraph(list, { collapsedGroups: new Set([g.groups[0].id]) });
    assert.equal(open.groups[0].collapsed, true);
    assert.equal(open.groups[2].collapsed, false, "an explicit set decides every group");
    assert.ok(open.groups[2].sources[0].y > 0);
  });

  it("lays rows out without overlap and sizes the drawing", () => {
    const g = buildLinkGraph(list);
    assert.equal(g.hidden, 0);
    const ys = g.targets.map((t) => t.y);
    for (let i = 1; i < ys.length; i++) assert.equal(ys[i] - ys[i - 1], LAYOUT.row);
    let prevEnd = 0;
    for (const grp of g.groups) {
      assert.ok(grp.y >= prevEnd, "groups do not overlap");
      prevEnd = grp.y + grp.height;
      for (const s of grp.collapsed ? [] : grp.sources) assert.ok(s.y >= grp.y + LAYOUT.header - 1 && s.y < prevEnd);
    }
    assert.ok(g.height >= prevEnd);
    assert.ok(g.height >= ys[ys.length - 1] + LAYOUT.row);
  });

  it("scales to many links on one document", () => {
    const many: LinkGraphCandidate[] = [];
    for (let i = 1; i <= 80; i++) {
      many.push(cand({ target_path: `dieu:${(i % 40) + 1}`, amender_doc_id: `b${i % 6}`, amender_doc_number: `${i % 6}/2026/NĐ-CP`,
        source_path: `dieu:${i}` }));
    }
    const g = buildLinkGraph(many);
    assert.equal(g.edges.length, 80);
    assert.equal(g.targets.length, 40);
    assert.equal(g.groups.length, 6);
    assert.equal(new Set(g.edges.map((e) => e.id)).size, 80);
  });

  it("finds the edges of a node and the next link to decide", () => {
    const g = buildLinkGraph(list, { undecidedOnly: true });
    const t2 = g.targets[0];
    assert.equal(edgesOf(g, t2.id).size, 2);
    assert.equal(edgesOf(g, g.groups[1].id).size, 2);
    assert.equal(edgesOf(g, "nope").size, 0);

    const first = nextApprovable(g, null);
    assert.equal(first?.from, "t:dieu:2");
    const second = nextApprovable(g, first!.candidateId);
    assert.notEqual(second?.candidateId, first?.candidateId);
    const done = new Set(g.edges.filter((e) => e.state === "pending").map((e) => e.candidateId));
    assert.equal(nextApprovable(g, null, done), null);
  });
});

describe("focus on one amending document", () => {
  const big: LinkGraphCandidate[] = [];
  // 20 provisions of A spread over Điều 1..90, one document acting on them
  // from 2 provisions; another document elsewhere; some decided links
  for (let i = 0; i < 20; i++) {
    big.push(cand({ target_path: `dieu:${1 + i * 4}`, amender_doc_id: "nq18", amender_doc_number: "18/2026/NQ-CP",
      source_path: i % 2 ? "dieu:1" : "dieu:2" }));
  }
  for (let i = 0; i < 30; i++) big.push(cand({ target_path: `dieu:${2 + i * 3}`, amender_doc_id: "nd15", source_path: `dieu:${i}` }));
  big.push(cand({ target_path: "dieu:200", amender_doc_id: "nq18", amender_doc_number: "18/2026/NQ-CP", review_status: "approved" }));
  big.push(cand({ target_path: "dieu:201", amender_doc_id: "nq18", amender_doc_number: "18/2026/NQ-CP", review_status: "rejected" }));

  it("lists the documents as chips and picks the first to review", () => {
    const chips = amenderChips(big);
    assert.deepEqual(
      chips.map((c) => [c.number, c.approvable, c.total]),
      [
        ["15/2026/NĐ-CP", 30, 30],
        ["18/2026/NQ-CP", 20, 22],
      ],
    );
    assert.equal(defaultFocus(chips), chips[0].id);
    const blockedOnly = amenderChips([cand({ amender_doc_id: "", amender_doc_number: "20/2026/NQ-CP" })]);
    assert.equal(defaultFocus(blockedOnly), blockedOnly[0].id);
    assert.equal(defaultFocus(amenderChips([cand({ review_status: "approved" })])), null, "nothing left: all");
  });

  it("draws only that document, centred against A's provisions", () => {
    const id = groupIdOf(big[0]);
    const g = buildLinkGraph(big, { focusGroup: id, undecidedOnly: true });
    assert.equal(g.focus, id);
    assert.equal(g.groups.length, 1);
    assert.equal(g.targets.length, 20, "only the provisions it acts on; decided ones hidden");
    assert.ok(g.edges.every((e) => e.groupId === id));
    const grp = g.groups[0];
    assert.equal(grp.collapsed, false);
    const top = g.targets[0].y;
    const bottom = g.targets[g.targets.length - 1].y + LAYOUT.nodeHeight;
    const leftMid = (top + bottom) / 2;
    const rightMid = grp.y + grp.height / 2;
    assert.ok(Math.abs(leftMid - rightMid) <= LAYOUT.row, `centred: ${leftMid} vs ${rightMid}`);
    // both ends of every edge lie within half the left column's span
    const half = (bottom - top) / 2 + LAYOUT.row;
    const ys = new Map(grp.sources.map((s) => [s.id, s.y]));
    for (const e of g.edges) {
      const t = g.targets.find((x) => x.id === e.from)!;
      assert.ok(Math.abs(t.y - (ys.get(e.to) ?? 0)) <= half);
    }

    const all = buildLinkGraph(big, { focusGroup: id });
    assert.equal(all.targets.length, 22, "undecided off: decided and rejected too");
    assert.equal(all.hidden, 30);
  });

  it("centres A's provisions when the document's block is the taller column", () => {
    const tall: LinkGraphCandidate[] = [];
    for (let i = 0; i < 10; i++) tall.push(cand({ target_path: "dieu:1", amender_doc_id: "b", source_path: `dieu:${i + 1}` }));
    const g = buildLinkGraph(tall, { focusGroup: groupIdOf(tall[0]) });
    const grp = g.groups[0];
    const t = g.targets[0];
    assert.ok(Math.abs(t.y + LAYOUT.nodeHeight / 2 - (grp.y + grp.height / 2)) <= LAYOUT.row);
  });

  it("is empty when the focused document has nothing undecided", () => {
    const g = buildLinkGraph(big, { focusGroup: "g:d:none", undecidedOnly: true });
    assert.equal(g.edges.length, 0);
    assert.equal(g.focus, null);
  });
});

describe("helpers", () => {
  it("folds catalogue numbers like the server", () => {
    assert.equal(numberKey("30 / 2020 / NĐ–CP"), "30/2020/ND-CP");
    assert.equal(numberKey("30/2020/ND-CP"), "30/2020/ND-CP");
  });

  it("reads a typed provision", () => {
    assert.equal(parseProvisionInput("Điều 5 khoản 2 điểm a"), "dieu:5/khoan:2/diem:a");
    assert.equal(parseProvisionInput("điểm đ khoản 3 Điều 12a"), "dieu:12a/khoan:3/diem:đ");
    assert.equal(parseProvisionInput("khoản 2 Điều 5"), "dieu:5/khoan:2");
    assert.equal(parseProvisionInput("dieu:7/khoan:1"), "dieu:7/khoan:1");
    assert.equal(parseProvisionInput("khoản 2"), null);
    assert.equal(parseProvisionInput(""), null);
  });
});
