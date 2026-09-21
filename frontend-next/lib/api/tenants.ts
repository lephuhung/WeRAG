/* Ported from frontend/src/api/tenant/{index,members,invitations,orgs}.ts.
 * i18n t() fallbacks replaced with plain strings — map ApiError.message
 * through useT() at call sites where localization is wanted.
 */
import { apiDel, apiGet, apiPost, apiPut, ApiError } from "@/lib/api-client";

// ---- workspace (tenant) -------------------------------------------------------

export interface TenantInfo {
  id: number;
  name: string;
  description?: string;
  status?: string;
  business?: string;
  storage_quota?: number;
  storage_used?: number;
  created_at: string;
  updated_at: string;
}

export type APIPrincipalMode = "tenant" | "direct_header" | "signed_token";

export interface APIPrincipalConfig {
  mode: APIPrincipalMode;
  direct_header_name: string;
  signed_token_header_name: string;
  require_direct_header: boolean;
  // The server never returns the plaintext secret; only its presence.
  has_hmac_secret: boolean;
}

export interface UpdateAPIPrincipalConfigPayload {
  mode: APIPrincipalMode;
  direct_header_name?: string;
  signed_token_header_name?: string;
  require_direct_header?: boolean;
  hmac_secret?: string;
}

export interface CreateAPIPrincipalTestTokenPayload {
  external_user_id: string;
  expires_in_seconds?: number;
}

export interface APIPrincipalTestToken {
  token: string;
  header_name: string;
  expires_in_seconds: number;
  expires_at_unix: number;
  external_user_id: string;
}

/* Bounded per-key grants for non-full-access API keys.
 *  - 'retrieve': read/search knowledge-base data within scope
 *  - 'chat': run the conversation flow (sessions + agent listing + self identity)
 *  - 'read_agents': list/read agents without chat or authoring
 *  - 'ingest': write content into allowed knowledge bases (docs/chunks/FAQ/tags/wiki)
 *  - 'manage_kbs': manage the KB lifecycle (create/copy/duplicate/update/delete + config)
 *  - 'manage_agents': create/update/delete/copy agents
 *  - 'message_history': search/read tenant chat-history metadata
 *  - 'manage_models': read the tenant-visible model catalog (writes are platform-owned)
 *  - 'system_models_manage': platform keys only — manage model definitions, credentials, probes
 *  - 'manage_mcp_services': manage MCP services, credentials, tool policies, OAuth state
 *  - 'manage_datasources': manage data-source connectors and sync jobs
 *  - 'manage_channels': manage embed and IM channels
 *  - 'manage_vector_stores': manage vector stores and parser/storage checks
 *  - 'manage_web_search': manage web-search providers
 *  - 'run_evaluations': run/read evaluation jobs
 *  - 'manage_members': manage tenant members and invitations
 *  - 'manage_spaces': manage organization/space collaboration
 *  - 'manage_tenant_settings': read/update tenant integration settings */
export type TenantAPIKeyCapability =
  | "retrieve"
  | "chat"
  | "read_agents"
  | "ingest"
  | "manage_kbs"
  | "manage_agents"
  | "message_history"
  | "manage_models"
  | "manage_mcp_services"
  | "manage_datasources"
  | "manage_channels"
  | "manage_vector_stores"
  | "manage_storage_backends"
  | "manage_web_search"
  | "run_evaluations"
  | "manage_members"
  | "manage_spaces"
  | "manage_tenant_settings"
  | "system_tenants_read"
  | "system_tenants_manage"
  | "system_settings_read"
  | "system_settings_manage"
  | "system_runtime_read"
  | "system_runtime_manage"
  | "system_audit_read"
  | "system_models_manage";

export interface TenantAPIKey {
  id: number;
  scope_type?: "tenant" | "platform";
  name: string;
  api_key: string;
  full_access: boolean;
  knowledge_base_ids: string[] | null;
  capabilities?: TenantAPIKeyCapability[];
  last_used_at?: string;
  expires_at?: string;
  created_at: string;
}

export interface CreatedTenantAPIKey extends TenantAPIKey {
  token?: string;
}

export interface CreateTenantAPIKeyPayload {
  name: string;
  full_access?: boolean;
  knowledge_base_ids?: string[];
  capabilities?: TenantAPIKeyCapability[];
  expires_at_unix?: number;
}

