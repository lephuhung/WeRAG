/* Ported from frontend/src/api/mcp-service.ts + mcp-endpoint.ts. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

// ---- MCP services -------------------------------------------------------------

export interface MCPService {
  id: string;
  tenant_id?: number;
  name: string;
  description: string;
  usage_instructions?: string;
  enabled: boolean;
  transport_type: "sse" | "http-streamable" | "stdio";
  url?: string; // required for SSE/HTTP Streamable
  headers?: Record<string, string>;
  auth_config?: {
    // Empty/absent means none. "oauth" enables the per-user OAuth2
    // authorization-code flow (zero-config: discovery + dynamic client
    // registration).
    auth_type?: "" | "api_key" | "bearer" | "oauth";
    // Secret fields (api_key, token) are NEVER returned by the server in
    // this shape — they live behind the /credentials subresource. Optional
    // typing remains so create-mode payloads can still carry them.
    api_key?: string;
    // Header carrying api_key when auth_type is "api_key". Non-secret;
    // empty defaults to "X-API-Key".
    api_key_header?: string;
    token?: string;
    custom_headers?: Record<string, string>;
    // OAuth-only, non-secret configuration.
    scopes?: string[];
    auth_server_metadata_url?: string;
  };
  advanced_config?: {
    timeout?: number;
    retry_count?: number;
    retry_delay?: number;
  };
  stdio_config?: {
    command: "uvx" | "npx";
    args: string[];
  };
  env_vars?: Record<string, string>; // for stdio transport
  is_builtin?: boolean;
  // Per-field "configured?" map embedded on the main response. Drives the
  // CredentialResource card without a follow-up GET. Absent for builtin
  // services.
  credentials?: Record<McpCredentialField, CredentialFieldMetadata>;
  created_at?: string;
  updated_at?: string;
  catalog?: {
    tool_count: number;
    stale: boolean;
    synced_at: string;
  };
}

export interface MCPTool {
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
  require_approval?: boolean;
  enabled?: boolean;
}

export interface MCPToolApprovalRow {
  id: string;
  tenant_id?: number;
  service_id: string;
  tool_name: string;
  require_approval: boolean;
  enabled: boolean;
}

export interface MCPResource {
  uri: string;
  name: string;
  description?: string;
  mimeType?: string;
}

export interface MCPTestResult {
  success: boolean;
  message?: string;
  description?: string;
  // Set when the server requires OAuth (RFC 9728) but the service was not
  // configured for it — the UI guides the user to switch to OAuth 2.0.
  oauth_required?: boolean;
  tools?: MCPTool[];
  resources?: MCPResource[];
}

type Envelope<T> = { success: boolean; data: T };

export async function listMCPServices(): Promise<MCPService[]> {
  const response = await apiGet<Envelope<MCPService[]>>("/api/v1/mcp-services");
  return response.data || [];
}

export async function getMCPService(id: string): Promise<MCPService> {
  const response = await apiGet<Envelope<MCPService>>(`/api/v1/mcp-services/${id}`);
  return response.data;
}

export async function createMCPService(data: Partial<MCPService>): Promise<MCPService> {
  const response = await apiPost<Envelope<MCPService>>("/api/v1/mcp-services", data);
  return response.data;
}

export async function updateMCPService(id: string, data: Partial<MCPService>): Promise<MCPService> {
  const response = await apiPut<Envelope<MCPService>>(`/api/v1/mcp-services/${id}`, data);
  return response.data;
}

export async function deleteMCPService(id: string): Promise<void> {
  await apiDel(`/api/v1/mcp-services/${id}`);
}

export async function testMCPService(id: string): Promise<MCPTestResult> {
  const response = await apiPost<Envelope<MCPTestResult>>(`/api/v1/mcp-services/${id}/test`, {});
  if (response && response.data) return response.data;
  return response as unknown as MCPTestResult;
}

export async function getMCPServiceTools(id: string): Promise<MCPTool[]> {
  const response = await apiGet<Envelope<MCPTool[]>>(`/api/v1/mcp-services/${id}/tools`);
  return response.data || [];
}

export async function getMCPServiceResources(id: string): Promise<MCPResource[]> {
  const response = await apiGet<Envelope<MCPResource[]>>(`/api/v1/mcp-services/${id}/resources`);
  return response.data || [];
}

/** Persisted per-tool human-approval flags (issue #1173). */
export async function getMCPToolApprovals(serviceId: string): Promise<MCPToolApprovalRow[]> {
  const response = await apiGet<Envelope<MCPToolApprovalRow[]>>(
    `/api/v1/mcp-services/${serviceId}/tool-approvals`,
  );
  return response.data || [];
}

