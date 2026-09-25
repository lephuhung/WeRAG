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

// ResolveKB authorizes a server-loaded KB by owner + visibility. The
// owner (OwnerTenantID; 0 = platform-owned) decides who may act, while the
// data-scope TenantID becomes the grant's effective execution tenant for
// content retrieval after authorization succeeds. ResolveKB never treats an
// effective resource tenant as the caller, and never lets public visibility
// or legacy tenant-wide grants authorize cross-tenant access beyond the
// platform-public read below: recipient-bound invites stay on the separate
// ResolveKBWithInvite path. The grants argument remains for source
// compatibility but is intentionally ignored: legacy approved rows cannot
// confer access. ResolveKB never grants content writes on platform-owned
// rows — not even to explicit SuperAdmins; management uses ResolveKBForManage.
func ResolveKB(ctx context.Context, request KBRequest, kb *types.KnowledgeBase, required types.KBPermission,
	_ KBGrantLookup,
) (*KBAccess, error) {
	if kb == nil || kb.ID == "" {
		return nil, ErrNotFound
	}
	if err := checkAPIKeyScope(ctx, kb.ID); err != nil {
		return nil, err
	}
	request.Caller = request.Caller.Normalize()
	if callerMismatch(ctx, request.Caller) {
		return nil, ErrForbidden
	}
	grant := func(permission types.KBPermission) (*KBAccess, error) {
		return &KBAccess{
			KnowledgeBase: kb, Caller: request.Caller,
			EffectiveTenantID: kb.TenantID, Permission: permission, operationPermission: required,
		}, nil
	}
	// Owning-tenant members keep their normal access on tenant-owned rows.
	if kb.OwnerTenantID != 0 && kb.OwnerTenantID == request.Caller.TenantID {
		return grant(types.KBPermissionAdmin)
	}
	if IsPlatformPublicKB(kb) {
		// Platform-public rows are readable by every authenticated human
		// (Viewer only) — including tenantless humans with no active
		// workspace. Writes stay closed here; the manage path below is
		// the only writer.
		if required == types.KBPermissionViewer {
			if IsExplicitHumanSuperAdmin(ctx, request.Caller) {
				return grant(types.KBPermissionViewer)
			}
			// API-key principals (any scope, any principal type) never
			// count as human here, even with an allowlisted public row.
			if IsAuthenticatedHuman(ctx, request.Caller) {
				return grant(types.KBPermissionViewer)
			}
			if request.Caller.TenantID == 0 {
				return nil, ErrUnauthorized
			}
		}
		// Anonymous callers without any tenant context stay unauthorized;
		// authenticated non-humans and human writers stay forbidden.
		if request.Caller.TenantID == 0 && !IsExplicitHumanSuperAdmin(ctx, request.Caller) {
			return nil, ErrUnauthorized
		}
		return nil, ErrForbidden
	}
	// Tenant-owned foreign rows, legacy public rows with a nonzero owner,
	// and malformed owner/visibility pairs fail closed. Ordinary access
	// without an authenticated tenant stays unauthorized.
	if request.Caller.TenantID == 0 {
		return nil, ErrUnauthorized
	}
	return nil, ErrForbidden
}

// ResolveKBForDownload is the dedicated original-download decision,
// distinct from write permission: platform-public viewers may fetch original
// bytes, same-owner-tenant callers keep the tenant download policy, and an
// explicit human SuperAdmin may download from platform-owned rows.
// Invitation viewers are never consulted here, so their existing
// no-download behavior is unchanged; uninvited private cross-tenant callers
// stay denied. The grant carries at most Viewer for public rows and never
// satisfies a later write check.
func ResolveKBForDownload(ctx context.Context, request KBRequest, kb *types.KnowledgeBase) (*KBAccess, error) {
	if kb == nil || kb.ID == "" {
		return nil, ErrNotFound
	}
	if err := checkAPIKeyScope(ctx, kb.ID); err != nil {
		return nil, err
	}
	request.Caller = request.Caller.Normalize()
	if callerMismatch(ctx, request.Caller) {
		return nil, ErrForbidden
	}
	grant := func(permission types.KBPermission) (*KBAccess, error) {
		return &KBAccess{
			KnowledgeBase: kb, Caller: request.Caller,
			EffectiveTenantID: kb.TenantID, Permission: permission,
			operationPermission: types.KBPermissionViewer,
		}, nil
	}
	if kb.OwnerTenantID != 0 && kb.OwnerTenantID == request.Caller.TenantID {
		return grant(types.KBPermissionAdmin)
	}
	if IsPlatformPublicKB(kb) {
		if IsExplicitHumanSuperAdmin(ctx, request.Caller) {
			return grant(types.KBPermissionViewer)
		}
		// Tenantless real humans download platform-public originals;
		// API-key principals and anonymous/synthetic callers without a
		// tenant stay unauthorized.
		if IsAuthenticatedHuman(ctx, request.Caller) {
			return grant(types.KBPermissionViewer)
		}
		if request.Caller.TenantID == 0 {
			return nil, ErrUnauthorized
		}
		return nil, ErrForbidden
	}
	if request.Caller.TenantID == 0 {
		return nil, ErrUnauthorized
	}
	return nil, ErrForbidden
}

// ResolveKBForManage authorizes content mutations by owner: the owning
// tenant keeps its existing access (role floors stay at the route layer),
// while platform-owned rows admit only an explicit human SuperAdmin.
// CanAccessAllTenants alone and every machine principal stay denied on
// platform-owned rows. KB creation and owner/scope transitions are out of
// scope here; Task 3 owns those privileged flows.
func ResolveKBForManage(ctx context.Context, request KBRequest, kb *types.KnowledgeBase) (*KBAccess, error) {
	if kb == nil || kb.ID == "" {
		return nil, ErrNotFound
	}
	if err := checkAPIKeyScope(ctx, kb.ID); err != nil {
		return nil, err
	}
	request.Caller = request.Caller.Normalize()
	if callerMismatch(ctx, request.Caller) {
		return nil, ErrForbidden
	}
	grant := func(permission types.KBPermission) (*KBAccess, error) {
		return &KBAccess{
			KnowledgeBase: kb, Caller: request.Caller,
			EffectiveTenantID: kb.TenantID, Permission: permission,
			operationPermission: types.KBPermissionEditor,
		}, nil
	}
	if IsPlatformPublicKB(kb) {
		if IsExplicitHumanSuperAdmin(ctx, request.Caller) {
			return grant(types.KBPermissionAdmin)
		}
		if request.Caller.TenantID == 0 {
			return nil, ErrUnauthorized
		}
		return nil, ErrForbidden
	}
	if kb.OwnerTenantID != 0 && kb.OwnerTenantID == request.Caller.TenantID {
		return grant(types.KBPermissionAdmin)
	}
	if request.Caller.TenantID == 0 {
		return nil, ErrUnauthorized
	}
	return nil, ErrForbidden
}

// RequireKBManage consumes an explicit operation grant for content
// mutations. Platform-owned rows admit only an explicit human SuperAdmin
// (machine principals stay denied even with a grant); tenant-owned rows
// keep the existing editor-grant plus ingest-capability policy. An execution
// tenant alone never authorizes a mutation.
func RequireKBManage(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil || kb.ID == "" {
		return ErrNotFound
	}
	if IsPlatformPublicKB(kb) {
		if IsExplicitHumanSuperAdmin(ctx, types.CallerFromContext(ctx)) {
			return nil
		}
		if types.CallerFromContext(ctx).TenantID == 0 {
			return ErrUnauthorized
		}
		return ErrForbidden
	}
	return RequireKBWrite(ctx, kb)
}
