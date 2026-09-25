# Tenant and Knowledge Base Permissions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce exactly three human authorization roles—SuperAdmin, Tenant Admin, and Member—while supporting tenant-scoped KBs, member ingestion, per-member cross-tenant read invitations, and memberships in multiple tenants.

**Architecture:** Keep system-wide authority separate from a user's per-tenant membership, but expose exactly three human roles: `IsSystemAdmin` is SuperAdmin; each `(user_id, tenant_id)` membership is Admin or Member. Tenant Admin owns KB lifecycle and member invitations; Member can retrieve tenant KBs and ingest into KBs owned by their tenant. Cross-tenant KB invitations create read-only grants bound to an individual recipient, not the entire tenant. Tenant join invitations remain a separate flow that creates another membership.

**Tech Stack:** Go, Gin, GORM, PostgreSQL migrations, Next.js/React/TypeScript, Go tests, frontend tests.

**Spec:** Approved requirements in the conversation, captured in the acceptance criteria below.

## Global Constraints

- Human authorization has exactly three roles: SuperAdmin, Tenant Admin, Member.
- A person's role belongs to a membership; one user may be a Member of multiple tenants, with each membership independent.
- Only a Tenant Admin may create or manage knowledge bases in their own tenant.
- A Member may read/search tenant KBs and upload documents to KBs owned by their tenant; they may not create/delete/configure KBs or edit/delete existing KB contents.
- Cross-tenant KB sharing is read-only and applies only to the specifically invited member after accepting the KB invitation.
- A tenant invitation adds a Member membership and does not replace any existing membership or change the user's current tenant.
- API-key capabilities remain a distinct machine-principal authorization mechanism, not additional human roles.
- Existing user changes in the dirty worktree must not be overwritten or included in implementation commits.

---

## Acceptance Criteria

1. Persisted/returned human roles are only `admin` and `member` at tenant scope; SuperAdmin is the platform-level role, never a tenant membership role.
2. Legacy `owner`, `contributor`, and `viewer` memberships migrate safely to Admin or Member. Legacy `CanAccessAllTenants` accounts are inventoried and mapped under an explicit, reviewed rule; no account gains SuperAdmin implicitly without authorization.
3. A Tenant Admin can create, configure, delete, and change membership/invitation state only in their tenant. A Member cannot perform those operations, including when the RBAC enforcement flag is disabled.
4. A Member can ingest/upload to a KB in their own tenant but cannot mutate existing KB settings, documents, chunks, FAQ entries, tags, or wiki pages unless an operation is specifically classified as ingestion.
5. KBs default to tenant-only visibility. Cross-tenant read access is granted only to the authenticated recipient bound to an accepted KB invite. A foreign Member cannot write, upload, mutate, re-share, or manage that KB.
6. An Admin can issue/revoke a KB invitation for a specific user in another tenant; accepting requires authentication as that recipient. Another user, even in the same tenant, cannot redeem/use the invitation as the recipient.
7. Tenant invitation links are separate: accepting one creates a Member membership in that tenant while preserving all existing memberships and the user's existing tenant selection.
8. Requests for a KB or tenant outside the caller's grants/memberships fail closed; list/search/chat/file-download routes do not leak inaccessible KBs or content.

## File Map

- `internal/types/tenant_member.go`, `internal/types/user.go`, `internal/types/kb_access_grant.go`: role and access model.
- `internal/middleware/rbac.go`, `internal/middleware/access.go`, `internal/router/rbac.go`: principal and role enforcement.
- `internal/application/access/knowledgebase.go`, `internal/application/access/kb_write.go`, `internal/application/service/knowledgebase.go`, `internal/application/service/knowledgebase_access.go`: KB resolution, create/manage, read/write policy.
- `internal/router/routes_knowledge.go`, `internal/router/routes_agent.go`, `internal/handler/knowledgebase.go`, `internal/handler/kb_access_grant.go`: route and HTTP policy adapters.
- `internal/application/service/kb_access_grant.go`, `internal/types/interfaces/kb_access_grant.go`, new repository/type files as needed: recipient-bound KB invite lifecycle.
- `internal/application/service/tenant_invitation.go`, `internal/handler/tenant_invitation.go`, `internal/handler/tenant_invite_link.go`, `internal/router/routes_auth_tenant.go`: separate tenant membership invitation behavior.
- `internal/application/repository/tenant_member.go`, `internal/application/repository/knowledgebase.go`, relevant migrations: data migration and query scopes.
- `frontend-next/lib/api/tenants.ts`, `frontend-next/lib/api/knowledge.ts`, `frontend-next/lib/auth.tsx`, KB and tenant settings components: UI/API role names and invite flows.
- Tests beside each changed Go/TypeScript file; add migration integration coverage where practical.

## Implementation Tasks

### Task 1: Normalize human role model and legacy memberships

**Files:** `internal/types/tenant_member.go`, `internal/types/user.go`, `internal/middleware/rbac.go`, `internal/router/rbac.go`, `internal/handler/auth.go`, tenant member/invitation tests, new `migrations/versioned/000110_tenant_three_roles.up.sql` and `.down.sql` (use the next available migration number if the branch changes).

