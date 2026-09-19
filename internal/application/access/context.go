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
	permission types.OrgMemberRole
	task       bool
}

type agentGrant struct {
	caller types.Caller
	scope  types.SharedAgentKBScope
}

// WithSharedAgent scopes execution after the caller has authorized this exact
// agent. Only its configured KBs receive read access; other source-tenant KBs
// and the source tenant's organization memberships are not inherited.
func WithSharedAgent(ctx context.Context, agent *types.CustomAgent) context.Context {
	grant := agentGrant{caller: types.CallerFromContext(ctx), scope: types.NewSharedAgentKBScope(agent)}
	ctx = context.WithValue(ctx, types.SharedAgentGrantContextKey, grant)
	if agent == nil {
		return ctx
	}
	return types.WithExecutionTenant(ctx, agent.TenantID)
}

// HasKBGrant checks only previously resolved resource access. It never infers
// ownership from the execution tenant, and always reapplies API-key scope.
func HasKBGrant(ctx context.Context, kbID string, tenantID uint64, required types.OrgMemberRole) bool {
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
	grant, ok := ctx.Value(types.SharedAgentGrantContextKey).(agentGrant)
	return ok && caller.TenantID != 0 && required == types.OrgRoleViewer && grant.caller == caller &&
		grant.scope.Allows(kbID, tenantID)
}

// KBPermissions combines caller ownership, exact context grants and organization
// shares for one service operation. Pass nil shares when that entry point does
// not permit organization expansion (for example, a caller without a user).
type KBPermissions struct {
	ctx    context.Context
	caller types.Caller
	shares *KBSharePermissions
	// lookup resolves KB visibility scopes and tenant-org membership.
	// Kept even when share expansion is disabled so 'public' and 'org'
	// semantics still apply to userless principals (API keys).
	lookup KBShareLookup
	scopes map[string]*types.KBScope
}

// NewKBPermissions resolves reads for one caller and operation.
func NewKBPermissions(ctx context.Context, shares KBShareLookup) *KBPermissions {
	caller := types.CallerFromContext(ctx)
	return &KBPermissions{
		ctx:    ctx,
		caller: caller,
		shares: NewKBSharePermissions(ctx, shares, caller.TenantID, caller.Role),
		lookup: shares,
		scopes: make(map[string]*types.KBScope),
	}
}

// WithoutShareExpansion keeps scope/org lookups but drops organization
// share grants. Entry points that must not widen a userless principal
// (kbReadPermissions) use this instead of dropping the whole lookup.
func (p *KBPermissions) WithoutShareExpansion() *KBPermissions {
	p.shares = nil
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

// Check combines caller ownership, exact grants and organization sharing.
func (p *KBPermissions) Check(kbID string, ownerTenantID uint64, required types.OrgMemberRole) (bool, error) {
	if kbID == "" || ownerTenantID == 0 || !required.IsValid() {
		return false, nil
	}
	if err := types.AuthorizeTenantAPIKeyKnowledgeBases(p.ctx, kbID); err != nil {
		return false, err
	}
	tenantID := ownerTenantID
	visibility := types.KBVisibilityTenant
	var orgID uint64
	if scope := p.scopeOf(kbID); scope != nil {
		if scope.TenantID != 0 {
			tenantID = scope.TenantID
		}
		visibility = scope.Visibility
		orgID = scope.OrgID
	}
	sameTenant := p.caller.TenantID != 0 && p.caller.TenantID == tenantID

	switch visibility {
	case types.KBVisibilityOrg:
		// Org-scoped KBs never cross the tenant boundary. Inside the
		// tenant, org members read; org managers additionally write;
		// tenant Admin/Owner, system admins and tenant-level API keys
		// administer.
		if !sameTenant {
			return false, nil
		}
		scope := &types.KBScope{TenantID: tenantID, Visibility: visibility, OrgID: orgID}
		if CallerOrgGrant(p.ctx, p.caller, scope, p.lookup) == "" {
			return false, nil
		}
		if required == types.OrgRoleViewer {
			return true, nil
		}
		return HasKBGrant(p.ctx, kbID, tenantID, required), nil
	case types.KBVisibilityPublic:
		// Every tenant's callers may read public KBs; writes belong to
		// the owning tenant and still need an upstream grant.
		if required == types.OrgRoleViewer {
			return p.caller.TenantID != 0, nil
		}
		if !sameTenant {
			return false, nil
		}
		return HasKBGrant(p.ctx, kbID, tenantID, required), nil
	}

	if (sameTenant && required == types.OrgRoleViewer) ||
		HasKBGrant(p.ctx, kbID, tenantID, required) {
		return true, nil
	}
	if p.caller.TenantID == 0 || sameTenant {
		return false, nil
	}
	return p.shares.Check(kbID, required)
}
