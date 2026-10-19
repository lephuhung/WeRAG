// Guest chat: who is a guest, and what a guest's turn may carry.
// Runs with: node --experimental-strip-types --test lib/guest-chat.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { BUILTIN_PUBLIC_LOOKUP_ID, guestChatSettings, guestSessionSurvives, isGuestAccount, shouldRefreshAccount } from "./guest-chat.ts";
import { ChatStreamError } from "./api/chat-errors.ts";
import { ApiError } from "./api-client.ts";

describe("isGuestAccount", () => {
  it("is a signed-in account without membership, never a SuperAdmin", () => {
    assert.equal(isGuestAccount({ loggedIn: true, membershipState: "guest" }), true);
    assert.equal(isGuestAccount({ loggedIn: true, membershipState: "guest", isSystemAdmin: true }), false);
    assert.equal(isGuestAccount({ loggedIn: true, membershipState: "member" }), false);
    assert.equal(isGuestAccount({ loggedIn: true, membershipState: "superadmin" }), false);
  });
  it("is not decided before /auth/me answered (the state defaults to guest)", () => {
    assert.equal(isGuestAccount({ loggedIn: false, membershipState: "guest" }), false);
  });
});

describe("guestChatSettings", () => {
  const stored = {
    selectedKnowledgeBases: ["kb1"],
    selectedFiles: ["f1"],
    selectedFileKbMap: { f1: "kb1" },
    selectedTags: [{ id: "t", name: "t", kbId: "kb1" }],
    selectedMCPServices: ["mcp1"],
    selectedSkills: ["legal"],
    isAgentEnabled: false,
    selectedAgentId: "builtin-document-assistant",
    webSearchEnabled: true,
    localBrowserEnabled: true,
    selectedChatModelId: "m1",
  };
  it("runs the public-lookup agent unscoped", () => {
    assert.deepEqual(guestChatSettings(stored), {
      selectedKnowledgeBases: [],
      selectedFiles: [],
      selectedFileKbMap: {},
      selectedTags: [],
      selectedMCPServices: [],
      selectedSkills: [],
      isAgentEnabled: true,
      selectedAgentId: BUILTIN_PUBLIC_LOOKUP_ID,
      webSearchEnabled: false,
      localBrowserEnabled: false,
      selectedChatModelId: "",
    });
    assert.equal(BUILTIN_PUBLIC_LOOKUP_ID, "builtin-public-lookup");
  });
  it("leaves the stored member settings untouched", () => {
    guestChatSettings(stored);
    assert.deepEqual(stored.selectedKnowledgeBases, ["kb1"]);
    assert.equal(stored.selectedAgentId, "builtin-document-assistant");
  });
});

describe("shouldRefreshAccount", () => {
  const tenantRequired = new ApiError(409, "Organization required", { error: "Organization required", code: "TENANT_REQUIRED" });
  const staleMembership = new ChatStreamError(403, undefined, "no membership", undefined, "TENANT_MEMBERSHIP_REQUIRED");
  it("refreshes on an organization refusal, for anyone and any call", () => {
    for (const isGuest of [true, false]) {
      assert.equal(shouldRefreshAccount(tenantRequired, { isGuest }), true);
      assert.equal(shouldRefreshAccount(staleMembership, { isGuest, sessionCall: false }), true);
    }
  });
  it("refreshes on a guest's 404 session call only", () => {
    const gone = new ApiError(404, "Session not found", { success: false, error: { code: 1003, message: "Session not found" } });
    assert.equal(shouldRefreshAccount(gone, { isGuest: true }), true);
    assert.equal(shouldRefreshAccount(new ChatStreamError(404, 1003), { isGuest: true }), true);
    assert.equal(shouldRefreshAccount(gone, { isGuest: true, sessionCall: false }), false);
    assert.equal(shouldRefreshAccount(gone, { isGuest: false }), false);
  });
  it("leaves other failures alone", () => {
    assert.equal(shouldRefreshAccount(new ChatStreamError(409, 1005), { isGuest: true }), false);
    assert.equal(shouldRefreshAccount(new ChatStreamError(429, 1006, "limit", "limit_exhausted"), { isGuest: true }), false);
    assert.equal(shouldRefreshAccount(new TypeError("Failed to fetch"), { isGuest: true }), false);
    assert.equal(shouldRefreshAccount(null, { isGuest: true }), false);
  });
});

describe("guestSessionSurvives", () => {
  it("keeps a guest's session only while the account stays a guest", () => {
    assert.equal(guestSessionSurvives("guest", "guest"), true);
    assert.equal(guestSessionSurvives("guest", "member"), false);
    assert.equal(guestSessionSurvives("guest", "superadmin"), false);
    assert.equal(guestSessionSurvives("member", "guest"), false);
    assert.equal(guestSessionSurvives(undefined, "guest"), false);
  });
});