- [ ] Before changing any existing symbol, run GitNexus `impact({target: "TenantRole", direction: "upstream"})`, `impact({target: "RequireRole", direction: "upstream"})`, and `impact({target: "IsCrossTenantSuperuser", direction: "upstream"})`; record callers and risks in the implementation review. If MCP is unavailable, use the indexed CLI/source references and record that limitation.
- [ ] Add table-driven tests for valid tenant roles (Admin, Member only), role thresholds, SuperAdmin access, tenant Admin isolation, and role absence defaulting to Member.
- [ ] Run the focused tenant-role/auth tests and verify the tests expose owner-only and `CanAccessAllTenants` legacy behavior before implementation.
- [ ] Define a reviewed migration mapping: owner/admin -> admin; contributor/viewer/member -> member. Inventory `CanAccessAllTenants` and `IsSystemAdmin`; require an explicit mapping decision for each legacy cross-tenant authority before any automatic promotion.
- [ ] Add reversible migration SQL for role-value conversion and any approved SuperAdmin representation. Preserve the existing `(user_id, tenant_id)` uniqueness and membership status columns.
- [ ] Update role constants/validation and auth projection so tenant roles cannot serialize as `owner`, `contributor`, or `viewer`; use the platform SuperAdmin flag for system-wide authorization rather than a tenant role.
- [ ] Remove accidental role bypasses through `CanAccessAllTenants` unless the account is deliberately designated SuperAdmin. Keep API-key principal checks separate.
- [ ] Run `go test ./internal/types ./internal/middleware ./internal/handler ./internal/application/service -run 'TenantRole|TenantMember|Invitation|Auth'` and the migration test target.

### Task 2: Establish tenant-private KB lifecycle and Admin-only management

**Files:** `internal/router/routes_knowledge.go`, `internal/application/service/knowledgebase.go`, `internal/application/access/knowledgebase.go`, `internal/handler/knowledgebase.go`, visibility tests, new migration if visibility/grant data must be transformed.

- [ ] Run GitNexus impact analysis on `CreateKnowledgeBase`, `SetKnowledgeBaseVisibility`, `ResolveKB`, and the `KBVisibility` type before editing these symbols; report direct callers and risk.
- [ ] Add failing route/service tests proving tenant Admin can create/manage own KB, Member cannot create/delete/configure one, and a Tenant Admin/SuperAdmin cannot manage another tenant's KB by changing tenant headers.
- [ ] Change the create/manage guards to the Admin role and make these denials unconditional; the RBAC rollout-off mode must not bypass KB create/delete/configuration authorization.
- [ ] Make `tenant` visibility the only default, tenant-private scope. Reject or safely normalize new `public` visibility writes. Do not use frontend-only restrictions as authorization.
- [ ] Decide and test treatment of existing public KBs and current tenant-wide grants before migration. Default safe behavior is to narrow public KBs to tenant visibility and not silently preserve tenant-wide grants as individual invitations.
- [ ] Verify KB detail, search, list, chat, file preview/download, and retrieval paths all use the same fail-closed scope policy.
- [ ] Run `go test ./internal/application/access ./internal/application/service ./internal/handler ./internal/router -run 'KnowledgeBase|KBAccess|Visibility|Search'`.

### Task 3: Separate Member ingestion from KB management and editing

**Files:** `internal/router/routes_knowledge.go`, `internal/router/routes_infra.go`, `internal/application/access/kb_write.go`, `internal/application/service/knowledgebase_access.go`, knowledge/document/chunk/FAQ/wiki handlers and route tests.

- [ ] Run GitNexus impact analysis for `RequireKBWrite`, `kbWritableIDs`, and `KBAccessWrite` before changing their policy.
- [ ] Add route matrix tests for a Member uploading file/URL/manual content into own-tenant KB (allowed), Member editing/deleting existing content or KB settings (denied), Admin content management (allowed), and cross-tenant invitee upload/write (denied).
- [ ] Define ingestion endpoints narrowly (new file/URL/manual document and FAQ import if applicable) and use a Member-level write/ingest guard only on those routes.
- [ ] Keep update/delete/reparse/chunk edits/FAQ entry edits/wiki edits/tag changes and KB configuration behind Admin-only or explicitly designated KB-management checks.
- [ ] Ensure a broad KB editor permission or an accepted read invitation cannot be upgraded to write by a shared agent, API key scope, request tenant header, or worker context.
- [ ] Test the same matrix with RBAC rollout enforcement enabled and disabled; resource-scoped authorization must remain enforced in both modes.
- [ ] Run focused router, access, and service tests for document, chunk, FAQ, wiki, and file ingestion.

### Task 4: Implement recipient-bound, read-only KB invitations

**Files:** replace or extend `internal/types/kb_access_grant.go`, `internal/types/interfaces/kb_access_grant.go`, `internal/application/repository/kb_access_grant.go`, `internal/application/service/kb_access_grant.go`, `internal/handler/kb_access_grant.go`, `internal/router/routes_agent.go`, `internal/application/access/knowledgebase.go`; add migration and tests.