export async function setMCPToolApproval(
  serviceId: string,
  toolName: string,
  requireApproval: boolean,
): Promise<void> {
  await apiPut(`/api/v1/mcp-services/${serviceId}/tool-approvals/${encodeURIComponent(toolName)}`, {
    require_approval: requireApproval,
  });
}

export async function setMCPToolEnabled(
  serviceId: string,
  toolName: string,
  enabled: boolean,
): Promise<void> {
  await apiPut(`/api/v1/mcp-services/${serviceId}/tool-approvals/${encodeURIComponent(toolName)}`, {
    enabled,
  });
}

// ---- credential subresource (issue #988 follow-up) ----------------------------
// Secrets travel through a dedicated /credentials endpoint instead of the main
// MCP PUT body. "Is this configured?" metadata rides on MCPService.credentials,
// so there is no GET here — only PUT (write) and DELETE (clear). Both trigger
// an MCP client reconnect server-side.

export type McpCredentialField = "api_key" | "token";

export interface CredentialFieldMetadata {
  configured: boolean;
}

export interface McpCredentialsResponse {
  fields: Record<McpCredentialField, CredentialFieldMetadata>;
}

export async function putMCPCredentials(
  serviceId: string,
  body: Partial<Record<McpCredentialField, string>>,
): Promise<McpCredentialsResponse> {
  const response = await apiPut<Envelope<McpCredentialsResponse>>(
    `/api/v1/mcp-services/${serviceId}/credentials`,
    body,
  );
  return response.data ?? (response as unknown as McpCredentialsResponse);
}

export async function deleteMCPCredentialField(
  serviceId: string,
  field: McpCredentialField,
): Promise<void> {
  await apiDel(`/api/v1/mcp-services/${serviceId}/credentials/${field}`);
}

// ---- per-user OAuth2 authorization-code flow ----------------------------------
// The user authorizes a service once; the backend stores their access/refresh
// token (per tenant + user + service) and refreshes it transparently.

/* Public backend OAuth callback path (registered outside /mcp-services to
 * avoid a route conflict, and allow-listed for no-auth). */
export const MCP_OAUTH_CALLBACK_PATH = "/api/v1/mcp-oauth/callback";

export interface MCPOAuthAuthorization {
  authorizationUrl: string;
  authorizationAttempt: string;
}

export type MCPOAuthTokenState = "authorized" | "refreshable" | "reauth_required" | "pending";

export interface MCPOAuthStatus {
  authorized: boolean;
  state: MCPOAuthTokenState;
  refresh_available: boolean;
  expires_at?: string;
}

/* Begin authorization for the current user. The attempt id binds polling to
 * this popup, so an older stored token cannot be mistaken for fresh consent. */
export async function getMCPOAuthAuthorizeURL(
  serviceId: string,
  body: { redirect_uri: string; frontend_redirect?: string },
): Promise<MCPOAuthAuthorization> {
  const response = await apiPost<Envelope<{ authorization_url?: string; authorization_attempt?: string }>>(
    `/api/v1/mcp-services/${serviceId}/oauth/authorize-url`,
    body,
  );
  const data = response.data ?? (response as unknown as Record<string, string>);
  return {
    authorizationUrl: data?.authorization_url ?? "",
    authorizationAttempt: data?.authorization_attempt ?? "",
  };
}

export async function getMCPOAuthStatus(
  serviceId: string,
  authorizationAttempt?: string,
): Promise<boolean> {
  const query = authorizationAttempt
    ? `?authorization_attempt=${encodeURIComponent(authorizationAttempt)}`
    : "";
  const response = await apiGet<Envelope<{ authorized?: boolean }>>(
    `/api/v1/mcp-services/${serviceId}/oauth/status${query}`,
  );
  return Boolean((response.data ?? (response as unknown as { authorized?: boolean }))?.authorized);
}

/* Full lifecycle status for management surfaces. Expired access tokens with
 * a refresh token are "refreshable", not falsely presented as usable. */
export async function getMCPOAuthAuthorizationStatus(serviceId: string): Promise<MCPOAuthStatus> {
  const response = await apiGet<Envelope<MCPOAuthStatus>>(
    `/api/v1/mcp-services/${serviceId}/oauth/status`,
  );
  const data = response.data ?? (response as unknown as MCPOAuthStatus);
  return {
    authorized: Boolean(data?.authorized),
    state: data?.state ?? "reauth_required",
    refresh_available: Boolean(data?.refresh_available),
    expires_at: data?.expires_at,
  };
}

