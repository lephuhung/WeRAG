# Platform Public Knowledge Bases Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore platform-owned public knowledge bases that every authenticated human can discover and read/download, while reserving creation and management to explicit SuperAdmins.

**Architecture:** Keep `knowledge_bases.tenant_id` as the immutable document/index execution partition and add `owner_tenant_id` as the authorization ownership field. Authorize by owner + visibility first, then continue using the stored data-scope tenant for content operations. Implement bounded public catalog discovery, a dedicated original-download permission path, SuperAdmin-only public lifecycle operations, and frontend controls derived from explicit system-admin status.

**Tech Stack:** Go, Gin, GORM, SQL migrations, PostgreSQL/SQLite tests, Next.js/TypeScript.

**Spec:** `docs/superpowers/specs/2026-09-24-platform-public-knowledge-bases-design.md`

## Global Constraints

- Public means authenticated human users only; anonymous access remains denied.
- `tenant_id` remains the data-scope/execution partition and is never rewritten by a scope transition.
- `owner_tenant_id > 0` means tenant-owned; `owner_tenant_id = 0` means platform-owned.
- Public visibility requires `owner_tenant_id = 0`; tenant visibility requires `owner_tenant_id > 0`.
- `CanAccessAllTenants` is not SuperAdmin authority; require explicit `is_system_admin`.
- Public visibility does not bypass API-key identity, explicit KB allowlists, or ingest/retrieve/manage capabilities.
- Preserve recipient-bound KB invitations and tenant-join invitations as separate features.
- A scope transition updates KB metadata only: no child-row tenant rewrite, reindex, or background job.
- Tenant deletion is rejected if any active KB uses the tenant as its data scope; return a useful error identifying the blocking KB.
- Preserve the existing dirty worktree. Do not stage, commit, reset, or clean any changes.

---

## File Map

- `internal/types/knowledgebase.go`: add `OwnerTenantID`; extend `KBScope` and owner/visibility invariants.
- `migrations/versioned/000113_platform_public_knowledge_bases.{up,down}.sql`: add/backfill/index/check ownership metadata without modifying data-scope IDs.
- `internal/application/repository/knowledgebase.go` and `internal/types/interfaces/knowledgebase.go`: owner-scoped access/catalog queries and data-scope reference query.
- `internal/application/access/knowledgebase.go`, `context.go`, `internal/middleware/kb_access.go`, `internal/router/routes_knowledge.go`: permission resolution, dedicated public original-download access, and route policies.
- `internal/application/service/knowledgebase.go`: owner-aware create/list/scope-transition behavior, platform defaults, and audit metadata.
- `internal/application/service/tenant.go`, `internal/application/repository/tenant.go`: block deletion when KB rows still depend on its data scope.
- `internal/handler/knowledgebase.go` plus download handlers: accept scope-change destination, expose owner in responses, and use the recorded data scope for reads/downloads.
- `frontend-next/lib/api/knowledge.ts`, capability/auth helpers, KB list/create/detail components: owner-aware rendering, public grouping/labels, and SuperAdmin controls.
- Focused Go and TypeScript tests beside the code above; migration coverage follows repository migration-test conventions.

## Task 1: Ownership Metadata and Migration

**Files:**
- Create: `migrations/versioned/000113_platform_public_knowledge_bases.up.sql`
- Create: `migrations/versioned/000113_platform_public_knowledge_bases.down.sql`
- Modify: `internal/types/knowledgebase.go`
- Modify: `internal/types/interfaces/knowledgebase.go` (scope projection only if needed)
- Test: `internal/application/repository/knowledgebase_sqlite_test.go` and migration test location established in this repository

**Interfaces:** `KnowledgeBase.OwnerTenantID uint64` serializes as `owner_tenant_id`; `KBScope` contains both `OwnerTenantID` and `TenantID`, where `TenantID` remains the execution scope.

- [ ] Write migration/model tests proving old tenant KB rows backfill `owner_tenant_id = tenant_id`, `tenant_id` values do not change, and invalid owner/visibility combinations are rejected.
- [ ] Run the focused tests and confirm the owner-column/schema assertions fail before the migration/model change.
- [ ] Add `owner_tenant_id` with a safe rollout default/backfill; add database checks for owner/visibility consistency where supported by the project's dialect/migration pattern, plus an index serving owner + visibility catalog queries.
- [ ] Add the model field and scope projection; ensure existing KB test fixtures/schema setup include the new column or run migrations as appropriate.
- [ ] Run `go test ./internal/application/repository ./internal/types/...` and focused migration tests.

