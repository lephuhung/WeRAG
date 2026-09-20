/* Ported from frontend/src/api/env-vars.ts. */
import { apiDel, apiGet, apiPut } from "@/lib/api-client";

/* Which value an execution would actually use for one variable.
 * `unset` = nothing stored anywhere, `workspace` = an admin filled it in for
 * everyone, `user` = the caller's own value (wins over workspace). */
export type EnvVarSource = "unset" | "workspace" | "user";

/* One variable as its owner may see it — deliberately no value field: the
 * endpoint reports what is missing, never what is stored. */
export interface EnvVarView {
  name: string;
  description?: string;
  required?: boolean;
  source: EnvVarSource;
  /** Present only for the caller's own value. */
  updated_at?: string;
}

export interface SkillEnvGroup {
  skill_id: string;
  skill_name: string;
  /** SKILL.md one-liner; not the instruction body. */
  description?: string;
  vars: EnvVarView[];
}

/* One sandbox config: the caller's own config-wide variables (injected into
 * every execution on it) plus the credentials its skills declared. */
export interface ConfigEnvGroup {
  sandbox_config_id: string;
  sandbox_config_name: string;
  description?: string;
  vars: EnvVarView[];
  skills: SkillEnvGroup[];
}

export function listMyEnvVars(): Promise<{ data: ConfigEnvGroup[] }> {
  return apiGet("/api/v1/me/env-vars");
}

export function setMySkillEnv(
  skillId: string,
  name: string,
  value: string,
): Promise<{ success: boolean }> {
  return apiPut("/api/v1/me/env-vars/skill", { skill_id: skillId, name, value });
}

/* Removes the caller's own value; the workspace value (if any) applies again.
 * Scope and name travel in the JSON body, matching the PUT above. */
export function deleteMySkillEnv(skillId: string, name: string): Promise<{ success: boolean }> {
  return apiDel("/api/v1/me/env-vars/skill", { skill_id: skillId, name });
}

export function setMySandboxEnv(
  configId: string,
  name: string,
  value: string,
): Promise<{ success: boolean }> {
  return apiPut("/api/v1/me/env-vars/sandbox", {
    sandbox_config_id: configId,
    name,
    value,
  });
}

export function deleteMySandboxEnv(configId: string, name: string): Promise<{ success: boolean }> {
  return apiDel("/api/v1/me/env-vars/sandbox", { sandbox_config_id: configId, name });
}
