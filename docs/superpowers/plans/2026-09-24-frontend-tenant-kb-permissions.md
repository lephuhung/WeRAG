# Frontend Tenant and KB Permissions Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Next.js role and knowledge-base controls match the current backend's Admin/Member/SuperAdmin and per-recipient, read-only KB invitation rules.

**Architecture:** Keep server APIs authoritative and align UI visibility with them. A shared pure capability helper will derive own-tenant versus foreign KB affordances; existing tenant-role helpers will represent Admin separately from platform SuperAdmin. Retire frontend paths that request or administer tenant-wide KB grants and remove public-access messaging, while preserving decoding of legacy visibility data.

**Tech Stack:** Next.js 16, React 19, TypeScript, Node 22 built-in test runner, existing API client.

**Spec:** `docs/superpowers/specs/2026-09-24-frontend-tenant-kb-permissions-design.md`

## Global Constraints

- This is frontend-only; do not modify backend routes or authorization policy.
- Tenant roles are Admin and Member; SuperAdmin is the platform `is_system_admin` flag.
- Cross-tenant KB access is only for the specifically invited user and is read-only.
- Own-tenant Members retain read and new-document upload affordances; existing-content mutations stay Admin-only.
- A user's Admin role in the recipient tenant must never imply management authority in the KB-owning tenant.
- Preserve API-key capabilities as a separate machine-principal model.
- Preserve pre-existing worktree changes; do not reset, clean, stage, or commit.

---

## File Map

- `frontend-next/lib/kb-capabilities.ts` (new) — pure resource-aware UI capability projection from KB owner tenant, active tenant, and caller Admin status.
- `frontend-next/lib/kb-capabilities.test.ts` (new) — Node built-in tests for own/foreign Admin/Member capability matrix.
- `frontend-next/lib/api/knowledge.ts` — correct visibility and invite/grant API comments/types; remove obsolete grant client surface if no live callers remain; retain legacy `public` decoding only.
- `frontend-next/components/knowledge/kb-share-modal.tsx` — explain link authorization correctly; remove public state and obsolete grant-settings link.
- `frontend-next/components/settings/kb-access-grants.tsx`, `frontend-next/app/platform/system/workspace/sharing/page.tsx`, `frontend-next/components/settings/nav-config.ts` — replace old grant inbox/API calls with per-KB invitation guidance and retain Admin-only navigation.
- `frontend-next/components/knowledge/kb-invite-panel.tsx`, `frontend-next/components/knowledge/my-kb-invites.tsx` — make recipient membership and read-only semantics explicit; preserve one-time token behavior.
- `frontend-next/components/kb-detail.tsx`, `frontend-next/components/doc-panel.tsx`, `frontend-next/components/knowledge/doc-actions-menu.tsx`, `frontend-next/components/wiki/wiki-browser.tsx` — apply capability results to controls and prevent foreign source-download fallbacks.
- Frontend files with direct `owner` permission checks (including `frontend-next/components/settings/tenant-info.tsx`, workspace settings, and Wiki views) — update only if their matching backend route is TenantAdmin-gated; preserve platform-only or explicitly Owner-specific checks.

## Tasks

### Task 1: Add a tested resource-aware KB capability projection

**Files:**
- Create: `frontend-next/lib/kb-capabilities.ts`
- Create: `frontend-next/lib/kb-capabilities.test.ts`
- Test: `frontend-next/lib/kb-capabilities.test.ts`

**Interface produced:**

```ts
export interface KBCapabilities {
  isForeign: boolean;
  canManage: boolean;
  canUpload: boolean;
  canDownloadOriginal: boolean;
}

export function getKBCapabilities(
  kbTenantId: number | string | null | undefined,
  activeTenantId: number | string | null | undefined,
  isTenantAdmin: boolean,
): KBCapabilities;
```

