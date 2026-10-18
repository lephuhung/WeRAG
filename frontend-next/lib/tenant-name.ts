/* Organization (tenant) names: the SuperAdmin creates each organization with
 * the agency's official name. The server normalizes the name (trim, collapse
 * inner whitespace), requires it, caps it at 255 characters and rejects a
 * name another live organization already uses (case-insensitive) with 409.
 * These helpers mirror the normalization client-side and turn the server's
 * 400/409 answers into i18n keys. Pure, so unit-testable. */

export const TENANT_NAME_MAX_LENGTH = 255;

export function normalizeTenantName(name: string): string {
  return name.trim().split(/\s+/).filter(Boolean).join(" ");
}

export type TenantNameErrorKey =
  "tnp.nameTaken" | "tnp.nameRequired" | "tnp.nameTooLong";

/* Client-side check before submitting; null when the name is acceptable. */
export function validateTenantName(name: string): TenantNameErrorKey | null {
  const normalized = normalizeTenantName(name);
  if (!normalized) return "tnp.nameRequired";
  if ([...normalized].length > TENANT_NAME_MAX_LENGTH) return "tnp.nameTooLong";
  return null;
}

/* AppError codes of internal/errors/errors.go the server answers with. */
const NAME_ERROR_CODES: Record<number, TenantNameErrorKey> = {
  2001: "tnp.nameTaken",
  2003: "tnp.nameRequired",
  2006: "tnp.nameTooLong",
};

/* Maps a rejected create/rename request to an i18n key, or null when the
 * failure is not about the name. Prefers the AppError code in the payload;
 * falls back to ApiError's status and English message. */
export function tenantNameErrorKey(err: unknown): TenantNameErrorKey | null {
  if (typeof err !== "object" || err === null) return null;
  const payload = (err as { payload?: unknown }).payload;
  const code =
    payload && typeof payload === "object"
      ? (payload as { error?: { code?: unknown } }).error?.code
      : undefined;
  if (typeof code === "number" && NAME_ERROR_CODES[code]) return NAME_ERROR_CODES[code];
  const status = (err as { status?: unknown }).status;
  const message = String(
    (err as { message?: unknown }).message ?? "",
  ).toLowerCase();
  if (
    status === 409 &&
    message.includes("organization with this name already exists")
  )
    return "tnp.nameTaken";
  if (status === 400 && message.includes("organization name is required"))
    return "tnp.nameRequired";
  if (status === 400 && message.includes("organization name must be at most"))
    return "tnp.nameTooLong";
  return null;
}
