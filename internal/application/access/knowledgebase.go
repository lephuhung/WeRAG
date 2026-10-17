// Package access resolves resource permissions independently of HTTP routing.
// Tenant roles, API-key capabilities, and resource ownership guards remain
// separate checks; a KB grant does not replace them.
package access

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

// Resource access errors retain distinct adapter response mappings.
var (
	ErrUnauthorized = errors.New("kb_access: unauthorized")
	ErrNotFound     = errors.New("kb_access: not found")
	ErrForbidden    = errors.New("kb_access: forbidden")
)

// IsPlatformPublicKB reports whether kb carries platform ownership:
// visibility public with no owning tenant. Authorization uses this
// owner + visibility pair; the data-scope TenantID is never consulted here
// so a converted KB keeps working after its scope transition.
func IsPlatformPublicKB(kb *types.KnowledgeBase) bool {
	return kb != nil && kb.Visibility == types.KBVisibilityPublic && kb.OwnerTenantID == 0
}

// IsHumanCaller reports whether caller is a real human identity as opposed
// to an anonymous context or the synthetic system-<tenant> user the API-key
// auth path attaches to machine principals.
func IsHumanCaller(caller types.Caller) bool {
	return caller.UserID != "" && !types.IsSyntheticUserID(caller.UserID)
}

// IsAuthenticatedHuman reports whether ctx and caller identify a genuine
// human UI principal. Beyond IsHumanCaller it rejects every machine
// identity that carries a user-ID-shaped value: any TenantAPIKeyScope
// (tenant keys use synthetic users, but platform keys use
// Principal.StorageID() == "api_platform:<id>" as their user ID), and any
// non-web principal type (api_tenant, api_platform, api_external_user,
// embed sessions). Public-visibility admission must consult this — never
// bare IsHumanCaller — so API-key identity can never widen into a human
// grant. Unit contexts without a principal fall back to the user-ID check.
func IsAuthenticatedHuman(ctx context.Context, caller types.Caller) bool {
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return false
	}
	if principal, ok := types.PrincipalFromContext(ctx); ok {
		switch principal.Type {
		case "", types.PrincipalWebUser:
			// Human session (or unset principal in unit contexts).
		default:
			return false
		}
	}
	return IsHumanCaller(caller)
}

// IsExplicitHumanSuperAdmin reports whether ctx carries an explicit human
// platform administrator: the system-admin flag plus a real non-synthetic
// user ID. CanAccessAllTenants alone never qualifies, and machine
// principals (API-key scopes, synthetic users) never qualify.
func IsExplicitHumanSuperAdmin(ctx context.Context, caller types.Caller) bool {
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return false
	}
	return types.IsSystemAdminFromContext(ctx) && IsHumanCaller(caller)
}

// checkAPIKeyScope rejects KB-restricted API-key callers outside their
// allow-list. Public visibility never expands this boundary.
func checkAPIKeyScope(ctx context.Context, kbID string) error {
	return types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbID)
}

// callerMismatch rejects a request whose ambient context caller differs from
// the explicitly captured request caller: a grant cannot replace identity.
func callerMismatch(ctx context.Context, caller types.Caller) bool {
	if ambient, ok := ctx.Value(types.CallerContextKey).(types.Caller); ok && ambient != caller {
		return true
	}
	return false
}

// KBRequest keeps the authenticated caller separate from the resource tenant.
type KBRequest struct {
	Caller types.Caller
}

// KBAccess binds a resolved resource permission to its original caller.
type KBAccess struct {
	KnowledgeBase     *types.KnowledgeBase
	Caller            types.Caller
	EffectiveTenantID uint64
	Permission        types.KBPermission
	// Source names the DecideKB rule that granted this access.
	Source AccessSource
	// CanDownloadOriginal reports whether the caller may fetch original
	// bytes from this KB (ACL readers may not).
	CanDownloadOriginal bool
	// A read resolution must not mint a write grant even for the KB owner.
	operationPermission types.KBPermission
}

// Context scopes resource operations without changing the authenticated caller.
func (a *KBAccess) Context(ctx context.Context) context.Context {
	if a == nil || a.KnowledgeBase == nil {
		return ctx
	}
	scoped := a.WithGrant(ctx)
	if types.CallerFromContext(scoped) != a.Caller.Normalize() {
		return ctx
	}
	return types.WithExecutionTenant(scoped, a.EffectiveTenantID)
}

// WithGrant carries this authorization without changing execution scope.
func (a *KBAccess) WithGrant(ctx context.Context) context.Context {
	if a == nil || a.KnowledgeBase == nil {
		return ctx
	}
	caller := a.Caller.Normalize()
	if existing, ok := ctx.Value(types.CallerContextKey).(types.Caller); ok && existing != caller {
		return ctx // A grant cannot replace an already authenticated caller.
	}
	grants, _ := ctx.Value(types.KBGrantsContextKey).([]kbGrant)
	permission := a.Permission
	if a.operationPermission.IsValid() && permission.HasPermission(a.operationPermission) {
		permission = a.operationPermission
	}
	grants = append(
		append([]kbGrant(nil), grants...),
		kbGrant{caller: caller, kbID: a.KnowledgeBase.ID, tenantID: a.EffectiveTenantID, permission: permission},
	)
	return context.WithValue(types.WithCaller(ctx, caller), types.KBGrantsContextKey, grants)
}

