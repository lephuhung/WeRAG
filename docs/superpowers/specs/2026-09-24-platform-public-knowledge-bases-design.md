# Platform Public Knowledge Bases

**Status:** Draft for user review  
**Supersedes:** The public-retirement decision in `2026-09-24-frontend-tenant-kb-permissions-design.md` for the scope of platform-owned public KBs. Recipient-bound invitations remain supported for tenant-owned private KBs.

## Goal

Allow an explicit platform library of public knowledge bases (for example, public laws, decrees, and circulars) that appears to every authenticated user. A public KB has no owning tenant. Users across tenants can read and download original files, but only a platform SuperAdmin can create or manage public content. Tenant-owned KB behavior remains unchanged.

## Approved Product Rules

- Public means readable and searchable by any authenticated human user in the platform, across tenants. It does not mean anonymous internet access.
- Public KBs appear alongside the active tenant's KBs and recipient-bound invited KBs in the KB view and discovery/search flows, with an explicit Public label and no duplicate entries.
- All authenticated users may read, preview, and download original files from public KBs. Non-SuperAdmins cannot upload, edit, delete, reparse, move, invite to, or otherwise mutate a public KB.
- Only an explicit platform SuperAdmin (`is_system_admin`) may create a public KB or change a KB's owner/scope. `CanAccessAllTenants` alone is not SuperAdmin authority.
- A SuperAdmin moving a public KB to tenant scope must choose the destination tenant. The same KB ID is retained.
- Tenant-owned KBs retain current policy: their members can read and upload new content; their Tenant Admins manage the KB and existing content. Cross-tenant invitations remain recipient-bound and read-only; their download policy is unchanged.
- API-key capabilities and KB allowlists remain independent. Public visibility does not grant an API key additional KB scope or capabilities.

## Ownership and Data Scope

The current `knowledge_bases.tenant_id` is used both as an ownership signal and as a data-execution/search partition. Changing it on a KB row alone would make document and index lookups inconsistent. Do not rewrite tenant IDs on documents, chunks, or indexes when changing a KB's public/tenant scope.

Add an explicit `owner_tenant_id` to the KB model and API response:

- `owner_tenant_id > 0`: the KB is owned by that tenant.
- `owner_tenant_id = 0`: the KB is platform-owned and has no tenant owner.
- Keep the existing `tenant_id` as the immutable data-scope/execution partition for the KB's documents and indexes. For tenant KBs created normally, it initially matches `owner_tenant_id`. Platform-created public KBs use the reserved platform data scope `tenant_id = 0`.
- `visibility = "tenant"` requires `owner_tenant_id > 0`; `visibility = "public"` requires `owner_tenant_id = 0`. Enforce this invariant in service validation and the database where supported.
- Backfill existing KBs with `owner_tenant_id = tenant_id`. Migration 000111 already converted historical public rows to tenant visibility; do not automatically republish them.

All authorization decisions use the KB's `owner_tenant_id` and `visibility`, never its data-scope `tenant_id`. After that authorization succeeds, content retrieval uses the stable data scope and KB ID. Public scope changes update the KB ownership/visibility metadata transactionally; they do not move child rows, rebuild vector indexes, or launch a background job.

Data-scope behavior on conversion is intentionally stable: publishing an existing tenant KB leaves its existing data/storage scope intact; moving a public KB to a tenant also leaves its platform data scope intact. New public KBs use platform defaults for models, storage, and indexing, never the currently selected tenant's defaults. Rebinding existing data to a different storage/index configuration is a separate future operation, not part of changing visibility.

## Authorization and API Behavior