## Task 2: Owner-Based Authorization and Read/Download Semantics

**Files:**
- Modify: `internal/application/access/knowledgebase.go`, `internal/application/access/context.go`
- Modify: `internal/application/repository/knowledgebase.go`, `internal/types/interfaces/knowledgebase.go`
- Modify: `internal/middleware/kb_access.go`, `internal/router/routes_knowledge.go`
- Modify: original-download/preview handlers and their route registrations (trace existing KB-guarded handlers before editing)
- Test: `internal/application/access/knowledgebase_test.go`, `internal/middleware/kb_access_test.go`, `internal/handler/knowledge_download_test.go`, route tests

**Interfaces:** Access checks consume `KBScope.OwnerTenantID` and `Visibility`; successful reads retain `KBScope.TenantID` as the effective content tenant. Add a download-level permission decision distinct from write permission: public viewers may download originals; invitation viewers keep existing no-download behavior; same-owner-tenant members keep tenant policy.

- [ ] Add failing authorization tests for authenticated cross-tenant public read, unauthenticated denial, Tenant Admin/member cross-tenant writes denied, explicit SuperAdmin management allowed only through the privileged route, and `CanAccessAllTenants` alone denied.
- [ ] Add failing download tests proving public authenticated users can retrieve original bytes through the recorded data scope, while anonymous and uninvited cross-tenant private users cannot.
- [ ] Update repository scope projection and access resolution to distinguish owner from data scope; public read grants Viewer only and never expands API-key allowlists/capabilities. Explicit SuperAdmins with no active tenant must still resolve public KBs; ordinary access continues to require authenticated tenant/user context.
- [ ] Add a dedicated public-capable original-download guard and apply it only to original-download routes; leave invitation download policy unchanged and keep preview/read routes behind Viewer access.
- [ ] Verify all content mutations (KB, document, chunk, FAQ, tags, Wiki, invitations, reparse) still reject non-SuperAdmin callers on platform-owned public KBs; add a KB-aware manager guard that permits only the owning tenant's existing role policy or an explicit human SuperAdmin for platform-owned rows. Do not globally make `KBAccessWrite` treat public Viewer as editor.
- [ ] Run focused Go tests for access, middleware, handlers, and router.

## Task 3: Public Lifecycle, Scope Changes, Auditing, and Tenant Deletion Safety

**Files:**
- Modify: `internal/application/service/knowledgebase.go`, `internal/handler/knowledgebase.go`
- Modify: `internal/router/routes_knowledge.go`, `internal/router/rbac.go` only as required for explicit SuperAdmin route guards
- Modify: `internal/application/service/tenant.go`, `internal/application/repository/tenant.go`, tenant repository/service interfaces if needed
- Test: knowledge-base service/handler/RBAC tests and tenant deletion service/repository tests

**Interfaces:** Scope-change request is `{visibility, target_tenant_id?}`. `public → tenant` requires an existing nonzero destination. Tenant create uses active tenant for both owner and data scope. SuperAdmin public create sets owner and data scope to zero and uses platform defaults. Scope transition preserves ID and data scope.

- [ ] Add failing service/route tests: Tenant Admin can create tenant KB but cannot create/promote to public; explicit SuperAdmin can create public or change scope without requiring tenant Admin membership; `CanAccessAllTenants` alone cannot; public-to-tenant rejects missing/invalid target and does not mutate the row.
- [ ] Add tests proving tenant→public and public→tenant change only owner/visibility metadata, preserving KB ID, `tenant_id`, and child/index tenant IDs; audit records actor, old/new scope, target tenant, KB ID, and outcome.
- [ ] Add failing tests that deleting a tenant is rejected with the blocking KB ID while any active KB has `tenant_id` equal to that tenant (including public-owned KBs); deletion with no dependent KB remains unchanged.
- [ ] Implement service validation and lifecycle behavior. Use explicit system-admin identity, never `CanAccessAllTenants`. Resolve public-create defaults from platform config and do not inherit selected-tenant defaults.
- [ ] Apply route guards so tenant-owned operations preserve tenant role gates while public-owned operations require a human explicit SuperAdmin, including callers without an active tenant; API-key `manage_kbs` does not become SuperAdmin and cannot create/manage public scope. Keep create handling distinct so tenant admins create only tenant-owned KBs and explicit SuperAdmins may use the public-create flow.
- [ ] Implement transactional tenant-deletion dependency check in the tenant repository and map the domain error to a clear handler response.
- [ ] Run focused knowledge-base and tenant service/handler/router tests.

