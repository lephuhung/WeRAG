// Guest chat: who is a guest, and what a guest's turn may carry.
// Runs with: node --experimental-strip-types --test lib/guest-chat.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { BUILTIN_PUBLIC_LOOKUP_ID, guestChatSettings, isGuestAccount } from "./guest-chat.ts";

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
