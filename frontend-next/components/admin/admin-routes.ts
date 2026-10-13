/* Paths of the SystemAdmin console. Kept apart from admin-sections so the
 * sidebar and command palette can link here without pulling the section
 * modules into their chunks. */

export const ADMIN_PATH = "/platform/admin";

export function adminSectionHref(key: string): string {
  return key === "dashboard" ? ADMIN_PATH : `${ADMIN_PATH}?section=${encodeURIComponent(key)}`;
}
