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
func HasKBGrant(ctx context.Context, kbID string, tenantID uint64, required types.KBPermission) bool {
	if !required.IsValid() || types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbID) != nil {
		return false
	}
	caller := types.CallerFromContext(ctx)
	if kbID == "" || tenantID == 0 {
		return false
	}
	grants, _ := ctx.Value(types.KBGrantsContextKey).([]kbGrant)
	for _, grant := range grants {
		if (caller.TenantID != 0 || grant.task) && grant.caller == caller && grant.kbID == kbID &&
			grant.tenantID == tenantID &&
			grant.permission.HasPermission(required) {
			return true
		}
	}
	return false
}

// KBPermissions combines caller ownership, exact context grants and
// tenant-to-tenant access grants for one service operation. Pass nil grants
// when that entry point does not permit cross-tenant expansion (for example,
// a caller without a user).
type KBPermissions struct {
	ctx    context.Context
	caller types.Caller
	grants *KBGrantPermissions
	// lookup resolves KB visibility scopes. Kept even when grant expansion
	// is disabled so 'public' semantics still apply to userless principals
	// (API keys).
	lookup KBGrantLookup
	scopes map[string]*types.KBScope
}

// NewKBPermissions resolves reads for one caller and operation.
func NewKBPermissions(ctx context.Context, grants KBGrantLookup) *KBPermissions {
	caller := types.CallerFromContext(ctx)
	return &KBPermissions{
		ctx:    ctx,
		caller: caller,
		grants: NewKBGrantPermissions(ctx, grants, caller.TenantID),
		lookup: grants,
		scopes: make(map[string]*types.KBScope),
	}
}

// WithoutGrantExpansion keeps scope lookups but drops cross-tenant access
// grants. Entry points that must not widen a userless principal
// (kbReadPermissions) use this instead of dropping the whole lookup.
func (p *KBPermissions) WithoutGrantExpansion() *KBPermissions {
	p.grants = nil
	return p
}

// scopeOf resolves and caches the visibility scope of one KB. A nil
// result means "unknown" — callers treat it as tenant-visibility so
// missing lookups fail closed.
func (p *KBPermissions) scopeOf(kbID string) *types.KBScope {
	if p.lookup == nil {
		return nil
	}
	if s, ok := p.scopes[kbID]; ok {
		return s
	}
	s, err := p.lookup.GetKBScope(p.ctx, kbID)
	if err != nil {
		// Lookup failures fail closed: cache a nil so one broken read
		// does not repeatedly hit the store during a fan-out.
		p.scopes[kbID] = nil
		return nil
	}
	p.scopes[kbID] = s
	return s
}

// Check combines caller ownership, exact grants and tenant access grants.
func (p *KBPermissions) Check(kbID string, ownerTenantID uint64, required types.KBPermission) (bool, error) {
	if kbID == "" || ownerTenantID == 0 || !required.IsValid() {
		return false, nil
	}
	if err := types.AuthorizeTenantAPIKeyKnowledgeBases(p.ctx, kbID); err != nil {
		return false, err
	}
	tenantID := ownerTenantID
	visibility := types.KBVisibilityTenant
	if scope := p.scopeOf(kbID); scope != nil {
		if scope.TenantID != 0 {
			tenantID = scope.TenantID
		}
		visibility = scope.Visibility
	}
	sameTenant := p.caller.TenantID != 0 && p.caller.TenantID == tenantID

	if visibility == types.KBVisibilityPublic {
		// Every tenant's callers may read public KBs; writes belong to
		// the owning tenant and still need an upstream grant.
		if required == types.KBPermissionViewer {
			return p.caller.TenantID != 0, nil
		}
		if !sameTenant {
			return false, nil
		}
		return HasKBGrant(p.ctx, kbID, tenantID, required), nil
	}

	if (sameTenant && required == types.KBPermissionViewer) ||
		HasKBGrant(p.ctx, kbID, tenantID, required) {
		return true, nil
	}
	if p.caller.TenantID == 0 || sameTenant {
		return false, nil
	}
	return p.grants.Check(kbID, required)
}
