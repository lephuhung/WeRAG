// Localized error text instead of the backend's raw message.
// Runs with: node --experimental-strip-types --test lib/user-error.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { ApiError } from "./api-client.ts";
import { ChatStreamError } from "./api/chat-errors.ts";
import { UserFacingError, classifyUserError, isVietnameseText, registerErrorKey, userErrorMessage, type Translate } from "./user-error.ts";

const t: Translate = (k, vars) => (vars ? `${k}${JSON.stringify(vars)}` : k);
const envelope = (code: number, message: string, details?: unknown) => ({ success: false, error: { code, message, details } });

describe("isVietnameseText", () => {
  it("tells Vietnamese from English", () => {
    assert.equal(isVietnameseText("Không tìm thấy kho tri thức"), true);
    assert.equal(isVietnameseText("Đ"), true);
    assert.equal(isVietnameseText("knowledge base not found"), false);
    assert.equal(isVietnameseText(""), false);
  });
});

describe("classifyUserError", () => {
  it("maps backend codes that name their failure, whatever the status", () => {
    assert.deepEqual(classifyUserError(new ApiError(409, "tenant exists", envelope(2001, "tenant exists"))), { kind: "key", key: "err.tenantExists" });
    assert.deepEqual(classifyUserError(new ApiError(403, "x", envelope(2005, "x"))), { kind: "key", key: "err.tenantCreationDisabled" });
    assert.deepEqual(classifyUserError(new ApiError(503, "x", envelope(2105, "x", { reason: "public_lookup_no_knowledge" }))), {
      kind: "key",
      key: "chat.err.publicLookup.noKnowledge",
    });
    assert.deepEqual(classifyUserError(new ApiError(403, "x", { error: "need tenant", code: "TENANT_REQUIRED" })), { kind: "key", key: "chat.err.tenantRequired" });
    assert.deepEqual(classifyUserError(new ApiError(403, "x", { error: "disabled", code: "ACCOUNT_DISABLED" })), { kind: "key", key: "auth.accountDisabled" });
  });
  it("maps usage-package refusals by reason", () => {
    assert.deepEqual(classifyUserError(new ApiError(429, "x", envelope(1006, "x", { reason: "no_seat" }))), { kind: "key", key: "chat.err.usage.noSeat" });
  });
  it("401 always asks to sign in again, even with a Vietnamese text", () => {
    assert.deepEqual(classifyUserError(new ApiError(401, "Please sign in again")), { kind: "key", key: "err.signInAgain" });
    assert.deepEqual(classifyUserError(new ApiError(401, "Hết phiên")), { kind: "key", key: "err.signInAgain" });
    assert.deepEqual(classifyUserError(new Error("unauthorized")), { kind: "key", key: "err.signInAgain" });
  });
  it("keeps a backend message already in Vietnamese", () => {
    assert.deepEqual(classifyUserError(new ApiError(400, "x", envelope(1000, "Tên kho đã tồn tại"))), { kind: "text", text: "Tên kho đã tồn tại" });
    assert.deepEqual(classifyUserError(new ApiError(409, "Tài liệu đã có")), { kind: "text", text: "Tài liệu đã có" });
    assert.deepEqual(classifyUserError(new Error("Không đọc được tệp")), { kind: "text", text: "Không đọc được tệp" });
  });
  it("maps English messages by status class", () => {
    const cases: [number, string][] = [
      [403, "err.forbidden"],
      [404, "err.notFound"],
      [409, "err.conflict"],
      [413, "err.tooLarge"],
      [429, "err.tooMany"],
      [500, "err.server"],
      [502, "err.unavailable"],
      [503, "err.unavailable"],
    ];
    for (const [status, key] of cases) {
      assert.deepEqual(classifyUserError(new ApiError(status, "Request failed")), { kind: "key", key }, String(status));
    }
    assert.deepEqual(classifyUserError(new ChatStreamError(404, 1003, "session not found")), { kind: "key", key: "err.notFound" });
    assert.deepEqual(classifyUserError(new Error("HTTP 500: {}")), { kind: "key", key: "err.server" });
  });
  it("uses the caller's fallback for an English 400 or a bare error", () => {
    assert.deepEqual(classifyUserError(new ApiError(400, "password must be 8-32 characters")), { kind: "fallback" });
    assert.deepEqual(classifyUserError(new Error("something broke")), { kind: "fallback" });
    assert.deepEqual(classifyUserError(undefined), { kind: "fallback" });
    assert.deepEqual(classifyUserError("plain english"), { kind: "fallback" });
  });
  it("reads transport failures", () => {
    assert.deepEqual(classifyUserError(new ApiError(0, "Network error during upload")), { kind: "key", key: "err.network" });
    assert.deepEqual(classifyUserError(new ApiError(0, "Upload timed out")), { kind: "key", key: "err.timeout" });
    assert.deepEqual(classifyUserError(new TypeError("Failed to fetch")), { kind: "key", key: "err.network" });
    assert.deepEqual(classifyUserError(new ApiError(504, "x", envelope(1009, "timeout"))), { kind: "key", key: "err.timeout" });
  });
  it("says nothing for an aborted request", () => {
    assert.deepEqual(classifyUserError(new ApiError(0, "Upload aborted")), { kind: "aborted" });
    const abort = new Error("The operation was aborted");
    abort.name = "AbortError";
    assert.deepEqual(classifyUserError(abort), { kind: "aborted" });
  });
  it("reads status-carrying wrappers (DocumentWorkspaceError)", () => {
    class Wrapped extends Error {
      status = 403;
      code = "request_failed";
    }
    assert.deepEqual(classifyUserError(new Wrapped("forbidden")), { kind: "key", key: "err.forbidden" });
  });
  it("shows a UserFacingError as is", () => {
    assert.deepEqual(classifyUserError(new UserFacingError("Upload failed")), { kind: "text", text: "Upload failed" });
  });
});

