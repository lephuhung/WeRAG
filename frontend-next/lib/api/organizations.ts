/* Ported from frontend/src/api/organization/index.ts — cross-tenant
 * organizations used for KB/agent sharing (distinct from tenant orgs in
 * ./tenants.ts). Every call preserves the old swallow-error →
 * {success:false, message} contract. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

export interface Organization {
  id: string;
  name: string;
  description: string;
  avatar?: string;
  owner_id: string;
  /* Persisted owner tenant (Plan 3, migration 000046). Member ownership is
   * tenant-keyed: the "owner row" in the members list is found by matching
   * member.tenant_id against owner_tenant_id (NOT user_id vs owner_id).
   * May be 0 on legacy rows — fall back to owner_id for display only. */
  owner_tenant_id: number;
  invite_code?: string;
  invite_code_expires_at?: string | null;
  invite_code_validity_days?: number;
  require_approval?: boolean;
  searchable?: boolean;
  /** Max members; 0 = unlimited */
  member_limit?: number;
  member_count?: number;
  share_count?: number;
  agent_share_count?: number;
  pending_join_request_count?: number;
  is_owner?: boolean;
  my_role?: string;
  has_pending_upgrade?: boolean;
  created_at: string;
  updated_at: string;
}

/* One row in the org's per-tenant member list (Plan 3 / migration 000045).
 * Each row is a (org, tenant) tuple; user fields describe the
 * *representative* user attached for display/audit, not the member identity.
 *
 * - `tenant_id` + `tenant_name` are the canonical member identity.
 * - `representative_user_id` is the explicit post-Plan-3 alias; `user_id`
 *   is kept for backward compat and points at the same value.
 * - Two users of the same tenant produce a single row (UNIQUE(org_id,
 *   tenant_id)); the rep is whoever first brought the tenant in. */
export interface OrganizationMember {
  id: string;
  user_id: string;
  representative_user_id?: string;
  username: string;
  email: string;
  avatar?: string;
  role: "admin" | "editor" | "viewer";
  tenant_id: number;
  tenant_name?: string;
  joined_at: string;
}

export interface KnowledgeBaseShare {
  id: string;
  knowledge_base_id: string;
  knowledge_base_name?: string;
  knowledge_base_type?: string;
  knowledge_count?: number;
  chunk_count?: number;
  organization_id: string;
  organization_name?: string;
  shared_by_user_id: string;
  shared_by_username?: string;
  source_tenant_id: number;
  /** What the space was granted (viewer/editor) */
  permission: "admin" | "editor" | "viewer";
  /** Current user's role in this organization */
  my_role_in_org?: "admin" | "editor" | "viewer";
  /** Effective permission = min(permission, my_role_in_org) */
  my_permission?: "admin" | "editor" | "viewer";
  created_at: string;
}

export interface SharedKnowledgeBase {
  knowledge_base: {
    id: string;
    name: string;
    description: string;
    type: string;
    knowledge_count?: number;
    chunk_count?: number;
  };
  share_id: string;
  organization_id: string;
  org_name: string;
  permission: "admin" | "editor" | "viewer";
  source_tenant_id: number;
  shared_at: string;
}

/** When set, this KB is visible via a shared agent (read-only, no direct KB share). */
export interface SourceFromAgentInfo {
  agent_id: string;
  agent_name: string;
  /** "all" | "selected" | "none" — the agent's KB strategy, for the drawer */
  kb_selection_mode?: string;
}

/** Item from GET /organizations/:id/shared-knowledge-bases (space-scoped,
 * including mine and agent-carried). */
export type OrganizationSharedKnowledgeBaseItem = SharedKnowledgeBase & {
  is_mine: boolean;
  source_from_agent?: SourceFromAgentInfo;
};

export interface OrganizationPreview {
  id: string;
  name: string;
  description: string;
  avatar?: string;
  member_count: number;
  share_count: number;
  agent_share_count?: number;
  is_already_member: boolean;
  require_approval: boolean;
  created_at: string;
}

/** Searchable (discoverable) organization item for the join flow. */
export interface SearchableOrganizationItem {
  id: string;
  name: string;
  description: string;
  avatar?: string;
  member_count: number;
  member_limit: number; // 0 = unlimited
  share_count: number;
  agent_share_count?: number;
  is_already_member: boolean;
  require_approval: boolean;
}

