package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// superAdminTenantHarness runs the JWT half of Auth with an X-Tenant-ID
// header in front of tenant routes guarded by the real role gates, so a
// test sees what a browser session gets on a tenant route.
func superAdminTenantHarness(
	t *testing.T, user *types.User, members *fakeMemberService, method, path string,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := cfgWithRBAC(true)
	tenants := &fakeTenantService{tenant: &types.Tenant{ID: 9}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if authenticateJWTUser(c, tenants, members, cfg, user, user.TenantID) {
			c.Next()
		}
	})
	ok := func(c *gin.Context) {
		caller := types.CallerFromContext(c.Request.Context())
		tenantID, _ := types.TenantIDFromContext(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"tenant": tenantID, "caller_tenant": caller.TenantID, "user": caller.UserID})
	}
	r.POST("/api/v1/knowledge-bases", RequireTenantAdmin(cfg), ok)
	r.GET("/api/v1/models", RequireRole(types.TenantRoleAdmin, cfg), ok)
	r.PUT("/api/v1/agents/a1", RequireOwnershipOrRole(types.TenantRoleAdmin,
		func(*gin.Context) (string, error) { return "someone-else", nil }, cfg), ok)
	r.GET("/api/v1/sessions", RequireRole(types.TenantRoleMember, cfg), ok)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Tenant-ID", "9")
	r.ServeHTTP(w, req)
	return w
}

var superAdminTenantRoutes = []struct{ method, path string }{
	{http.MethodPost, "/api/v1/knowledge-bases"},
	{http.MethodGet, "/api/v1/models"},
	{http.MethodPut, "/api/v1/agents/a1"},
	{http.MethodGet, "/api/v1/sessions"},
}

// Phase 2: a SuperAdmin needs no membership in the selected tenant; it
// runs there with no tenant role and passes every role gate as SuperAdmin.
func TestSuperAdminWithoutMembershipPassesTenantRoleGates(t *testing.T) {
	members := newFakeMemberService()
	members.seedActive("tenant-9-admin", 9, types.TenantRoleAdmin)
	user := &types.User{ID: "root", TenantID: 7, IsSystemAdmin: true}
	for _, rt := range superAdminTenantRoutes {
		w := superAdminTenantHarness(t, user, members, rt.method, rt.path)
		if w.Code != http.StatusOK {
			t.Errorf("%s %s = %d %s, want 200", rt.method, rt.path, w.Code, w.Body.String())
		}
	}
}

// The legacy flag grants nothing: a non-SuperAdmin without a membership
// in the selected tenant is refused before any route runs.
func TestLegacyFlagWithoutMembershipStillRefused(t *testing.T) {
	members := newFakeMemberService()
	members.seedActive("flagged", 9, types.TenantRoleMember)
	// Member of tenant 9 with the legacy flag: role gates above Member
	// still refuse.
	user := &types.User{ID: "flagged", TenantID: 9, CanAccessAllTenants: true}
	for _, rt := range superAdminTenantRoutes[:3] {
		w := superAdminTenantHarness(t, user, members, rt.method, rt.path)
		if w.Code != http.StatusForbidden {
			t.Errorf("member with flag: %s %s = %d, want 403", rt.method, rt.path, w.Code)
		}
	}
	// No membership in tenant 9 at all: refused (403) on every route.
	stranger := &types.User{ID: "stranger", TenantID: 9, CanAccessAllTenants: true}
	for _, rt := range superAdminTenantRoutes {
		w := superAdminTenantHarness(t, stranger, newFakeMemberService(), rt.method, rt.path)
		if w.Code != http.StatusForbidden {
			t.Errorf("flag without membership: %s %s = %d, want 403", rt.method, rt.path, w.Code)
		}
	}
}

// A synthetic identity carrying the system-admin flag is not a human
// SuperAdmin and gets no membership-free access.
func TestSyntheticSystemAdminWithoutMembershipRefused(t *testing.T) {
	user := &types.User{ID: "system-9", TenantID: 9, IsSystemAdmin: true}
	w := superAdminTenantHarness(t, user, newFakeMemberService(), http.MethodGet, "/api/v1/sessions")
	if w.Code != http.StatusForbidden {
		t.Fatalf("synthetic system admin without membership = %d, want 403", w.Code)
	}
}

// API keys never gain anything from the SuperAdmin branch: the role gates
// still short-circuit them to the API-key gate only, and the
// IsExplicitHumanSuperAdmin check is false for any key scope.
func TestAPIKeyNeverCountsAsSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := cfgWithRBAC(true)
	r := gin.New()
	r.Use(ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: 9, UserID: "root"})
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
		ctx = withSystemAdmin(ctx)
		c.Request = c.Request.WithContext(ctx)
		if IsPlatformSuperAdmin(c.Request.Context()) {
			_ = c.Error(errors.New("API key counted as SuperAdmin"))
			c.AbortWithStatus(http.StatusTeapot)
			return
		}
		c.Next()
	})
	r.GET("/x", RequireCrossTenantAccess(cfg), func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("tenant API key with system-admin flag on cross-tenant route = %d, want 403", w.Code)
	}
}

func withSystemAdmin(ctx context.Context) context.Context {
	return context.WithValue(ctx, types.SystemAdminContextKey, true)
}

// guestHarness runs the JWT half of Auth with no X-Tenant-ID in front of
// the KB-creating routes guarded as in production (RequireTenantAdmin).
func guestHarness(t *testing.T, user *types.User, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := cfgWithRBAC(true)
	members := newFakeMemberService()
	tenants := &fakeTenantService{tenant: &types.Tenant{ID: user.TenantID}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if authenticateJWTUser(c, tenants, members, cfg, user, user.TenantID) {
			c.Next()
		}
	})
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.POST("/api/v1/knowledge-bases", RequireTenantAdmin(cfg), ok)
	r.POST("/api/v1/knowledge-bases/copy", RequireTenantAdmin(cfg), ok)
	r.POST("/api/v1/knowledge-bases/:id/duplicate", RequireTenantAdmin(cfg), ok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// Phase 2: a Guest (no active membership) cannot create, copy or
// duplicate a KB: tenantless it gets TENANT_REQUIRED, with a stale home
// tenant it gets 403.
func TestGuestCannotCreateKnowledgeBase(t *testing.T) {
	paths := []string{
		"/api/v1/knowledge-bases",
		"/api/v1/knowledge-bases/copy",
		"/api/v1/knowledge-bases/kb-1/duplicate",
	}
	for _, p := range paths {
		w := guestHarness(t, &types.User{ID: "guest"}, http.MethodPost, p)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "TENANT_REQUIRED") {
			t.Errorf("tenantless guest POST %s = %d %s, want 409 TENANT_REQUIRED", p, w.Code, w.Body.String())
		}
		w = guestHarness(t, &types.User{ID: "guest", TenantID: 7}, http.MethodPost, p)
		if w.Code != http.StatusForbidden {
			t.Errorf("guest with stale home tenant POST %s = %d, want 403", p, w.Code)
		}
	}
}

// RequireTenantAdmin itself refuses a caller without a tenant role even if
// a tenant context is present (defence in depth for the empty role).
func TestRequireTenantAdminRefusesEmptyRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := cfgWithRBAC(true)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(),
			types.Caller{TenantID: 7, UserID: "guest"}))
		c.Next()
	})
	r.POST("/api/v1/knowledge-bases", RequireTenantAdmin(cfg), func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("empty role = %d, want 403", w.Code)
	}
}
