/* Ported from frontend/src/api/auth/index.ts.
 * i18n `t()` fallbacks are replaced with plain strings — callers should map
 * ApiError.message through useT() where a localized string is wanted.
 */
import { apiGet, apiPost, apiPut } from "@/lib/api-client";

// ---- request/response types ----------------------------------------------

export interface LoginRequest {
  email: string;
  password: string;
}

export interface LoginResponse {
  success: boolean;
  message?: string;
  user?: {
    id: string;
    username: string;
    email: string;
    avatar?: string;
    tenant_id: number;
    can_access_all_tenants?: boolean;
    is_system_admin?: boolean;
    is_active: boolean;
    created_at: string;
    updated_at: string;
  };
  tenant?: {
    id: number;
    name: string;
    description: string;
    status: string;
    business: string;
    storage_quota: number;
    storage_used: number;
    created_at: string;
    updated_at: string;
  } | null;
  // active_tenant mirrors `tenant` for endpoints that distinguish home
  // tenant from current tenant (e.g. /auth/register-by-invite). Only
  // one of `tenant` / `active_tenant` is populated by any given endpoint.
  active_tenant?: {
    id: number;
    name: string;
    description?: string;
    status?: string;
    business?: string;
    storage_quota?: number;
    storage_used?: number;
    created_at?: string;
    updated_at?: string;
  } | null;
  memberships?: MembershipInfo[];
  token?: string;
  refresh_token?: string;
}

export interface OIDCAuthURLResponse {
  success: boolean;
  authorization_url?: string;
  state?: string;
  message?: string;
}

export interface OIDCConfigResponse {
  success: boolean;
  enabled: boolean;
  provider_display_name?: string;
  message?: string;
}

export interface RegisterRequest {
  username: string;
  email: string;
  password: string;
}

export interface RegisterResponse {
  success: boolean;
  message?: string;
  data?: {
    user: { id: string; username: string; email: string };
    tenant: { id: string; name: string };
  };
}

/* User preferences — mirrors backend types.UserPreferences; absent keys mean
 * "never explicitly set". New keys must also be handled in the backend's
 * UpdateUserPreferences merge branch. */
export interface UserPreferences {
  browser_search_instructions?: string | null;
  // last_active_tenant_id persists "return to last workspace" across
  // refresh / device change / re-login. The backend only honors it when
  // membership is still valid, else falls back to home and clears it.
  // Pass 0 to PATCH to clear the preference.
  last_active_tenant_id?: number | null;
  // oidc_only_login=true: account was auto-provisioned via OIDC and has
  // no known password set yet.
  oidc_only_login?: boolean;
  // language is the UI locale this user picked ("en" | "vi"); persisted
  // server-side so the choice follows the account across browsers/devices.
  language?: string | null;
}

export interface UserInfo {
  id: string;
  username: string;
  email: string;
  avatar?: string;
  tenant_id: string;
  can_access_all_tenants?: boolean;
  preferences?: UserPreferences;
  is_system_admin?: boolean;
  created_at: string;
  updated_at: string;
}

/* Normalizes the backend user JSON into UserInfo. This single factory
 * exists because historically 4 separate setUser paths each hand-picked a
 * field whitelist — a new user field silently vanished if one copy was
 * missed (is_system_admin shipped that way once). **Add new user fields
 * here only**.
 *
 * fallbackTenantId backs up tenant_id when missing (autoSetup returns
 * tenant.id at top level but not on the user object; /auth/me occasionally
 * returns user without tenant).
 *
 * Boolean reads use `=== true` to strictly converge odd truthy payloads
 * (1/0, strings) instead of accidentally granting permissions. */
export function userInfoFromApi(
  u: {
    id?: string;
    username?: string;
    email?: string;
    avatar?: string;
    tenant_id?: string | number | null;
    can_access_all_tenants?: unknown;
    is_system_admin?: unknown;
    preferences?: UserPreferences;
    created_at?: string;
    updated_at?: string;
  } | null | undefined,
  fallbackTenantId?: string | number | null,
): UserInfo {
  const rawTenantId =
    u?.tenant_id !== undefined && u?.tenant_id !== null && u.tenant_id !== ""
      ? u.tenant_id
      : fallbackTenantId ?? "";
  const tid = Number(rawTenantId) > 0 ? rawTenantId : "";
  return {
    id: u?.id || "",
    username: u?.username || "",
    email: u?.email || "",
    avatar: u?.avatar,
    tenant_id: String(tid) || "",
    can_access_all_tenants: u?.can_access_all_tenants === true,
    is_system_admin: u?.is_system_admin === true,
    preferences: u?.preferences,
    created_at: u?.created_at || new Date().toISOString(),
    updated_at: u?.updated_at || new Date().toISOString(),
  };
}