## Task 4: Catalog Discovery, Search, and Data-Scope Preservation

**Files:**
- Modify: `internal/application/repository/knowledgebase.go`, `internal/types/interfaces/knowledgebase.go`
- Modify: `internal/application/service/knowledgebase.go` and knowledge search/discovery entry points identified by call sites
- Modify: KB/list/search handlers if pagination or response metadata is needed
- Test: repository and service list/search tests; handler pagination/API-scope tests

**Interfaces:** The user-facing KB catalog combines active-owner-tenant KBs, platform-owned public KBs, and accepted recipient-bound invitations, deduplicated by KB ID and bounded/paginated. Internal `ListKnowledgeBasesByTenantID` remains data/agent scoped and must not implicitly gain all public KBs.

- [ ] Add failing tests for catalog composition, deduplication, temporary/deleted KB exclusion, public pagination/bounds, and API-key allowlist filtering.
- [ ] Add owner-scoped repository queries for a tenant's owned KBs and platform public KBs; retain separate data-scope methods used by agents and execution pipelines.
- [ ] Update the main user-facing KB listing to append the public catalog plus accepted invites without leaking tenant-owned KBs; preserve pin behavior and creator filters.
- [ ] Trace chat `@` discovery and backend search paths; include public KBs only for authenticated human caller flows with permitted KB IDs, preserving execution queries on each KB's `tenant_id`.
- [ ] Run focused service/repository/handler tests and relevant search tests.

## Task 5: Frontend Public Catalog and SuperAdmin UX

**Files:**
- Modify: `frontend-next/lib/api/knowledge.ts`, `frontend-next/lib/kb-capabilities.ts`, auth/role helpers
- Modify: `frontend-next/app/platform/knowledge-bases/page.tsx`, `frontend-next/app/platform/knowledge-bases/new/page.tsx`, KB detail/list components
- Test: adjacent standalone TypeScript tests for KB capabilities, listing, and create/scope controls

**Interfaces:** API `KnowledgeBase` includes `owner_tenant_id`; UI determines public ownership from owner+visibility, not data `tenant_id`. Explicit `is_system_admin` controls public create/manage affordances. Public readers see read/preview/original-download actions only.

- [ ] Add failing frontend tests for tenant/public/invited grouping and dedupe; SuperAdmin vs Tenant Admin vs Member action visibility; public original-download visibility; and public upload/mutation controls hidden from non-SuperAdmins.
- [ ] Update API types and capability helpers to distinguish `owner_tenant_id` from `tenant_id` and explicit system-admin state from cross-tenant access.
- [ ] Add public labels/catalog presentation and public creation/scope UI; require destination tenant when a SuperAdmin moves a public KB into tenant scope.
- [ ] Keep tenant-admin controls for their KBs, invitation behavior, and API-key permissions unchanged.
- [ ] Run relevant frontend standalone tests, `npm run typecheck`, and `npm run build`.

## Task 6: Integration Verification and Review

**Files:** No planned product-code edits unless review uncovers a defect; preserve all unrelated dirty files.

- [ ] Run `go test ./internal/...` and `go build ./...`.
- [ ] Run frontend standalone tests, `npm run typecheck`, and `npm run build`.
- [ ] Run `git diff --check` and inspect changed-file/symbol scope against this plan; do not stage or commit.
- [ ] Confirm no anonymous public access, no API-key privilege widening, no tenant-data rewrite/reindex, and no loss of recipient-bound invitation semantics.
- [ ] Report tests, residual risks, and any GitNexus/tooling unavailability. Before any completion claim, verify command output and run the available change-scope detection requested by repository policy; do not commit.
