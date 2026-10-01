# Frontend-next Skill Activity Labels Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the name of a successfully read or executed skill on its existing tool row in the `frontend-next` Smart Reasoning thinking timeline, both live and after reload.

**Architecture:** Derive the skill name/action from the existing `tool_call` arguments plus a *confirmed* successful `tool_result`, or from the persisted `agent_steps` call/result pair. Keep the parsing and success rules in one dependency-free TypeScript module; the existing React timeline attaches its short localized label to the matching tool row. No backend events, duplicate rows, or changes to the Vue client.

**Tech Stack:** TypeScript/React (Next 16), Node 22 `--experimental-strip-types --test` for pure `.ts` tests, existing `frontend-next/lib/i18n.tsx` translations, `npm run typecheck` / `npm run build`.

**Spec:** `docs/superpowers/specs/2026-10-01-frontend-next-skill-activity-design.md`

## Global Constraints

- Only `frontend-next`. Do not modify backend event/persistence contracts, Vue `frontend/`, or skill execution permissions.
- `read_file` counts only on confirmed success reading `skill://<name>/SKILL.md`; `shell_exec` counts only on confirmed success with a nonempty `skill_name`. A mention, metadata, pending call, failed result or inferred success from `finalizeSteps` must **not** become a successful skill label.
- The existing tool row is the only UI row. Show a localized short title (`Dùng skill: <name>` or `Chạy skill: <name>` in Vietnamese), never raw shell command, env variables, tool output or secret in the skill title. Keep other tool rows unchanged.
- Names must be one segment of 1–64 Unicode letters/digits/`_`/`-`; never display a traversal path, query, fragment or unvalidated name as a skill title.
- Preserve pre-existing worktree changes, especially `frontend-next/package-lock.json`, abbreviation work and AGENTS/CLAUDE docs. Do not run npm install unless user authorizes it. The existing baseline `npm run typecheck` passed on 2026-10-01.
- Before editing **each** existing function/class/method, run `node .gitnexus/run.cjs impact <symbol> -d upstream -f <file>` and report direct callers, affected processes and risk. Warn the user before any HIGH/CRITICAL edit. Before **each commit**, stage only scoped files and run `node .gitnexus/run.cjs detect-changes --scope staged` plus `git diff --cached --check`. If GitNexus cannot analyze, stop instead of bypassing it.
- Test first: write and run a failing regression, add minimum code, rerun targeted tests and typecheck. Keep commits small and scoped.

## File map / interfaces

- Create `frontend-next/lib/skill-activity.ts`: pure exported `confirmedStreamToolResult(responseType: string, chunkSuccess: unknown, dataSuccess: unknown): boolean`, `confirmedHistoryToolResult(result: {success?: boolean} | null | undefined): boolean`, and `confirmedSkillActivity(input: SkillActivityInput): SkillActivity | null`. `SkillActivityInput` is `{toolName?: string; args?: unknown; resultData?: unknown; confirmedSuccess: boolean}`; `SkillActivity` is `{kind: "read" | "execute"; name: string}`. No React/path aliases/runtime imports.
- Create `frontend-next/lib/skill-activity.test.ts`: Node built-in test runner covers validation, false positives, both confirmation paths, and no command/secret in returned object.
- Modify `frontend-next/components/chat/agent-steps.tsx`: `ToolStep.confirmedSuccess?: boolean`, wire the two confirmation helpers into live reducer/history, and call `confirmedSkillActivity` in `toolStepTitle` for the success label. Preserve `finalizeSteps` and other tool titles.
- Modify `frontend-next/lib/i18n.tsx`: new keys `step.skillUsed` and `step.skillExecuted` for `en`, `vi`, `zh`; do not alter existing keys. `vi` is `Record<keyof typeof en,string>`, so new keys must exist there; `zh` is partial but also provide both.

---

### Task 1: Pure, conservative skill-use classification

**Files:** Create `frontend-next/lib/skill-activity.ts`, `frontend-next/lib/skill-activity.test.ts`.

**Interfaces:** Export exactly the four types/functions in the file map. `confirmedStreamToolResult` requires response type `tool_result`, an explicit `true` from `data.success` or top-level `chunk.success`, and no explicit `false` in either. `confirmedHistoryToolResult` requires `result?.success === true`. `confirmedSkillActivity` returns `null` unless `confirmedSuccess` is true and action/name can be established from permitted fields.

