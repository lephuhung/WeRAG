// Chat error messages shown to the user.
// Runs with: node --experimental-strip-types --test lib/api/chat-errors.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { ChatStreamError, ERR_AGENT_UNAVAILABLE, ERR_PUBLIC_LOOKUP_UNAVAILABLE, ERR_TURN_RUNNING, chatErrorKey, chatErrorText, chatErrorTextKey, isSessionLimitError, limitResetDate, parseErrorEnvelope } from "./chat-errors.ts";
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
  it("tells an unavailable agent apart from a missing conversation", () => {
    const msg = "Trợ lý này không tồn tại hoặc không áp dụng cho Tổ chức của bạn.";
    assert.equal(chatErrorKey(new ChatStreamError(404, ERR_AGENT_UNAVAILABLE, msg)), "chat.err.agentUnavailable");
    const body = { success: false, error: { code: 2104, message: msg } };
    assert.equal(chatErrorKey(new ApiError(404, msg, body)), "chat.err.agentUnavailable");
    assert.equal(chatErrorKey(new ChatStreamError(404, 1003, "Session not found")), "chat.err.notFound");
  });
  it("tells a Guest the public lookup cannot run, not to pick another assistant", () => {
    const msg = "Hiện không có kho tri thức công khai nào để trả lời.";
    assert.equal(
      chatErrorKey(new ChatStreamError(503, ERR_PUBLIC_LOOKUP_UNAVAILABLE, msg, "public_lookup_no_knowledge")),
      "chat.err.publicLookup.noKnowledge",
    );
    assert.equal(
      chatErrorKey(new ChatStreamError(503, ERR_PUBLIC_LOOKUP_UNAVAILABLE, "tạm tắt", "public_lookup_off")),
      "chat.err.publicLookup.off",
    );
    const body = { success: false, error: { code: 2105, message: msg, details: { reason: "public_lookup_no_knowledge" } } };
    assert.equal(chatErrorKey(new ApiError(503, msg, body)), "chat.err.publicLookup.noKnowledge");
    assert.equal(chatErrorTextKey("chat.err.publicLookup.off", true), "chat.err.publicLookup.off");
    assert.equal(chatErrorKey(new ChatStreamError(503, 1008, "busy")), "chat.err.unavailable");
  });
  it("names the usage-package refusal", () => {
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "no package", "no_subscription")), "chat.err.usage.noSubscription");
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "limit", "limit_exhausted")), "chat.err.usage.limit");
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "seat", "no_seat")), "chat.err.usage.noSeat");
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "expired", "expired")), "chat.err.usage.expired");
    // an "expired" refusal that carries a start date: not started yet
    const notStarted = parseErrorEnvelope(JSON.stringify({ error: { code: 1006, message: "x", details: { reason: "expired", starts_at: "2026-11-09T17:00:00Z" } } }));
    assert.equal(notStarted.reason, "not_started");
    assert.equal(chatErrorKey(new ChatStreamError(429, 1006, "x", notStarted.reason)), "chat.err.usage.notStarted");
    assert.equal(chatErrorTextKey("chat.err.usage.notStarted", true), "chat.err.guest.unavailable");
    // the stream error carries the start date: the sentence names it (GMT+7)
    assert.equal(notStarted.startsAt, "2026-11-09T17:00:00Z");
    const withDate = new ChatStreamError(429, 1006, "x", "not_started", undefined, notStarted.startsAt);
    assert.deepEqual(chatErrorText("chat.err.usage.notStarted", false, withDate), {
      key: "chat.err.usage.notStartedOn",
      vars: { date: "10/11/2026" },
    });
    // no date (another error kind): the dateless sentence; a guest keeps its own
    assert.deepEqual(chatErrorText("chat.err.usage.notStarted", false, new Error("x")), { key: "chat.err.usage.notStarted" });
    assert.deepEqual(chatErrorText("chat.err.usage.notStarted", true, withDate), { key: "chat.err.guest.unavailable" });
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