export interface TenantInfo {
  id: string;
  name: string;
  description?: string;
  status?: string;
  business?: string;
  owner_id: string;
  storage_quota?: number;
  storage_used?: number;
  created_at: string;
  updated_at: string;
  knowledge_bases?: KnowledgeBaseInfo[];
}

export interface KnowledgeBaseInfo {
  id: string;
  name: string;
  description: string;
  tenant_id: string;
  // creator_id is the user id of whoever originally created the KB;
  // nullable for legacy KBs created before the column was backfilled.
  creator_id?: string;
  // creator_name is batch-backfilled by the list endpoint (username first,
  // email fallback) purely for the list-card source badge.
  creator_name?: string;
  created_at: string;
  updated_at: string;
  document_count?: number;
  chunk_count?: number;
}

export interface ModelInfo {
  id: string;
  name: string;
  type: string;
  source: string;
  description?: string;
  is_default?: boolean;
  created_at: string;
  updated_at: string;
}

export interface MembershipInfo {
  tenant_id: number;
  tenant_name?: string;
  role: string;
}

export interface AuthCapabilities {
  can_create_tenant: boolean;
  auto_accept_invitation: boolean;
}

export interface AuthConfigResponse {
  success: boolean;
  registration_mode: "self_serve" | "invite_only" | string;
  complex_password_enabled: boolean;
}

export interface ChangePasswordRequest {
  old_password: string;
  new_password: string;
}

export interface InviteLookup {
  tenant_id: number;
  tenant_name?: string;
  role: string;
  expires_at: string;
  org_id?: number;
  org_name?: string;
}

export interface InviteLookupResponse {
  success: boolean;
  data?: InviteLookup;
  message?: string;
}

export interface RegisterByInviteRequest {
  token: string;
  email: string;
  username: string;
  password: string;
}

export type CurrentUserResponse = {
  success: boolean;
  data?: {
    user: UserInfo;
    tenant?: TenantInfo | null;
    memberships?: MembershipInfo[];
    tenant_required?: boolean;
    capabilities?: AuthCapabilities;
    preference_defaults?: { browser_search_instructions: string };
  };
  message?: string;
};

// ---- endpoints ------------------------------------------------------------

export async function login(data: LoginRequest): Promise<LoginResponse> {
  try {
    return await apiPost<LoginResponse>("/api/v1/auth/login", data);
  } catch (error) {
    return { success: false, message: error instanceof Error ? error.message : "Login failed" };
  }
}

export async function getOIDCAuthorizationURL(redirectURI: string): Promise<OIDCAuthURLResponse> {
  try {
    return await apiGet<OIDCAuthURLResponse>(
      `/api/v1/auth/oidc/url?redirect_uri=${encodeURIComponent(redirectURI)}`,
    );
  } catch (error) {
    return { success: false, message: error instanceof Error ? error.message : "Login failed" };
  }
}

export async function getOIDCConfig(): Promise<OIDCConfigResponse> {
  try {
    return await apiGet<OIDCConfigResponse>("/api/v1/auth/oidc/config");
  } catch (error) {
    return {
      success: false,
      enabled: false,
      message: error instanceof Error ? error.message : "Login failed",
    };
  }
}

/* Public auth config — only fields the UI needs (registration mode etc).
 * Falls back to self_serve on failure so a broken endpoint doesn't hide the
 * registration entry point. */
export async function getAuthConfig(): Promise<AuthConfigResponse> {
  try {
    return await apiGet<AuthConfigResponse>("/api/v1/auth/config");
  } catch {
    return { success: false, registration_mode: "self_serve", complex_password_enabled: false };
  }
}

export async function register(data: RegisterRequest): Promise<RegisterResponse> {
  try {
    return await apiPost<RegisterResponse>("/api/v1/auth/register", data);
  } catch (error) {
    return {
      success: false,
      message: error instanceof Error ? error.message : "Registration failed",
    };
  }
}

/* Lite-edition auto-setup: creates default user/tenant and returns tokens.
 * Requires the Wails desktop token; unavailable in the browser build. */
