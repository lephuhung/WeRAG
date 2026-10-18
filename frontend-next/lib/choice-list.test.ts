// Runs with: node --experimental-strip-types --test lib/choice-list.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { AGENT_HANDOFF_TYPE, parseChoiceList, takeChoiceList } from "./choice-list.ts";

const named = {
  display_type: AGENT_HANDOFF_TYPE, target_agent_id: "builtin-document-assistant", target_agent_name: "Soạn thảo văn bản",
  template_id: "ke-hoach", template_name: "Kế hoạch", resend: "Soạn kế hoạch từ nội dung đã trao đổi", from_conversation: true,
};

test("a hand-off card naming the type is one switch option (older payload without options)", () => {
  const c = parseChoiceList(named);
  assert.equal(c?.kind, AGENT_HANDOFF_TYPE);
  assert.equal(c?.options.length, 1);
  assert.deepEqual(c?.options[0].handoff, { templateId: "ke-hoach", agentId: "builtin-document-assistant" });
  assert.equal(c?.options[0].resend, "Soạn kế hoạch từ nội dung đã trao đổi");
  assert.equal(c?.options[0].label, "Kế hoạch");
});

test("with no type named, one option per type", () => {
  const c = parseChoiceList({
    ...named, template_id: "", resend: undefined,
    options: [
      { template_id: "cong-van", label: "Công văn", resend: "Soạn công văn từ nội dung đã trao đổi" },
      { template_id: "bao-cao", label: "Báo cáo", resend: "Soạn báo cáo từ nội dung đã trao đổi" },
      { template_id: "", label: "x", resend: "y" },
    ],
  });
  assert.deepEqual(c?.options.map((o) => o.handoff?.templateId), ["cong-van", "bao-cao"]);
});

test("a hand-off card with no target agent is not shown", () => {
  assert.equal(parseChoiceList({ ...named, target_agent_id: "" }), null);
});

test("history steps keep the card out of the timeline", () => {
  const { card, steps } = takeChoiceList([{ tool_data: named }, { tool_data: { display_type: "x" } }]);
  assert.equal(card?.kind, AGENT_HANDOFF_TYPE);
  assert.equal(steps?.length, 1);
});

test("the guessed type comes first and is the main button; the other types follow", () => {
  const c = parseChoiceList({
    ...named,
    options: [
      { template_id: "cong-van", label: "Công văn", resend: "Soạn công văn từ nội dung đã trao đổi" },
      { template_id: "ke-hoach", label: "Kế hoạch", resend: "Soạn kế hoạch từ nội dung đã trao đổi" },
      { template_id: "bao-cao", label: "Báo cáo", resend: "Soạn báo cáo từ nội dung đã trao đổi" },
    ],
  });
  assert.deepEqual(c?.options.map((o) => [o.handoff?.templateId, !!o.primary]), [["ke-hoach", true], ["cong-van", false], ["bao-cao", false]]);
});