describe("organization required", () => {
  const flat = '{"error":"Organization required","code":"TENANT_REQUIRED"}';
  it("reads the flat middleware body's string code", () => {
    assert.deepEqual(parseErrorEnvelope(flat), { message: "Organization required", errorCode: "TENANT_REQUIRED" });
    assert.deepEqual(parseErrorEnvelope('{"code":"TENANT_REQUIRED"}'), { errorCode: "TENANT_REQUIRED" });
  });
  it("keeps the nested envelope as before, with a top-level string code beside it", () => {
    const body = '{"error":{"code":1005,"message":"busy"},"code":"TENANT_REQUIRED"}';
    assert.deepEqual(parseErrorEnvelope(body), { code: 1005, message: "busy", errorCode: "TENANT_REQUIRED" });
    assert.deepEqual(parseErrorEnvelope('{"error":"x","code":7}'), { message: "x" });
  });
  it("says an organization is needed, not that a turn is running", () => {
    const { code, message, reason, errorCode } = parseErrorEnvelope(flat);
    assert.equal(chatErrorKey(new ChatStreamError(409, code, message, reason, errorCode)), "chat.err.tenantRequired");
    assert.equal(chatErrorKey(new ApiError(409, "Organization required", JSON.parse(flat))), "chat.err.tenantRequired");
    const stale = { error: "Organization membership required", code: "TENANT_MEMBERSHIP_REQUIRED" };
    assert.equal(chatErrorKey(new ApiError(403, stale.error, stale)), "chat.err.tenantRequired");
    // a plain 409 is still the running turn
    assert.equal(chatErrorKey(new ChatStreamError(409)), "chat.err.turnRunning");
  });
});

describe("chatErrorTextKey", () => {
  it("gives a guest its own usage texts", () => {
    assert.equal(chatErrorTextKey("chat.err.usage.limit", true), "chat.err.guest.usageLimit");
    assert.equal(chatErrorTextKey("chat.err.usage.noSubscription", true), "chat.err.guest.unavailable");
    assert.equal(chatErrorTextKey("chat.err.usage.expired", true), "chat.err.guest.unavailable");
    assert.equal(chatErrorTextKey("chat.err.turnRunning", true), "chat.err.turnRunning");
    assert.equal(chatErrorTextKey(null, true), null);
  });
  it("keeps the member texts", () => {
    for (const k of ["chat.err.usage.limit", "chat.err.usage.noSubscription", "chat.err.usage.expired"] as const) {
      assert.equal(chatErrorTextKey(k, false), k);
    }
  });
});

describe("backend code before HTTP status", () => {
  it("names a specific refusal whatever status carries it", () => {
    // a gateway or a handler change may send these with another status
    assert.equal(
      chatErrorKey(new ChatStreamError(500, ERR_PUBLIC_LOOKUP_UNAVAILABLE, "x", "public_lookup_off")),
      "chat.err.publicLookup.off",
    );
    assert.equal(chatErrorKey(new ChatStreamError(502, ERR_PUBLIC_LOOKUP_UNAVAILABLE)), "chat.err.publicLookup.off");
    assert.equal(chatErrorKey(new ChatStreamError(503, ERR_AGENT_UNAVAILABLE, "x")), "chat.err.agentUnavailable");
    assert.equal(chatErrorKey(new ChatStreamError(503, ERR_TURN_RUNNING, "x")), "chat.err.turnRunning");
  });
  it("falls back to the status for generic codes", () => {
    assert.equal(chatErrorKey(new ChatStreamError(503, 1008, "busy")), "chat.err.unavailable");
    assert.equal(chatErrorKey(new ChatStreamError(503, 2201, "vector store")), "chat.err.unavailable");
    assert.equal(chatErrorKey(new ChatStreamError(500, 1007, "internal")), "chat.err.server");
    assert.equal(chatErrorKey(new ChatStreamError(409, 2001, "exists")), null);
  });
});

