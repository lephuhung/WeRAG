package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ConfigProfileScope moves a SuperAdmin's configuration write onto the
// configuration profile the current workspace inherits from.
//
// Configuration is platform-wide: models, agents and the other shared rows
// live in the profile and every workspace on it reads them from there. A
// SuperAdmin works from some workspace, so without this a save would land in
// that one workspace and reach nobody else. The guard rewrites the tenant on
// both context surfaces (gin keys and the request context) so handlers,
// services and repositories all see the profile; the user, the system-admin
// flag and the principal stay as they are.
//
// Callers that are not human SystemAdmins pass through untouched: the route
// guard in front of this one already decided whether they may proceed, and
// API keys keep their own scope. A workspace without a profile also passes
// through, so a deployment that never created one behaves as before.
func ConfigProfileScope(tenantService interfaces.TenantService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if _, isAPIKey := types.TenantAPIKeyScopeFromContext(ctx); isAPIKey || !types.IsSystemAdminFromContext(ctx) {
			c.Next()
			return
		}
		tenant, _ := types.TenantInfoFromContext(ctx)
		if tenant == nil || tenant.IsConfigProfile() || tenant.ConfigProfileID == nil || tenantService == nil {
			c.Next()
			return
		}
		profile, err := tenantService.GetTenantByID(ctx, *tenant.ConfigProfileID)
		if err != nil || profile == nil || !profile.IsConfigProfile() {
			logger.Errorf(ctx, "[config-profile] cannot load profile %d for tenant %d: %v",
				*tenant.ConfigProfileID, tenant.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "configuration profile unavailable"})
			c.Abort()
			return
		}

		set := func(key types.ContextKey, v any) {
			c.Set(key.String(), v)
			ctx = context.WithValue(ctx, key, v)
		}
		set(types.TenantIDContextKey, profile.ID)
		set(types.TenantInfoContextKey, profile)
		set(types.TenantRoleContextKey, types.TenantRoleAdmin)
		caller := types.CallerFromContext(ctx)
		caller.TenantID = profile.ID
		caller.Role = types.TenantRoleAdmin
		ctx = types.WithCaller(ctx, caller)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
