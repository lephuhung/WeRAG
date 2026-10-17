/* KB access management: GET /knowledge-bases/:id/access (the overview),
 * PUT /knowledge-bases/:id/visibility (scope + member_default) and the
 * SuperAdmin-only POST /knowledge-bases/:id/transfer-owner. Types mirror
 * internal/handler/kb_access_overview.go and TransferKnowledgeBaseOwner in
 * internal/handler/knowledgebase.go. Invites and unit grants reuse
 * issueKBInvite / grantKBToTenant / revokeKBInvite / revokeKBGrant from
 * ./knowledge. */
import { apiGet, apiPost, apiPut } from "@/lib/api-client";
import type { KnowledgeBaseRow } from "./knowledge";
import type { KBAccessOverview, KBMemberDefault, KBScope } from "../kb-access";

export type {
  KBAccessOverview,
  KBEffectivePermissions,
  KBGrantee,
  KBMemberDefault,
  KBScope,
} from "../kb-access";

export function getKBAccess(kbId: string): Promise<KBAccessOverview> {
  return apiGet<{ success: boolean; data: KBAccessOverview }>(
    `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/access`,
  ).then((r) => r.data);
}

/* Scope and/or member_default. "public" is a SuperAdmin promotion of a
 * tenant KB to the platform; an owner change is never a visibility change
 * (see transferKBOwner). */
export function setKBScope(kbId: string, data: { visibility?: KBScope; member_default?: KBMemberDefault }) {
  return apiPut<{ success: boolean; data?: KnowledgeBaseRow }>(
    `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/visibility`,
    data,
  );
}

export interface KBOwnerTransferResult {
  knowledge_base: KnowledgeBaseRow;
  previous_owner_tenant_id: number;
  previous_visibility: KBScope;
  revoked_invitations: number;
  revoked_grants: number;
  withdrawn_from_published: boolean;
}

/* SuperAdmin only: move the KB to another unit. Every share is revoked and
 * a published KB is withdrawn (tenant scope afterwards). */
export function transferKBOwner(kbId: string, targetTenantId: number): Promise<KBOwnerTransferResult> {
  return apiPost<{ success: boolean; data: KBOwnerTransferResult }>(
    `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/transfer-owner`,
    { target_tenant_id: targetTenantId },
  ).then((r) => r.data);
}