export interface CreateOrganizationRequest {
  name: string;
  description?: string;
  avatar?: string;
  invite_code_validity_days?: number; // 0=never, 1, 7, 30; default 7
  member_limit?: number; // 0=unlimited; default 50
}

export interface UpdateOrganizationRequest {
  name?: string;
  description?: string;
  avatar?: string;
  require_approval?: boolean;
  searchable?: boolean;
  invite_code_validity_days?: number; // 0=never, 1, 7, 30
  member_limit?: number; // 0=unlimited
}

export interface UpdateMemberRoleRequest {
  role: "admin" | "editor" | "viewer";
}

export interface JoinOrganizationRequest {
  invite_code: string;
}

export interface ShareKnowledgeBaseRequest {
  organization_id: string;
  permission: "admin" | "editor" | "viewer";
}

export interface UpdateSharePermissionRequest {
  permission: "admin" | "editor" | "viewer";
}

export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  message?: string;
}

/** Per-org resource counts (avoids an extra GET /me/resource-counts). */
export interface ResourceCountsByOrg {
  knowledge_bases: { by_organization: Record<string, number> };
  agents: { by_organization: Record<string, number> };
}

export interface ListOrganizationsResponse {
  organizations: Organization[];
  total: number;
  resource_counts?: ResourceCountsByOrg;
}

export interface ListOrgMembersResponse {
  members: OrganizationMember[];
  total: number;
}

export interface JoinRequestResponse {
  id: string;
  user_id: string;
  username: string;
  email: string;
  message: string;
  request_type: "join" | "upgrade";
  prev_role?: string;
  requested_role: string;
  status: string;
  created_at: string;
  reviewed_at?: string;
}

export interface ListJoinRequestsResponse {
  requests: JoinRequestResponse[];
  total: number;
}

export interface SubmitJoinRequestRequest {
  invite_code: string;
  message?: string;
  role?: "admin" | "editor" | "viewer"; // applicant's requested role; default viewer
}

export interface ReviewJoinRequestRequest {
  approved: boolean;
  message?: string;
  role?: "admin" | "editor" | "viewer"; // overrides applicant's requested role on approve
}

export interface RequestRoleUpgradeRequest {
  requested_role: "admin" | "editor" | "viewer";
  message?: string;
}

/* Enrols a *tenant* into the organization (Plan 3):
 * - `tenant_id` is the preferred invitee identity.
 * - `representative_user_id` is optional display/audit metadata; the server
 *   picks a default when omitted.
 * - `user_id` kept for pre-Plan-3 callers (server resolves their tenant). */
export interface InviteMemberRequest {
  tenant_id?: number;
  representative_user_id?: string;
  user_id?: string;
  role: "admin" | "editor" | "viewer";
}

export interface UserSearchResult {
  id: string;
  username: string;
  email: string;
  avatar?: string;
}

/* One row in the search-tenants-for-invite picker. Tenant-centric; the
 * representative_* fields describe the user that surfaced the tenant. */
export interface TenantInviteCandidate {
  tenant_id: number;
  tenant_name: string;
  representative_user_id: string;
  representative_username: string;
  representative_email: string;
  representative_avatar?: string;
}

export interface ListSharesResponse {
  shares: KnowledgeBaseShare[];
  total: number;
}

export interface AgentShareResponse {
  id: string;
  agent_id: string;
  agent_name?: string;
  organization_id: string;
  organization_name?: string;
  shared_by_user_id: string;
  shared_by_username?: string;
  source_tenant_id: number;
  permission: string;
  my_role_in_org?: string;
  my_permission?: string;
  created_at: string;
  scope_kb?: string;
  scope_kb_count?: number;
  scope_web_search?: boolean;
  scope_mcp?: string;
  scope_mcp_count?: number;
  agent_avatar?: string;
}

export interface SharedAgentInfo {
  agent: { id: string; name: string; description?: string; [key: string]: unknown };
  share_id: string;
  organization_id: string;
  org_name: string;
  permission: string;
  source_tenant_id: number;
  shared_at: string;
  shared_by_user_id?: string;
  shared_by_username?: string;
  /** Resolved in the source tenant by the backend — do NOT compare against
   * the current space's search-engine list. */
  web_search_ready: boolean;
  /** Whether the current user disabled this shared agent (their own
   * chat-dropdown visibility only). */
  disabled_by_me?: boolean;
}

