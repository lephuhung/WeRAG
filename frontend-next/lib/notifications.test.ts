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
  ABBREVIATION_REVIEW_PATH,
  KIND_ABBREVIATION_PENDING,
  KIND_BACKUP_ALERT,
  BACKUP_STATUS_PATH,
  type BackupProblem,
  KIND_LEGAL_SYNC_ALERT,
  LEGAL_SYNC_STATUS_PATH,
  type LegalSyncAlertWorker,
  type LegalSyncProblem,
  type LegalSyncStep,
  KIND_LEGAL_DRAFT_READY,
  KIND_LEGAL_IDENTITY_PENDING,
  KIND_LEGAL_LINK_PENDING,
  KIND_LEGAL_REQUEST_PENDING,
  KIND_PAYMENT_ORDER_PENDING,
  QUEUE_KINDS,
  isReviewDigest,
  linkParam,
  type QueueKind,
  mergeFresh,
  toastKey,
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
  abbreviationPendingTitle: (count: number) => `${count} new ${count === 1 ? "abbreviation" : "abbreviations"} waiting for review`,
  abbreviationProperName: (short: string) => `${short} (proper name)`,
  abbreviationPendingMore: (count: number) => `and ${count} more`,
  queueTitle: (kind: QueueKind, count: number) =>
    ({
      payment_order_pending: `${count} new payment order(s) awaiting confirmation`,
      legal_request_pending: `${count} new legal document request(s)`,
      legal_draft_ready: `${count} new OCR draft(s) ready for review`,
      legal_link_pending: `${count} new amendment link(s) awaiting review`,
      legal_identity_pending: `${count} new document(s) awaiting identity review`,
    })[kind],
  queueMore: (kind: Exclude<QueueKind, "legal_link_pending">, count: number) =>
    kind === "payment_order_pending" ? `and ${count} more orders` : `and ${count} more`,
  queueRequestCount: (count: number) => `${count} requests`,
  queueLinkCount: (count: number) => `${count} links`,
  money: (vnd: number) => `${vnd.toLocaleString("en-US")} VND`,
  queueUntitled: "untitled document",
  backupAlertTitle: (count: number) => `Backup problems: ${count} job(s)`,
  backupProblem: (problem: BackupProblem, at: string) =>
    ({ failed: at ? `failed at ${at}` : "failed", stale: "overdue", missing: "no status yet", unreadable: "status unreadable" })[problem],
  backupLastOk: (at: string) => `last success ${at}`,
  legalSyncAlertTitle: (count: number) => `Legal data collection problems: ${count}`,
  legalSyncWorker: (worker: LegalSyncAlertWorker) =>
    ({ vbpl_sync: "vbpl.vn nightly sync", dcs_sync: "Party documents sync", auto_ingest: "Auto-ingest", expired_retention: "Expired text removal" })[worker],
  legalSyncStep: (step: LegalSyncStep) =>
    ({ prepare: "prepare", crawl: "crawl", import: "import", push: "MinIO backup", fetch: "fetch", build: "classify and build", upload: "push to WeRAG" })[step],
  legalSyncProblem: (problem: LegalSyncProblem, step: string, at: string, hours: number) =>
    ({
      last_failed: "failed",
      running_long: hours ? `running for over ${hours} hours` : "running for over 12 hours",
      stale: hours ? `overdue: no successful run in the last ${hours} hours` : "overdue: no successful run in the last day",
      heartbeat_stale: "stopped (missed three passes)",
      no_heartbeat: "no pass since the server started",
      tick_error: "its passes keep failing",
    })[problem] +
    (step ? ` at step ${step}` : "") +
    (at ? ` (run of ${at})` : ""),
  // the clients pass lib/datetime formatDateTime; a fixed stand-in here
  dateTime: (iso: string) => (Number.isNaN(Date.parse(iso)) ? "" : `T(${iso.slice(0, 16)})`),
};