describe("userErrorMessage", () => {
  it("translates keys and falls back", () => {
    assert.equal(userErrorMessage(new ApiError(404, "not found"), t, "kb.loadFailed" as never), "err.notFound");
    assert.equal(userErrorMessage(new Error("boom"), t, "kb.loadFailed" as never), "kb.loadFailed");
    assert.equal(userErrorMessage(new ApiError(0, "Upload aborted"), t, "kb.loadFailed" as never), "");
  });
  it("prefixes the fallback when asked", () => {
    const tt: Translate = (k) => (k === "err.forbidden" ? "Không có quyền." : "Không mở được.");
    assert.equal(userErrorMessage(new ApiError(403, "x"), tt, "docws.openFailed", { prefix: true }), "Không mở được: Không có quyền.");
    assert.equal(userErrorMessage(new Error("x"), tt, "docws.openFailed", { prefix: true }), "Không mở được.");
  });
});

describe("registerErrorKey", () => {
  it("maps the password policy sentinels", () => {
    assert.equal(
      registerErrorKey(new ApiError(400, "x", envelope(1000, "password must be 8-32 characters and contain at least one letter and one number"))),
      "auth.err.passwordRule",
    );
    assert.equal(
      registerErrorKey(new ApiError(400, "x", envelope(1000, "password must be 8-32 characters and must contain uppercase and lowercase letters, numbers, and special characters"))),
      "auth.err.passwordRuleComplex",
    );
  });
  it("maps duplicate accounts, including the invite path's 409", () => {
    assert.equal(registerErrorKey(new ApiError(400, "x", envelope(1000, "user with this email already exists"))), "auth.err.emailExists");
    assert.equal(
      registerErrorKey(new ApiError(409, "x", envelope(1004, "this email already has an account; please log in to join the organization"))),
      "auth.err.emailExists",
    );
    assert.equal(registerErrorKey(new ApiError(400, "x", envelope(1000, "user with this username already exists"))), "auth.err.usernameExists");
  });
  it("reads gin binding details", () => {
    const bind = (field: string, tag: string) =>
      new ApiError(400, "x", envelope(1000, "Invalid registration parameters", `Key: 'RegisterRequest.${field}' Error:Field validation for '${field}' failed on the '${tag}' tag`));
    assert.equal(registerErrorKey(bind("Username", "min")), "auth.err.usernameLength");
    assert.equal(registerErrorKey(bind("Email", "email")), "auth.err.emailInvalid");
    assert.equal(registerErrorKey(bind("Password", "min")), "auth.err.passwordRule");
    assert.equal(registerErrorKey(new ApiError(400, "x", envelope(1000, "Invalid registration parameters", "EOF"))), "auth.err.invalidFields");
    assert.equal(registerErrorKey(new ApiError(400, "x", envelope(1000, "Username, email and password are required"))), "auth.err.fieldsRequired");
  });
  it("leaves other failures to userErrorMessage", () => {
    assert.equal(registerErrorKey(new ApiError(403, "x", envelope(1003, "Registration is invite-only"))), null);
    assert.equal(registerErrorKey(new ApiError(500, "Request failed")), null);
    assert.equal(registerErrorKey(undefined), null);
  });
});
