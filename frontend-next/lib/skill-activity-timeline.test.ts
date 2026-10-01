// Exercise the real TSX reducer without adding a browser test dependency.
// Runs with: node --experimental-strip-types --test lib/skill-activity-timeline.test.ts
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import ts from "typescript";
import * as skillActivity from "./skill-activity.ts";

type TimelineStep = {
  type: string;
  id: string;
  status?: string;
  confirmedSuccess?: boolean;
  tool_name?: string;
};
type Timeline = {
  applyChunkToSteps: (steps: TimelineStep[], chunk: unknown) => void;
  finalizeSteps: (steps: TimelineStep[]) => void;
  stepsFromHistory: (message: unknown) => TimelineStep[];
  toolStepTitle: (
    t: (key: string, vars?: Record<string, string | number>) => string,
    step: TimelineStep,
  ) => string;
};

const requireModule = createRequire(import.meta.url);
const source = readFileSync(new URL("../components/chat/agent-steps.tsx", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2020 },
}).outputText;
const exports: Record<string, unknown> = {};
const timelineModule = { exports };
const load = (specifier: string): unknown => {
  if (specifier === "@/lib/skill-activity") return skillActivity;
  if (specifier === "@/components/icons") return {};
  return requireModule(specifier);
};
new Function("module", "exports", "require", compiled)(timelineModule, exports, load);
const timeline = timelineModule.exports as Timeline;
const t = (key: string, vars?: Record<string, string | number>) =>
  vars?.name === undefined ? key : `${key}:${vars.name}`;

const path = "skill://legal-document-summary/SKILL.md";
const args = { path };
const readResult = { skill_name: "legal-document-summary", file_path: "SKILL.md" };

describe("skill labels in the real agent-step timeline", () => {
  it("labels only confirmed live reads and keeps one row on replay of the same ID", () => {
    const steps: TimelineStep[] = [];
    const call = {
      response_type: "tool_call", data: { tool_call_id: "call-1", tool_name: "read_file", arguments: args },
    };
    const result = {
      response_type: "tool_result", data: { tool_call_id: "call-1", tool_name: "read_file", success: true, ...readResult },
    };
    timeline.applyChunkToSteps(steps, call);
    assert.equal(steps.length, 1);
    assert.doesNotMatch(timeline.toolStepTitle(t, steps[0]), /^step\.skillUsed:/);
    timeline.applyChunkToSteps(steps, result);
    assert.equal(timeline.toolStepTitle(t, steps[0]), "step.skillUsed:legal-document-summary");
    timeline.applyChunkToSteps(steps, call);
    timeline.applyChunkToSteps(steps, result);
    assert.equal(steps.length, 1);
    assert.equal(timeline.toolStepTitle(t, steps[0]), "step.skillUsed:legal-document-summary");
  });

  it("never labels an aborted/pending read that finalizeSteps marks as done", () => {
    const steps: TimelineStep[] = [];
    timeline.applyChunkToSteps(steps, {
      response_type: "tool_call", data: { tool_call_id: "call-2", tool_name: "read_file", arguments: args },
    });
    timeline.finalizeSteps(steps);
    assert.equal(steps[0].status, "success");
    assert.notEqual(steps[0].confirmedSuccess, true);
    assert.doesNotMatch(timeline.toolStepTitle(t, steps[0]), /^step\.skillUsed:/);
  });

  it("labels only confirmed shell runs, never a failed result or arbitrary command", () => {
    const steps: TimelineStep[] = [];
    timeline.applyChunkToSteps(steps, {
      response_type: "tool_call", data: {
        tool_call_id: "call-3", tool_name: "shell_exec",
        arguments: { skill_name: "pdf-tools", command: "TOKEN=secret python x.py" },
      },
    });
    timeline.applyChunkToSteps(steps, {
      response_type: "tool_result", data: { tool_call_id: "call-3", tool_name: "shell_exec", success: true },
    });
    assert.equal(timeline.toolStepTitle(t, steps[0]), "step.skillExecuted:pdf-tools");
    timeline.applyChunkToSteps(steps, {
      response_type: "tool_result", data: { tool_call_id: "call-3", tool_name: "shell_exec", success: false, error: "failed" },
    });
    assert.doesNotMatch(timeline.toolStepTitle(t, steps[0]), /^step\.skillExecuted:/);
  });

  it("reconstructs reads and runs from persisted results, not missing or failed ones", () => {
    const steps = timeline.stepsFromHistory({ agent_steps: [{ tool_calls: [
      { id: "h1", name: "read_file", args, result: { success: true, data: readResult } },
      { id: "h2", name: "read_file", args },
      { id: "h3", name: "read_file", args, result: { success: false } },
      { id: "h4", name: "shell_exec", args: { skill_name: "pdf-tools", command: "TOKEN=secret" }, result: { success: true } },
    ] }] });
    assert.equal(timeline.toolStepTitle(t, steps[0]), "step.skillUsed:legal-document-summary");
    assert.doesNotMatch(timeline.toolStepTitle(t, steps[1]), /^step\.skillUsed:/);
    assert.doesNotMatch(timeline.toolStepTitle(t, steps[2]), /^step\.skillUsed:/);
    assert.equal(timeline.toolStepTitle(t, steps[3]), "step.skillExecuted:pdf-tools");
  });
});
