# Built-in Read-only Legal Skills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make four repository-tracked Vietnamese legal SKILL.md files available by default to every ordinary Smart Reasoning agent without sandbox or script privileges.

**Architecture:** Embed exactly four existing `examples/skills/legal-*/SKILL.md` files in the Go binary. A read-only `BuiltinSource` joins the existing tenant/host `skills.Manager` with reserved-name precedence, while the Smart Reasoning engine attaches that manager and `read_file` even when tenant skills/sandbox are disabled. Keep shell registration and tenant allowlists separate from built-in visibility.

**Tech Stack:** Go `embed.FS`/`fs.FS`, existing `internal/agent/skills.SkillSource`, `Manager`, `ReadFileTool`, Go test/testify.

**Spec:** `docs/superpowers/specs/2026-09-30-builtin-readonly-legal-skills-design.md`

## Global Constraints

- Exactly `legal-document-summary`, `legal-document-comparison`, `legal-latest-guidance`, `legal-question-abbreviations`; source Markdown stays in `examples/skills/` and is editable in Git.
- Smart Reasoning only; SkillInstallMode and Quick Answer/RAG unchanged; no external legal lookups or inference that an old legal document is repealed without evidence.
- Built-ins are instruction-only: no sandbox dependency, no host script staging, no shell rights, even for tenant skill names that collide.
- Existing tenant skill selection (`none`/`selected`/`all`), snapshot isolation, file permissions and shell execution gate remain unchanged for **tenant** skills.
- Before modifying **each** existing function/class/method run `node .gitnexus/run.cjs impact <symbol> -d upstream -f <file>`, report callers/processes/risk to the user, and warn before editing for HIGH/CRITICAL. After staging **only feature files**, run `node .gitnexus/run.cjs detect-changes --scope staged` before **each commit**. GitNexus full rebuild succeeded on 2026-09-30; repeat analysis if index becomes stale. Never overwrite unrelated dirty files (notably abbreviation backend, Docker compose, frontend lockfile).
- TDD per task: demonstrate RED for new tests, minimal implementation, GREEN, then commit scoped paths only. `go test ./...` already has unrelated fixture failures in `internal/application/service` and `/memory`; compare with baseline and report rather than patching unrelated files.

## File map / interfaces

- `examples/skills/embed.go` (new): package `legalskillassets`, export `var FS embed.FS` with four explicit embed patterns; no generated copy.
- `internal/agent/skills/builtin_source.go` (new): `NewBuiltinSource(fsys fs.FS) (*BuiltinSource, error)`, `Has(name string) bool`, implement `SkillSource`; reject missing/invalid asset, name mismatch, non-SKILL.md read, and make file paths virtual rather than executable.
- `internal/agent/skills/builtin_source_test.go` (new): valid embedded assets and malformed `fstest.MapFS` table cases.
- `internal/agent/skills/manager.go`, `shell_environment.go`, matching `manager_test.go`/`shell_environment_test.go`: explicit built-in source precedence, tenant selection scoped to tenant/host, shell refusal.
- `internal/application/service/agent_service.go`, `agent_service_test.go`: construct built-in source for ordinary Smart Reasoning; attach manager + `read_file` with no sandbox. Keep existing sandbox/shell gates. Initialize error for invalid built-in must propagate, not be silently logged.
- `internal/agent/tools/read_file.go`, `read_file_test.go` (or existing relevant read-file test file): correct read-only execution hint, no sandbox reader regression.
- `internal/application/service/session_agent_qa.go`, its existing scope tests, `agent_service.go` pin resolver tests: allow `@` pinning of available built-ins even when tenant skill mode is `none`; reject unknown/unavailable pins; present built-in description for collision.
- `examples/skills/README.md`, `docs/agent-skills.md`: describe auto availability, read-only execution, precedence, redeploy requirement; preserve custom tenant installation docs.
- `examples/skills/legal-question-abbreviations/SKILL.md`: already authored and tested but uncommitted; include in the first scoped commit so embed builds from a tracked file. Leave existing unrelated abbreviation backend edits alone.

---

### Task 1: Embed and validate the four instruction-only skills

**Files:** Create `examples/skills/embed.go`, `internal/agent/skills/builtin_source.go`, `internal/agent/skills/builtin_source_test.go`; add existing untracked `examples/skills/legal-question-abbreviations/SKILL.md`.

