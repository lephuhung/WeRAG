/* Pure helpers of the in-app notifications, shared by the web bell and the
 * Word add-in pane (no imports, so node --test runs it as is).
 * Runs with: node --experimental-strip-types --test lib/notifications.test.ts */

/** How often the unread count is polled while the app is open. */
export const NOTIFICATION_POLL_MS = 60_000;
/** A focus / visibility poll is skipped within this long of the last one. */
export const NOTIFICATION_MIN_GAP_MS = 10_000;
/** At most this many toasts / banners at once; the rest are summarised. */
export const NOTIFICATION_MAX_TOASTS = 3;
/** Every notification link is a page of the app. */
export const NOTIFICATION_LINK_PREFIX = "/platform/";
/** Window event (detail: the path) the web bell dispatches when it opens a
 * notification's link: a page already showing that link re-applies its
 * query (the same URL pushed again changes nothing in the router). */
export const NOTIFICATION_OPENED_EVENT = "weknora:notification-opened";

/** linkParam reads the query parameter name of an app path ("" if none). */
export function linkParam(path: string, name: string): string {
  const i = path.indexOf("?");
  if (i < 0) return "";
  return new URLSearchParams(path.slice(i + 1).split("#")[0]).get(name) ?? "";
}

export const KIND_LEGAL_REQUEST_ADDED = "legal_request_added";
/** SuperAdmins: new abbreviation suggestions wait for review (a digest). */
export const KIND_ABBREVIATION_PENDING = "abbreviation_pending";
/** The page listing the suggestions waiting for review. */
export const ABBREVIATION_REVIEW_PATH = "/platform/system/extensions/abbreviations?status=pending";
/** A full form longer than this is cut with "…" in the digest body. */
const ABBREVIATION_FULL_FORM_MAX = 40;

/* SuperAdmin review-queue digests (payload: { count, items: [{ ref, title?,
 * count?, plan?, amount_vnd?, org? }] }), one unread per SuperAdmin and
 * kind, like abbreviation_pending. */
/** Payment orders waiting for their transfer / confirmation. */
export const KIND_PAYMENT_ORDER_PENDING = "payment_order_pending";
/** Legal documents people asked for in chat. */
export const KIND_LEGAL_REQUEST_PENDING = "legal_request_pending";
/** OCR drafts of scanned legal documents ready for review. */
export const KIND_LEGAL_DRAFT_READY = "legal_draft_ready";
/** Amendment links a legal feed pushed, waiting for review. */
export const KIND_LEGAL_LINK_PENDING = "legal_link_pending";
/** Public-KB documents waiting for their identity review. */
export const KIND_LEGAL_IDENTITY_PENDING = "legal_identity_pending";
export const QUEUE_KINDS = [
  KIND_PAYMENT_ORDER_PENDING,
  KIND_LEGAL_REQUEST_PENDING,
  KIND_LEGAL_DRAFT_READY,
  KIND_LEGAL_LINK_PENDING,
  KIND_LEGAL_IDENTITY_PENDING,
] as const;
export type QueueKind = (typeof QUEUE_KINDS)[number];
/** A plan, organization or title longer than this is cut in the body. */
const QUEUE_TEXT_MAX = 60;

/** SuperAdmins: backup jobs failed or are overdue (payload: { count, jobs:
 * [{ job, problem, finished_at?, last_ok_at?, note? }], signature }), one
 * unread per SuperAdmin, closed once every job is healthy again. */
export const KIND_BACKUP_ALERT = "backup_alert";
/** The SuperAdmin page listing the backup jobs. */
export const BACKUP_STATUS_PATH = "/platform/admin?section=backups";
export const BACKUP_PROBLEMS = ["failed", "stale", "missing", "unreadable"] as const;
export type BackupProblem = (typeof BACKUP_PROBLEMS)[number];
/** A job's note longer than this is cut in the body. */
const BACKUP_NOTE_MAX = 120;

export function isQueueKind(kind: string): kind is QueueKind {
  return (QUEUE_KINDS as readonly string[]).includes(kind);
}

/** isReviewDigest: a SuperAdmin digest (review queues, backup alert),
 * whose page is in the web app's admin console (the Word add-in only says
 * where). */
export function isReviewDigest(kind: string): boolean {
  return kind === KIND_ABBREVIATION_PENDING || kind === KIND_BACKUP_ALERT || isQueueKind(kind);
}