- [ ] Run GitNexus impact analysis for `RequestAccess`, `ApprovedKBPermission`, `ResolveKB`, and grant routes before refactoring.
- [ ] Add failing tests that show current tenant-wide grant behavior is too broad: an unrelated member in the grantee tenant must not gain access to an invite for another user.
- [ ] Add a KB invitation record bound to `kb_id`, owner tenant, recipient user (and recipient tenant for validation), inviter, status, expiry, and hashed random token. Store no raw bearer token.
- [ ] Add Admin-only create/list/revoke operations scoped to the KB-owning tenant. Require a real Member account in another tenant as the intended recipient.
- [ ] Add authenticated invitation lookup/accept routes. Acceptance must verify token hash, expiry/status, current recipient user ID, and current active membership in the intended tenant; consume/activate atomically and return a non-enumerating error for mismatched recipients.
- [ ] Resolve KB reads for an external caller only through their accepted individual invitation; keep permission read-only. Revocation must immediately deny future API reads/search and not alter tenant membership.
- [ ] Remove or retire tenant-wide KB request/review paths once legacy data migration behavior is approved. Keep audit events for issue, accept, and revoke.
- [ ] Add tests for valid recipient, wrong user, wrong tenant, expired/revoked/replayed token, owner admin isolation, immediate revoke, and proof that foreign writes are denied.
- [ ] Run `go test ./internal/application/access ./internal/application/repository ./internal/application/service ./internal/handler ./internal/router -run 'KB.*(Invite|Grant|Access)|KnowledgeBase'`.

### Task 5: Preserve multi-tenant memberships and keep tenant invitations separate

**Files:** `internal/application/service/tenant_invitation.go`, `internal/handler/tenant_invitation.go`, `internal/handler/tenant_invite_link.go`, `internal/router/routes_auth_tenant.go`, `internal/application/repository/tenant_member.go`, `internal/handler/auth.go`, frontend auth/tenant APIs and tenant invitation UI.

- [ ] Run GitNexus impact analysis for `AcceptByToken`, `AddMember`, `GetMemberships`, and tenant switching before edits.
- [ ] Add regression tests proving a user in tenant A can accept a tenant-B Member link, retain both membership rows, keep roles independent, switch active tenant, and retain A's authorization after switching back.
- [ ] Require Tenant Admin authority to create/revoke tenant invitation links; issue Member-role invitations for the described member flow. Any ability to invite an Admin must be Admin-only and explicit.
- [ ] Ensure accepting a tenant invite changes only membership/invitation state; do not overwrite `User.TenantID` or delete other memberships. Active tenant selection remains client/session selection, validated against memberships.
- [ ] Preserve the separation between tenant invitation acceptance and KB share invitation acceptance, with separate routes, tokens, storage, audit actions, and UI copy.
- [ ] Run focused tenant invitation, auth, and tenant-switch tests plus their frontend tests.

### Task 6: Align frontend role gates, KB sharing UI, and end-to-end matrix

**Files:** `frontend-next/lib/api/tenants.ts`, `frontend-next/lib/api/knowledge.ts`, `frontend-next/lib/auth.tsx`, `frontend-next/app/platform/knowledge-bases/**`, `frontend-next/components/knowledge/**`, `frontend-next/components/settings/tenant-members.tsx`, organization/tenant switcher components, UI tests.

- [ ] Add tests for role labels and affordances: Admin sees KB lifecycle controls; Member sees retrieval and allowed ingestion only; SuperAdmin sees platform controls; external invited Member sees read-only KB.
- [ ] Change TypeScript tenant-role unions and membership projections to Admin/Member, with system SuperAdmin represented as a platform flag/role rather than a tenant membership.
- [ ] Replace the current copy-link/visibility toggle UX with Admin-only issue/revoke invite for a selected recipient; show invite status and make its read-only and tenant scope explicit.
- [ ] Keep tenant join links in tenant member settings; accepting one must refresh memberships and allow selecting the new tenant without dropping the old one.
- [ ] Remove `public` visibility and tenant-wide grant UI after the backend compatibility/migration decision is applied.
- [ ] Run frontend lint/typecheck and affected tests from `frontend-next`; run backend `go test ./...` or documented CI test subsets.
- [ ] Add a cross-layer acceptance test or reproducible manual matrix covering SuperAdmin, Tenant Admin, own-tenant Member, invited foreign Member, non-invited foreign Member, and Member with memberships in two tenants.
- [ ] Run GitNexus `detect_changes()` before any implementation commit and review whether affected processes match this plan. Do not stage pre-existing unrelated dirty files.

## Migration and Rollout Gate

Before deploying schema/data changes, capture counts and sample IDs for `owner/admin/contributor/viewer/member` memberships, `CanAccessAllTenants`, `IsSystemAdmin`, public KBs, and pending/approved tenant-wide KB grants. Review the mapping and user impact with the product owner. Apply the migration to a copy of production data, verify role counts and that only explicitly invited users retain cross-tenant access, then roll out route enforcement. Keep migration rollback scripts and do not promote legacy cross-tenant accounts to SuperAdmin without an approved list.