export type OrganizationSharedAgentItem = SharedAgentInfo & { is_mine: boolean };

export interface ListAgentSharesResponse {
  shares: AgentShareResponse[];
  total: number;
}

function fail(error: unknown, fallback: string): { success: false; message: string } {
  return { success: false, message: error instanceof Error ? error.message : fallback };
}

export async function createOrganization(
  req: CreateOrganizationRequest,
): Promise<ApiResponse<Organization>> {
  try {
    return await apiPost("/api/v1/organizations", req);
  } catch (error) {
    return fail(error, "Failed to create organization");
  }
}

export async function getOrganization(id: string): Promise<ApiResponse<Organization>> {
  try {
    return await apiGet(`/api/v1/organizations/${id}`);
  } catch (error) {
    return fail(error, "Failed to get organization");
  }
}

export async function listMyOrganizations(): Promise<ApiResponse<ListOrganizationsResponse>> {
  try {
    return await apiGet("/api/v1/organizations");
  } catch (error) {
    return fail(error, "Failed to list organizations");
  }
}

export async function updateOrganization(
  id: string,
  req: UpdateOrganizationRequest,
): Promise<ApiResponse<Organization>> {
  try {
    return await apiPut(`/api/v1/organizations/${id}`, req);
  } catch (error) {
    return fail(error, "Failed to update organization");
  }
}

export async function deleteOrganization(id: string): Promise<ApiResponse<void>> {
  try {
    return await apiDel(`/api/v1/organizations/${id}`);
  } catch (error) {
    return fail(error, "Failed to delete organization");
  }
}

export async function joinOrganization(
  req: JoinOrganizationRequest,
): Promise<ApiResponse<Organization>> {
  try {
    return await apiPost("/api/v1/organizations/join", req);
  } catch (error) {
    return fail(error, "Failed to join organization");
  }
}

/** For organizations that require approval; optional requested role. */
export async function submitJoinRequest(req: SubmitJoinRequestRequest): Promise<ApiResponse<void>> {
  try {
    return await apiPost("/api/v1/organizations/join-request", req);
  } catch (error) {
    return fail(error, "Failed to submit join request");
  }
}

/** Preview organization by invite code (without joining). */
export async function previewOrganization(
  inviteCode: string,
): Promise<ApiResponse<OrganizationPreview>> {
  try {
    return await apiGet(`/api/v1/organizations/preview/${inviteCode}`);
  } catch (error) {
    return fail(error, "Failed to preview organization");
  }
}

export async function searchSearchableOrganizations(
  q = "",
  limit = 20,
): Promise<ApiResponse<{ data: SearchableOrganizationItem[]; total: number }>> {
  try {
    const params = new URLSearchParams();
    if (q) params.set("q", q);
    params.set("limit", String(limit));
    const res = await apiGet<{
      success: boolean;
      data?: SearchableOrganizationItem[];
      total?: number;
      message?: string;
    }>(`/api/v1/organizations/search?${params.toString()}`);
    return {
      success: res.success,
      data: res.success ? { data: res.data || [], total: res.total ?? 0 } : undefined,
      message: res.message,
    };
  } catch (error) {
    return fail(error, "Failed to search organizations");
  }
}

/** Join a searchable organization by ID (no invite code). */
export async function joinOrganizationById(
  organizationId: string,
  message?: string,
  role?: "admin" | "editor" | "viewer",
): Promise<ApiResponse<Organization>> {
  try {
    const body: { organization_id: string; message?: string; role?: string } = {
      organization_id: organizationId,
    };
    if (message) body.message = message;
    if (role) body.role = role;
    return await apiPost("/api/v1/organizations/join-by-id", body);
  } catch (error) {
    return fail(error, "Failed to join organization");
  }
}

export async function leaveOrganization(id: string): Promise<ApiResponse<void>> {
  try {
    return await apiPost(`/api/v1/organizations/${id}/leave`, {});
  } catch (error) {
    return fail(error, "Failed to leave organization");
  }
}