describe("chatErrorText by role", () => {
  const member = { hasTenant: true, isTenantAdmin: false };
  const admin = { hasTenant: true, isTenantAdmin: true };
  const self = { hasTenant: false };
  const err = (reason: string) => new ChatStreamError(429, 1006, "x", reason);

  it("tells a member to ask the admin and the others to buy", () => {
    assert.deepEqual(chatErrorText("chat.err.usage.noSubscription", false, err("no_subscription"), member), { key: "chat.err.usage.noSubscription" });
    assert.deepEqual(chatErrorText("chat.err.usage.noSubscription", false, err("no_subscription"), admin), { key: "chat.err.usage.noSubscriptionAdmin", action: "billing" });
    assert.deepEqual(chatErrorText("chat.err.usage.noSubscription", false, err("no_subscription"), self), { key: "chat.err.usage.noSubscriptionSelf", action: "billing" });
    assert.deepEqual(chatErrorText("chat.err.usage.expired", false, err("expired"), member), { key: "chat.err.usage.expired" });
    assert.deepEqual(chatErrorText("chat.err.usage.expired", false, err("expired"), admin), { key: "chat.err.usage.expiredBuy", action: "billing" });
    assert.deepEqual(chatErrorText("chat.err.usage.noSeat", false, err("no_seat"), admin), { key: "chat.err.usage.noSeatAdmin", action: "seats" });
    assert.deepEqual(chatErrorText("chat.err.usage.noSeat", false, err("no_seat"), member), { key: "chat.err.usage.noSeat" });
  });

  it("names the limit that ran out and when it counts again", () => {
    const body = JSON.stringify({ error: { code: 1006, message: "x", details: { reason: "limit_exhausted", metric: "questions", period: "month", limit: 500, used: 500 } } });
    const env = parseErrorEnvelope(body);
    assert.deepEqual(env.usage, { metric: "questions", period: "month", limit: 500, used: 500 });
    const e = new ChatStreamError(429, 1006, "x", env.reason);
    e.usage = env.usage;
    const now = new Date("2026-12-31T18:00:00Z"); // 01/01/2027 01:00 VN
    assert.deepEqual(chatErrorText("chat.err.usage.limit", false, e, member, now), {
      key: "chat.err.usage.limitDetail",
      vars: { limit: "500", date: "01/02/2027" },
      varKeys: { unit: "pbn.questions", period: "chat.err.usage.perMonth" },
    });
    assert.equal(chatErrorText("chat.err.usage.limit", false, e, self, now).action, "billing");
    // the ApiError path reads the same details
    const api = new ApiError(429, "x", JSON.parse(body));
    assert.equal(chatErrorText("chat.err.usage.limit", false, api, admin, now).key, "chat.err.usage.limitDetailBuy");
    // no details: the plain sentence
    assert.deepEqual(chatErrorText("chat.err.usage.limit", false, err("limit_exhausted"), admin, now), { key: "chat.err.usage.limitBuy", action: "billing" });
  });

  it("limitResetDate follows the Vietnam calendar", () => {
    assert.equal(limitResetDate("day", new Date("2026-11-30T18:00:00Z")), "02/12/2026");
    assert.equal(limitResetDate("month", new Date("2026-11-30T16:00:00Z")), "01/12/2026");
    assert.equal(limitResetDate("session"), "");
  });

  it("a guest whose allowance ran out is offered a package, as the banner does", () => {
    assert.deepEqual(chatErrorText("chat.err.usage.limit", true, err("limit_exhausted"), self), { key: "chat.err.guest.usageLimit", action: "billing" });
  });

  it("says a specialist assistant's right ended", () => {
    const body = JSON.stringify({ error: { code: ERR_AGENT_UNAVAILABLE, message: "x", details: { reason: "addon_expired", agent_name: "Thuế", ends_at: "2026-11-03T17:00:00Z" } } });
    const env = parseErrorEnvelope(body);
    const e = new ChatStreamError(404, env.code, env.message, env.reason);
    e.addon = env.addon;
    assert.deepEqual(chatErrorText("chat.err.agentUnavailable", false, e, member), {
      key: "chat.err.agentAddonExpired",
      vars: { name: "Thuế", date: "03/11/2026" },
    });
    assert.equal(chatErrorText("chat.err.agentUnavailable", false, e, admin).action, "billing");
    assert.deepEqual(chatErrorText("chat.err.agentUnavailable", false, new ChatStreamError(404, ERR_AGENT_UNAVAILABLE, "x"), admin), { key: "chat.err.agentUnavailable" });
  });
});
