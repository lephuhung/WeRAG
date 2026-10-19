import { test } from "node:test";
import assert from "node:assert/strict";
import { resolveChatMode, availableChatChoices } from "./mobile-chat-policy.ts";

test("desktop preserves every selected agent and allows editing", () => {
  for (const agentId of ["builtin-document-assistant", "builtin-smart-reasoning", "custom-agent"]) {
    assert.deepEqual(resolveChatMode(agentId, false, false), { agentId, readOnly: false });
  }
});

test("new mobile chat always uses reasoning, including a saved document preference", () => {
  for (const agentId of ["builtin-document-assistant", "builtin-quick-answer", "custom-agent", ""]) {
    assert.deepEqual(resolveChatMode(agentId, true, true), { agentId: "builtin-smart-reasoning", readOnly: false });
  }
});

test("existing document conversations stay document conversations and become read-only", () => {
  assert.deepEqual(resolveChatMode("builtin-document-assistant", true, false, "builtin-document-assistant"), {
    agentId: "builtin-document-assistant", readOnly: true,
  });
});

test("fresh session after mobile navigation does not inherit desktop Word mode", () => {
  assert.deepEqual(resolveChatMode("builtin-document-assistant", true, false, null), {
    agentId: "builtin-smart-reasoning", readOnly: false,
  });
});

test("session evidence distinguishes Word history from a saved desktop preference", () => {
  assert.deepEqual(resolveChatMode("builtin-document-assistant", true, false, "builtin-smart-reasoning"), {
    agentId: "builtin-smart-reasoning", readOnly: false,
  });
  assert.deepEqual(resolveChatMode("builtin-smart-reasoning", true, false, "builtin-document-assistant"), {
    agentId: "builtin-document-assistant", readOnly: true,
  });
});

test("existing non-document mobile chat uses reasoning without changing stored preference", () => {
  const original = "custom-agent";
  assert.equal(resolveChatMode(original, true, false).agentId, "builtin-smart-reasoning");
  assert.equal(resolveChatMode(original, false, false).agentId, original);
});

test("mobile keeps ordinary clarifications but removes all document handoff choices", () => {
  const options = [{ label: "Clarify" }, { label: "Create Word", handoff: { agentId: "builtin-document-assistant" } }];
  assert.deepEqual(availableChatChoices(options, true), [options[0]]);
  assert.deepEqual(availableChatChoices(options, false), options);
});