export async function autoSetup(): Promise<LoginResponse> {
  try {
    const nativeApp = (window as unknown as { go?: { main?: { App?: { GetAutoSetupToken?: () => Promise<string> } } } })
      .go?.main?.App;
    if (!nativeApp?.GetAutoSetupToken) {
      return { success: false, message: "Desktop authentication required" };
    }
    const token = await nativeApp.GetAutoSetupToken();
    return await apiPost<LoginResponse>(
      "/api/v1/auth/auto-setup",
      {},
      { headers: { "X-WeKnora-Desktop-Token": token } },
    );
  } catch (error) {
    return {
      success: false,
      message: error instanceof Error ? error.message : "Auto-setup unavailable",
    };
  }
}

export async function getCurrentUser(): Promise<CurrentUserResponse> {
  try {
    return await apiGet<CurrentUserResponse>("/api/v1/auth/me");
  } catch (error) {
    return {
      success: false,
      message: error instanceof Error ? error.message : "Failed to load user",
    };
  }
}

/* PATCH semantics over PUT: only sent keys are overwritten; the backend
 * returns the complete updated preferences object. */
export async function updateMyPreferences(
  patch: Partial<UserPreferences>,
): Promise<{ success: boolean; data?: UserPreferences; message?: string }> {
  try {
    return await apiPut("/api/v1/auth/me/preferences", patch);
  } catch (error) {
    return {
      success: false,
      message: error instanceof Error ? error.message : "Failed to update preferences",
    };
  }
}

export async function refreshToken(
  refreshTokenValue: string,
): Promise<{ success: boolean; data?: { token: string; refreshToken: string }; message?: string }> {
  try {
    const response = await apiPost<{
      success?: boolean;
      access_token?: string;
      refresh_token?: string;
      message?: string;
    }>("/api/v1/auth/refresh", { refreshToken: refreshTokenValue });
    if (response && response.success && (response.access_token || response.refresh_token)) {
      return {
        success: true,
        data: { token: response.access_token!, refreshToken: response.refresh_token! },
      };
    }
    return { success: false, message: response?.message || "Failed to refresh token" };
  } catch (error) {
    return {
      success: false,
      message: error instanceof Error ? error.message : "Failed to refresh token",
    };
  }
}

export async function logout(): Promise<{ success: boolean; message?: string }> {
  try {
    await apiPost("/api/v1/auth/logout", {});
    return { success: true };
  } catch (error) {
    return { success: false, message: error instanceof Error ? error.message : "Logout failed" };
  }
}

/* Map change-password API failures to stable keys — callers translate. */
export function resolveChangePasswordError(error: unknown): string {
  const e = error as { payload?: { error?: { details?: unknown } }; message?: string } | undefined;
  const details =
    e?.payload && typeof e.payload === "object"
      ? String((e.payload as { error?: { details?: unknown } }).error?.details ?? "")
      : "";
  switch (details) {
    case "invalid_old_password":
      return "Incorrect current password";
    case "password_policy":
      return "Password does not meet policy requirements";
    case "same_password":
      return "New password must differ from the current one";
    default:
      return e?.message || "Failed to change password";
  }
}

/* Self-service password rotation. On success the backend revokes every
 * outstanding session — the caller must clear local auth state and return
 * to /login. */
export async function changePassword(
  data: ChangePasswordRequest,
): Promise<{ success: boolean; message?: string }> {
  try {
    return await apiPost("/api/v1/auth/change-password", data);
  } catch (error) {
    return { success: false, message: resolveChangePasswordError(error) };
  }
}

export async function validateToken(): Promise<{
  success: boolean;
  valid?: boolean;
  message?: string;
}> {
  try {
    return await apiGet("/api/v1/auth/validate");
  } catch (error) {
    return {
      success: false,
      valid: false,
      message: error instanceof Error ? error.message : "Token validation failed",
    };
  }
}

// ---- share-link registration ----------------------------------------------

/* Resolve a share-link token (no auth) into the context the registration
 * page needs. POST + body so the plaintext token never appears in access
 * logs, browser history, or tracing spans. Returns 410 when invalid /
 * revoked / expired. */
export async function getInvitationByToken(token: string): Promise<InviteLookupResponse> {
  try {
    return await apiPost<InviteLookupResponse>("/api/v1/auth/invitations/lookup", { token });
  } catch (error) {
    return { success: false, message: error instanceof Error ? error.message : "" };
  }
}

/* Complete registration via share-link token. The invitee supplies their
 * own email — the token is the authorisation, not an identity lock. */
export async function registerByInvite(data: RegisterByInviteRequest): Promise<LoginResponse> {
  try {
    return await apiPost<LoginResponse>("/api/v1/auth/register-by-invite", data);
  } catch (error) {
    return {
      success: false,
      message: error instanceof Error ? error.message : "Registration failed",
    };
  }
}
