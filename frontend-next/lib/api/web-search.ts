/* Ported from frontend/src/api/web-search-provider.ts + web-search.ts. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

export interface WebSearchProviderEntity {
  id?: string;
  tenant_id?: number;
  name: string;
  provider:
    | "brave"
    | "bing"
    | "google"
    | "duckduckgo"
    | "tavily"
    | "ollama"
    | "baidu"
    | "searxng"
    | "keenable"
    | "zhipu"
    | "metaso"
    | "exa"
    | "bocha"
    | "serply";
  description?: string;
  parameters: {
    // api_key is never returned in this shape; it lives behind the
    // /credentials subresource. Typed so the initial create POST can carry it.
    api_key?: string;
    engine_id?: string;
    base_url?: string;
    proxy_url?: string;
    extra_config?: Record<string, string>;
  };
  is_default?: boolean;
  /** Per-field configured? metadata from the main response. */
  credentials?: Record<WebSearchCredentialField, { configured: boolean }>;
  created_at?: string;
  updated_at?: string;
}

export interface WebSearchProviderTypeInfo {
  id: string;
  name: string;
  requires_api_key: boolean;
  /** Keyless-by-default providers that still accept an optional key. */
  supports_optional_api_key?: boolean;
  requires_engine_id?: boolean;
  requires_base_url?: boolean;
  supports_proxy?: boolean;
  description?: string;
  docs_url?: string;
  config_fields?: WebSearchProviderConfigField[];
}

export interface WebSearchProviderConfigField {
  key: string;
  label: string;
  label_key?: string;
  type: "select";
  required?: boolean;
  default?: string;
  description?: string;
  description_key?: string;
  options?: Array<{ label: string; label_key?: string; value: string }>;
}

export function createWebSearchProvider(data: Partial<WebSearchProviderEntity>) {
  return apiPost("/api/v1/web-search-providers", data);
}

export function listWebSearchProviders() {
  return apiGet<{ success: boolean; data?: WebSearchProviderEntity[] }>(
    "/api/v1/web-search-providers",
  );
}

export function getWebSearchProvider(id: string) {
  return apiGet<{ success: boolean; data?: WebSearchProviderEntity }>(
    `/api/v1/web-search-providers/${id}`,
  );
}

export function updateWebSearchProvider(id: string, data: Partial<WebSearchProviderEntity>) {
  return apiPut(`/api/v1/web-search-providers/${id}`, data);
}

export function deleteWebSearchProvider(id: string) {
  return apiDel(`/api/v1/web-search-providers/${id}`);
}

export function listWebSearchProviderTypes(): Promise<WebSearchProviderTypeInfo[]> {
  return apiGet<{ success: boolean; data?: WebSearchProviderTypeInfo[] }>(
    "/api/v1/web-search-providers/types",
  ).then((res) => (res.success && res.data ? res.data : []));
}

// ---- credential subresource ---------------------------------------------------------

export type WebSearchCredentialField = "api_key";

export interface WebSearchCredentialsResponse {
  fields: Record<WebSearchCredentialField, { configured: boolean }>;
}

export async function putWebSearchProviderCredentials(
  id: string,
  body: Partial<Record<WebSearchCredentialField, string>>,
): Promise<WebSearchCredentialsResponse> {
  const response = await apiPut<{ data?: WebSearchCredentialsResponse }>(
    `/api/v1/web-search-providers/${id}/credentials`,
    body,
  );
  return (response.data ?? response) as WebSearchCredentialsResponse;
}

export async function deleteWebSearchProviderCredentialField(
  id: string,
  field: WebSearchCredentialField,
): Promise<void> {
  await apiDel(`/api/v1/web-search-providers/${id}/credentials/${field}`);
}

/* Test a provider connection. With `id`, tests the saved provider; with
 * `data`, tests raw credentials without persisting. */
export function testWebSearchProvider(
  id?: string,
  data?: { provider: string; parameters: Record<string, unknown> },
): Promise<unknown> {
  if (id) {
    return apiPost(`/api/v1/web-search-providers/${id}/test`, {});
  }
  return apiPost("/api/v1/web-search-providers/test", data ?? {});
}

// ---- tenant web-search config (web-search.ts) ---------------------------------------

/** @deprecated Use WebSearchProviderTypeInfo instead. */
export interface WebSearchProviderConfig {
  id: string;
  name: string;
  free: boolean;
  requires_api_key: boolean;
  description?: string;
  api_url?: string;
}

export interface WebSearchConfig {
  /** References a WebSearchProviderEntity by ID. */
  default_provider_id?: string;
  /** @deprecated kept for backward compatibility */
  provider?: string;
  api_key?: string;
  max_results: number;
  include_date: boolean;
  compression_method: string;
  blacklist: string[];
  embedding_model_id?: string;
  embedding_dimension?: number;
  rerank_model_id?: string;
  document_fragments?: number;
}

export function getWebSearchProviders() {
  return apiGet("/api/v1/web-search/providers");
}

export function getTenantWebSearchConfig() {
  return apiGet<{ success: boolean; data?: WebSearchConfig }>(
    "/api/v1/tenants/kv/web-search-config",
  );
}

export function updateTenantWebSearchConfig(config: WebSearchConfig) {
  return apiPut("/api/v1/tenants/kv/web-search-config", config);
}
