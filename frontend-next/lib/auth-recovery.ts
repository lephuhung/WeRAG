/* Stale-tenant recovery for /auth/me (used by AuthProvider.refreshMe).
 *
 * A stored X-Tenant-ID can go stale (removed membership, deleted tenant)
 * while the session tokens are still valid. The backend then rejects
 * /auth/me with 401/403 even though the user could sign in fine — signing
 * the user out for that is wrong. Transient failures (network blip, 5xx)
 * must likewise retain tokens: only a 401/403 that persists WITHOUT the
 * tenant header is confirmed invalid authentication worth clearing.
 *
 * Pure orchestrator (loader injected) so the policy is unit-testable; the
 * provider maps outcomes onto state + storage.
 */

import { ApiError } from "./api-client.ts";

export type MeLoadOutcome<T> =
  | { kind: "ok"; value: T; recoveredFromStaleTenant: boolean }
  | { kind: "invalid-auth" }
  | { kind: "transient"; error: unknown };

/* 401/403 may be the stale tenant, not the session — worth one retry
 * without the X-Tenant-ID header. A stale stored tenant id (deleted
 * tenant, lost membership, malformed value) is instead rejected by
 * internal/middleware/auth.go with HTTP 400 {error: "Invalid target
 * workspace ID"} / {error: "Invalid X-Tenant-ID header"} — that exact
 * tenant-header-related 400 is likewise worth one tenantless retry.
 * Anything else (network TypeError, 5xx, arbitrary 400, 0/abort) is
 * transient: retain tokens, do not retry. */
function tenantHeaderErrorMessage(error: unknown): string | null {
  if (!(error instanceof ApiError) || error.status !== 400) return null;
  const payload = error.payload;
  if (typeof payload === "string") return payload;
  if (payload && typeof payload === "object") {
    const record = payload as {
      error?: unknown;
      message?: unknown;
    };
    if (typeof record.error === "string") return record.error;
    if (record.error && typeof record.error === "object") {
      const nested = (record.error as { message?: unknown }).message;
      if (typeof nested === "string") return nested;
    }
    if (typeof record.message === "string") return record.message;
  }
  return typeof error.message === "string" ? error.message : null;
}

/* Matches only the tenant-header rejections above — never an arbitrary
 * 400, which must stay transient so a bad request cannot sign the user
 * out or trigger a pointless retry. */
function isTenantHeaderRejection(message: string | null): boolean {
  if (!message) return false;
  return /x-tenant-id|target workspace|invalid[^a-z0-9]+tenant|tenant[^a-z0-9]+invalid|unknown tenant|tenant not found|no tenant|not[^a-z0-9]+member|membership/i.test(
    message,
  );
}

/* Classify without conflating: a 400 can only implicate the stored tenant
 * header when one was actually stored — without it the header was never
 * sent, so the 400 did not come from us and must stay transient (retain
 * tokens) rather than clearing a valid session as invalid-auth. */
function rejectionKind(error: unknown): "auth" | "tenant" | "transient" {
  if (!(error instanceof ApiError)) return "transient";
  if (error.status === 401 || error.status === 403) return "auth";
  return isTenantHeaderRejection(tenantHeaderErrorMessage(error))
    ? "tenant"
    : "transient";
}

export async function loadWithStaleTenantRecovery<T>(
  load: (skipTenant: boolean) => Promise<T>,
  tenantStored: boolean,
): Promise<MeLoadOutcome<T>> {
  try {
    const value = await load(false);
    return { kind: "ok", value, recoveredFromStaleTenant: false };
  } catch (err) {
    const kind = rejectionKind(err);
    if (!tenantStored || kind === "transient") {
      return kind === "auth"
        ? { kind: "invalid-auth" }
        : { kind: "transient", error: err };
    }
    try {
      const value = await load(true);
      return { kind: "ok", value, recoveredFromStaleTenant: true };
    } catch (retryErr) {
      // A tenantless retry that still fails on auth is confirmed invalid;
      // any other tenantless failure (including a repeated tenant 400,
      // which cannot implicate a header we no longer send) retains tokens.
      return rejectionKind(retryErr) === "auth"
        ? { kind: "invalid-auth" }
        : { kind: "transient", error: retryErr };
    }
  }
}