**Interfaces:** Produce `legalskillassets.FS` (`embed.FS`), `skills.NewBuiltinSource(fsys fs.FS) (*BuiltinSource,error)`, `(*BuiltinSource).Has(name string) bool`, `skills.IsBuiltinName(name string) bool`, and `BuiltinSource` implementing `SkillSource`. `GetSkillBasePath` returns a virtual `skill://<name>` (never a host/sandbox path). Define `ErrInvalidBuiltin` sentinel for fatal configuration failures. Source has fixed ordered names listed under Global Constraints, even if fs.FS has extra files.

- [ ] **Step 1: Write failing tests.** In `builtin_source_test.go`, test real `legalskillassets.FS` (four names in order, body nonempty, no resource files) and errors for missing, mismatched, empty-body and broken-frontmatter `SKILL.md`. Construct fixture FS with the four valid files, then alter one entry per subtest:

```go
files := fstest.MapFS{}
for _, name := range []string{"legal-document-summary", "legal-document-comparison", "legal-latest-guidance", "legal-question-abbreviations"} {
    path := name + "/SKILL.md"
    data, err := fs.ReadFile(legalskillassets.FS, path)
    require.NoError(t, err)
    files[path] = &fstest.MapFile{Data: data}
}
files["legal-document-summary/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: wrong\ndescription: x\n---\nbody")}
_, err := NewBuiltinSource(files)
require.ErrorIs(t, err, ErrInvalidBuiltin)
```

Also assert `LoadSkillFile(name,"scripts/run.sh")` and unknown names fail; repeat via table-driven mutation for `delete(files,path)`, empty body, and malformed frontmatter.
- [ ] **Step 2: Run RED.** `go test ./internal/agent/skills -run '^TestBuiltinSource' -count=1` must fail on missing `NewBuiltinSource`/asset package; do not count an existing unrelated failure as RED.
- [ ] **Step 3: Implement minimal source.** In `examples/skills/embed.go`:

```go
package legalskillassets
import "embed"
//go:embed legal-document-summary/SKILL.md legal-document-comparison/SKILL.md legal-latest-guidance/SKILL.md legal-question-abbreviations/SKILL.md
var FS embed.FS
```

In `builtin_source.go`, use fixed ordered `builtinNames`, `fs.ReadFile(fsys,name+"/SKILL.md")`, `ParseSkillFile(string(data))`; check `skill.Name == name`, `!skill.FrontmatterRepaired` and nonempty `strings.TrimSpace(skill.Instructions)`; cache parsed bodies/metadata, with `BasePath`/`FilePath` as `skill://` URIs. Define `var ErrInvalidBuiltin = errors.New("invalid built-in skill")` and `IsBuiltinName` as a lookup over the fixed list; wrap validation failures with `fmt.Errorf("%w: %s: %v", ErrInvalidBuiltin, name, err)`. Implement `LoadSkillFile` only for `SKILL.md` (returns original Markdown bytes in `SkillFile.Content`), `ListSkillFiles` returns empty list; unknown name/path fails.
- [ ] **Step 4: Run GREEN.** `go test ./internal/agent/skills -run '^TestBuiltinSource' -count=1` and `go test ./examples/skills ./internal/agent/skills -count=1` must pass.
- [ ] **Step 5: Stage only these four paths, run `node .gitnexus/run.cjs detect-changes --scope staged` and `git diff --cached --check`, then commit with `git commit -m 'feat: embed readonly Vietnamese legal skills'`.** No existing symbol edited in this task; if that changes, run upstream impact first.

### Task 2: Compose sources without broadening the execution/selection gate

**Files:** Modify `internal/agent/skills/manager.go`, `internal/agent/skills/shell_environment.go`; add focused tests in `internal/agent/skills/builtin_manager_test.go` (new). Do not change `TenantSkillSource` row interpretation.

**Interfaces:** `(*Manager).WithBuiltinSource(*BuiltinSource) *Manager`, `(*Manager).IsBuiltinSkill(name string) bool`; `resolveSource` checks built-in `Has` first; `discoverAllSkills` returns four built-ins first, then filtered tenant/host metadata excluding name collisions. Existing `ManagerConfig.Enabled` remains true for sessions with built-ins, even if tenant skills are off (service sets it in Task 3). Existing allowlist filters only tenant/host names.

