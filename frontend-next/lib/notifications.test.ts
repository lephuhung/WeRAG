import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  badgeLabel,
  notificationText,
  paneHint,
  resolveTarget,
  safeAppLink,
  shouldPoll,
  toastPlan,
  toastable,
  unseenIds,
  wordFileTarget,
  type NotificationLike,
} from "./notifications.ts";

const ORIGIN = "https://ai.example.vn";

const n = (id: string, over: Partial<NotificationLike> = {}): NotificationLike => ({
  id,
  kind: "legal_request_added",
  title: "Văn bản 53/2022/NĐ-CP đã được bổ sung",
  body: "Bạn có thể hỏi lại ngay trong cuộc trò chuyện trước đó.",
  link: "/platform/chat/s1",
  tenant_id: 7,
  payload: { document_number: "53/2022/NĐ-CP", title: "Quy định chi tiết", session_id: "s1", channel: "web" },
  seen_at: null,
  read_at: null,
  ...over,
});

const texts = {
  legalAddedTitle: (doc: string) => `${doc} was added`,
  legalAddedChat: "Ask again in the conversation.",
  legalAddedWord: "Reopen the file in Word.",
  legalAddedWeb: "Open the conversation on the web.",
  legalAddedLookup: "Look it up now.",
  legalAddedOtherOrg: (org: string) => (org ? `It belongs to ${org}; look it up now.` : "It belongs to another organization."),
};

describe("notifications", () => {
  it("toasts unseen unread ones once, oldest first, at most three", () => {
    const items = [n("c"), n("b", { seen_at: "x" }), n("a"), n("r", { read_at: "x" })]; // newest first
    assert.deepEqual(toastable(items, new Set()).map((x) => x.id), ["a", "c"]);
    assert.deepEqual(toastable(items, new Set(["a"])).map((x) => x.id), ["c"]);
    assert.deepEqual(toastPlan([1, 2, 3, 4, 5]), { show: [3, 4, 5], more: 2 });
    assert.deepEqual(toastPlan([1, 2]), { show: [1, 2], more: 0 });
  });

  it("marks every unseen fetched one seen, also those toasted before", () => {
    assert.deepEqual(unseenIds([n("a"), n("b", { seen_at: "x" }), n("c", { read_at: "x" })]), ["a", "c"]);
  });

  it("throttles focus polls, never the timer", () => {
    assert.equal(shouldPoll(1_000, 5_000, false), false);
    assert.equal(shouldPoll(1_000, 11_000, false), true);
    assert.equal(shouldPoll(1_000, 2_000, true), true);
  });

  it("keeps app links only", () => {
    assert.equal(safeAppLink("/platform/chat/s1", ORIGIN), "/platform/chat/s1");
    assert.equal(safeAppLink("/platform/legal-lookup?q=53%2F2022#x", ORIGIN), "/platform/legal-lookup?q=53%2F2022#x");
    for (const bad of [
      "https://evil.example",
      "//evil.example",
      "/\t/evil.example",
      "/\n/evil.example",
      "/platform/\r\nx",
      "/platform/a\u007fb",
      "/\\evil.example",
      "/%2F%2Fevil.example",
      "/platform/../login",
      "/login",
      "javascript:alert(1)",
      "",
    ]) {
      assert.equal(safeAppLink(bad, ORIGIN), "", JSON.stringify(bad));
    }
  });

  it("badges", () => {
    assert.equal(badgeLabel(0), "");
    assert.equal(badgeLabel(3), "3");
    assert.equal(badgeLabel(12), "9+");
  });

  it("renders known kinds in the reader's language, else the stored text", () => {
    assert.deepEqual(notificationText(n("a"), texts), {
      title: "53/2022/NĐ-CP was added",
      body: "Quy định chi tiết. Ask again in the conversation.",
    });
    const word = n("w", { payload: { document_number: "53/2022/NĐ-CP", channel: "word_addin" } });
    assert.equal(notificationText(word, texts).body, "Reopen the file in Word.");
    const gone = n("g", { link: "/platform/legal-lookup?q=x" });
    assert.equal(notificationText(gone, texts).body, "Quy định chi tiết. Look it up now.");
    const other = n("o", { kind: "package_expiry", title: "Gói sắp hết hạn", body: "Còn 7 ngày" });
    assert.deepEqual(notificationText(other, texts), { title: "Gói sắp hết hạn", body: "Còn 7 ngày" });
    assert.deepEqual(notificationText(n("e", { payload: {} }), texts), { title: n("e").title, body: n("e").body });
  });

  it("never switches organization: another one's conversation opens the lookup", () => {
    const memberships = [{ tenant_id: 7, tenant_name: "Sở Tư pháp" }, { tenant_id: 9, tenant_name: "UBND tỉnh" }];
    const ctx = { origin: ORIGIN, currentTenantId: 7, memberships, isGuest: false };
    assert.deepEqual(resolveTarget(n("a"), ctx), { path: "/platform/chat/s1", hint: "chat", orgName: "" });
    const lookup = "/platform/legal-lookup?q=53%2F2022%2FN%C4%90-CP";
    assert.deepEqual(resolveTarget(n("a", { tenant_id: 9 }), ctx), { path: lookup, hint: "otherOrg", orgName: "UBND tỉnh" });
    assert.deepEqual(resolveTarget(n("a", { tenant_id: 11 }), ctx), { path: lookup, hint: "otherOrg", orgName: "" });
    assert.deepEqual(resolveTarget(n("a", { tenant_id: 10012 }), { ...ctx, isGuest: true }), {
      path: "/platform/chat/s1",
      hint: "chat",
      orgName: "",
    });
    assert.equal(resolveTarget(n("a", { link: "/\t/evil.example" }), ctx).path, "");
    // the hint follows the click
    const target = resolveTarget(n("a", { tenant_id: 9 }), ctx);
    assert.equal(notificationText(n("a", { tenant_id: 9 }), texts, target).body, "Quy định chi tiết. It belongs to UBND tỉnh; look it up now.");
  });

  it("words the pane's hint after what a click there does", () => {
    const ctx = { openable: false, currentTenantId: 7, memberships: [], isGuest: false };
    assert.deepEqual(paneHint(n("a"), { ...ctx, openable: true }), { hint: "chat", orgName: "" });
    assert.deepEqual(paneHint(n("a"), ctx), { hint: "web", orgName: "" });
    const word = n("w", { payload: { document_number: "X", session_id: "s1", word_file_id: "f9" } });
    assert.deepEqual(paneHint(word, ctx), { hint: "word", orgName: "" });
    assert.deepEqual(paneHint(n("o", { tenant_id: 9 }), ctx), { hint: "otherOrg", orgName: "" });
    assert.deepEqual(paneHint(n("l", { link: "/platform/legal-lookup?q=x" }), ctx), { hint: "lookup", orgName: "" });
  });

  it("opens a conversation in Word only from its own file and organization", () => {
    const x = n("a", { payload: { session_id: "s1", word_file_id: "f1" } });
    assert.equal(wordFileTarget(x, "f1"), "s1");
    assert.equal(wordFileTarget(x, "f1", 7), "s1");
    assert.equal(wordFileTarget(x, "f1", 9), "");
    assert.equal(wordFileTarget(x, "f2"), "");
    assert.equal(wordFileTarget(x, null), "");
    assert.equal(wordFileTarget(n("b", { payload: { session_id: "s1" } }), "f1"), "");
  });
});
