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

/** The fields these helpers read (lib/api/notifications UserNotification). */
export interface NotificationLike {
  id: string;
  kind: string;
  title: string;
  body: string;
  link: string;
  tenant_id?: number;
  payload: Record<string, unknown> | null;
  seen_at: string | null;
  read_at: string | null;
}

/** toastable: unread notifications no toast was shown for yet, oldest
 * first, skipping the ids this tab already toasted (a POST seen may lag). */
export function toastable<T extends NotificationLike>(items: T[], alreadyToasted: ReadonlySet<string>): T[] {
  return items.filter((n) => !n.seen_at && !n.read_at && !alreadyToasted.has(n.id)).reverse();
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

/** The words a notification is shown with, in the reader's language. */
export interface NotificationTexts {
  legalAddedTitle: (doc: string) => string;
  /** "you can ask again in the conversation" */
  legalAddedChat: string;
  /** "you asked in Word: reopen the file there" */
  legalAddedWord: string;
  /** "look it up now" (the conversation is gone) */
  legalAddedLookup: string;
}

/** notificationText renders a known kind from its payload in the reader's
 * language; an unknown kind (or a payload without what it needs) keeps the
 * stored (Vietnamese) title and body. */
export function notificationText(n: NotificationLike, texts: NotificationTexts): { title: string; body: string } {
  if (n.kind === KIND_LEGAL_REQUEST_ADDED) {
    const number = payloadString(n, "document_number").trim();
    const docTitle = payloadString(n, "title").trim();
    const doc = number || docTitle;
    if (doc) {
      let hint = texts.legalAddedLookup;
      if (n.link.startsWith("/platform/chat/")) {
        hint = payloadString(n, "channel") === "word_addin" ? texts.legalAddedWord : texts.legalAddedChat;
      }
      return { title: texts.legalAddedTitle(doc), body: docTitle && docTitle !== doc ? `${docTitle}. ${hint}` : hint };
    }
  }
  return { title: n.title, body: n.body };
}

/** Where a click on n goes in the web app: its link, after switching to the
 * organization it happened in when that is another one the person belongs
 * to; the legal lookup page when that organization is out of reach. */
export function resolveTarget(
  n: NotificationLike,
  ctx: { origin: string; currentTenantId: number | null; memberTenantIds: number[]; isGuest: boolean },
): { path: string; switchTenantId: number | null } {
  const path = safeAppLink(n.link, ctx.origin);
  if (!path) return { path: "", switchTenantId: null };
  const tenant = n.tenant_id ?? 0;
  if (!path.startsWith("/platform/chat/") || ctx.isGuest || !tenant || ctx.currentTenantId == null || tenant === ctx.currentTenantId) {
    return { path, switchTenantId: null };
  }
  if (ctx.memberTenantIds.includes(tenant)) return { path, switchTenantId: tenant };
  const doc = payloadString(n, "document_number") || payloadString(n, "title");
  return { path: doc ? legalLookupPath(doc) : "", switchTenantId: null };
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