/** The fields these helpers read (lib/api/notifications UserNotification). */
export interface NotificationLike {
  id: string;
  kind: string;
  title: string;
  body: string;
  link: string;
  tenant_id?: number;
  payload: Record<string, unknown> | null;
  /** Moves forward when a digest (abbreviation_pending) is updated. */
  created_at?: string;
  seen_at: string | null;
  read_at: string | null;
}

/** toastKey names one version of a notification: a digest updated in
 * place keeps its id but gets a new created_at (and seen_at cleared), so
 * it is toasted again. Tabs remember what they toasted by this key. */
export function toastKey(n: Pick<NotificationLike, "id" | "created_at">): string {
  return `${n.id}@${n.created_at ?? ""}`;
}

/** toastable: unread notifications no toast was shown for yet, oldest
 * first, skipping the versions this tab already toasted (toastKey; a POST
 * seen may lag). */
export function toastable<T extends NotificationLike>(items: T[], alreadyToasted: ReadonlySet<string>): T[] {
  return items.filter((n) => !n.seen_at && !n.read_at && !alreadyToasted.has(toastKey(n))).reverse();
}

/** mergeFresh puts freshly fetched notifications into a list already
 * shown (null: none fetched yet, kept null): a known id is replaced by its
 * new version (an updated digest), an unknown one is added; newest first. */
export function mergeFresh<T extends NotificationLike>(shown: T[] | null, fresh: T[]): T[] | null {
  if (!shown) return shown;
  const byId = new Map(fresh.map((n) => [n.id, n]));
  const merged = shown.map((n) => byId.get(n.id) ?? n);
  const known = new Set(shown.map((n) => n.id));
  merged.push(...fresh.filter((n) => !known.has(n.id)));
  const at = (n: T) => Date.parse(n.created_at ?? "") || 0;
  return merged.sort((a, b) => at(b) - at(a));
}

/** unseenIds: every fetched notification still without seen_at, so a seen
 * POST that failed before is sent again. */
export function unseenIds(items: Pick<NotificationLike, "id" | "seen_at">[]): string[] {
  return items.filter((n) => !n.seen_at).map((n) => n.id);
}

/** toastPlan keeps the newest `max` of fresh (oldest first) to toast and
 * counts the rest for one "+N more" summary. */
export function toastPlan<T>(fresh: T[], max: number = NOTIFICATION_MAX_TOASTS): { show: T[]; more: number } {
  if (fresh.length <= max) return { show: fresh, more: 0 };
  return { show: fresh.slice(fresh.length - max), more: fresh.length - max };
}

/** shouldPoll: a focus / visibility poll waits NOTIFICATION_MIN_GAP_MS
 * after the last one; the timer's poll always runs. */
export function shouldPoll(lastPollAt: number, now: number, fromTimer: boolean): boolean {
  return fromTimer || now - lastPollAt >= NOTIFICATION_MIN_GAP_MS;
}

// eslint-disable-next-line no-control-regex
const CONTROL_CHARS = /[\u0000-\u001f\u007f]/;

/** safeAppLink keeps a link to a page of this app under /platform/ and
 * returns its path (with query and hash); "" otherwise. Control characters
 * (browsers drop tabs and newlines, so "/\t/evil.example" would leave the
 * site), backslashes and protocol-relative links are refused, and the link
 * must resolve to this origin. */
export function safeAppLink(link: string | null | undefined, origin: string): string {
  const l = link ?? "";
  if (!l || CONTROL_CHARS.test(l) || !l.startsWith("/") || l.startsWith("//") || l.includes("\\")) return "";
  let url: URL;
  try {
    url = new URL(l, origin);
  } catch {
    return "";
  }
  if (url.origin !== new URL(origin).origin || !url.pathname.startsWith(NOTIFICATION_LINK_PREFIX)) return "";
  return url.pathname + url.search + url.hash;
}

/** badgeLabel is the bell badge text: "" for none, "9+" past nine. */
export function badgeLabel(unread: number): string {
  if (!unread || unread < 0) return "";
  return unread > 9 ? "9+" : String(unread);
}

/** payloadString reads a string field of a notification payload. */
export function payloadString(n: Pick<NotificationLike, "payload">, key: string): string {
  const v = n.payload?.[key];
  return typeof v === "string" ? v : "";
}

/** The lookup page searching for a document. */
export function legalLookupPath(doc: string): string {
  return `/platform/legal-lookup?q=${encodeURIComponent(doc)}`;
}

