package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func TestTenantOptionalAPISurface(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/api/v1/auth/me", true},
		{http.MethodPut, "/api/v1/auth/me", true},
		{http.MethodPut, "/api/v1/auth/me/preferences", true},
		{http.MethodPost, "/api/v1/tenants", true},
		{http.MethodGet, "/api/v1/me/invitations", true},
		{http.MethodPost, "/api/v1/me/invitations/12/accept", true},
		{http.MethodGet, "/api/v1/knowledge-bases", false},
		{http.MethodGet, "/api/v1/tenants", false},
	}
	for _, tt := range tests {
		if got := isTenantOptionalAPI(tt.path, tt.method); got != tt.want {
			t.Errorf("isTenantOptionalAPI(%s %s) = %v, want %v", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestResolveFirstMembershipTarget(t *testing.T) {
	members := newFakeMemberService()
	members.seedActive("tenantless-user", 42, types.TenantRoleMember)
	tenants := &fakeTenantService{tenant: &types.Tenant{ID: 42}}

	got := resolveFirstMembershipTarget(
		context.Background(),
		&types.User{ID: "tenantless-user"},
		members,
		tenants,
	)
	if got != 42 {
		t.Fatalf("resolved tenant = %d, want 42", got)
	}
}

// tenantlessAuthHarness runs the JWT half of Auth (tenant + role
// resolution) in front of the real SystemAdmin group guard, so a test sees
// what a browser session gets for identity and platform routes.
func tenantlessAuthHarness(t *testing.T, user *types.User, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := cfgWithRBAC(true)
	members := newFakeMemberService()
	// Tenant 7 already has an Admin, so no orphan-tenant path applies.
	members.seedActive("tenant-7-admin", 7, types.TenantRoleAdmin)
	tenants := &fakeTenantService{tenant: &types.Tenant{ID: user.TenantID}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if authenticateJWTUser(c, tenants, members, cfg, user, user.TenantID) {
			c.Next()
		}
	})
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	admin := r.Group("/api/v1/system/admin", RequireSystemAdmin(cfg))
	admin.GET("/settings", ok)
	admin.GET("/stats", ok)
	r.GET("/api/v1/auth/2fa/status", ok)
	r.POST("/api/v1/auth/2fa/setup", ok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// F3: a SuperAdmin needs no workspace for platform and identity routes.
func TestTenantlessSuperAdminReachesPlatformAndIdentityRoutes(t *testing.T) {
	cases := []struct {
		name string
		user *types.User
	}{
		{"no home tenant", &types.User{ID: "root", IsSystemAdmin: true}},
		{"home tenant without membership", &types.User{ID: "root", TenantID: 7, IsSystemAdmin: true}},
	}
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/system/admin/settings"},
		{http.MethodGet, "/api/v1/system/admin/stats"},
		{http.MethodGet, "/api/v1/auth/2fa/status"},
		{http.MethodPost, "/api/v1/auth/2fa/setup"},
	}
	for _, tc := range cases {
		for _, rt := range routes {
			w := tenantlessAuthHarness(t, tc.user, rt.method, rt.path)
			if w.Code != http.StatusOK {
				t.Errorf("%s: %s %s = %d %s, want 200", tc.name, rt.method, rt.path, w.Code, w.Body.String())
			}
		}
	}
}

func TestTenantlessNonAdminCannotReachPlatformRoutes(t *testing.T) {
	user := &types.User{ID: "plain"}
	w := tenantlessAuthHarness(t, user, http.MethodGet, "/api/v1/system/admin/settings")
	if w.Code == http.StatusOK {
		t.Fatalf("tenantless non-admin must not reach /system/admin/settings, got 200")
	}
	if w.Code != http.StatusForbidden && !strings.Contains(w.Body.String(), "TENANT_REQUIRED") {
		t.Fatalf("want 403 or TENANT_REQUIRED, got %d %s", w.Code, w.Body.String())
	}
	// 2FA is identity-level: every signed-in user manages their own.
	if w := tenantlessAuthHarness(t, user, http.MethodGet, "/api/v1/auth/2fa/status"); w.Code != http.StatusOK {
		t.Fatalf("tenantless user must reach 2FA status, got %d %s", w.Code, w.Body.String())
	}
}
