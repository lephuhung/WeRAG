/* The signed-in person's in-app notifications (/api/v1/notifications).
 * Every call reads or changes the caller's own rows only, whichever
 * organization (or the public portal, for a Guest) they are in. Shared
 * with the Word add-in (scripts/shared-files.json there). */
import { apiGet, apiPost } from "@/lib/api-client";

/** payload of kind "legal_request_added". */
export interface LegalRequestAddedPayload {
  request_id: string;
  document_number: string;
  title: string;
  session_id: string;
  /** "web" | "word_addin" | "portal" | "" */
  channel: string;
  /** The Word file the conversation is bound to (Word add-in sessions). */
  word_file_id?: string;
}

export interface UserNotification {
  id: string;
  tenant_id: number;
  kind: string;
  title: string;
  body: string;
  /** App-relative path ("/platform/chat/<id>"), "" for none. */
  link: string;
  payload: Record<string, unknown> | null;
  created_at: string;
  /** The toast was shown. */
  seen_at: string | null;
  read_at: string | null;
}

export interface NotificationListResponse {
  success: boolean;
  data?: { items: UserNotification[]; next_before: string | null };
}

export interface UnreadCountResponse {
  success: boolean;
  data?: { unread: number; unseen: number };
}

export function listNotifications(
  opts: { unread?: boolean; limit?: number; before?: string } = {},
): Promise<NotificationListResponse> {
  const q = new URLSearchParams();
  if (opts.unread) q.set("unread", "true");
  if (opts.limit) q.set("limit", String(opts.limit));
  if (opts.before) q.set("before", opts.before);
  const qs = q.toString();
  return apiGet(`/api/v1/notifications${qs ? `?${qs}` : ""}`);
}

export function getUnreadNotificationCount(): Promise<UnreadCountResponse> {
  return apiGet("/api/v1/notifications/unread-count");
}

export function markNotificationsSeen(ids: string[]): Promise<{ success: boolean }> {
  return apiPost("/api/v1/notifications/seen", { ids });
}

export function markNotificationRead(id: string): Promise<{ success: boolean }> {
  return apiPost(`/api/v1/notifications/${encodeURIComponent(id)}/read`, {});
}

export function markAllNotificationsRead(): Promise<{ success: boolean }> {
  return apiPost("/api/v1/notifications/read-all", {});
}
