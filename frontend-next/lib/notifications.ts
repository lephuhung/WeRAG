/* Pure helpers of the in-app notifications, shared by the web bell and the
 * Word add-in pane (no imports, so node --test runs it as is).
 * Runs with: node --experimental-strip-types --test lib/notifications.test.ts */

/** How often the unread count is polled while the app is open. */
export const NOTIFICATION_POLL_MS = 60_000;

/** The fields these helpers read (lib/api/notifications UserNotification). */
export interface NotificationLike {
  id: string;
  kind: string;
  link: string;
  payload: Record<string, unknown> | null;
  seen_at: string | null;
  read_at: string | null;
}

/** toastable: unread notifications no toast was shown for yet, oldest
 * first, skipping the ids this tab already toasted (a POST seen may lag). */
export function toastable<T extends NotificationLike>(items: T[], alreadyToasted: ReadonlySet<string>): T[] {
  return items.filter((n) => !n.seen_at && !n.read_at && !alreadyToasted.has(n.id)).reverse();
}

/** safeAppLink keeps an app-relative path ("/platform/…"), "" otherwise: a
 * notification never sends the browser to another site. */
export function safeAppLink(link: string | null | undefined): string {
  const l = (link ?? "").trim();
  if (!l.startsWith("/") || l.startsWith("//") || l.includes("\\")) return "";
  return l;
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

/** wordFileTarget: the conversation the Word add-in may open for n, when
 * the notification names a session bound to the Word file open now; ""
 * otherwise (the pane then shows the text only). */
export function wordFileTarget(n: Pick<NotificationLike, "payload">, openWordFileId: string | null | undefined): string {
  const session = payloadString(n, "session_id");
  const file = payloadString(n, "word_file_id");
  if (!session || !file || !openWordFileId) return "";
  return file === openWordFileId ? session : "";
}
