package middleware

import (
	"context"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// access.go centralises the tenant-access checks that previously lived
// scattered across handlers and the various RBAC middlewares:
//
//   - "is the caller a platform SuperAdmin" (IsPlatformSuperAdmin) —
//     used by the cross-tenant route guard and the path-tenant match.
//     Platform authority comes only from User.IsSystemAdmin on an
//     explicit human session; the legacy CanAccessAllTenants attribute
//     and the cluster-wide EnableCrossTenantAccess flag grant nothing.
//
//   - "can the caller select target tenant X" — used by the
//     X-Tenant-ID branch in auth.go to decide whether to honour the
//     header (home tenant, SuperAdmin, or an active membership).
//
//   - "must the URL :id match the active tenant" — used by every
//     tenant-scoped route. Was duplicated in tenant.go (4 callers) and
//     tenant_member.go (1 caller) before; now lives here as a route
//     middleware so the route declaration is the single source of
//     truth.
//
// Rejections are reported via c.Error(*apperrors.AppError) + c.Abort(),
// not c.JSON: this keeps the response shape consistent with the
// {success,error:{code,message,details}} envelope that ErrorHandler
// renders for the rest of the API. The handler-level checks these
// guards replaced (authorizeTenantAccess, resolveTenantIDFromPath,
// ListAllTenants/SearchTenants if-blocks) all used that envelope, so
// preserving it avoids breaking SDK / frontend consumers that key off
// `error.code`.

// IsPlatformSuperAdmin reports whether ctx carries an explicit human
// SuperAdmin (access.IsExplicitHumanSuperAdmin): the system-admin flag
// on a real, non-synthetic user and no API-key scope. It replaces the
// former IsCrossTenantSuperuser, which honoured CanAccessAllTenants.
func IsPlatformSuperAdmin(ctx context.Context) bool {
	return access.IsExplicitHumanSuperAdmin(ctx, types.CallerFromContext(ctx))
}

// IsTenantAccessible reports whether `user` may select `targetTenantID`
// (X-Tenant-ID). Selecting a tenant is scope selection, not a grant: the
// role inside it still comes only from an active membership
// (resolveTenantRole). The decision order is:
//
//  1. Home tenant (user.TenantID == targetTenantID): always.
//  2. SuperAdmin (user.IsSystemAdmin): any tenant. The legacy
//     CanAccessAllTenants attribute no longer qualifies.
//  3. Multi-tenant member: an active tenant_members row in the target
//     tenant grants access (this is what makes cross-tenant browsing
//     work for non-superusers added via PR 3's member management).
//
// Lookup errors are treated as "not a member" — the safest fallback
// that doesn't expose other tenants on a transient DB hiccup.
func IsTenantAccessible(
	ctx context.Context,
	user *types.User,
	targetTenantID uint64,
	memberService interfaces.TenantMemberService,
	cfg *config.Config,
) bool {
	if user == nil || targetTenantID == 0 {
		return false
	}
	if user.TenantID == targetTenantID {
		return true
	}
	if user.IsSystemAdmin {
		return true
	}
	if memberService == nil {
		return false
	}
	m, err := memberService.GetMembership(ctx, user.ID, targetTenantID)
	if err != nil || m == nil {
		return false
	}
	return m.Status == types.TenantMemberStatusActive
}

// RequireCrossTenantAccess gates a route on the caller being a platform
// SuperAdmin (or a platform API key). Used by /tenants/all and
// /tenants/search — endpoints that operate across tenants rather than
// inside one.
//
// Unlike RequireRole this is NOT modulated by cfg.Tenant.EnableRBAC:
// cross-workspace operations are always sensitive regardless of whether
// per-tenant RBAC is being enforced, so we never log-and-pass. The legacy
// CanAccessAllTenants attribute and the EnableCrossTenantAccess cluster
// flag no longer play any part: platform authority is IsSystemAdmin only.
func RequireCrossTenantAccess(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if scope, ok := types.TenantAPIKeyScopeFromContext(ctx); ok && scope.IsPlatform() {
			c.Next()
			return
		}
		if !IsPlatformSuperAdmin(ctx) {
			uid, _ := types.UserIDFromContext(ctx)
			logger.Warnf(ctx,
				"[rbac] cross-tenant route blocked (not a SuperAdmin): user=%s path=%s",
				uid, c.Request.URL.Path)
			_ = c.Error(apperrors.NewForbiddenError(
				"Insufficient permissions for cross-organization operation"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequirePathTenantMatch gates a route on the URL :id matching the
// caller's active tenant context. A platform SuperAdmin bypasses the
// match: SuperAdmin authority does not depend on the active tenant.
//
// The router places this on the /tenants/:id group so every per-tenant
// endpoint (GetTenant / UpdateTenant / DeleteTenant /
// member management / leave) shares the same check, replacing what was
// previously a copy-pasted block in each handler.
//
// Reads :id off c.Param. If the param is missing or non-numeric the
// request is rejected as 400 — the route can't have meaningfully
// matched without it.
func RequirePathTenantMatch(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		raw := strings.TrimSpace(c.Param("id"))
		if raw == "" {
			_ = c.Error(apperrors.NewValidationError("organization id is required"))
			c.Abort()
			return
		}
		pathTenantID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || pathTenantID == 0 {
			_ = c.Error(apperrors.NewValidationError("organization id must be a positive integer"))
			c.Abort()
			return
		}
		ctxTenantID, ok := types.TenantIDFromContext(ctx)
		if !ok || ctxTenantID == 0 {
			// Auth middleware should always have set this; if not we
			// fail closed rather than silently treating "no context" as
			// a match.
			logger.Warnf(ctx, "[rbac] path-tenant-match: no tenant in ctx, path=%s", c.Request.URL.Path)
			_ = c.Error(apperrors.NewUnauthorizedError("organization context missing"))
			c.Abort()
			return
		}
		if pathTenantID == ctxTenantID {
			c.Next()
			return
		}
		if IsPlatformSuperAdmin(ctx) {
			c.Next()
			return
		}
		uid, _ := types.UserIDFromContext(ctx)
		logger.Warnf(ctx,
			"[rbac] path-tenant-match rejected: user=%s ctx_tenant=%d path_tenant=%d path=%s",
			uid, ctxTenantID, pathTenantID, c.Request.URL.Path)
		_ = c.Error(apperrors.NewForbiddenError(
			"Access denied: URL organization does not match the active organization"))
		c.Abort()
	}
}
