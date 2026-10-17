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

// KBPermissions answers repeated KB read checks for one caller (search
// scopes, chat Q&A target selection, custom agents, FAQ/tag reads). Reads
// go through DecideKB (ActionView) against the KB's live scope, so a
// restricted KB is readable only by its owner tenant's Admins, SuperAdmins
// and live ACL holders, and a revoked ACL stops reads on the next request.
// Writes are never decided here: Editor/Admin checks only accept exact
// grants established upstream by a write guard.
type KBPermissions struct {
	ctx       context.Context
	caller    types.Caller
	lookup    KBGrantLookup
	invites   KBInviteLookup
	scopes    map[string]*types.KBScope
	decisions map[string]bool
}

// NewKBPermissions resolves reads for one caller and operation. The lookup
// supplies KB owner/visibility scopes and tenant grants; when it also
// implements KBInviteLookup, live recipient-bound invites count as an ACL
// source. A nil lookup keeps the ownerTenantID argument as the owner with
// tenant visibility; a lookup that cannot load the scope fails closed
// (the KB is treated as restricted).
func NewKBPermissions(ctx context.Context, lookup KBGrantLookup) *KBPermissions {
	p := &KBPermissions{ctx: ctx, caller: types.CallerFromContext(ctx), lookup: lookup}
	if invites, ok := lookup.(KBInviteLookup); ok {
		p.invites = invites
	}
	return p
}

// WithInvites adds a recipient-bound invite lookup as an ACL source.
func (p *KBPermissions) WithInvites(invites KBInviteLookup) *KBPermissions {
	if invites != nil {
		p.invites = invites
		p.decisions = nil
	}
	return p
}

// WithoutGrantExpansion remains as a compatibility no-op. Tenant-wide grant
// expansion stays disabled for every caller regardless of this setting.
func (p *KBPermissions) WithoutGrantExpansion() *KBPermissions { return p }

// scopeOf resolves and caches the ownership scope of one KB. ok=false means
// the lookup exists but could not produce a scope (missing row or error).
func (p *KBPermissions) scopeOf(kbID string) (scope *types.KBScope, ok bool) {
	if p.lookup == nil {
		return nil, true
	}
	if s, cached := p.scopes[kbID]; cached {
		return s, s != nil
	}
	if p.scopes == nil {
		p.scopes = make(map[string]*types.KBScope)
	}
	s, err := p.lookup.GetKBScope(p.ctx, kbID)
	if err != nil {
		s = nil
	}
	p.scopes[kbID] = s
	return s, s != nil
}

// Check reports whether the caller may perform required on kbID. Viewer
// reads are decided by DecideKB(ActionView) on the KB's live scope
// (owner, visibility, member_default) with tenant grants and, when
// available, invites as ACL sources. Every requirement also accepts an
// exact grant established upstream in the context (HasKBGrant). Without a
// lookup, ownerTenantID stands in as a tenant-visibility owner; with a
// lookup that cannot load the row, the KB is treated as restricted.
func (p *KBPermissions) Check(kbID string, ownerTenantID uint64, required types.KBPermission) (bool, error) {
	if kbID == "" || !required.IsValid() {
		return false, nil
	}
	if err := types.AuthorizeTenantAPIKeyKnowledgeBases(p.ctx, kbID); err != nil {
		return false, err
	}
	kb := &types.KnowledgeBase{
		ID: kbID, TenantID: ownerTenantID, OwnerTenantID: ownerTenantID,
		Visibility: types.KBVisibilityTenant, MemberDefault: types.KBMemberDefaultContribute,
	}
	scope, ok := p.scopeOf(kbID)
	switch {
	case scope != nil:
		kb.TenantID, kb.OwnerTenantID = scope.TenantID, scope.OwnerTenantID
		kb.Visibility, kb.MemberDefault = scope.Visibility, scope.MemberDefault
		if !kb.Visibility.IsValid() {
			kb.Visibility = types.KBVisibilityTenant
		}
		if !kb.MemberDefault.IsValid() {
			kb.MemberDefault = types.KBMemberDefaultContribute
		}
	case !ok:
		// Unknown scope: only the owner tenant's managers keep reads.
		kb.Visibility = types.KBVisibilityRestricted
	}
	if required == types.KBPermissionViewer && p.decideView(kb) {
		return true, nil
	}
	return HasKBGrant(p.ctx, kbID, kb.TenantID, required), nil
}

// decideView runs DecideKB(ActionView) once per KB for this caller.
func (p *KBPermissions) decideView(kb *types.KnowledgeBase) bool {
	if allowed, cached := p.decisions[kb.ID]; cached {
		return allowed
	}
	_, err := DecideKB(p.ctx, p.caller, kb, ActionView, KBLookups{Grants: p.lookup, Invites: p.invites})
	if p.decisions == nil {
		p.decisions = make(map[string]bool)
	}
	p.decisions[kb.ID] = err == nil
	return err == nil
}