/** Where a click on a notification goes, which its hint names. */
export type NotificationHint =
  | "chat" // the conversation it was asked in opens
  | "word" // asked in the Word add-in, from another file: reopen that file
  | "web" // (add-in) a web conversation: open it on the web
  | "lookup" // the conversation is gone: the legal lookup page
  | "otherOrg"; // the conversation belongs to another organization

/** The words a notification is shown with, in the reader's language. */
export interface NotificationTexts {
  legalAddedTitle: (doc: string) => string;
  legalAddedChat: string;
  legalAddedWord: string;
  legalAddedWeb: string;
  legalAddedLookup: string;
  /** org "" = its name is not known */
  legalAddedOtherOrg: (org: string) => string;
  /** "{n} new abbreviations waiting for review" */
  abbreviationPendingTitle: (count: number) => string;
  /** a proper name kept as typed: "PV01 (proper name)" */
  abbreviationProperName: (short: string) => string;
  /** the suggestions the digest does not name: "and {n} more" */
  abbreviationPendingMore: (count: number) => string;
  /** a review-queue digest: "{n} new payment orders awaiting confirmation" */
  queueTitle: (kind: QueueKind, count: number) => string;
  /** the items a review-queue digest does not name: "and {n} more orders"
   * (never for links: their items carry the link counts) */
  queueMore: (kind: Exclude<QueueKind, typeof KIND_LEGAL_LINK_PENDING>, count: number) => string;
  /** how often a legal document was requested: "3 requests" */
  queueRequestCount: (count: number) => string;
  /** the pending links of a pushed document: "12 links" */
  queueLinkCount: (count: number) => string;
  /** an amount in đồng: "1,250,000 VND" / "1.250.000 đ" */
  money: (vnd: number) => string;
  /** an item with neither số hiệu nor title: "untitled document" */
  queueUntitled: string;
  /** "Backup problems: {n} job(s)" */
  backupAlertTitle: (count: number) => string;
  /** a job's problem; at = its last run (failed) in the reader's format, "" if unknown */
  backupProblem: (problem: BackupProblem, at: string) => string;
  /** "last success {at}" */
  backupLastOk: (at: string) => string;
  /** an ISO time in Vietnam time ("14h05 - 16/10/2026"); "" when invalid */
  dateTime: (iso: string) => string;
}

type QueueItem = { ref: string; title: string; count: number; plan: string; amount: number; org: string; untitled: boolean };

/** The items a review-queue digest names, newest first. */
function queueItems(n: Pick<NotificationLike, "payload">): QueueItem[] {
  const raw = n.payload?.items;
  if (!Array.isArray(raw)) return [];
  const str = (o: Record<string, unknown>, k: string) => (typeof o[k] === "string" ? (o[k] as string).trim() : "");
  const num = (o: Record<string, unknown>, k: string) => (typeof o[k] === "number" && Number.isFinite(o[k]) ? (o[k] as number) : 0);
  return raw.flatMap((it) => {
    if (!it || typeof it !== "object") return [];
    const o = it as Record<string, unknown>;
    const ref = str(o, "ref");
    const untitled = o.untitled === true && !ref;
    return ref || untitled
      ? [{ ref, title: str(o, "title"), count: num(o, "count"), plan: str(o, "plan"), amount: num(o, "amount_vnd"), org: str(o, "org"), untitled }]
      : [];
  });
}

/** "53/2022/NĐ-CP – Quy định chi tiết…" */
function refTitle(it: QueueItem, texts: NotificationTexts): string {
  if (it.untitled) return texts.queueUntitled;
  return it.title && it.title.toLowerCase() !== it.ref.toLowerCase()
    ? `${it.ref} – ${cut(it.title, QUEUE_TEXT_MAX)}`
    : cut(it.ref, QUEUE_TEXT_MAX);
}

function queueItemText(kind: QueueKind, it: QueueItem, texts: NotificationTexts): string {
  switch (kind) {
    case KIND_PAYMENT_ORDER_PENDING:
      return [it.ref, cut(it.plan, QUEUE_TEXT_MAX), it.amount > 0 ? texts.money(it.amount) : "", cut(it.org, QUEUE_TEXT_MAX)]
        .filter(Boolean)
        .join(" · ");
    case KIND_LEGAL_REQUEST_PENDING:
      return it.count > 1 ? `${refTitle(it, texts)} (${texts.queueRequestCount(it.count)})` : refTitle(it, texts);
    case KIND_LEGAL_LINK_PENDING:
      return it.count > 0 ? `${it.ref} (${texts.queueLinkCount(it.count)})` : it.ref;
    case KIND_LEGAL_DRAFT_READY:
      return refTitle(it, texts);
    default:
      return it.untitled ? texts.queueUntitled : cut(it.ref, QUEUE_TEXT_MAX);
  }
}

