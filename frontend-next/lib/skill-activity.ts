/* Confirmed skill use is derived from tool results, not mentions or inferred
 * "done" status. Shared by the live and persisted agent-step timelines. */
export type SkillActivity = { kind: "read" | "execute"; name: string };

export type SkillActivityInput = {
  toolName?: string;
  args?: unknown;
  resultData?: unknown;
  confirmedSuccess: boolean;
};

const SKILL_NAME = /^[\p{L}\p{N}_-]{1,64}$/u;
const SKILL_PATH = /^skill:\/\/([^/]+)\/SKILL\.md$/u;

function asRecord(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function validName(value: unknown): string | null {
  return typeof value === "string" && SKILL_NAME.test(value) ? value : null;
}

export function confirmedStreamToolResult(
  responseType: string,
  chunkSuccess: unknown,
  dataSuccess: unknown,
): boolean {
  return responseType === "tool_result" &&
    (chunkSuccess === true || dataSuccess === true) &&
    chunkSuccess !== false && dataSuccess !== false;
}

export function confirmedHistoryToolResult(
  result: { success?: boolean } | null | undefined,
): boolean {
  return result?.success === true;
}

export function confirmedSkillActivity(input: SkillActivityInput): SkillActivity | null {
  if (!input.confirmedSuccess) return null;
  const args = asRecord(input.args);
  if (input.toolName === "shell_exec") {
    const name = validName(args.skill_name);
    return name ? { kind: "execute", name } : null;
  }
  if (input.toolName !== "read_file") return null;

  const data = asRecord(input.resultData);
  // The tool's result describes the file actually read and takes precedence
  // over the request, which may be absent or stale on replay.
  if (data.file_path !== undefined && data.file_path !== "SKILL.md") return null;
  if (data.skill_name !== undefined) {
    const name = validName(data.skill_name);
    return name && data.file_path === "SKILL.md" ? { kind: "read", name } : null;
  }

  const path = args.path;
  if (typeof path !== "string") return null;
  const match = SKILL_PATH.exec(path);
  const name = validName(match?.[1]);
  return name ? { kind: "read", name } : null;
}