- [ ] **Step 1: Run upstream `impact` before touching existing methods.** At minimum: `NewManager` (only if edited), `resolveSource`, `discoverAllSkills`, `Initialize`, `filterAllowedSkills`, `isSkillAllowed`, `Reload`, `SandboxSkillDir`, `PrepareShellEnvironment`. Example: `node .gitnexus/run.cjs impact resolveSource -d upstream -f internal/agent/skills/manager.go`. Report direct callers/affected flows/risk; stop and warn for HIGH/CRITICAL.
- [ ] **Step 2: Write RED tests.** In `builtin_manager_test.go`, use:

```go
builtins, err := NewBuiltinSource(legalskillassets.FS)
require.NoError(t, err)
mgr := NewManager(&ManagerConfig{Enabled: true, AllowedSkills: []string{"tenant-only"}}, nil).WithBuiltinSource(builtins)
require.NoError(t, mgr.Initialize(context.Background()))
require.Len(t, mgr.GetAllMetadata(), 4)
_, err = mgr.PrepareShellEnvironment(context.Background(), "s", "legal-document-summary", "true", nil)
require.Error(t, err)
```

Assert built-in `LoadSkill`/`ReadSkillFile(SKILL.md)` ignores tenant allowlist. Use `NewTenantSkillSource` with ready enabled rows named `tenant-only`, `legal-document-summary` and an allowlist-denied `other` to assert built-in wins metadata/body, `tenant-only` remains listed, `other` is hidden, no duplicate appears; assert `Reload` retains ordering. Existing `shell_environment_test.go` already proves tenant-only installed script execution stays possible.
- [ ] **Step 3: Run RED.** `go test ./internal/agent/skills -run '^TestManagerBuiltin' -count=1` must fail on missing method/incorrect allowlist or executable duplicate.
- [ ] **Step 4: Implement manager source role separation.** Add `builtinSource *BuiltinSource`; `WithBuiltinSource` sets it before `Initialize`, and `IsBuiltinSkill` delegates to `.Has`. In `discoverAllSkills`, obtain built-ins, then existing tenant-or-host metadata, filter **only that list** with `filterAllowedSkills`, omit duplicates via `.Has`, concatenate in stable order. Remove the post-discovery allowlist pass from `Initialize`/`Reload`; `isSkillAllowed` returns true for known built-in before tenant allowlist; `resolveSource` prefers built-in. In `PrepareShellEnvironment`, before staging:

```go
if m.IsBuiltinSkill(skillName) {
    return "", nil, fmt.Errorf("skill %q is read-only and cannot execute", skillName)
}
```

Also have `SandboxSkillDir` return false for built-ins. Preserve tenant source over host fallback.
- [ ] **Step 5: Run GREEN and regressions.** `go test ./internal/agent/skills -count=1`, including `shell_environment_test.go`, `shell_staging_test.go`, `tenant_source_test.go`. Stage only Task 2 files, run `detect-changes --scope staged` and `git diff --cached --check`, commit `feat: compose readonly builtins with tenant skills`.

### Task 3: Attach built-ins and read_file to ordinary Smart Reasoning without sandbox

**Files:** Modify `internal/application/service/agent_service.go`, `internal/application/service/agent_service_test.go`; add `internal/application/service/builtin_legal_skills_test.go` if separating tests makes the existing file smaller.

**Interfaces:** Task 1 `NewBuiltinSource(legalskillassets.FS)`; Task 2 `WithBuiltinSource`. `CreateAgentEngine` offers built-ins for every `!config.SkillInstallMode()` regardless of `config.SkillsEnabled`; tenant source/host dirs only when tenant skills enabled. `read_file` is registered and `WithSkills` attached whenever built-ins exist. Registration of `shell_exec` remains under the old `SkillsEnabled || SkillInstallMode` gate. `errors.Is(err,skills.ErrInvalidBuiltin)` triggers fatal return for bundled asset defects; other tenant-init failure behavior is not silently changed.