// ResolveKB authorizes a server-loaded KB through DecideKB with tenant
// grants as the only ACL source (recipient-bound invites stay on
// ResolveKBWithInvite). required maps to an action: Viewer -> View,
// Editor -> Contribute, Admin -> Manage. The returned grant carries the
// caller's effective permission, while the context grant is capped to
// required so a read never mints a reusable write grant. The data-scope
// TenantID becomes the effective execution tenant; the caller identity is
// never replaced.
func ResolveKB(ctx context.Context, request KBRequest, kb *types.KnowledgeBase, required types.KBPermission,
	grants KBGrantLookup,
) (*KBAccess, error) {
	return resolveKBAction(ctx, request, kb, ActionForPermission(required), required, KBLookups{Grants: grants})
}

// resolveKBAction runs DecideKB and projects the result onto KBAccess.
func resolveKBAction(ctx context.Context, request KBRequest, kb *types.KnowledgeBase, action KBAction,
	operation types.KBPermission, lookups KBLookups,
) (*KBAccess, error) {
	decision, err := DecideKB(ctx, request.Caller, kb, action, lookups)
	if err != nil {
		return nil, err
	}
	return accessFromDecision(kb, request.Caller, decision, operation), nil
}

// CanReadPublishedKB reports whether caller may read a tenant-published KB:
// every authenticated human (tenantless included), never an API-key
// principal. Writes stay with the owning tenant.
func CanReadPublishedKB(ctx context.Context, caller types.Caller, kb *types.KnowledgeBase) bool {
	return kb.IsTenantPublished() &&
		(IsExplicitHumanSuperAdmin(ctx, caller) || IsAuthenticatedHuman(ctx, caller))
}

// HasTenantGrant reports whether a live owner-issued grant lets the
// caller's tenant read a tenant-owned KB. Only authenticated humans with an
// active tenant other than the owner qualify; lookup errors fail closed.
func HasTenantGrant(ctx context.Context, caller types.Caller, kbID string, ownerTenantID uint64, grants KBGrantLookup) bool {
	if grants == nil || kbID == "" || ownerTenantID == 0 ||
		caller.TenantID == 0 || caller.TenantID == ownerTenantID {
		return false
	}
	if !IsAuthenticatedHuman(ctx, caller) {
		return false
	}
	permission, ok, err := grants.ApprovedKBPermission(ctx, kbID, caller.TenantID)
	return err == nil && ok && permission.HasPermission(types.KBPermissionViewer)
}

// ResolveKBForDownload is the original-download decision (DecideKB,
// ActionDownloadOriginal). Public/published readers, the owner tenant's
// members and Admins, and SuperAdmins download; ACL holders (invites,
// tenant grants) never do, so no ACL lookup is consulted. The context
// grant is capped to Viewer and never satisfies a later write check.
func ResolveKBForDownload(ctx context.Context, request KBRequest, kb *types.KnowledgeBase) (*KBAccess, error) {
	return resolveKBAction(ctx, request, kb, ActionDownloadOriginal, types.KBPermissionViewer, KBLookups{})
}

// ResolveKBForManage authorizes content mutations (DecideKB,
// ActionContribute): uploads and document/chunk/FAQ/tag/wiki writes.
// Admin-only operations keep their TenantAdmin route floor on top, and
// creator-or-admin document guards still apply. Members contribute only
// when the KB's member_default allows it; ACL holders, public/published
// readers and Guests never write; SuperAdmins write everywhere. The
// context grant is capped to Editor.
func ResolveKBForManage(ctx context.Context, request KBRequest, kb *types.KnowledgeBase) (*KBAccess, error) {
	return resolveKBAction(ctx, request, kb, ActionContribute, types.KBPermissionEditor, KBLookups{})
}

// RequireKBManage consumes an explicit operation grant for content
// mutations; the grant itself comes from a DecideKB wrapper
// (ResolveKBForManage, or ResolveKB with Editor/Admin), so the decision
// stays in one place. An explicit human SuperAdmin passes on every KB
// (DecideKB grants it Admin everywhere); otherwise
// platform-owned rows deny (machine principals stay denied even with a
// grant) and tenant-owned rows keep the existing editor-grant plus
// ingest-capability policy. An execution tenant alone never authorizes a
// mutation.
func RequireKBManage(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil || kb.ID == "" {
		return ErrNotFound
	}
	if IsExplicitHumanSuperAdmin(ctx, types.CallerFromContext(ctx)) {
		return nil
	}
	if IsPlatformPublicKB(kb) {
		if types.CallerFromContext(ctx).TenantID == 0 {
			return ErrUnauthorized
		}
		return ErrForbidden
	}
	return RequireKBWrite(ctx, kb)
}
