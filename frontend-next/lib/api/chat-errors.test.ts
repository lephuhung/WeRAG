// Chat error messages shown to the user.
// Runs with: node --experimental-strip-types --test lib/api/chat-errors.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { ChatStreamError, ERR_TURN_RUNNING, chatErrorKey, isSessionLimitError, parseErrorEnvelope } from "./chat-errors.ts";
import { ApiError } from "../api-client.ts";

describe("parseErrorEnvelope", () => {
  it("reads the backend envelope", () => {
    const body = '{"error":{"code":1005,"details":null,"message":"another turn is already running in this session"},"success":false}';
    assert.deepEqual(parseErrorEnvelope(body), { code: 1005, message: "another turn is already running in this session" });
  });
  it("keeps the usage-package reason", () => {
    const body = '{"error":{"code":1006,"details":{"reason":"limit_exhausted","metric":"questions"},"message":"usage limit reached"},"success":false}';
    assert.deepEqual(parseErrorEnvelope(body), { code: 1006, message: "usage limit reached", reason: "limit_exhausted" });
  });
  it("tolerates other shapes and non-JSON", () => {
    assert.deepEqual(parseErrorEnvelope('{"message":"bad"}'), { message: "bad" });
    assert.deepEqual(parseErrorEnvelope("<html>502</html>"), {});
  });
});

describe("chatErrorKey", () => {
  it("turns the running-turn conflict into a sentence, not raw JSON", () => {
    assert.equal(chatErrorKey(new ChatStreamError(409, ERR_TURN_RUNNING, "another turn…")), "chat.err.turnRunning");
  });
  it("maps statuses", () => {
    assert.equal(chatErrorKey(new ChatStreamError(503)), "chat.err.unavailable");
    assert.equal(chatErrorKey(new ChatStreamError(500, 1000, "internal")), "chat.err.server");
    assert.equal(chatErrorKey(new ChatStreamError(429)), "chat.err.tooMany");
    assert.equal(chatErrorKey(new ApiError(404, "Session not found")), "chat.err.notFound");
    assert.equal(chatErrorKey(new Error("unauthorized")), "chat.err.unauthorized");
    assert.equal(chatErrorKey(new TypeError("Failed to fetch")), "chat.err.network");
  });
  it("names the usage-package refusal", () => {
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "no package", "no_subscription")), "chat.err.usage.noSubscription");
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "limit", "limit_exhausted")), "chat.err.usage.limit");
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "seat", "no_seat")), "chat.err.usage.noSeat");
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "expired", "expired")), "chat.err.usage.expired");
  });
  it("recognizes the per-session cap", () => {
    const e = new ChatStreamError(429, 1006, "session limit reached", "session_limit");
    assert.equal(chatErrorKey(e), "chat.err.usage.sessionLimit");
    assert.equal(isSessionLimitError(e), true);
    assert.equal(isSessionLimitError(new ChatStreamError(429, 1006, "limit", "limit_exhausted")), false);
    assert.equal(isSessionLimitError(new Error("x")), false);
  });
  it("keeps a 4xx the backend explained", () => {
    assert.equal(chatErrorKey(new ChatStreamError(400, 1001, "a message can use at most 5 attachments")), null);
    assert.equal(chatErrorKey(new ChatStreamError(400)), "chat.err.generic");
  });
  it("leaves errors that carry readable text", () => {
    assert.equal(chatErrorKey(new Error("Kiểm tra lại tệp đính kèm")), null);
  });
});