- [ ] **Step 1: Run upstream impact for methods actually edited**, especially `CreateAgentEngine` and `initializeSkillsManager` in `agent_service.go`, and `registerSandboxFileTools` only if editing it. Report direct callers/flows/risk and warn before HIGH/CRITICAL.
- [ ] **Step 2: Write RED engine tests.** Use existing `fakeAgentChatModel`/`toolOffered` helpers and `&agentService{}` with `SandboxConfigID:"",SkillsEnabled:false` (the selection mode lives on `CustomAgent`, not `AgentConfig`; assert `none` in the separate session-scope test). Call `CreateAgentEngine`, then `Execute`; assert manager has exactly the four names, model tool set includes `read_file` but excludes `shell_exec`, `list_sandbox_files`, `write_sandbox_file`, `edit_sandbox_file`; separately call `initializeSkillsManager(ctx,"sess-1",cfg,registry)` using `tools.NewToolRegistry()`, get `registry.GetTool(tools.ToolReadFile)`, execute it with `json.RawMessage(`{"path":"skill://legal-document-summary/SKILL.md"}`)` and assert the result contains legal summary instructions. In installer subtest use `config.EnableSkillInstallMode(types.BuiltinSkillInstallerID, sandbox.SkillsImageRoot+"/pptx")` and assert `GetSkillsManager()==nil`. With `SkillsEnabled:true` plus tenant row, assert four built-ins + tenant row; with `SkillsEnabled:false` and injected tenant row, assert tenant row remains hidden. Existing tests which expected nil or exactly one tenant metadata must be updated to assert old *tenant* behavior plus four built-ins (do not weaken shell assertions).
- [ ] **Step 3: Run RED.** `go test ./internal/application/service -run 'TestBuiltinLegalSkills|TestCreateAgentEngine|TestSkillsManagerOffers|TestSkillToolsFollow' -count=1`; new test should fail on missing manager/reader.
- [ ] **Step 4: Implement smallest wiring.** In `initializeSkillsManager`, call `builtins, err := skills.NewBuiltinSource(legalskillassets.FS)` and attach `.WithBuiltinSource(builtins)`; return that error directly when malformed. Set `Enabled: true` for ordinary agent, but only copy `SkillDirs`, `AllowedSkills` and call `tenantSkillSource` if `config.SkillsEnabled`; use `if !config.SkillInstallMode()` at the `CreateAgentEngine` call site instead of the old `offerSkills` expression. Always attach reader to the manager:

```go
tool, err := toolRegistry.GetTool(tools.ToolReadFile)
if err != nil {
    toolRegistry.RegisterTool(tools.NewReadFileTool(nil))
    tool, _ = toolRegistry.GetTool(tools.ToolReadFile)
}
reader := tool.(*tools.ReadFileTool)
reader.WithSkills(skillsManager, shellEnabled)
```

If `errors.Is(err, skills.ErrInvalidBuiltin)`, propagate from `CreateAgentEngine`; otherwise retain its previous tenant/sandbox warning behavior. Leave `registerSandboxShellIfAllowed`, writer registration and Quick Answer code untouched.
- [ ] **Step 5: Run GREEN.** `go test ./internal/application/service -run 'TestBuiltinLegalSkills|TestCreateAgentEngine|TestSkillsManagerOffers|TestSkillToolsFollow' -count=1` and `go test ./internal/agent/... -count=1`. Stage Task 3 files only, `detect-changes --scope staged`, `git diff --cached --check`; commit `feat: offer builtin legal skills to Smart Reasoning agents`.

### Task 4: Make read-only instructions and optional @Skill priority honest

**Files:** Modify `internal/agent/tools/read_file.go`, its read-file tests; `internal/application/service/session_agent_qa.go`, `internal/application/service/agent_service.go`, and focused scope/pin tests (create `internal/application/service/builtin_legal_mentions_test.go` if needed).

**Interfaces:** `Manager.IsBuiltinSkill(name)` from Task 2. Built-in SKILL.md response states read-only/no script execution regardless of `shellEnabled`. `applyPerRequestSkillScope` may pin any requested available built-in even if tenant `SkillsSelectionMode=none`, but `resolvePinnedSkillInfos` only produces hints for actually available manager metadata (nil manager => no hints), with built-in description winning collision. Do not grant tools through pinning.

- [ ] **Step 1: Run upstream impact** for `ReadFileTool.readSkillResource`, `applyPerRequestSkillScope`, `agentService.resolvePinnedSkillInfos`, and `CreateAgentEngine` if moving its pin call. Report blast radius and warn for HIGH/CRITICAL before edits.
- [ ] **Step 2: Write RED tests.** Reader test with both `t.shell=true` and false must show `read_file(skill://legal-document-summary/SKILL.md)` contains `read-only` and does **not** contain `shell_exec(skill_name=` or instructions to configure sandbox; `skill://.../../other/SKILL.md` and an unknown name fail. Scope test `SkillsSelectionMode="none", SkillsEnabled=false, requested=["legal-document-summary","unknown"]` pins only summary. Pin resolver test tenant duplicate summary has a different description; result uses embedded description and drops unknown names. Existing tenant @mention tests must still pass.
- [ ] **Step 3: Run RED.** `go test ./internal/agent/tools ./internal/application/service -run 'TestBuiltinLegalReadFile|TestBuiltinLegalMention|TestPinnedSkill' -count=1` must fail on incorrect hint/scope/description.
- [ ] **Step 4: Implement minimal change.** In `readSkillResource`, branch before `if t.shell`:

