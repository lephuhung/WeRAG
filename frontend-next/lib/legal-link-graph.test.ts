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
      ["dieu:2", "dieu:3", "dieu:5", "dieu:10/khoan:2"],
    );
    assert.equal(g.hidden, 1, "rejected links are hidden by default");
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
    const g = buildLinkGraph(list, { showRejected: true });
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
    const g = buildLinkGraph(list);
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