- [ ] **Step 1: Write failing tests** in `lib/kb-capabilities.test.ts` with `node:test` and `node:assert/strict`. Assert own-tenant Member gets `{isForeign:false, canManage:false, canUpload:true, canDownloadOriginal:true}`; own-tenant Admin gets `canManage:true`; foreign Member and foreign Admin both get `{isForeign:true, canManage:false, canUpload:false, canDownloadOriginal:false}`; missing/zero IDs fail closed as foreign/no capabilities. Use a dynamic import that maps the not-yet-exported function to `undefined`, so RED is an assertion failure rather than module-resolution failure.
- [ ] **Step 2: Verify RED.** From `frontend-next`, run `node --experimental-strip-types --test lib/kb-capabilities.test.ts`; expected result is a clear failing assertion for the missing capability function.
- [ ] **Step 3: Implement the minimal helper.** Compare normalized non-empty string IDs; if either ID is missing, report foreign and deny all capability booleans. For a same-tenant KB, set `canManage` from `isTenantAdmin`, with upload and original-download true.
- [ ] **Step 4: Verify GREEN.** Run the same Node test command and confirm all capability cases pass.

### Task 2: Remove legacy sharing affordances and align API types/copy

**Files:**
- Modify: `frontend-next/lib/api/knowledge.ts`
- Modify: `frontend-next/components/knowledge/kb-share-modal.tsx`
- Modify: `frontend-next/components/settings/kb-access-grants.tsx`
- Modify: `frontend-next/app/platform/system/workspace/sharing/page.tsx`
- Modify: `frontend-next/components/settings/nav-config.ts`
- Modify: `frontend-next/lib/i18n.tsx` (EN/VI/ZH copy for the retired-share notices)
- Test: `frontend-next/lib/tenant-roles.test.ts`

- [ ] **Step 1: Inventory callers before removing client functions.** From `frontend-next`, run `rg -n "requestKBAccess|listIncomingKBGrants|listOutgoingKBGrants|reviewKBGrant|revokeKBGrant|updateKnowledgeBaseVisibility|KBAccessGrants" --glob '!node_modules/**'`; record remaining callers in the implementation review.
- [ ] **Step 2: Update the knowledge API model.** Document `tenant` as the only writable visibility. Keep `"public"` only as a legacy response value if callers need to deserialize old rows; ensure create/update payload types permit only `"tenant"`. Remove unused tenant-grant request/review/revoke/list clients and types only after Step 1 confirms there are no other consumers. Keep KB invitation functions and tenant invitation APIs separate.
- [ ] **Step 3: Fix the share modal.** Remove React visibility state and all public-visibility copy. Explain with localized EN/VI/ZH text that copying the URL does not grant access and only workspace members or the named invitee can open it. Remove the link to retired tenant-wide Sharing grants and link to `/platform/knowledge-bases`, where Tenant Admins can open a KB-specific Invite panel.
- [ ] **Step 4: Replace the old Sharing page without grant fetches.** Retain its route and Admin navigation for compatibility, but render a localized EN/VI/ZH explanation that sharing is managed per KB through a recipient-bound invite, with a link to `/platform/knowledge-bases`. Ensure the component no longer calls grant list endpoints and replace the obsolete `kbGrants.desc` translations that tell users to approve/request retired grants.
- [ ] **Step 5: Verify retired API callers are gone and run checks.** From `frontend-next`, run `rg -n "requestKBAccess|listIncomingKBGrants|listOutgoingKBGrants|reviewKBGrant|revokeKBGrant|visibility: \"public\"|linkNotePublic" --glob '!node_modules/**'`; expected result is no grant calls, public-write payloads, or obsolete public-link strings. Then run `node --experimental-strip-types --test lib/tenant-roles.test.ts lib/kb-capabilities.test.ts`; expected result is all tests pass.

### Task 3: Apply capabilities to KB list/detail and invitation experiences

