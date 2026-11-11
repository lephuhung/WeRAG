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

export const KIND_LEGAL_REQUEST_ADDED = "legal_request_added";
/** SuperAdmins: new abbreviation suggestions wait for review (a digest). */
export const KIND_ABBREVIATION_PENDING = "abbreviation_pending";
/** The page listing the suggestions waiting for review. */
export const ABBREVIATION_REVIEW_PATH = "/platform/system/extensions/abbreviations?status=pending";
/** A full form longer than this is cut with "…" in the digest body. */
const ABBREVIATION_FULL_FORM_MAX = 40;

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