```go
if t.skills.IsBuiltinSkill(name) {
    b.WriteString("Read-only instructions: this built-in skill has no executable scripts or sandbox directory.\n\n")
} else if t.shell { /* existing installed/host guidance */ } else { /* existing no-shell guidance */ }
```

In `applyPerRequestSkillScope`, classify requested built-in names via Task 1's `skills.IsBuiltinName` and retain those even for `skillsMode=="none"`/`SkillsEnabled==false`; apply existing `pinPreservingRequestOrder(requested,agentConfig.AllowedSkills)` only to tenant names and preserve request order without duplicates. In `resolvePinnedSkillInfos`, change signature to accept `manager *skills.Manager`, build `descByName` from `manager.GetAllMetadata()`, and skip names absent there; call it **after** `engine.SetSkillsManager` in `CreateAgentEngine`. This ensures selected tenant allowlist cannot pin a duplicate over built-in and unknown names receive no hint.
- [ ] **Step 5: Run GREEN.** `go test ./internal/agent/tools ./internal/application/service -run 'TestBuiltinLegal|TestPinnedSkill|TestApplyPerRequestSkillScope' -count=1`; run `go test ./internal/agent/skills -count=1`. Stage Task 4 paths only, run `detect-changes --scope staged` and `git diff --cached --check`, commit `fix: keep builtin skill reads and mentions read-only`.

### Task 5: Update docs and verify regressions without touching unrelated work

**Files:** Modify `examples/skills/README.md`, `docs/agent-skills.md`; optionally add a targeted service test covering prompt metadata if Task 3 tests only check manager/tool registration.

**Interfaces:** Document default Smart Reasoning availability, markdown-only read flow, name collision priority, deployment update, tenant catalog/UI distinction; preserve manual installation instructions for other skills and Quick Answer exclusion.

- [ ] **Step 1: Run RED (if prompt test missing).** Add an engine test asserting rendered system prompt includes all four names and `skill://<name>/SKILL.md` reader hint while not embedding all four instruction bodies. Execute with `fakeAgentChatModel`; inspect prompt capture helpers in `agent_service_test.go`/`internal/agent` first. If covered by Task 3, cite its exact test instead of adding a duplicate.
- [ ] **Step 2: Update docs with concrete usage.** Add a Smart Reasoning section to `examples/skills/README.md` and `docs/agent-skills.md`:

```markdown
Bốn skill pháp luật tích hợp có sẵn trong Smart Reasoning, không cần sandbox hay cài zip.
Agent đọc hướng dẫn qua `read_file(path="skill://legal-document-comparison/SKILL.md")`.
Các skill này chỉ-đọc, không được chạy bằng `shell_exec(skill_name=...)`.
`none`/`selected` chỉ kiểm soát skill tenant; nếu trùng tên, bản tích hợp được ưu tiên.
Sửa `SKILL.md` trong Git cần build và triển khai lại; Quick Answer/RAG chưa dùng các skill này.
```

Preserve existing README edits already in the worktree; do not overwrite the uncommitted abbreviation documentation.
- [ ] **Step 3: Run verification.** `gofmt -w` only modified Go files; `go test ./internal/agent/skills ./internal/agent/tools ./internal/application/service -count=1` and `go test ./... -count=1` (capture whether known fixture failures persist); `git diff --check`, `git status --short` to verify unrelated files unchanged. If suite fails, use systematic-debugging and compare failure with recorded baseline before classifying as unrelated.
- [ ] **Step 4: Review scoped diff.** Check no `shell_exec` granted by built-ins, no sandbox file tool unless existing sandbox already provided it, reserved names cannot stage a tenant duplicate, installer/Quick Answer untouched, default prompt contains metadata not full content. Apply focused fixes with fresh upstream impact before editing affected symbols.
- [ ] **Step 5: Stage docs and any test added in this task only; run `node .gitnexus/run.cjs detect-changes --scope staged` and `git diff --cached --check`; commit `docs: describe builtin legal skill availability`.** Report passing test commands, known unrelated full-suite failures, and resulting commit IDs.