/** Revoke the current user's token (forces re-authorization). */
export async function revokeMCPOAuthToken(serviceId: string): Promise<void> {
  await apiDel(`/api/v1/mcp-services/${serviceId}/oauth/token`);
}

export async function resolveToolApproval(
  pendingId: string,
  body: { decision: "approve" | "reject"; modified_args?: Record<string, unknown>; reason?: string },
): Promise<void> {
  await apiPost(`/api/v1/agent/tool-approvals/${encodeURIComponent(pendingId)}`, body);
}

/* Resume an agent run paused on an in-conversation MCP OAuth prompt. Call
 * after the per-user authorization popup completes; the backend verifies
 * the token exists before unblocking the paused tool call. */
export async function resolveMCPOAuth(
  pendingId: string,
  body: { service_id: string; decision?: "authorize" | "cancel" },
): Promise<void> {
  await apiPost(`/api/v1/agent/mcp-oauth-resolutions/${encodeURIComponent(pendingId)}`, body);
}

export async function cancelMCPOAuth(pendingId: string): Promise<void> {
  await apiPost(`/api/v1/agent/mcp-oauth-resolutions/${encodeURIComponent(pendingId)}/cancel`, {});
}

/* Persisted directory: GET never opens an upstream MCP connection. */
export interface MCPMetadata {
  service_id: string;
  tools: MCPTool[];
  instructions: string;
  server_name: string;
  server_version: string;
  server_description: string;
  synced_at: string;
  stale: boolean;
}

export async function getMCPMetadata(id: string): Promise<MCPMetadata | null> {
  const response = await apiGet<Envelope<MCPMetadata>>(`/api/v1/mcp-services/${id}/metadata`);
  return response.data ?? null;
}

export async function refreshMCPMetadata(id: string): Promise<MCPMetadata> {
  const response = await apiPost<Envelope<MCPMetadata>>(
    `/api/v1/mcp-services/${id}/metadata/refresh`,
    {},
  );
  return response.data;
}

export async function generateMCPUsageInstructions(id: string, language: string): Promise<string> {
  const response = await apiPost<Envelope<{ usage_instructions: string }>>(
    `/api/v1/mcp-services/${id}/usage-instructions/generate`,
    { language },
    { timeoutMs: 65_000 },
  );
  return response.data.usage_instructions;
}

// ---- MCP endpoints (public tool surface) ---------------------------------------

export type McpEndpointToolGroup = "retrieve" | "chat" | "wiki" | "ingest";

export interface McpEndpoint {
  id: string;
  tenant_id: number;
  name: string;
  description: string;
  enabled: boolean;
  token_hint: string;
  knowledge_base_ids: string[];
  tools: string[];
  default_agent_id: string;
  rate_limit_per_minute: number;
  /** Public path the MCP client connects to, relative to the server origin. */
  path: string;
  last_used_at?: string | null;
  created_at: string;
  updated_at: string;
  /** Only present on create / rotate responses; shown once. */
  token?: string;
}

export interface McpEndpointToolDefinition {
  name: string;
  group: McpEndpointToolGroup;
  destructive: boolean;
}

export interface McpEndpointToolCatalog {
  groups: McpEndpointToolGroup[];
  tools: McpEndpointToolDefinition[];
  default_tools: string[];
}

export interface McpEndpointPayload {
  name?: string;
  description?: string;
  enabled?: boolean;
  knowledge_base_ids?: string[];
  tools?: string[];
  default_agent_id?: string;
  rate_limit_per_minute?: number;
}

export function listMcpEndpoints() {
  return apiGet<Envelope<McpEndpoint[]>>("/api/v1/mcp-endpoints");
}

export function getMcpEndpointToolCatalog() {
  return apiGet<Envelope<McpEndpointToolCatalog>>("/api/v1/mcp-endpoints/tools");
}

export function createMcpEndpoint(data: McpEndpointPayload) {
  return apiPost<Envelope<McpEndpoint>>("/api/v1/mcp-endpoints", data);
}

export function updateMcpEndpoint(id: string, data: McpEndpointPayload) {
  return apiPut<Envelope<McpEndpoint>>(`/api/v1/mcp-endpoints/${id}`, data);
}

export function deleteMcpEndpoint(id: string) {
  return apiDel(`/api/v1/mcp-endpoints/${id}`);
}

export function rotateMcpEndpointToken(id: string) {
  return apiPost<Envelope<McpEndpoint>>(`/api/v1/mcp-endpoints/${id}/rotate-token`, {});
}