type BackupJob = { job: string; problem: BackupProblem; finishedAt: string; lastOkAt: string; note: string };

/** The jobs a backup_alert names, in job order. */
function backupJobs(n: Pick<NotificationLike, "payload">): BackupJob[] {
  const raw = n.payload?.jobs;
  if (!Array.isArray(raw)) return [];
  const str = (o: Record<string, unknown>, k: string) => (typeof o[k] === "string" ? (o[k] as string).trim() : "");
  return raw.flatMap((it) => {
    if (!it || typeof it !== "object") return [];
    const o = it as Record<string, unknown>;
    const job = str(o, "job");
    const problem = str(o, "problem");
    if (!job || !(BACKUP_PROBLEMS as readonly string[]).includes(problem)) return [];
    return [{ job, problem: problem as BackupProblem, finishedAt: str(o, "finished_at"), lastOkAt: str(o, "last_ok_at"), note: str(o, "note") }];
  });
}

/** "files: failed at 09h00 - 12/11/2026, last success … — tar: …" */
function backupJobText(j: BackupJob, texts: NotificationTexts): string {
  const at = j.problem === "failed" && j.finishedAt ? texts.dateTime(j.finishedAt) : "";
  let s = `${j.job}: ${texts.backupProblem(j.problem, at)}`;
  const ok = j.lastOkAt ? texts.dateTime(j.lastOkAt) : "";
  if (ok) s += `, ${texts.backupLastOk(ok)}`;
  if (j.note) s += ` — ${cut(j.note, BACKUP_NOTE_MAX)}`;
  return s;
}

/** The suggestions an abbreviation_pending digest names, newest first. */
function abbreviationItems(n: Pick<NotificationLike, "payload">): { short: string; full: string; proper: boolean }[] {
  const raw = n.payload?.items;
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((it) => {
    if (!it || typeof it !== "object") return [];
    const o = it as Record<string, unknown>;
    const short = typeof o.short_form === "string" ? o.short_form.trim() : "";
    const full = typeof o.full_form === "string" ? o.full_form.trim() : "";
    return short ? [{ short, full, proper: o.proper_name === true }] : [];
  });
}

function cut(s: string, max: number): string {
  const chars = Array.from(s);
  return chars.length <= max ? s : `${chars.slice(0, max - 1).join("").trim()}…`;
}

/** The hint of n when nothing else is known: from its link and channel. */
export function defaultHint(n: Pick<NotificationLike, "link" | "payload">): NotificationHint {
  if (!n.link.startsWith("/platform/chat/")) return "lookup";
  return payloadString(n, "channel") === "word_addin" ? "word" : "chat";
}

/** notificationText renders a known kind from its payload in the reader's
 * language, its hint matching where a click goes (dest; the link's when
 * absent); an unknown kind (or a payload without what it needs) keeps the
 * stored (Vietnamese) title and body. */
