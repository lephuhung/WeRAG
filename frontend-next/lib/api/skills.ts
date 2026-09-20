/* Ported from frontend/src/api/skill/index.ts. */
import { apiDel, apiGet, apiPost, apiUpload } from "@/lib/api-client";
import type { ConfigSkillFileContent, ConfigSkillFileEntry } from "./system";

export interface SkillInfo {
  name: string;
  description: string;
}

export interface SkillCatalogInstall {
  skill_id: string;
  sandbox_config_id: string;
  sandbox_config_name?: string;
  sandbox_type?: string;
  status: string;
  enabled: boolean;
  error?: string;
  version?: string;
  bundle_sha256?: string;
  // Present while a newer install is in flight or has failed and the sandbox
  // still runs the previous version.
  served?: { version?: string };
  updated_at: string;
}

export interface SkillCatalogItem {
  id: string;
  name: string;
  version?: string;
  description?: string;
  bundle_sha256?: string;
  created_at: string;
  updated_at: string;
  installations: SkillCatalogInstall[];
}

export interface SkillCatalogRegisterResult {
  id: string;
  name: string;
  version?: string;
  description?: string;
}

/* Skills executable on the given sandbox config; when sandboxConfigId is
 * omitted or skills_available=false, callers should hide/disable the skills
 * configuration. */
export function listSkills(sandboxConfigId?: string) {
  const qs = sandboxConfigId ? `?sandbox_config_id=${encodeURIComponent(sandboxConfigId)}` : "";
  return apiGet<{ data: SkillInfo[]; skills_available?: boolean }>(`/api/v1/skills${qs}`);
}

export function listSkillCatalog() {
  return apiGet<{ data: SkillCatalogItem[] }>("/api/v1/skills/catalog");
}

export function registerSkillCatalogFromSource(source: string) {
  return apiPost<{ data: SkillCatalogRegisterResult }>(
    "/api/v1/skills/catalog",
    { source },
    { timeoutMs: 2 * 60 * 1000 },
  );
}

export function registerSkillCatalogFromFile(file: File, onProgress?: (percent: number) => void) {
  const form = new FormData();
  form.append("file", file);
  return apiUpload<{ data: SkillCatalogRegisterResult }>("/api/v1/skills/catalog", form, onProgress, {
    timeoutMs: 5 * 60 * 1000,
  });
}

export function installSkillCatalog(catalogId: string, sandboxConfigIds: string[]) {
  return apiPost<{ data: { installs: Record<string, string>; errors?: Record<string, string> } }>(
    `/api/v1/skills/catalog/${catalogId}/install`,
    { sandbox_config_ids: sandboxConfigIds },
  );
}

export function deleteSkillCatalog(catalogId: string) {
  return apiDel(`/api/v1/skills/catalog/${catalogId}`);
}

export function listCatalogSkillFiles(catalogId: string) {
  return apiGet<{ data: ConfigSkillFileEntry[] }>(`/api/v1/skills/catalog/${catalogId}/files`);
}

export function getCatalogSkillFile(catalogId: string, path: string) {
  return apiGet<{ data: ConfigSkillFileContent }>(
    `/api/v1/skills/catalog/${catalogId}/files/content?path=${encodeURIComponent(path)}`,
  );
}