**Files:**
- Modify: `frontend-next/app/platform/knowledge-bases/page.tsx`
- Modify: `frontend-next/components/kb-detail.tsx`
- Modify: `frontend-next/components/knowledge/kb-invite-panel.tsx`
- Modify: `frontend-next/components/knowledge/my-kb-invites.tsx`
- Modify: `frontend-next/components/wiki/wiki-browser.tsx`
- Modify: `frontend-next/lib/chat-context.tsx` (chat KB discovery uses invitation-aware `listKnowledgeBases()`)
- Modify: `frontend-next/lib/i18n.tsx` (EN/VI/ZH invitation guidance)
- Test: `frontend-next/lib/kb-capabilities.test.ts`

**Consumes:** Task 1's `getKBCapabilities` and `KBCapabilities` from `@/lib/kb-capabilities`.

- [ ] **Step 1: Verify the helper test still passes before integration.** Run `node --experimental-strip-types --test lib/kb-capabilities.test.ts` from `frontend-next`.
- [ ] **Step 2: Integrate the helper in KB list/detail.** Derive capabilities using each KB's `tenant_id`, active tenant ID, and `isTenantAdmin`. Use `isForeign` for badges and `canManage` for invite/settings/manage actions. Do not display a legacy public badge as if it granted access. Keep the own-tenant Member upload action available.
- [ ] **Step 3: Use invitation-aware KB discovery everywhere.** Inspect `components/agents/agent-editor.tsx`, `components/agents/im-channels.tsx`, the KB list, and chat's persisted KB selection. Ensure every user-facing KB selector calls `listKnowledgeBases()` (which includes accepted invitees' KBs); in `lib/chat-context.tsx`, replace the raw KB + legacy `/shared-knowledge-bases` merge with this single invitation-aware source. Do not add a second cross-tenant listing path. Keep stale local IDs subject to server authorization and verify invitation acceptance triggers a KB-list refresh.
- [ ] **Step 4: Gate nested Wiki mutations by resource capabilities.** Add an explicit `canMutate` prop to `WikiBrowser` and `WikiPageView` and thread it from `KbDetail`; require both own-tenant manage authority and Tenant Admin before rendering Wiki edit/mutation controls. Do not rely solely on the invitee's active-tenant role.
- [ ] **Step 5: Clarify invite forms and inbox copy.** Add localized EN/VI/ZH copy stating invitees must already be active members of the entered recipient workspace; emphasize read-only and that tenant invitations are separate. Preserve token-only-on-create, accept-by-ID, token redemption, and post-accept list refresh.
- [ ] **Step 6: Run focused role/capability tests.** Run `node --experimental-strip-types --test lib/tenant-roles.test.ts lib/kb-capabilities.test.ts` from `frontend-next`; expected result is all tests pass.

### Task 4: Enforce invited-KB preview/download and document action affordances

**Files:**
- Modify: `frontend-next/components/kb-detail.tsx`
- Modify: `frontend-next/components/doc-panel.tsx`
- Modify: `frontend-next/components/knowledge/doc-actions-menu.tsx`
- Modify: `frontend-next/components/doc-preview-modal.tsx`
- Test: `frontend-next/lib/kb-capabilities.test.ts`

**Consumes:** Task 1's capability projection.

- [ ] **Step 1: Confirm the existing policy regression covers download denial.** Run `node --experimental-strip-types --test lib/kb-capabilities.test.ts`; verify its foreign Member and foreign Admin cases both assert `canDownloadOriginal:false` before connecting those capabilities to UI controls.
- [ ] **Step 2: Pass resource capabilities to document controls.** Pass `canDownloadOriginal` into `DocPanel`, `DocActionsMenu`, and `DocPreviewModal`; pass `canMutate` only when the KB is own-tenant and caller is Tenant Admin. A foreign-tenant Admin must see neither mutation nor original-download actions.
- [ ] **Step 3: Keep preview read-only without a download fallback.** In `DocPanel`, call `previewKnowledgeFile` for the preview route. On preview failure, surface the preview error; only allow fallback to `downloadKnowledge` when `canDownloadOriginal` is true. Hide the standalone download button when false, and suppress `DocPreviewModal`'s download buttons for foreign KBs while leaving its preview path available.
- [ ] **Step 4: Verify focused tests.** Run `node --experimental-strip-types --test lib/kb-capabilities.test.ts` from `frontend-next`; expected result is all cases pass. Inspect rendered conditions and confirm `DocActionsMenu` still allows download for own-tenant Members without exposing mutation actions.