export function notificationText(
  n: NotificationLike,
  texts: NotificationTexts,
  dest?: { hint: NotificationHint; orgName?: string },
): { title: string; body: string } {
  if (n.kind === KIND_ABBREVIATION_PENDING) {
    const items = abbreviationItems(n);
    const count = typeof n.payload?.count === "number" ? Math.max(n.payload.count, items.length) : 0;
    if (count > 0 && items.length > 0) {
      const parts = items.map((it) =>
        it.proper || !it.full ? texts.abbreviationProperName(it.short) : `${it.short} → ${cut(it.full, ABBREVIATION_FULL_FORM_MAX)}`,
      );
      if (count > items.length) parts.push(texts.abbreviationPendingMore(count - items.length));
      return { title: texts.abbreviationPendingTitle(count), body: parts.join(", ") };
    }
  }
  if (isQueueKind(n.kind)) {
    const items = queueItems(n);
    const count = typeof n.payload?.count === "number" ? Math.max(n.payload.count, items.length) : 0;
    // the title alone when no item is named: never the stored Vietnamese
    if (count > 0) {
      const parts = items.map((it) => queueItemText(n.kind as QueueKind, it, texts));
      // links count links while the items name documents: no "and N more"
      if (n.kind !== KIND_LEGAL_LINK_PENDING && items.length > 0 && count > items.length) parts.push(texts.queueMore(n.kind, count - items.length));
      return { title: texts.queueTitle(n.kind, count), body: parts.join(", ") };
    }
  }
  if (n.kind === KIND_BACKUP_ALERT) {
    const jobs = backupJobs(n);
    if (jobs.length > 0) {
      return { title: texts.backupAlertTitle(jobs.length), body: jobs.map((j) => backupJobText(j, texts)).join("; ") };
    }
  }
  if (n.kind === KIND_LEGAL_REQUEST_ADDED) {
    const number = payloadString(n, "document_number").trim();
    const docTitle = payloadString(n, "title").trim();
    const doc = number || docTitle;
    if (doc) {
      const hintKind = dest?.hint ?? defaultHint(n);
      const hint = {
        chat: texts.legalAddedChat,
        word: texts.legalAddedWord,
        web: texts.legalAddedWeb,
        lookup: texts.legalAddedLookup,
        otherOrg: texts.legalAddedOtherOrg(dest?.orgName ?? ""),
      }[hintKind];
      return { title: texts.legalAddedTitle(doc), body: docTitle && docTitle !== doc ? `${docTitle}. ${hint}` : hint };
    }
  }
  return { title: n.title, body: n.body };
}

export type MembershipLike = { tenant_id: number; tenant_name?: string };

/** The organization of n when it is not the one the person works in now
 * (a Guest, or an unknown current organization, never differs). */
function otherOrg(
  n: Pick<NotificationLike, "tenant_id">,
  currentTenantId: number | null,
  memberships: MembershipLike[],
  isGuest: boolean,
): { differs: boolean; orgName: string } {
  const tenant = n.tenant_id ?? 0;
  if (isGuest || !tenant || currentTenantId == null || tenant === currentTenantId) return { differs: false, orgName: "" };
  return { differs: true, orgName: memberships.find((m) => m.tenant_id === tenant)?.tenant_name ?? "" };
}

/** Where a click on n goes in the web app, and the hint to show: its link;
 * the legal lookup page when the conversation belongs to another
 * organization (the web app has no organization switcher, so it never
 * switches on its own). */
export function resolveTarget(
  n: NotificationLike,
  ctx: { origin: string; currentTenantId: number | null; memberships: MembershipLike[]; isGuest: boolean },
): { path: string; hint: NotificationHint; orgName: string } {
  const path = safeAppLink(n.link, ctx.origin);
  const hint = defaultHint(n);
  if (!path) return { path: "", hint, orgName: "" };
  if (!path.startsWith("/platform/chat/")) return { path, hint, orgName: "" };
  const other = otherOrg(n, ctx.currentTenantId, ctx.memberships, ctx.isGuest);
  if (!other.differs) return { path, hint, orgName: "" };
  const doc = payloadString(n, "document_number") || payloadString(n, "title");
  return { path: doc ? legalLookupPath(doc) : "", hint: "otherOrg", orgName: other.orgName };
}

/** wordFileTarget: the conversation the Word add-in may open for n, when
 * the notification names a session bound to the Word file open now (and
 * happened in the organization the pane works in); "" otherwise (the pane
 * then shows the text only). */
export function wordFileTarget(
  n: Pick<NotificationLike, "payload" | "tenant_id">,
  openWordFileId: string | null | undefined,
  currentTenantId?: number | null,
): string {
  const session = payloadString(n, "session_id");
  const file = payloadString(n, "word_file_id");
  if (!session || !file || !openWordFileId || file !== openWordFileId) return "";
  if (currentTenantId != null && n.tenant_id && n.tenant_id !== currentTenantId) return "";
  return session;
}

/** The hint of n in the Word add-in pane: what a click there does. */
export function paneHint(
  n: NotificationLike,
  ctx: { openable: boolean; currentTenantId: number | null; memberships: MembershipLike[]; isGuest: boolean },
): { hint: NotificationHint; orgName: string } {
  if (ctx.openable) return { hint: "chat", orgName: "" };
  if (!n.link.startsWith("/platform/chat/")) return { hint: "lookup", orgName: "" };
  const other = otherOrg(n, ctx.currentTenantId, ctx.memberships, ctx.isGuest);
  if (other.differs) return { hint: "otherOrg", orgName: other.orgName };
  return { hint: payloadString(n, "word_file_id") ? "word" : "web", orgName: "" };
}