export async function requestRoleUpgrade(
  orgId: string,
  request: RequestRoleUpgradeRequest,
): Promise<ApiResponse<JoinRequestResponse>> {
  try {
    return await apiPost(`/api/v1/organizations/${orgId}/request-upgrade`, request);
  } catch (error) {
    return fail(error, "Failed to submit upgrade request");
  }
}

export async function generateInviteCode(
  id: string,
): Promise<ApiResponse<{ invite_code: string }>> {
  try {
    return await apiPost(`/api/v1/organizations/${id}/invite-code`, {});
  } catch (error) {
    return fail(error, "Failed to generate invite code");
  }
}

// ---- members --------------------------------------------------------------------

export async function listOrganizationMembers(
  orgId: string,
): Promise<ApiResponse<ListOrgMembersResponse>> {
  try {
    return await apiGet(`/api/v1/organizations/${orgId}/members`);
  } catch (error) {
    return fail(error, "Failed to list members");
  }
}

/** Member is identified by tenant_id. */
export async function updateOrgMemberRole(
  orgId: string,
  tenantId: number,
  req: UpdateMemberRoleRequest,
): Promise<ApiResponse<void>> {
  try {
    return await apiPut(`/api/v1/organizations/${orgId}/members/${tenantId}`, req);
  } catch (error) {
    return fail(error, "Failed to update member role");
  }
}

/** Member is identified by tenant_id. */
export async function removeOrgMember(orgId: string, tenantId: number): Promise<ApiResponse<void>> {
  try {
    return await apiDel(`/api/v1/organizations/${orgId}/members/${tenantId}`);
  } catch (error) {
    return fail(error, "Failed to remove member");
  }
}

/** Pending join requests (admin only). */
export async function listJoinRequests(
  orgId: string,
): Promise<ApiResponse<ListJoinRequestsResponse>> {
  try {
    return await apiGet(`/api/v1/organizations/${orgId}/join-requests`);
  } catch (error) {
    return fail(error, "Failed to list join requests");
  }
}

/** Approve or reject (admin only). */
export async function reviewJoinRequest(
  orgId: string,
  requestId: string,
  req: ReviewJoinRequestRequest,
): Promise<ApiResponse<void>> {
  try {
    return await apiPut(`/api/v1/organizations/${orgId}/join-requests/${requestId}/review`, req);
  } catch (error) {
    return fail(error, "Failed to review join request");
  }
}

// ---- knowledge base sharing ------------------------------------------------------

export async function shareKnowledgeBase(
  kbId: string,
  req: ShareKnowledgeBaseRequest,
): Promise<ApiResponse<KnowledgeBaseShare>> {
  try {
    return await apiPost(`/api/v1/knowledge-bases/${kbId}/shares`, req);
  } catch (error) {
    return fail(error, "Failed to share knowledge base");
  }
}

export async function listKBShares(kbId: string): Promise<ApiResponse<ListSharesResponse>> {
  try {
    return await apiGet(`/api/v1/knowledge-bases/${kbId}/shares`);
  } catch (error) {
    return fail(error, "Failed to list shares");
  }
}

export async function updateSharePermission(
  kbId: string,
  shareId: string,
  req: UpdateSharePermissionRequest,
): Promise<ApiResponse<void>> {
  try {
    return await apiPut(`/api/v1/knowledge-bases/${kbId}/shares/${shareId}`, req);
  } catch (error) {
    return fail(error, "Failed to update share permission");
  }
}

export async function removeShare(kbId: string, shareId: string): Promise<ApiResponse<void>> {
  try {
    return await apiDel(`/api/v1/knowledge-bases/${kbId}/shares/${shareId}`);
  } catch (error) {
    return fail(error, "Failed to remove share");
  }
}

/** Knowledge bases shared to me through organizations. */
export async function listSharedKnowledgeBases(): Promise<ApiResponse<SharedKnowledgeBase[]>> {
  try {
    return await apiGet("/api/v1/shared-knowledge-bases");
  } catch (error) {
    return fail(error, "Failed to list shared knowledge bases");
  }
}

/** All KBs in an organization including those shared by the current tenant. */
export async function listOrganizationSharedKnowledgeBases(
  orgId: string,
): Promise<ApiResponse<OrganizationSharedKnowledgeBaseItem[]>> {
  try {
    return await apiGet(`/api/v1/organizations/${orgId}/shared-knowledge-bases`);
  } catch (error) {
    return fail(error, "Failed to list organization shared knowledge bases");
  }
}