- `GET /knowledge-bases` (or its successor list contract) returns tenant-owned KBs, all public KBs, and the caller's accepted invited KBs, deduplicated and bounded/paginated for large public catalogs.
- Detail, search, preview, source download, and other read endpoints resolve authorization from the parent KB. Public viewers receive read permission; tenant membership and accepted invitations keep their existing rules.
- Content-write routes must require platform SuperAdmin for platform-owned public KBs. Tenant-owned KB write rules remain role-gated by the owning tenant. A caller's role in another tenant never grants mutation access to a public KB.
- KB create/update visibility payloads are validated server-side. Tenant Admins may create tenant KBs but cannot create or promote a KB to public. Only explicit SuperAdmin routes may create public KBs or change owner/scope.
- Public visibility does not bypass API-key identity, explicit KB-ID allowlists, or ingest/retrieve capabilities.
- The visibility/owner transition records an audit event with actor, prior/new scope, target tenant when present, KB ID, and outcome.

## Create and Scope-Change Flows

- **Create tenant KB:** existing flow; set `owner_tenant_id` and data-scope `tenant_id` to the active tenant.
- **Create public KB:** SuperAdmin-only flow; set `owner_tenant_id = 0`, `visibility = "public"`, and data-scope `tenant_id = 0`; resolve all defaults from platform configuration, not an active tenant.
- **Tenant → public:** SuperAdmin-only; set `owner_tenant_id = 0` and `visibility = "public"`; preserve KB ID and data scope.
- **Public → tenant:** SuperAdmin selects a valid destination tenant; set `owner_tenant_id` to that tenant and `visibility = "tenant"`; preserve KB ID and data scope. Reject missing/invalid target tenant IDs without changing state.

## Frontend

- Add public-scope labels and a public KB section/entries in the KB list; show tenant-owned and invited KBs in their own categories and deduplicate by KB ID.
- Show public creation/scope controls only to explicit SuperAdmins. Tenant Admins continue to manage their own tenant KBs but cannot promote them to public.
- Public readers see preview and original-download actions but no upload or mutation controls. Tenant scope transfers prompt for a destination tenant.
- Capability helpers must distinguish KB ownership (`owner_tenant_id`) from data execution scope (`tenant_id`). They are display gates only; backend authorization remains authoritative.

## Compatibility and Failure Handling

- Migration 000111 remains applied; no prior public markers are restored implicitly.
- Existing recipient-bound KB invitations and tenant-join invitations remain separate and unchanged.
- Scope transitions fail closed: a failed database update leaves the previous owner/visibility intact. No child-data migration is performed by the transition.
- Since a tenant-owned KB published as public can retain that tenant's data scope/storage, tenant deletion must not silently invalidate those resources. The implementation plan should choose a safe v1 rule (preferably reject tenant deletion while an active KB still uses its data scope, with a clear reference to the KB).
- If public read data includes tenant-scoped object storage, all download/preview handlers must use the KB's recorded data scope after public authorization and must not grant access to unrelated KBs in that tenant.

## Validation

- Migration tests: backfill existing owners; preserve all data-scope IDs; enforce public/platform-owner and tenant/tenant-owner invariants.
- Backend tests: public read/list/search/preview/original-download across tenants; deny anonymous access; deny cross-tenant upload and every mutation; allow explicit SuperAdmin public management; deny Tenant Admin promotion and `CanAccessAllTenants`-only promotion; preserve API-key scope/capability boundaries.
- Backend tests: scope changes alter only KB ownership/visibility, preserve KB ID and all child data/index tenant IDs, validate selected destination tenant, and handle audit/failure cases.
- Frontend tests: public list grouping/deduplication, explicit SuperAdmin visibility controls, tenant Admin/member controls, original-download visibility, and no upload/mutation for public readers.
- Run `go test ./internal/...`, `go build ./...`, frontend standalone tests, `npm run typecheck`, and `npm run build`.

## Out of Scope

- Anonymous internet access.
- Public access to tenant API keys or broader API-key KB scopes.
- Automatic republishing of rows normalized by migration 000111.
- Bulk migration/reindex/storage copying during visibility changes.
- Changes to tenant-join invitations or recipient-bound private-KB invitation semantics.
