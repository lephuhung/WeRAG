package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBScopeOf projects a loaded KnowledgeBase into its scope form.
// Exported for service-layer callers that already hold the KB row.
func KBScopeOf(kb *types.KnowledgeBase) *types.KBScope {
	return kbScopeOf(kb)
}

// CallerOrgGrant maps the caller's position inside an org-scoped KB to
// an OrgMemberRole-equivalent grant:
//
//	tenant Admin/Owner, system admin, tenant-level API key -> admin
//	org manager                                          -> editor
//	org member                                           -> viewer
//	everyone else                                        -> "" (deny)
//
// The grant is only meaningful inside the KB's own tenant; callers must
// establish caller.TenantID == scope.TenantID before consulting this.
func CallerOrgGrant(
	ctx context.Context,
	caller types.Caller,
	scope *types.KBScope,
	lookup KBShareLookup,
) types.OrgMemberRole {
	if caller.Role.HasPermission(types.TenantRoleAdmin) || types.IsSystemAdminFromContext(ctx) {
		return types.OrgRoleAdmin
	}
	if caller.UserID == "" {
		// Tenant-scoped API keys carry no user; they act with tenant
		// authority (only admins can mint them) — OrgRoleAdmin mirrors
		// the same-tenant grant tenant users get on tenant KBs.
		if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
			return types.OrgRoleAdmin
		}
		return ""
	}
	if lookup == nil || scope.OrgID == 0 {
		return ""
	}
	orgRole, member, err := lookup.OrgMemberRole(ctx, scope.TenantID, scope.OrgID, caller.UserID)
	if err != nil || !member {
		return ""
	}
	if orgRole == types.TenantOrgRoleManager {
		return types.OrgRoleEditor
	}
	return types.OrgRoleViewer
}

// kbScopeOf projects a loaded KnowledgeBase into its scope form.
func kbScopeOf(kb *types.KnowledgeBase) *types.KBScope {
	if kb == nil {
		return nil
	}
	visibility := kb.Visibility
	if !visibility.IsValid() {
		visibility = types.KBVisibilityTenant
	}
	scope := &types.KBScope{TenantID: kb.TenantID, Visibility: visibility}
	if visibility == types.KBVisibilityOrg && kb.OrgID != nil {
		scope.OrgID = *kb.OrgID
	}
	return scope
}
