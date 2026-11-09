// Runs with: node --experimental-strip-types --test lib/turn-failure.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { isTurnFailure, turnFailureView } from "./turn-failure.ts";

const tr = (k: string) => `T(${k})`;

test("an unavailable model shows the translated note", () => {
  assert.deepEqual(turnFailureView("llm_unavailable", tr), { isError: true, errorText: "T(chat.err.llmUnavailable)" });
  assert.deepEqual(turnFailureView("llm_unavailable_after_search", tr), {
    isError: true,
    errorText: "T(chat.err.llmUnavailableAfterSearch)",
  });
});

test("an interrupted answer is an error without a second note", () => {
  assert.deepEqual(turnFailureView("llm_interrupted", tr), { isError: true });
});

test("anything else is not a turn failure", () => {
  assert.equal(turnFailureView(undefined, tr), null);
  assert.equal(turnFailureView("", tr), null);
  assert.equal(turnFailureView("toString", tr), null);
  assert.equal(isTurnFailure("other"), false);
});