- [ ] **Step 1: Write the failing Node test.** In `frontend-next/lib/skill-activity.test.ts`, use `node:test` and `node:assert/strict` with real pure inputs:

```ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { confirmedStreamToolResult, confirmedHistoryToolResult, confirmedSkillActivity } from "./skill-activity.ts";

describe("confirmed skill activity", () => {
  it("counts a successful legal SKILL.md read but no other file", () => {
    assert.deepEqual(confirmedSkillActivity({toolName:"read_file", args:{path:"skill://legal-document-summary/SKILL.md"}, confirmedSuccess:true}), {kind:"read",name:"legal-document-summary"});
    assert.equal(confirmedSkillActivity({toolName:"read_file", args:{path:"skill://legal-document-summary/FORMS.md"}, confirmedSuccess:true}), null);
  });
  it("does not turn a shell command into a credential-bearing title", () => {
    assert.deepEqual(confirmedSkillActivity({toolName:"shell_exec", args:{skill_name:"pdf-tools",command:"TOKEN=secret python x.py"}, confirmedSuccess:true}), {kind:"execute",name:"pdf-tools"});
  });
  it("requires a real successful result", () => {
    assert.equal(confirmedStreamToolResult("tool_call", true, true), false);
    assert.equal(confirmedStreamToolResult("tool_result", undefined, true), true);
    assert.equal(confirmedStreamToolResult("tool_result", undefined, undefined), false);
    assert.equal(confirmedHistoryToolResult(undefined), false);
    assert.equal(confirmedHistoryToolResult({success:true}), true);
    assert.equal(confirmedSkillActivity({toolName:"read_file",args:{path:"skill://legal-document-summary/SKILL.md"},confirmedSuccess:false}), null);
  });
});
```

Add table cases for failed tool result, explicit contradictory success flags, missing args, invalid name/traversal/query, `read_file` result with `skill_name` and `file_path=SKILL.md` when args are absent, result `file_path=FORMS.md` overriding a stale args path, no `skill_name` in shell, and non-skill tool names. Use literal expectations, not expected values derived by the classifier.
- [ ] **Step 2: Watch RED.** Run `cd frontend-next && node --experimental-strip-types --test lib/skill-activity.test.ts`; expect module-not-found for the new pure helper. Fix only test syntax if this command fails for another reason.
- [ ] **Step 3: Implement the parser minimally.** The name/path rule may use `const NAME = /^[\p{L}\p{N}_-]{1,64}$/u` and `^skill://([^/]+)/SKILL\.md$` followed by `NAME.test(name)`. Normalize only strings; parse `args`/`resultData` as non-array records. For `read_file`, if a result-provided `skill_name` exists, require `file_path === "SKILL.md"`; otherwise accept only the exact args path. For `shell_exec`, take only `args.skill_name`, never `args.command`. Implement confirmation as:

```ts
export const confirmedStreamToolResult = (type: string, chunk: unknown, data: unknown) =>
  type === "tool_result" && (chunk === true || data === true) && chunk !== false && data !== false;
export const confirmedHistoryToolResult = (result: {success?: boolean} | null | undefined) =>
  result?.success === true;
```

- [ ] **Step 4: Watch GREEN.** `cd frontend-next && node --experimental-strip-types --test lib/skill-activity.test.ts && npm run typecheck`. Expect all tests and typecheck pass without touching package-lock.
- [ ] **Step 5: Stage only two new files, run GitNexus `detect-changes --scope staged`, `git diff --cached --check`, and commit `feat: classify confirmed skill activity in Next timeline`**. Neither file edits an existing symbol; if that changes, run upstream impact first.

### Task 2: Wire the existing tool row for live and history

**Files:** Modify `frontend-next/components/chat/agent-steps.tsx`, `frontend-next/lib/i18n.tsx`; test Task 1's `frontend-next/lib/skill-activity.test.ts` if extending tests for the live/history confirmation inputs.

