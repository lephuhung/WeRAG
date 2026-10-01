// Runs with: node --experimental-strip-types --test lib/skill-activity.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  confirmedHistoryToolResult,
  confirmedSkillActivity,
  confirmedStreamToolResult,
} from "./skill-activity.ts";

describe("confirmed skill activity", () => {
  it("counts a successful SKILL.md read, not other resources", () => {
    assert.deepEqual(
      confirmedSkillActivity({
        toolName: "read_file",
        args: { path: "skill://legal-document-summary/SKILL.md" },
        confirmedSuccess: true,
      }),
      { kind: "read", name: "legal-document-summary" },
    );
    assert.equal(
      confirmedSkillActivity({
        toolName: "read_file",
        args: { path: "skill://legal-document-summary/FORMS.md" },
        confirmedSuccess: true,
      }),
      null,
    );
  });

  it("accepts confirmed read result metadata without arguments, but never a different file", () => {
    assert.deepEqual(
      confirmedSkillActivity({
        toolName: "read_file",
        resultData: { skill_name: "legal-document-summary", file_path: "SKILL.md" },
        confirmedSuccess: true,
      }),
      { kind: "read", name: "legal-document-summary" },
    );
    assert.equal(
      confirmedSkillActivity({
        toolName: "read_file",
        args: { path: "skill://legal-document-summary/SKILL.md" },
        resultData: { skill_name: "legal-document-summary", file_path: "FORMS.md" },
        confirmedSuccess: true,
      }),
      null,
    );
  });

  it("exposes only the skill name, not the shell command or secret", () => {
    assert.deepEqual(
      confirmedSkillActivity({
        toolName: "shell_exec",
        args: { skill_name: "pdf-tools", command: "TOKEN=secret python x.py" },
        confirmedSuccess: true,
      }),
      { kind: "execute", name: "pdf-tools" },
    );
  });

  it("requires an observed successful tool result, not a pending call or a contradiction", () => {
    assert.equal(confirmedStreamToolResult("tool_call", true, true), false);
    assert.equal(confirmedStreamToolResult("tool_result", undefined, true), true);
    assert.equal(confirmedStreamToolResult("tool_result", true, undefined), true);
    assert.equal(confirmedStreamToolResult("tool_result", undefined, undefined), false);
    assert.equal(confirmedStreamToolResult("tool_result", true, false), false);
    assert.equal(confirmedStreamToolResult("tool_result", false, true), false);
    assert.equal(confirmedStreamToolResult("error", true, true), false);
    assert.equal(confirmedHistoryToolResult(undefined), false);
    assert.equal(confirmedHistoryToolResult({ success: false }), false);
    assert.equal(confirmedHistoryToolResult({ success: true }), true);
    assert.equal(
      confirmedSkillActivity({
        toolName: "read_file",
        args: { path: "skill://legal-document-summary/SKILL.md" },
        confirmedSuccess: false,
      }),
      null,
    );
  });

  it("rejects unrelated tools, missing arguments, and unsafe names or paths", () => {
    const invalid: Array<{ toolName: string; args?: unknown }> = [
      { toolName: "read_file" },
      { toolName: "read_file", args: { path: "skill://../SKILL.md" } },
      { toolName: "read_file", args: { path: "skill://a/SKILL.md?token=x" } },
      { toolName: "read_file", args: { path: "skill://a/SKILL.md#fragment" } },
      { toolName: "read_file", args: { path: "skill://a/sub/SKILL.md" } },
      { toolName: "read_file", args: { path: "skill:///SKILL.md" } },
      { toolName: "shell_exec", args: { command: "python x.py" } },
      { toolName: "shell_exec", args: { skill_name: "", command: "python x.py" } },
      { toolName: "shell_exec", args: { skill_name: "../unsafe", command: "python x.py" } },
      { toolName: "shell_exec", args: { skill_name: "a".repeat(65) } },
      { toolName: "other", args: { skill_name: "legal-document-summary" } },
    ];
    for (const { toolName, args } of invalid) {
      assert.equal(confirmedSkillActivity({ toolName, args, confirmedSuccess: true }), null, toolName);
    }
  });
});