// the vi dictionary's wording (lib/i18n.tsx notif.abbrevPending.*)
const textsVi = {
  ...texts,
  abbreviationPendingTitle: (count: number) => `Có ${count} từ viết tắt mới chờ duyệt`,
  abbreviationProperName: (short: string) => `${short} (tên riêng)`,
  abbreviationPendingMore: (count: number) => `và ${count} từ khác`,
  queueTitle: (kind: QueueKind, count: number) =>
    ({
      payment_order_pending: `Có ${count} đơn hàng mới chờ xác nhận thanh toán`,
      legal_request_pending: `Có ${count} văn bản mới được đề nghị bổ sung`,
      legal_draft_ready: `Có ${count} bản nháp OCR mới chờ duyệt`,
      legal_link_pending: `Có ${count} liên kết sửa đổi mới chờ duyệt`,
      legal_identity_pending: `Có ${count} văn bản mới chờ duyệt định danh`,
    })[kind],
  queueMore: (kind: Exclude<QueueKind, "legal_link_pending">, count: number) =>
    kind === "payment_order_pending" ? `và ${count} đơn khác` : kind === "legal_draft_ready" ? `và ${count} bản nháp khác` : `và ${count} văn bản khác`,
  queueRequestCount: (count: number) => `${count} lượt đề nghị`,
  queueLinkCount: (count: number) => `${count} liên kết`,
  money: (vnd: number) => `${String(vnd).replace(/\B(?=(\d{3})+(?!\d))/g, ".")} đ`,
  queueUntitled: "văn bản không số",
  backupAlertTitle: (count: number) => `Sao lưu gặp sự cố: ${count} việc`,
  backupProblem: (problem: BackupProblem, at: string) =>
    ({ failed: at ? `lỗi lúc ${at}` : "lỗi", stale: "quá hạn", missing: "chưa có trạng thái", unreadable: "không đọc được trạng thái" })[problem],
  backupLastOk: (at: string) => `lần thành công cuối ${at}`,
  legalSyncAlertTitle: (count: number) => `Thu thập dữ liệu pháp lý gặp sự cố: ${count} việc`,
  legalSyncWorker: (worker: LegalSyncAlertWorker) =>
    ({ vbpl_sync: "Đồng bộ vbpl.vn", dcs_sync: "Đồng bộ văn bản của Đảng", auto_ingest: "Tự nạp văn bản", expired_retention: "Xóa toàn văn hết hiệu lực" })[worker],
  legalSyncStep: (step: LegalSyncStep) =>
    ({ prepare: "chuẩn bị", crawl: "cào dữ liệu", import: "nạp danh mục", push: "sao lưu MinIO", fetch: "tải trang", build: "phân loại và dựng HTML", upload: "đẩy lên WeRAG" })[step],
  legalSyncProblem: (problem: LegalSyncProblem, step: string, at: string, hours: number) =>
    ({
      last_failed: "lỗi",
      running_long: hours ? `chạy quá ${hours} giờ` : "chạy quá 12 giờ",
      stale: hours ? `quá hạn, không có lần chạy thành công trong ${hours} giờ qua` : "quá hạn, không có lần chạy thành công trong ngày qua",
      heartbeat_stale: "đã dừng (lỡ ba lượt liên tiếp)",
      no_heartbeat: "chưa chạy lượt nào từ khi khởi động",
      tick_error: "các lượt chạy liên tiếp bị lỗi",
    })[problem] +
    (step ? ` ở bước ${step}` : "") +
    (at ? ` (lần chạy lúc ${at})` : ""),
};

const digest = (over: Partial<NotificationLike> = {}): NotificationLike => ({
  id: "d",
  kind: KIND_ABBREVIATION_PENDING,
  title: "Có 3 từ viết tắt mới chờ duyệt",
  body: "CA → Công an, PV01 (tên riêng), VP → Văn phòng",
  link: ABBREVIATION_REVIEW_PATH,
  tenant_id: 10000,
  payload: {
    count: 3,
    items: [
      { short_form: "CA", full_form: "Công an" },
      { short_form: "PV01", full_form: "PV01", proper_name: true },
      { short_form: "VP", full_form: "Văn phòng" },
    ],
  },
  seen_at: null,
  read_at: null,
  ...over,
});

