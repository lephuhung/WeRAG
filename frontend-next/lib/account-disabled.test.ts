import { test } from "node:test";
import assert from "node:assert/strict";
import { isAccountDisabledReply } from "./account-disabled.ts";

test("isAccountDisabledReply matches only a 401 naming a locked account", () => {
  assert.equal(isAccountDisabledReply(401, { error: "Account is disabled", code: "ACCOUNT_DISABLED" }), true);
  assert.equal(isAccountDisabledReply(401, { code: "ACCOUNT_DISABLED" }), true);
  assert.equal(isAccountDisabledReply(401, { success: false, error: { code: 1001, message: "Account is disabled" } }), true);
  assert.equal(isAccountDisabledReply(401, { success: false, message: "Account is disabled" }), true);
  assert.equal(isAccountDisabledReply(403, { error: "Account is disabled" }), false);
  assert.equal(isAccountDisabledReply(401, { error: { code: 1001, message: "Token refresh failed" } }), false);
  assert.equal(isAccountDisabledReply(401, null), false);
});
