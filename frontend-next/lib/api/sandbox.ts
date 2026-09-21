/* Ported from frontend/src/api/system/index.ts (sandbox section). */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

export const NAMED_SANDBOX_BACKEND_TYPES = ["cube", "e2b", "docker"] as const;
export type SandboxBackendType = (typeof NAMED_SANDBOX_BACKEND_TYPES)[number] | string;

export interface SandboxConfigData {
  type: string;
  docker?: {
    endpoint?: string;
    image?: string;
    network?: string;
    auto_remove?: boolean;
    cpu_limit?: number;
    memory_limit_mb?: number;
  };
  cube?: {
    endpoint?: string;
    api_key?: string;
    cluster_id?: string;
  };
  e2b?: {
    api_key?: string;
    template?: string;
  };
  [key: string]: unknown;
}

export interface SandboxConfigRecord {
  id: string;
  name: string;
  description?: string;
  sandbox_type: string;
  config: SandboxConfigData;
  created_at: string;
  updated_at: string;
}

export interface SandboxConfigUpsert {
  name: string;
  description?: string;
  config: SandboxConfigData;
}

export interface SandboxInventory {
  sandbox_count: number;
  session_ids?: string[];
  agent_names?: string[];
  unverifiable?: boolean;
}

export function listSandboxConfigs() {
  return apiGet<{
    data: SandboxConfigRecord[];
    workspace_scripts_disabled?: boolean;
  }>("/api/v1/sandbox-configs");
}

export function setSandboxWorkspacePolicy(scriptsDisabled: boolean) {
  return apiPut<{ workspace_scripts_disabled: boolean }>(
    "/api/v1/sandbox-configs/workspace-policy",
    { scripts_disabled: scriptsDisabled },
  );
}

export function createSandboxConfig(payload: SandboxConfigUpsert) {
  return apiPost<{ data: SandboxConfigRecord }>("/api/v1/sandbox-configs", payload);
}

export function getSandboxConfigById(id: string) {
  return apiGet<{ data: SandboxConfigRecord }>(`/api/v1/sandbox-configs/${encodeURIComponent(id)}`);
}

export function updateSandboxConfigById(id: string, payload: SandboxConfigUpsert) {
  return apiPut<{ data: SandboxConfigRecord }>(
    `/api/v1/sandbox-configs/${encodeURIComponent(id)}`,
    payload,
  );
}

export function deleteSandboxConfig(id: string, force = false) {
  const query = force ? "?force=true" : "";
  return apiDel<{ success?: boolean }>(`/api/v1/sandbox-configs/${encodeURIComponent(id)}${query}`);
}

export function getSandboxConfigInventory(id: string) {
  return apiGet<{ data: SandboxInventory }>(
    `/api/v1/sandbox-configs/${encodeURIComponent(id)}/sandboxes`,
  );
}