/** KBs shared to a specific organization. */
export async function listOrgShares(orgId: string): Promise<ApiResponse<ListSharesResponse>> {
  try {
    return await apiGet(`/api/v1/organizations/${orgId}/shares`);
  } catch (error) {
    return fail(error, "Failed to list organization shares");
  }
}

// ---- agent sharing -----------------------------------------------------------------

export async function shareAgent(
  agentId: string,
  req: ShareKnowledgeBaseRequest,
): Promise<ApiResponse<AgentShareResponse>> {
  try {
    return await apiPost(`/api/v1/agents/${agentId}/shares`, req);
  } catch (error) {
    return fail(error, "Failed to share agent");
  }
}

export async function listAgentShares(
  agentId: string,
): Promise<ApiResponse<ListAgentSharesResponse>> {
  try {
    return await apiGet(`/api/v1/agents/${agentId}/shares`);
  } catch (error) {
    return fail(error, "Failed to list agent shares");
  }
}

export async function updateAgentSharePermission(
  agentId: string,
  shareId: string,
  req: UpdateSharePermissionRequest,
): Promise<ApiResponse<void>> {
  try {
    return await apiPut(`/api/v1/agents/${agentId}/shares/${shareId}`, req);
  } catch (error) {
    return fail(error, "Failed to update share permission");
  }
}

export async function removeAgentShare(
  agentId: string,
  shareId: string,
): Promise<ApiResponse<void>> {
  try {
    return await apiDel(`/api/v1/agents/${agentId}/shares/${shareId}`);
  } catch (error) {
    return fail(error, "Failed to remove share");
  }
}

export async function listSharedAgents(): Promise<ApiResponse<SharedAgentInfo[]>> {
  try {
    return await apiGet("/api/v1/shared-agents");
  } catch (error) {
    return fail(error, "Failed to list shared agents");
  }
}

/** All agents in an organization including those shared by the current tenant. */
export async function listOrganizationSharedAgents(
  orgId: string,
): Promise<ApiResponse<OrganizationSharedAgentItem[]>> {
  try {
    return await apiGet(`/api/v1/organizations/${orgId}/shared-agents`);
  } catch (error) {
    return fail(error, "Failed to list organization shared agents");
  }
}

/** Toggle whether a shared agent shows in MY chat dropdown. */
export async function setSharedAgentDisabledByMe(
  agentId: string,
  disabled: boolean,
): Promise<ApiResponse<void>> {
  try {
    return await apiPost("/api/v1/shared-agents/disabled", { agent_id: agentId, disabled });
  } catch (error) {
    return fail(error, "Failed to update preference");
  }
}

export async function listOrgAgentShares(
  orgId: string,
): Promise<ApiResponse<ListAgentSharesResponse>> {
  try {
    return await apiGet(`/api/v1/organizations/${orgId}/agent-shares`);
  } catch (error) {
    return fail(error, "Failed to list organization agent shares");
  }
}

/* Candidate tenants for an org invite (excludes existing members). The
 * endpoint resolves one exact workspace ID — it does not expose global
 * workspace-name, username, or email search. */
export async function searchTenantsForInvite(
  orgId: string,
  query: string,
  limit = 10,
): Promise<ApiResponse<TenantInviteCandidate[]>> {
  try {
    return await apiGet(
      `/api/v1/organizations/${orgId}/search-tenants?q=${encodeURIComponent(query)}&limit=${limit}`,
    );
  } catch (error) {
    return fail(error, "Failed to search tenants");
  }
}

/** @deprecated Use searchTenantsForInvite. */
export async function searchUsersForInvite(
  orgId: string,
  query: string,
  limit = 10,
): Promise<ApiResponse<TenantInviteCandidate[]>> {
  return searchTenantsForInvite(orgId, query, limit);
}

/** Direct invite (admin only). */
export async function inviteMember(
  orgId: string,
  req: InviteMemberRequest,
): Promise<ApiResponse<void>> {
  try {
    return await apiPost(`/api/v1/organizations/${orgId}/invite`, req);
  } catch (error) {
    return fail(error, "Failed to invite member");
  }
}