describe("notifications", () => {
  it("toasts unseen unread ones once, oldest first, at most three", () => {
    const items = [n("c"), n("b", { seen_at: "x" }), n("a"), n("r", { read_at: "x" })]; // newest first
    assert.deepEqual(toastable(items, new Set()).map((x) => x.id), ["a", "c"]);
    assert.deepEqual(toastable(items, new Set([toastKey(n("a"))])).map((x) => x.id), ["c"]);
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

  it("words the abbreviation review digest from its payload, en and vi", () => {
    assert.deepEqual(notificationText(digest(), texts), {
      title: "3 new abbreviations waiting for review",
      body: "CA → Công an, PV01 (proper name), VP → Văn phòng",
    });
    assert.deepEqual(notificationText(digest(), textsVi), {
      title: "Có 3 từ viết tắt mới chờ duyệt",
      body: "CA → Công an, PV01 (tên riêng), VP → Văn phòng",
    });
    // more suggestions than it names; a long full form is cut
    const long = "Một tên đầy đủ rất dài vượt quá bốn mươi ký tự của thông báo";
    const many = digest({ payload: { count: 7, items: [{ short_form: "X", full_form: long }] } });
    assert.deepEqual(notificationText(many, texts), {
      title: "7 new abbreviations waiting for review",
      body: "X → Một tên đầy đủ rất dài vượt quá bốn mươ…, and 6 more",
    });
    assert.equal(notificationText(many, textsVi).body, "X → Một tên đầy đủ rất dài vượt quá bốn mươ…, và 6 từ khác");
    const one = digest({ payload: { count: 1, items: [{ short_form: "VP", full_form: "Văn phòng" }] } });
    assert.equal(notificationText(one, texts).title, "1 new abbreviation waiting for review");
    // a payload without what it needs keeps the stored text
    for (const payload of [null, {}, { count: 2 }, { count: 2, items: "x" }, { count: 2, items: [{ full_form: "x" }] }]) {
      const d = digest({ payload });
      assert.deepEqual(notificationText(d, texts), { title: d.title, body: d.body });
    }
  });

  it("opens the review page of a digest, whatever organization is current", () => {
    const ctx = { origin: ORIGIN, currentTenantId: 7, memberships: [], isGuest: false };
    assert.deepEqual(resolveTarget(digest(), ctx), { path: ABBREVIATION_REVIEW_PATH, hint: "lookup", orgName: "" });
  });

  it("toasts an updated digest again, once per version, and shows its new count", () => {
    const v1 = digest({ created_at: "2026-11-11T09:00:00Z", payload: { count: 1, items: [{ short_form: "VP", full_form: "Văn phòng" }] } });
    const toasted = new Set([toastKey(v1)]);
    assert.deepEqual(toastable([v1], toasted), [], "the same version is not toasted twice");
    // the server updated it in place: same id, seen_at cleared, created_at moved
    const v2 = digest({ created_at: "2026-11-11T09:05:00Z" });
    assert.notEqual(toastKey(v2), toastKey(v1));
    assert.deepEqual(toastable([v2], toasted).map((x) => x.id), ["d"]);
    toasted.add(toastKey(v2));
    assert.deepEqual(toastable([v2], toasted), []);

    // the list already shown takes the new version in place, newest first
    const older = n("a", { created_at: "2026-11-11T08:00:00Z", read_at: "x" });
    const merged = mergeFresh([{ ...v1, seen_at: "x" }, older], [v2, n("new", { created_at: "2026-11-11T09:01:00Z" })]);
    assert.deepEqual(merged?.map((x) => x.id), ["d", "new", "a"]);
    assert.equal(notificationText(merged![0], texts).title, "3 new abbreviations waiting for review");
    assert.equal(mergeFresh(null, [v2]), null, "a list never fetched stays unfetched");
  });

  it("words the SuperAdmin review-queue digests from their payload, en and vi", () => {
    const q = (kind: string, payload: Record<string, unknown> | null): NotificationLike =>
      n("q", { kind, title: "stored title", body: "stored body", link: "/platform/admin?section=billing&status=pending", payload });
    const order = q(KIND_PAYMENT_ORDER_PENDING, {
      count: 3,
      items: [
        { ref: "DH42", plan: "Gói Go", amount_vnd: 1250000, org: "Sở Tư pháp" },
        { ref: "DH41", plan: "Cơ bản", amount_vnd: 0 },
      ],
    });
    assert.deepEqual(notificationText(order, texts), {
      title: "3 new payment order(s) awaiting confirmation",
      body: "DH42 · Gói Go · 1,250,000 VND · Sở Tư pháp, DH41 · Cơ bản, and 1 more orders",
    });
    assert.deepEqual(notificationText(order, textsVi), {
      title: "Có 3 đơn hàng mới chờ xác nhận thanh toán",
      body: "DH42 · Gói Go · 1.250.000 đ · Sở Tư pháp, DH41 · Cơ bản, và 1 đơn khác",
    });

    const request = q(KIND_LEGAL_REQUEST_PENDING, {
      count: 2,
      items: [
        { ref: "53/2022/NĐ-CP", title: "Quy định chi tiết Luật An ninh mạng", count: 3 },
        { ref: "05/2023/TT-BNV", count: 1 },
      ],
    });
    assert.deepEqual(notificationText(request, texts), {
      title: "2 new legal document request(s)",
      body: "53/2022/NĐ-CP – Quy định chi tiết Luật An ninh mạng (3 requests), 05/2023/TT-BNV",
    });
    assert.equal(notificationText(request, textsVi).body, "53/2022/NĐ-CP – Quy định chi tiết Luật An ninh mạng (3 lượt đề nghị), 05/2023/TT-BNV");

    const draft = q(KIND_LEGAL_DRAFT_READY, { count: 4, items: [{ ref: "12/2025/NĐ-CP", title: "12/2025/NĐ-CP" }] });
    assert.deepEqual(notificationText(draft, texts), { title: "4 new OCR draft(s) ready for review", body: "12/2025/NĐ-CP, and 3 more" });
    assert.deepEqual(notificationText(draft, textsVi), { title: "Có 4 bản nháp OCR mới chờ duyệt", body: "12/2025/NĐ-CP, và 3 bản nháp khác" });

    // links count links, the items name documents: no "and N more"
    const links = q(KIND_LEGAL_LINK_PENDING, { count: 15, items: [{ ref: "68/2026/NĐ-CP", count: 12 }, { ref: "5/2026/NĐ-CP", count: 3 }] });
    assert.deepEqual(notificationText(links, texts), {
      title: "15 new amendment link(s) awaiting review",
      body: "68/2026/NĐ-CP (12 links), 5/2026/NĐ-CP (3 links)",
    });
    assert.equal(notificationText(links, textsVi).body, "68/2026/NĐ-CP (12 liên kết), 5/2026/NĐ-CP (3 liên kết)");

    const identity = q(KIND_LEGAL_IDENTITY_PENDING, { count: 2, items: [{ ref: "29/2018/QH14" }, { ref: "cong-van.pdf" }] });
    assert.deepEqual(notificationText(identity, texts), {
      title: "2 new document(s) awaiting identity review",
      body: "29/2018/QH14, cong-van.pdf",
    });
    assert.equal(notificationText(identity, textsVi).title, "Có 2 văn bản mới chờ duyệt định danh");

    // an item with neither số hiệu nor title is named in the reader's language
    const untitled = q(KIND_LEGAL_IDENTITY_PENDING, { count: 3, items: [{ untitled: true }, { ref: "29/2018/QH14" }] });
    assert.deepEqual(notificationText(untitled, texts), {
      title: "3 new document(s) awaiting identity review",
      body: "untitled document, 29/2018/QH14, and 1 more",
    });
    assert.equal(notificationText(untitled, textsVi).body, "văn bản không số, 29/2018/QH14, và 1 văn bản khác");
    const untitledDraft = q(KIND_LEGAL_DRAFT_READY, { count: 1, items: [{ ref: "", untitled: true }] });
    assert.equal(notificationText(untitledDraft, texts).body, "untitled document");

    for (const kind of QUEUE_KINDS) {
      // a count without named items: the title in the reader's language, not
      // the stored Vietnamese
      for (const payload of [{ count: 2 }, { count: 2, items: "x" }, { count: 2, items: [{ title: "x" }, { ref: "", untitled: false }] }]) {
        const t = notificationText(q(kind, payload), texts);
        assert.equal(t.body, "", kind);
        assert.notEqual(t.title, "stored title", kind);
      }
      // nothing to count keeps the stored text
      for (const payload of [null, {}, { count: 0 }, { items: [{ ref: "x" }] }]) {
        assert.deepEqual(notificationText(q(kind, payload), texts), { title: "stored title", body: "stored body" }, kind);
      }
      assert.equal(isReviewDigest(kind), true);
    }
    assert.equal(isReviewDigest(KIND_ABBREVIATION_PENDING), true);
    assert.equal(isReviewDigest("legal_request_added"), false);

    // the admin console opens whatever organization is current
    const ctx = { origin: ORIGIN, currentTenantId: 7, memberships: [], isGuest: false };
    assert.equal(linkParam("/platform/admin?section=legal-documents&view=drafts&status=ready#x", "status"), "ready");
    assert.equal(linkParam("/platform/admin?section=billing", "status"), "");
    assert.equal(linkParam("/platform/chat/s1", "view"), "");
    for (const link of [
      "/platform/admin?section=billing&status=pending",
      "/platform/admin?section=legal-documents&view=requests",
      "/platform/admin?section=legal-documents&view=drafts&status=ready",
      "/platform/admin?section=legal-documents&view=links",
      "/platform/admin?section=legal-documents&view=review",
    ]) {
      assert.equal(resolveTarget({ ...identity, link }, ctx).path, link);
    }
  });
});

describe("backup alert", () => {
  const alert = (payload: Record<string, unknown> | null): NotificationLike => ({
    id: "b",
    kind: KIND_BACKUP_ALERT,
    title: "stored title",
    body: "stored body",
    link: BACKUP_STATUS_PATH,
    tenant_id: 0,
    payload,
    seen_at: null,
    read_at: null,
  });

  it("names each job with its problem, last success and note", () => {
    const n = alert({
      count: 2,
      signature: "files:failed,wal:stale",
      jobs: [
        { job: "wal", problem: "stale", last_ok_at: "2026-11-12T02:40:00Z" },
        { job: "files", problem: "failed", finished_at: "2026-11-12T02:30:05Z", note: "tar: lỗi đọc" },
      ],
    });
    assert.deepEqual(notificationText(n, texts), {
      title: "Backup problems: 2 job(s)",
      body: "wal: overdue, last success T(2026-11-12T02:40); files: failed at T(2026-11-12T02:30) — tar: lỗi đọc",
    });
    assert.deepEqual(notificationText(n, textsVi), {
      title: "Sao lưu gặp sự cố: 2 việc",
      body: "wal: quá hạn, lần thành công cuối T(2026-11-12T02:40); files: lỗi lúc T(2026-11-12T02:30) — tar: lỗi đọc",
    });
    assert.equal(isReviewDigest(KIND_BACKUP_ALERT), true);
    assert.equal(safeAppLink(n.link, ORIGIN), BACKUP_STATUS_PATH);
  });

  it("cuts long notes and skips unknown problems or bad times", () => {
    const long = "x".repeat(300);
    const t = notificationText(
      alert({ jobs: [{ job: "env", problem: "missing", note: long, last_ok_at: "not a time" }, { job: "x", problem: "weird" }, { problem: "failed" }] }),
      texts,
    );
    assert.equal(t.title, "Backup problems: 1 job(s)");
    assert.ok(t.body.startsWith("env: no status yet — xxx"));
    assert.ok(t.body.endsWith("…"));
    assert.ok(Array.from(t.body).length < 160);
  });

  it("keeps the stored text when no job is named", () => {
    for (const payload of [null, {}, { count: 1 }, { jobs: "x" }, { jobs: [] }]) {
      assert.deepEqual(notificationText(alert(payload), texts), { title: "stored title", body: "stored body" });
    }
  });
});

describe("legal sync alert", () => {
  const alert = (payload: Record<string, unknown> | null): NotificationLike => ({
    id: "l",
    kind: KIND_LEGAL_SYNC_ALERT,
    title: "stored title",
    body: "stored body",
    link: LEGAL_SYNC_STATUS_PATH,
    tenant_id: 0,
    payload,
    seen_at: null,
    read_at: null,
  });

  it("names each worker with its problem, step, last success and note", () => {
    const n = alert({
      count: 2,
      signature: "auto_ingest:heartbeat_stale,vbpl_sync:last_failed",
      workers: [
        {
          worker: "vbpl_sync",
          problem: "last_failed",
          step: "crawl",
          at: "2026-11-11T19:30:00Z",
          last_ok_at: "2026-11-11T03:30:00Z",
          note: "HTTP Error 503",
        },
        { worker: "auto_ingest", problem: "heartbeat_stale", at: "2026-11-12T01:00:00Z" },
      ],
    });
    assert.deepEqual(notificationText(n, texts), {
      title: "Legal data collection problems: 2",
      body:
        "vbpl.vn nightly sync: failed at step crawl (run of T(2026-11-11T19:30)), last success T(2026-11-11T03:30) — HTTP Error 503; " +
        "Auto-ingest: stopped (missed three passes)",
    });
    assert.deepEqual(notificationText(n, textsVi), {
      title: "Thu thập dữ liệu pháp lý gặp sự cố: 2 việc",
      body:
        "Đồng bộ vbpl.vn: lỗi ở bước cào dữ liệu (lần chạy lúc T(2026-11-11T19:30)), lần thành công cuối T(2026-11-11T03:30) — HTTP Error 503; " +
        "Tự nạp văn bản: đã dừng (lỡ ba lượt liên tiếp)",
    });
    assert.equal(isReviewDigest(KIND_LEGAL_SYNC_ALERT), true);
    assert.equal(safeAppLink(n.link, ORIGIN), LEGAL_SYNC_STATUS_PATH);
    assert.equal(linkParam(n.link, "view"), "sync");
  });

  it("keeps the step of a stuck run, shows an unknown step as is, skips unknown workers and problems", () => {
    const t = notificationText(
      alert({
        workers: [
          { worker: "vbpl_sync", problem: "running_long", step: "verify", at: "2026-11-11T19:30:00Z" },
          { worker: "expired_retention", problem: "tick_error", step: "crawl", note: "y".repeat(300) },
          { worker: "other", problem: "stale" },
          { worker: "vbpl_sync", problem: "weird" },
        ],
      }),
      texts,
    );
    assert.equal(t.title, "Legal data collection problems: 2");
    assert.ok(t.body.startsWith("vbpl.vn nightly sync: running for over 12 hours at step verify; Expired text removal: its passes keep failing — yyy"));
    assert.ok(t.body.endsWith("…"));
  });

  it("names the Party documents sync, its steps and its own thresholds", () => {
    const n = alert({
      count: 2,
      workers: [
        {
          worker: "dcs_sync",
          problem: "last_failed",
          step: "upload",
          at: "2026-11-14T01:15:00Z",
          last_ok_at: "2026-11-12T07:20:00Z",
          note: "2 pushes failed (HTTP 404/429/5xx, transport…): retried next run",
          hours: 9,
        },
        { worker: "vbpl_sync", problem: "stale", hours: 26 },
      ],
    });
    assert.deepEqual(notificationText(n, texts), {
      title: "Legal data collection problems: 2",
      body:
        "Party documents sync: failed at step push to WeRAG (run of T(2026-11-14T01:15)), last success T(2026-11-12T07:20) — 2 pushes failed (HTTP 404/429/5xx, transport…): retried next run; " +
        "vbpl.vn nightly sync: overdue: no successful run in the last 26 hours",
    });
    const vi = notificationText(
      alert({ workers: [{ worker: "dcs_sync", problem: "running_long", step: "fetch", hours: 4 }, { worker: "dcs_sync", problem: "stale", hours: 9 }] }),
      textsVi,
    );
    assert.equal(vi.body, "Đồng bộ văn bản của Đảng: chạy quá 4 giờ ở bước tải trang; Đồng bộ văn bản của Đảng: quá hạn, không có lần chạy thành công trong 9 giờ qua");
    // an alert stored before the thresholds were sent keeps the old wording
    const old = notificationText(alert({ workers: [{ worker: "vbpl_sync", problem: "stale", hours: "x" }] }), texts);
    assert.equal(old.body, "vbpl.vn nightly sync: overdue: no successful run in the last day");
  });

  it("keeps the stored text when no worker is named", () => {
    for (const payload of [null, {}, { count: 1 }, { workers: "x" }, { workers: [] }]) {
      assert.deepEqual(notificationText(alert(payload), texts), { title: "stored title", body: "stored body" });
    }
  });
});