export interface UpdateTenantAPIKeyPayload {
  name: string;
  full_access: boolean;
  knowledge_base_ids: string[];
  capabilities: TenantAPIKeyCapability[];
  expires_at_unix?: number;
}

export interface SearchTenantsParams {
  keyword?: string;
  tenant_id?: number;
  page?: number;
  page_size?: number;
}

export interface SearchTenantsResponse {
  success: boolean;
  data?: { items: TenantInfo[]; total: number; page: number; page_size: number };
  message?: string;
}

function errMsg(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

/** @deprecated Prefer searchTenants — supports pagination + search. */
export async function listAllTenants(): Promise<{
  success: boolean;
  data?: { items: TenantInfo[] };
  message?: string;
}> {
  try {
    return await apiGet("/api/v1/tenants/all");
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to list workspaces") };
  }
}

export async function getAPIPrincipalConfig(
  tenantId: number,
): Promise<{ success: boolean; data?: APIPrincipalConfig; message?: string }> {
  try {
    return await apiGet(`/api/v1/tenants/${tenantId}/api-principal-config`);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to load API principal config") };
  }
}

export async function updateAPIPrincipalConfig(
  tenantId: number,
  payload: UpdateAPIPrincipalConfigPayload,
): Promise<{ success: boolean; data?: APIPrincipalConfig; message?: string }> {
  try {
    return await apiPut(`/api/v1/tenants/${tenantId}/api-principal-config`, payload);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to update API principal config") };
  }
}

export async function createAPIPrincipalTestToken(
  tenantId: number,
  payload: CreateAPIPrincipalTestTokenPayload,
): Promise<{ success: boolean; data?: APIPrincipalTestToken; message?: string }> {
  try {
    return await apiPost(`/api/v1/tenants/${tenantId}/api-principal-test-token`, payload);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to create test token") };
  }
}

export async function listTenantAPIKeys(
  tenantId: number,
): Promise<{ success: boolean; data?: TenantAPIKey[]; message?: string }> {
  try {
    return await apiGet(`/api/v1/tenants/${tenantId}/api-keys`);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to list API keys") };
  }
}

export async function createTenantAPIKey(
  tenantId: number,
  payload: CreateTenantAPIKeyPayload,
): Promise<{ success: boolean; data?: CreatedTenantAPIKey; message?: string }> {
  try {
    return await apiPost(`/api/v1/tenants/${tenantId}/api-keys`, payload);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to create API key") };
  }
}

/** Update an existing tenant API key's grant scope and other attributes. */
export async function updateTenantAPIKey(
  tenantId: number,
  keyId: number,
  payload: UpdateTenantAPIKeyPayload,
): Promise<{ success: boolean; data?: TenantAPIKey; message?: string }> {
  try {
    return await apiPut(`/api/v1/tenants/${tenantId}/api-keys/${keyId}`, payload);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to update API key scope") };
  }
}

export async function deleteTenantAPIKey(
  tenantId: number,
  keyId: number,
): Promise<{ success: boolean; message?: string }> {
  try {
    return await apiDel(`/api/v1/tenants/${tenantId}/api-keys/${keyId}`);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to delete API key") };
  }
}

/* PUT /tenants/:id uses pointer fields to distinguish "absent" from
 * "explicit empty" — send only the fields to change. Owner only. */
export async function updateTenant(
  tenantId: number,
  payload: { name?: string; description?: string },
): Promise<{ success: boolean; data?: TenantInfo; message?: string }> {
  try {
    return await apiPut(`/api/v1/tenants/${tenantId}`, payload);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to update workspace") };
  }
}

/** Delete the workspace. Owner only. */
export async function deleteTenant(
  tenantId: number,
): Promise<{ success: boolean; message?: string }> {
  try {
    return await apiDel(`/api/v1/tenants/${tenantId}`);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to delete workspace") };
  }
}

/* Create a new workspace (any logged-in user). The backend writes the
 * caller as Owner and fills storage_quota; API keys are created manually
 * on the integrations page. POST /api/v1/tenants has no g.CrossTenant()
 * guard — self-service flow. */
export async function createTenant(payload: {
  name: string;
  description?: string;
}): Promise<{ success: boolean; data?: TenantInfo; message?: string }> {
  try {
    return await apiPost("/api/v1/tenants", payload);
  } catch (error) {
    const payloadErr =
      error instanceof ApiError && error.payload && typeof error.payload === "object"
        ? (error.payload as { error?: { code?: number } }).error?.code
        : undefined;
    return {
      success: false,
      message:
        payloadErr === 2005
          ? "Workspace creation is disabled on this deployment"
          : errMsg(error, "Failed to create workspace"),
    };
  }
}

export async function searchTenants(params: SearchTenantsParams = {}): Promise<SearchTenantsResponse> {
  try {
    const queryParams = new URLSearchParams();
    if (params.keyword) queryParams.append("keyword", params.keyword);
    if (params.tenant_id) queryParams.append("tenant_id", String(params.tenant_id));
    if (params.page) queryParams.append("page", String(params.page));
    if (params.page_size) queryParams.append("page_size", String(params.page_size));
    const queryString = queryParams.toString();
    return await apiGet(`/api/v1/tenants/search${queryString ? "?" + queryString : ""}`);
  } catch (error) {
    return { success: false, message: errMsg(error, "Failed to search workspaces") };
  }
}

// ---- members --------------------------------------------------------------------

/* TenantRole mirrors internal/types/tenant_member.go's three-role enum. */
export type TenantRole = "owner" | "admin" | "member";

export type TenantMemberStatus = "active" | "invited" | "suspended";

/* API projection of a (user, tenant) membership row, already joined with
 * the user's email/username/avatar by the backend. */
export interface TenantMember {
  user_id: string;
  email: string;
  username: string;
  avatar?: string;
  role: TenantRole;
  status: TenantMemberStatus;
  invited_by?: string | null;
  joined_at: string;
}

export interface ListMembersResponse {
  success: boolean;
  data?: { members: TenantMember[]; total: number; page?: number; page_size?: number };
  message?: string;
}

export interface ListMembersParams {
  page?: number;
  page_size?: number;
  /** Fuzzy email/username filter (server-side). */
  q?: string;
}

function buildMembersQuery(params: ListMembersParams | undefined): string {
  if (!params) return "";
  const u = new URLSearchParams();
  if (params.page != null && params.page > 0) u.set("page", String(params.page));
  if (params.page_size != null && params.page_size > 0) u.set("page_size", String(params.page_size));
  const q = params.q?.trim();
  if (q) u.set("q", q);
  const qs = u.toString();
  return qs ? `?${qs}` : "";
}

export interface AddMemberRequest {
  email: string;
  role: TenantRole;
}

export interface AddMemberResponse {
  success: boolean;
  data?: TenantMember;
  message?: string;
}

export interface SimpleResponse {
  success: boolean;
  message?: string;
}

/** GET /api/v1/tenants/:id/members (Viewer+). Query: q, page, page_size. */
export function listMembers(
  tenantId: number,
  params: ListMembersParams = {},
): Promise<ListMembersResponse> {
  return apiGet(`/api/v1/tenants/${tenantId}/members${buildMembersQuery(params)}`);
}

/* Page through the full member list (page size 100, 500-page guard). For
 * lightweight all-member checks like "leave workspace"; tables should use
 * listMembers pagination instead. */
export async function fetchAllTenantMembers(tenantId: number): Promise<TenantMember[]> {
  const pageSize = 100;
  let page = 1;
  const out: TenantMember[] = [];
  let total = Number.POSITIVE_INFINITY;
  for (let guard = 0; guard < 500 && out.length < total; guard++) {
    const resp = await listMembers(tenantId, { page, page_size: pageSize });
    if (!resp.success || !resp.data) break;
    total = resp.data.total;
    const batch = resp.data.members || [];
    if (batch.length === 0 && page >= 2) break;
    out.push(...batch);
    if (batch.length < pageSize) break;
    page++;
  }
  return out;
}

/* Invite an existing user (by email) with a role. POST (Owner+).
 * 404 when the email matches no registered user — ask them to register
 * first; email-based invites for account-less users aren't supported. */
export function addMember(tenantId: number, body: AddMemberRequest): Promise<AddMemberResponse> {
  return apiPost(`/api/v1/tenants/${tenantId}/members`, body);
}

/* PUT (Owner+). 409 when this would demote the last active Owner. */
export function updateMemberRole(
  tenantId: number,
  userId: string,
  role: TenantRole,
): Promise<SimpleResponse> {
  return apiPut(`/api/v1/tenants/${tenantId}/members/${userId}`, { role });
}

/* DELETE (Owner+). 409 when this would remove the last active Owner. */
export function removeMember(tenantId: number, userId: string): Promise<SimpleResponse> {
  return apiDel(`/api/v1/tenants/${tenantId}/members/${userId}`);
}

/* Quit the tenant yourself. Same last-Owner invariant as removeMember but
 * any active member can call it. POST (Viewer+). */
export function leaveTenant(tenantId: number): Promise<SimpleResponse> {
  return apiPost(`/api/v1/tenants/${tenantId}/leave`, {});
}

// ---- invitations ----------------------------------------------------------------

/* Mirrors internal/types/tenant_invitation.go's five-state machine;
 * pending is the only non-terminal state. */
export type TenantInvitationStatus = "pending" | "accepted" | "declined" | "revoked" | "expired";

export interface TenantInvitation {
  id: number;
  tenant_id: number;
  tenant_name?: string;
  invitee_user_id: string;
  invitee_email?: string;
  invitee_name?: string;
  invited_by?: string | null;
  inviter_email?: string;
  inviter_name?: string;
  role: TenantRole;
  status: TenantInvitationStatus;
  message?: string;
  expires_at: string;
  responded_at?: string | null;
  created_at: string;
  // invite_url is set on still-pending share-link rows — re-emitted on
  // every list/get so Owners can copy the link on demand.
  invite_url?: string;
  // is_share_link distinguishes multi-use share-link rows (no specific
  // invitee, copyable URL) from per-user invitations.
  is_share_link?: boolean;
  // accepted_count = registrations completed through this link.
  accepted_count?: number;
}

export interface ListInvitationsResponse {
  success: boolean;
  data?: { invitations: TenantInvitation[]; total: number; page?: number; page_size?: number };
  message?: string;
}

export interface ListTenantInvitationsParams {
  includeTerminal?: boolean;
  page?: number;
  page_size?: number;
}

function buildTenantInvitationsQuery(options: ListTenantInvitationsParams): string {
  const u = new URLSearchParams();
  if (options.includeTerminal) u.set("include_terminal", "true");
  if (options.page != null && options.page > 0) u.set("page", String(options.page));
  if (options.page_size != null && options.page_size > 0)
    u.set("page_size", String(options.page_size));
  const qs = u.toString();
  return qs ? `?${qs}` : "";
}

export interface CreateInvitationRequest {
  email: string;
  role: TenantRole;
  message?: string;
}

export interface CreateInvitationResponse {
  success: boolean;
  // With tenant.auto_accept_invitation the backend returns a TenantMember
  // (user_id set) instead of a pending TenantInvitation.
  data?: TenantInvitation | TenantMember;
  message?: string;
}

export interface AcceptInvitationResponse {
  success: boolean;
  data?: {
    membership: { tenant_id: number; role: TenantRole; status: string; joined_at: string };
  };
  message?: string;
}

export interface AcceptInvitationByTokenResponse {
  success: boolean;
  data?: {
    membership: { tenant_id: number; role: TenantRole; status: string; joined_at: string };
    tenant_name?: string;
  };
  message?: string;
}

export interface PendingCountResponse {
  success: boolean;
  data?: { pending_count: number };
  message?: string;
}

/** GET /api/v1/tenants/:id/invitations (Viewer+). Pending only unless
 * includeTerminal=true (adds accepted/declined/revoked/expired). */
export function listTenantInvitations(
  tenantId: number,
  options: ListTenantInvitationsParams = {},
): Promise<ListInvitationsResponse> {
  return apiGet(`/api/v1/tenants/${tenantId}/invitations${buildTenantInvitationsQuery(options)}`);
}

/* POST (Owner+). 404 when the email isn't registered; 409 when a pending
 * invitation already covers the pair or the invitee is already a member. */
export function createInvitation(
  tenantId: number,
  body: CreateInvitationRequest,
): Promise<CreateInvitationResponse> {
  return apiPost(`/api/v1/tenants/${tenantId}/invitations`, body);
}

/* Revoke a still-pending invitation (Owner+). Finalised rows → 409; rows
 * from another tenant → 404 to avoid existence leaks. */
export function revokeInvitation(tenantId: number, invId: number): Promise<SimpleResponse> {
  return apiDel(`/api/v1/tenants/${tenantId}/invitations/${invId}`);
}

/** List MY invitations (authenticated). Pending only unless
 * includeTerminal=true. */
export function listMyInvitations(
  options: { includeTerminal?: boolean } = {},
): Promise<ListInvitationsResponse> {
  const qs = options.includeTerminal ? "?include_terminal=true" : "";
  return apiGet(`/api/v1/me/invitations${qs}`);
}

/** Lightweight pending-count for badge pollers — kept separate from the
 * list endpoint so polling doesn't transfer the full payload. */
export function getMyPendingInvitationCount(): Promise<PendingCountResponse> {
  return apiGet(`/api/v1/me/invitations/pending-count`);
}

/* Accept one of MY pending invitations; creates the tenant_members row in
 * the same flow — refresh memberships afterwards. */
export function acceptInvitation(invId: number): Promise<AcceptInvitationResponse> {
  return apiPost(`/api/v1/me/invitations/${invId}/accept`, {});
}

/* Logged-in user joining via a share-link token (no new account). Powers
 * invite_only-mode invite links: link → login → token redemption. */
export function acceptInvitationByToken(token: string): Promise<AcceptInvitationByTokenResponse> {
  return apiPost(`/api/v1/me/invitations/accept-by-token`, { token });
}

export function declineInvitation(invId: number): Promise<SimpleResponse> {
  return apiPost(`/api/v1/me/invitations/${invId}/decline`, {});
}

// ---- share links ------------------------------------------------------------------

export interface CreateInviteLinkRequest {
  role: TenantRole;
  message?: string;
}

export interface CreateInviteLinkResponse {
  success: boolean;
  data?: TenantInvitation;
  message?: string;
}

/* Mint a multi-use share-link invitation. The returned row carries
 * invite_url for copying. Valid until expiry or revocation — revoking is
 * the same DELETE as a per-user invitation. POST (Owner+). */
export function createInviteLink(
  tenantId: number,
  body: CreateInviteLinkRequest,
): Promise<CreateInviteLinkResponse> {
  return apiPost(`/api/v1/tenants/${tenantId}/invite-links`, body);
}

// ---- tenant orgs (user groups inside one tenant — NOT cross-tenant
// Organizations used for KB sharing) ---------------------------------------------
// Org-scoped KBs are readable only by members of the bound org plus tenant
// Admin/Owner and system admins.

export interface TenantOrg {
  id: number;
  tenant_id: number;
  name: string;
  description?: string;
  created_by: string;
  member_count?: number;
  created_at: string;
}

export type TenantOrgRole = "manager" | "member";

export interface TenantOrgMember {
  org_id: number;
  user_id: string;
  username?: string;
  email?: string;
  role: TenantOrgRole;
  created_at: string;
}

export interface TenantOrgInvitation {
  id: number;
  tenant_id: number;
  org_id: number;
  role: TenantRole;
  expires_at: string;
}

interface ApiResponse<T> {
  success: boolean;
  data?: T;
  message?: string;
}

export function listOrgs() {
  return apiGet<ApiResponse<TenantOrg[]>>(`/api/v1/orgs`);
}

export function createOrg(data: { name: string; description?: string }) {
  return apiPost<ApiResponse<TenantOrg>>(`/api/v1/orgs`, data);
}

export function updateOrg(id: number, data: { name?: string; description?: string }) {
  return apiPut<ApiResponse<TenantOrg>>(`/api/v1/orgs/${id}`, data);
}

export function deleteOrg(id: number) {
  return apiDel<ApiResponse<null>>(`/api/v1/orgs/${id}`);
}

export function listOrgMembers(orgId: number) {
  return apiGet<ApiResponse<TenantOrgMember[]>>(`/api/v1/orgs/${orgId}/members`);
}

export function addOrgMember(orgId: number, data: { user_id: string; role: TenantOrgRole }) {
  return apiPost<ApiResponse<null>>(`/api/v1/orgs/${orgId}/members`, data);
}

export function updateOrgMemberRole(orgId: number, userId: string, data: { role: TenantOrgRole }) {
  return apiPut<ApiResponse<null>>(`/api/v1/orgs/${orgId}/members/${userId}`, data);
}

export function removeOrgMember(orgId: number, userId: string) {
  return apiDel<ApiResponse<null>>(`/api/v1/orgs/${orgId}/members/${userId}`);
}

/* Mint a multi-use registration link bound to the org: accepting it
 * registers the account, joins the tenant with the given role, and enrols
 * the user into the org. */
export function createOrgInviteLink(orgId: number, data: { role: TenantRole; message?: string }) {
  return apiPost<ApiResponse<{ invitation: TenantOrgInvitation; url: string }>>(
    `/api/v1/orgs/${orgId}/invite-links`,
    data,
  );
}
