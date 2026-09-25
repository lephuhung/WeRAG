# Frontend Tenant and KB Permissions Alignment

## Goal

Align the Next.js frontend with the current backend authorization model: platform SuperAdmin is separate from tenant membership; tenant memberships are Admin or Member; KBs are private to their owning tenant; cross-tenant KB read access is recipient-bound and read-only; Member may upload new content only to own-tenant KBs.

This is a frontend-only alignment. Do not change backend routes or authorization policy as part of this work.

## Current Backend Contract

- `GET /knowledge-bases` returns the caller's own-tenant KBs and accepted, unexpired invitations addressed to that user.
- `POST/GET /knowledge-bases/:id/invites` and `DELETE /knowledge-bases/:id/invites/:invite_id` manage a specific recipient's invitation; issuing requires the KB-owning tenant Admin and an active membership of the recipient in the target tenant.
- `GET /kb-invites`, `POST /kb-invites/:id/accept`, and `POST /kb-invites/accept` support the user's invitation inbox and acceptance.
- Legacy `public` markers do not grant cross-tenant access. Public creation/change and tenant-wide grant mutation are retired.
- A foreign invitee can read/preview but cannot manage the KB, upload, mutate content, or download original source files. An invitee's role in their own tenant must not grant authority in the KB-owning tenant.
- Own-tenant Members can read and upload new files; Tenant Admins manage KBs and existing content.

## Design

### API and visibility presentation

Update frontend types, comments, and affordances to describe the active invitation contract. Do not offer public visibility or tenant-wide grant requests/reviews. Legacy `public` data may still be decoded for compatibility, but the UI must not present it as granting public access or send it in create/update requests. Preserve API-key capabilities as a separate machine-principal model.

The retired Sharing settings screen should explain that sharing is now managed per KB and direct Tenant Admins to the KB list/detail invite action. It should not call legacy grant endpoints. Keep tenant-join invitations separate.

### KB invitation UI

Retain the existing email plus recipient-tenant-ID workflow because the backend requires the recipient to be an active member of that tenant and no cross-tenant directory/search API is in scope. Make the requirement and read-only scope explicit. Continue to show the one-time token only on creation; list pending/accepted invites and support revoke. Recipient inbox supports accept-by-ID and token redemption, then refreshes KB listing.

### Resource-aware permissions in KB views

Determine whether a KB belongs to the selected tenant using the KB's authoritative `tenant_id` and active tenant identity. For foreign KBs, suppress all management/settings/share-as-owner/upload/existing-content mutation actions regardless of the invitee's role in their own tenant. Own-tenant Members retain upload affordances; existing-content mutation stays Tenant Admin-only.

For invited foreign KBs, allow read/preview and hide or clearly disable original-file download, which the backend intentionally denies. Do not fall back from a failed preview to original download for a foreign KB. Keep server authorization authoritative.

Replace UI checks that require legacy `owner` with the shared Tenant Admin predicate only where the corresponding backend operation is Tenant Admin-gated. Preserve true platform-only gates and legacy owner data normalization.

### Discovery and chat

Use the existing `GET /knowledge-bases` result as the source of truth for accepted invitees' KB discovery. Do not invent broader cross-tenant search or list access. Keep stale locally selected KB IDs from being treated as authorization; the server remains authoritative for every request.

## Error Handling

- Surface invitation create/list/revoke/accept failures using existing API error handling; no optimistic authorization assumptions.
- On forbidden/not-found KB detail fetch, show the invite inbox/token redemption route rather than a legacy tenant-wide access-request action.
- Preview failures should be reported as preview failures, not silently retried through a forbidden foreign source download.

## Validation

- Add/update frontend unit tests for role normalization and permission affordances, legacy visibility rendering, invite API payloads, and foreign KB read-only actions.
- Run the frontend typecheck and relevant tests/build. If dependencies or TypeScript are unavailable, report that as an explicit validation limitation.
- Run backend tests only as a regression check; backend behavior is not being changed.
- Preserve all pre-existing worktree changes; do not stage or commit.

## Scope Exclusions

- No backend recipient-discovery/search API or invitation contract changes.
- No changes to tenant join invitations, SuperAdmin account inventory, API-key authorization, or shared-agent execution policy.
- No backend authorization changes; hidden frontend controls are usability only and do not replace server checks.
