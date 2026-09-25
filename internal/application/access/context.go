package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Context grants contain values, not mutable KB objects. Deriving a context
// copies the grant slice so parallel search branches cannot widen each other.
type kbGrant struct {
	caller     types.Caller
	kbID       string
	tenantID   uint64
	permission types.KBPermission
	task       bool
}

// HasKBGrant checks only previously resolved resource access. It never infers
// ownership from the execution tenant, and always reapplies API-key scope.
// A platform data scope (tenant 0) still matches exactly: grants are keyed by
// (caller, kbID, tenantID), so a zero tenant cannot widen to another KB.
func HasKBGrant(ctx context.Context, kbID string, tenantID uint64, required types.KBPermission) bool {
	if !required.IsValid() || types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbID) != nil {
		return false
	}
	caller := types.CallerFromContext(ctx)
	if kbID == "" {
		return false
	}
	grants, _ := ctx.Value(types.KBGrantsContextKey).([]kbGrant)
	for _, grant := range grants {
		// Zero-tenant grants belong to the approved no-active-tenant
		// SuperAdmin path and match only their identical caller; every
		// other caller still needs a nonzero tenant or a task grant.
		if (caller.TenantID != 0 || grant.task || grant.caller.TenantID == 0) &&
			grant.caller == caller && grant.kbID == kbID &&
			grant.tenantID == tenantID &&
			grant.permission.HasPermission(required) {
			return true
		}
	}
	return false
}

// KBPermissions combines caller ownership with exact authorization grants
// already resolved into the request context. Owner-based checks consult the
// KB scope lookup (owner + visibility) when available; recipient-bound
// invitations arrive as exact context grants resolved upstream.
type KBPermissions struct {
	ctx    context.Context
	caller types.Caller
	lookup KBGrantLookup
	scopes map[string]*types.KBScope
}

// NewKBPermissions resolves reads for one caller and operation. The lookup
// supplies KB owner/visibility scopes; a nil lookup keeps the
// ownerTenantID argument as the owner with tenant visibility (fail-closed).
func NewKBPermissions(ctx context.Context, lookup KBGrantLookup) *KBPermissions {
	return &KBPermissions{ctx: ctx, caller: types.CallerFromContext(ctx), lookup: lookup}
}

// WithoutGrantExpansion remains as a compatibility no-op. Tenant-wide grant
// expansion stays disabled for every caller regardless of this setting.
func (p *KBPermissions) WithoutGrantExpansion() *KBPermissions { return p }

// scopeOf resolves and caches the ownership scope of one KB. A nil result
// means "unknown" — callers fall back to the owner argument with tenant
// visibility so missing lookups fail closed.
func (p *KBPermissions) scopeOf(kbID string) *types.KBScope {
	if p.lookup == nil {
		return nil
	}
	if s, ok := p.scopes[kbID]; ok {
		return s
	}
	if p.scopes == nil {
		p.scopes = make(map[string]*types.KBScope)
	}
	s, err := p.lookup.GetKBScope(p.ctx, kbID)
	if err != nil {
		p.scopes[kbID] = nil
		return nil
	}
	p.scopes[kbID] = s
	return s
}

// Check permits owning-tenant reads, platform-public reads, and exact grants
// established by an upstream authorization (including recipient-bound KB
// invitations). Owner and visibility come from the scope lookup when
// available; otherwise ownerTenantID is treated as a tenant-visibility
// owner. Public reads grant Viewer only and never consult legacy
// tenant-wide grants.
func (p *KBPermissions) Check(kbID string, ownerTenantID uint64, required types.KBPermission) (bool, error) {
	if kbID == "" || !required.IsValid() {
		return false, nil
	}
	if err := types.AuthorizeTenantAPIKeyKnowledgeBases(p.ctx, kbID); err != nil {
		return false, err
	}
	owner := ownerTenantID
	visibility := types.KBVisibilityTenant
	dataTenant := ownerTenantID
	if scope := p.scopeOf(kbID); scope != nil {
		owner = scope.OwnerTenantID
		visibility = scope.Visibility
		if !visibility.IsValid() {
			visibility = types.KBVisibilityTenant
		}
		dataTenant = scope.TenantID
	}
	if visibility == types.KBVisibilityPublic && owner == 0 {
		if required == types.KBPermissionViewer {
			if IsExplicitHumanSuperAdmin(p.ctx, p.caller) {
				return true, nil
			}
			// Human-only: API-key principals (any scope/type) never
			// pass, even allowlisted to this row.
			if p.caller.TenantID != 0 && IsAuthenticatedHuman(p.ctx, p.caller) {
				return true, nil
			}
		}
		return HasKBGrant(p.ctx, kbID, dataTenant, required), nil
	}
	if owner == 0 {
		return HasKBGrant(p.ctx, kbID, dataTenant, required), nil
	}
	sameTenant := p.caller.TenantID != 0 && p.caller.TenantID == owner
	if sameTenant && required == types.KBPermissionViewer {
		return true, nil
	}
	return HasKBGrant(p.ctx, kbID, dataTenant, required), nil
}
