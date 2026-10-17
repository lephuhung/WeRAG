package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Task 3: KB lifecycle routes run the owner-aware manage guard, and public
// creation has a dedicated explicit-human-SuperAdmin route with no API-key
// policy (API keys stay default-denied).
func newLifecycleRouteEngine(
	t *testing.T,
	tenantID uint64,
	userID string,
	role types.TenantRole,
	sysAdmin bool,
	kb *types.KnowledgeBase,
	register func(r *gin.RouterGroup, g *rbacGuards),
) (*gin.Engine, *rbacGuards) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enabled := true
	guards := &rbacGuards{
		cfg:       &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
		kbService: &stubWikiKBLookup{kbs: map[string]*types.KnowledgeBase{kb.ID: kb}},
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: tenantID, UserID: userID, Role: role,
		})
		if sysAdmin {
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
		}
		ctx = types.WithExecutionTenant(ctx, tenantID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	register(r.Group("/api/v1"), guards)
	return r, guards
}

func lifecycleRegister(r *gin.RouterGroup, g *rbacGuards) {
	kbgrp := r.Group("/knowledge-bases")
	kb := g.apiKeyGroup(kbgrp, apiKeyRetrieve(apiKeyFullAccess()))
	kbManagement := kb.With(apiKeyManageKnowledgeBases(apiKeyFullAccess()))
	kbManagement.POST("", g.TenantAdmin(), okStub)
	kbgrp.POST("/public", g.PlatformSuperAdmin(), okStub)
	kbManagement.PUT("/:id", g.TenantAdmin(), g.KBAccessManage("id"), okStub)
	kbManagement.PUT("/:id/visibility", g.TenantAdmin(), g.KBAccessManage("id"), okStub)
	kbManagement.DELETE("/:id", g.TenantAdmin(), g.KBAccessManage("id"), okStub)
	kbgrp.POST("/:id/transfer-owner", g.PlatformSuperAdmin(), okStub)
}

func TestLifecycleUpdateManageGuard(t *testing.T) {
	pub := publicRouteKB("kb-pub", 0)
	// Tenantless explicit human SuperAdmin reaches the public row: route
	// middleware must not reject for missing tenant context.
	superEngine, _ := newLifecycleRouteEngine(t, 0, "super-1", types.TenantRoleMember, true, pub, lifecycleRegister)
	rec := httptest.NewRecorder()
	superEngine.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-bases/kb-pub", nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())

	// Tenant Admin of another tenant cannot touch the public row.
	adminEngine, _ := newLifecycleRouteEngine(t, 3, "admin-1", types.TenantRoleAdmin, false, pub, lifecycleRegister)
	rec = httptest.NewRecorder()
	adminEngine.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-bases/kb-pub", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())

	// Settled 2026-10-17 (Phase 2, F7): an explicit human SuperAdmin holds
	// every right on every KB, tenant-owned included, without a membership.
	owned := ownedRouteKB("kb-own", 1)
	foreignSuper, _ := newLifecycleRouteEngine(t, 0, "super-1", types.TenantRoleMember, true, owned, lifecycleRegister)
	rec = httptest.NewRecorder()
	foreignSuper.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge-bases/kb-own", nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())

	// A tenantless non-SuperAdmin still cannot touch the tenant-owned row.
	tenantless, _ := newLifecycleRouteEngine(t, 0, "user-a", types.TenantRoleMember, false, owned, lifecycleRegister)
	rec = httptest.NewRecorder()
	tenantless.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge-bases/kb-own", nil))
	require.NotEqual(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())
}

func TestLifecycleVisibilityManageGuard(t *testing.T) {
	pub := publicRouteKB("kb-pub", 0)
	engine, _ := newLifecycleRouteEngine(t, 0, "super-1", types.TenantRoleMember, true, pub, lifecycleRegister)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-bases/kb-pub/visibility", nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())

	member, _ := newLifecycleRouteEngine(t, 5, "user-a", types.TenantRoleMember, false, pub, lifecycleRegister)
	rec = httptest.NewRecorder()
	member.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-bases/kb-pub/visibility", nil))
	require.NotEqual(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())
}

func TestPublicCreateRouteSuperAdminOnly(t *testing.T) {
	pub := publicRouteKB("kb-pub", 0)
	superEngine, _ := newLifecycleRouteEngine(t, 0, "super-1", types.TenantRoleMember, true, pub, lifecycleRegister)
	rec := httptest.NewRecorder()
	superEngine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/public", nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())

	adminEngine, _ := newLifecycleRouteEngine(t, 3, "admin-1", types.TenantRoleAdmin, false, pub, lifecycleRegister)
	rec = httptest.NewRecorder()
	adminEngine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/public", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

func TestPublicCreateRouteDeclaresNoAPIKeyPolicy(t *testing.T) {
	pub := publicRouteKB("kb-pub", 0)
	_, guards := newLifecycleRouteEngine(t, 0, "super-1", types.TenantRoleMember, true, pub, lifecycleRegister)
	for method, paths := range guards.apiKeyAuthorizer.RegisteredRoutes() {
		for _, p := range paths {
			require.NotEqual(t, "POST /api/v1/knowledge-bases/public", method+" "+p,
				"public create must not be API-key reachable; manage_kbs is not system-admin authority")
		}
	}
}

// Phase 4: owner transfer is explicit-human-SuperAdmin only — Tenant
// Admins (owner or target) and API keys are denied, and no API-key policy
// is declared for the route.
func TestTransferOwnerRouteSuperAdminOnly(t *testing.T) {
	owned := ownedRouteKB("kb-own", 1)
	path := "/api/v1/knowledge-bases/kb-own/transfer-owner"

	superEngine, guards := newLifecycleRouteEngine(t, 0, "super-1", types.TenantRoleMember, true, owned, lifecycleRegister)
	rec := httptest.NewRecorder()
	superEngine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())
	for method, paths := range guards.apiKeyAuthorizer.RegisteredRoutes() {
		for _, p := range paths {
			require.NotEqual(t, "POST /api/v1/knowledge-bases/:id/transfer-owner", method+" "+p,
				"transfer-owner must not be API-key reachable")
		}
	}

	ownerAdmin, _ := newLifecycleRouteEngine(t, 1, "admin-1", types.TenantRoleAdmin, false, owned, lifecycleRegister)
	rec = httptest.NewRecorder()
	ownerAdmin.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())

	// An API key carrying manage_kbs and even the system-admin flag is not
	// an explicit human SuperAdmin.
	gin.SetMode(gin.TestMode)
	enabled := true
	g := &rbacGuards{
		cfg:       &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
		kbService: &stubWikiKBLookup{kbs: map[string]*types.KnowledgeBase{owned.ID: owned}},
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: 1, UserID: "system-1", Role: types.TenantRoleAdmin})
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{
			KeyID: 7, Name: "k", Capabilities: types.StringArray{string(types.APIKeyCapabilityManageKnowledgeBases)},
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	lifecycleRegister(r.Group("/api/v1"), g)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}