**Interfaces:** Consume Task 1's functions. `ToolStep.confirmedSuccess` records an observed result independently of `status`. `applyChunkToSteps`: on `tool_result`, set `confirmedSuccess = confirmedStreamToolResult(kind, c.success, d?.success)`; on `error`, set false. `stepsFromHistory`: set `confirmedSuccess = confirmedHistoryToolResult(call.result)`. Never set this property in `finalizeSteps`. `toolStepTitle` uses `confirmedSkillActivity({toolName:step.tool_name,args:step.args,resultData:step.tool_data,confirmedSuccess:step.confirmedSuccess === true})` only for `step.status === "success"`, before the generic `read_file` / `shell_exec` title branches.

- [ ] **Step 1: Run upstream impact before editing** `applyChunkToSteps`, `stepsFromHistory`, `toolStepTitle`, and `ToolStep` type location in `frontend-next/components/chat/agent-steps.tsx`; inspect/report blast radius. `i18n.tsx` adds keys, not changes an existing function; if a function is edited, run impact for it first. Stop and warn before HIGH/CRITICAL.
- [ ] **Step 2: Write an additional failing regression if the live/history gate still has a gap.** In `skill-activity.test.ts`, give identical read inputs to stream and history confirmation and ensure only true result yields the same `{kind:"read",name:"legal-document-summary"}`:

```ts
const input = {toolName:"read_file",args:{path:"skill://legal-document-summary/SKILL.md"}};
assert.deepEqual(confirmedSkillActivity({...input,confirmedSuccess:confirmedStreamToolResult("tool_result",undefined,true)}),
  confirmedSkillActivity({...input,confirmedSuccess:confirmedHistoryToolResult({success:true})}));
assert.equal(confirmedSkillActivity({...input,confirmedSuccess:confirmedHistoryToolResult(undefined)}),null);
```

Run `cd frontend-next && node --experimental-strip-types --test lib/skill-activity.test.ts`; if already green, this is a characterization of the Task 1 helper, so do not claim it tests the React reducer. The missing `confirmedSuccess` wiring is verified during review/typecheck/build, not by a test that merely mocks the reducer.
- [ ] **Step 3: Wire one verified outcome to both timelines.** Import the Task 1 helpers in `agent-steps.tsx` (path `@/lib/skill-activity` for bundler). Add `confirmedSuccess?: boolean` to `ToolStep`; set it on tool_result/error in `applyChunkToSteps`, and from `call.result` in `stepsFromHistory`. The success title branch should use:

```tsx
const activity = step.status === "success" ? confirmedSkillActivity({
  toolName: step.tool_name, args: step.args, resultData: step.tool_data,
  confirmedSuccess: step.confirmedSuccess === true,
}) : null;
if (activity) return t(activity.kind === "read" ? "step.skillUsed" : "step.skillExecuted", {name:activity.name});
```

Place this branch after `pending` handling, before generic completed tool handling; leave tool results/details and all non-skill branches unchanged.
- [ ] **Step 4: Add only two i18n keys to each locale in `frontend-next/lib/i18n.tsx`.** Exact values:

```ts
// en
"step.skillUsed": "Used skill: {name}",
"step.skillExecuted": "Ran skill: {name}",
// vi
"step.skillUsed": "Dùng skill: {name}",
"step.skillExecuted": "Chạy skill: {name}",
// zh
"step.skillUsed": "使用技能：{name}",
"step.skillExecuted": "运行技能：{name}",
```

- [ ] **Step 5: Verify.** Run `cd frontend-next && node --experimental-strip-types --test lib/skill-activity.test.ts && npm run typecheck && npm run build`; inspect live code path and history path for result confirmation and ensure a pending call finalized as success does not get the success label. If build fails on an external environment requirement, report the exact error and do not claim it passed. Also run `git diff --check` and check `git status --short frontend-next/package-lock.json` to ensure the pre-existing lockfile change is untouched.
- [ ] **Step 6: Stage only `frontend-next/components/chat/agent-steps.tsx`, `frontend-next/lib/i18n.tsx`, and a changed feature test if applicable; run `detect-changes --scope staged` and `git diff --cached --check`, then commit `feat: show confirmed skill names in Next thinking steps`.** Report the test/build results and the commits; leave all unrelated dirty paths untouched.
