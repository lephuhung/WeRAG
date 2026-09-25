package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func tenantAdminTestCtx(role types.TenantRole, sysAdmin bool) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/knowledge-bases", nil)
	c.Request = req
	ctx := req.Context()
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: "u1", Role: role})
	if sysAdmin {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	c.Request = req.WithContext(ctx)
	return c
}

func rolloutOffConfig() *config.Config {
	cfg := &config.Config{}
	off := false
	cfg.Tenant = &config.TenantConfig{EnableRBAC: &off}
	return cfg
}

func TestRequireTenantAdminEnforcedWhenRolloutOff(t *testing.T) {
	cfg := rolloutOffConfig()
	// Sanity: rollout flag off.
	if cfg.Tenant.IsRBACEnforced() {
		t.Fatal("expected RBAC enforcement off for this test")
	}
	cases := []struct {
		name    string
		role    types.TenantRole
		sys     bool
		allowed bool
	}{
		{"member denied even with enforcement off", types.TenantRoleMember, false, false},
		{"empty role denied", types.TenantRole(""), false, false},
		{"admin allowed", types.TenantRoleAdmin, false, true},
		{"legacy owner allowed", types.TenantRoleOwner, false, true},
		{"member+sysadmin allowed", types.TenantRoleMember, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tenantAdminTestCtx(tc.role, tc.sys)
			RequireTenantAdmin(cfg)(c)
			if tc.allowed && c.IsAborted() {
				t.Errorf("role %q (sys=%v) should pass, got %d", tc.role, tc.sys, c.Writer.Status())
			}
			if !tc.allowed && !c.IsAborted() {
				t.Errorf("role %q (sys=%v) should be denied", tc.role, tc.sys)
			}
			if !tc.allowed && c.Writer.Status() != 403 {
				t.Errorf("expected 403, got %d", c.Writer.Status())
			}
		})
	}
}

func TestRequireRoleLogsAndPassesWhenRolloutOff(t *testing.T) {
	// Contrast: the regular RequireRole guard must NOT deny when off
	// (this documents why sensitive ops need RequireTenantAdmin).
	cfg := rolloutOffConfig()
	c := tenantAdminTestCtx(types.TenantRoleMember, false)
	RequireRole(types.TenantRoleAdmin, cfg)(c)
	if c.IsAborted() {
		t.Error("RequireRole should log-and-pass when enforcement is off")
	}
}