### Task 5: Reconcile role-gated frontend affordances with backend routes

**Files:**
- Inspect and, when route policy confirms TenantAdmin access, modify relevant files among `frontend-next/lib/auth.tsx`, `frontend-next/components/wiki/wiki-browser.tsx`, `frontend-next/components/settings/tenant-info.tsx`, `frontend-next/components/settings/abbreviations-settings.tsx`, `frontend-next/components/settings/chat-history-settings.tsx`, `frontend-next/components/settings/memory-workspace-settings.tsx`, `frontend-next/components/settings/skills-settings.tsx`, `frontend-next/components/settings/sandbox-settings.tsx`, `frontend-next/components/settings/tenant-orgs.tsx`, and corresponding system settings components.
- Test: `frontend-next/lib/tenant-roles.test.ts` and any existing pure unit test for a modified permission helper.

- [ ] **Step 1: Inventory direct role comparisons.** Run `rg -n 'currentRole\s*===\s*["\x27]owner|role\s*===\s*["\x27]owner|isOwner' components app lib --glob '*.{ts,tsx}'` from `frontend-next`.
- [ ] **Step 2: Compare each candidate gate with backend route middleware.** For each UI control, inspect its API wrapper route and matching `internal/router` registration. Replace the frontend gate with `isTenantAdmin` only when the backend uses TenantAdmin/Admin; retain SuperAdmin-only checks and do not broaden controls for endpoints with stricter policy.
- [ ] **Step 3: Add or extend pure role tests before changing shared role logic.** If `useTenantRole` or role-normalization logic changes, extend `lib/tenant-roles.test.ts` first, run it to observe the expected failure, then implement the minimum change and rerun it.
- [ ] **Step 4: Run role/capability regression tests.** Run `node --experimental-strip-types --test lib/tenant-roles.test.ts lib/kb-capabilities.test.ts`; expected result is all tests pass. Keep top-level system-only navigation gates unless changing them can be done without exposing unrelated platform configuration; report any Admin-accessible route that remains undiscoverable through navigation.

### Task 6: Frontend validation and worktree review

**Files:** All frontend files changed by Tasks 1–5; no backend source changes.

- [ ] **Step 1: Run all standalone frontend unit tests.** From `frontend-next`, run `node --experimental-strip-types --test $(find lib -name '*.test.ts' -print)`; expected result is exit code 0. If the runtime cannot strip types or a test fails, record the exact output and fix only failures introduced by this work.
- [ ] **Step 2: Run TypeScript validation.** From `frontend-next`, run `npm run typecheck`; expected result is `tsc --noEmit` exit code 0. Do not install/change dependencies without approval; if TypeScript is unavailable, report the limitation.
- [ ] **Step 3: Run the production frontend build.** From `frontend-next`, run `npm run build`; expected result is a successful Next.js production build. If environment configuration blocks the build, report the exact missing prerequisite.
- [ ] **Step 4: Review only this task's diff.** Run `git diff -- frontend-next` and `git status --short`; verify no backend files changed, no unrelated pre-existing changes were reverted, and no files were staged. Do not commit.
- [ ] **Step 5: Report outcomes and residual limitations.** Summarize implementation, test/build results, any existing dirty worktree preserved, and any validation or API limitations.

## Execution Notes

- Before modifying any existing function/component symbol, run GitNexus upstream impact analysis if the integration is available. The current agent tool surface did not expose GitNexus MCP; if still unavailable during execution, record that explicitly and inspect direct usages with `rg` before editing. Do not claim GitNexus impact was run when it was not.
- Keep one writer in the shared worktree. Execute tasks sequentially because they share the same UI files.
- No staging or commits: this plan honors the existing user instruction to preserve the dirty worktree.
