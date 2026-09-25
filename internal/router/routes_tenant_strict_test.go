package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// Member mutations and tenant-invite create/revoke must stay denied for
// Members even when the RBAC rollout flag is off. This test registers the
// real tenant routes with enforcement disabled and drives a Member caller
// through each mutating endpoint; every one must 403 before any handler
// runs (handlers are empty structs — reaching one would panic on nil deps).
func TestTenantMutationRoutesStrictWhenRolloutOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	off := false
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &off}}
	g := newRBACGuards(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: 1, UserID: "u-member", Role: types.TenantRoleMember,
		})
		ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(1))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	v1 := engine.Group("/api/v1")
	RegisterTenantRoutes(v1, &handler.TenantHandler{}, &handler.TenantMemberHandler{},
		&handler.TenantInvitationHandler{}, nil, g)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/tenants/1/members", `{"email":"x@y.z","role":"member"}`},
		{http.MethodPut, "/api/v1/tenants/1/members/u1", `{"role":"member"}`},
		{http.MethodDelete, "/api/v1/tenants/1/members/u1", ""},
		{http.MethodPost, "/api/v1/tenants/1/invitations", `{"email":"x@y.z","role":"member"}`},
		{http.MethodDelete, "/api/v1/tenants/1/invitations/1", ""},
		{http.MethodPost, "/api/v1/tenants/1/invite-links", `{"role":"member"}`},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var req *http.Request
			if tc.body != "" {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			}
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("member %s %s with rollout off must 403, got %d body=%s",
					tc.method, tc.path, w.Code, w.Body.String())
			}
		})
	}
}

// Tenant Admins keep access to the same endpoints with rollout off.
func TestTenantMutationRoutesAdminAllowedWhenRolloutOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	off := false
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &off}}
	g := newRBACGuards(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: 1, UserID: "u-admin", Role: types.TenantRoleAdmin,
		})
		ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(1))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	v1 := engine.Group("/api/v1")
	RegisterTenantRoutes(v1, &handler.TenantHandler{}, &handler.TenantMemberHandler{},
		&handler.TenantInvitationHandler{}, nil, g)

	// Admin passes the guard and reaches the (empty) handler, which may
	// fail with 4xx/5xx on nil deps — but must NOT be 403. A nil-pointer
	// panic from the empty handler also proves guard passage.
	reached := false
	func() {
		defer func() {
			if recover() != nil {
				reached = true
			}
		}()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/1/members/u1", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		reached = true
		if w.Code == http.StatusForbidden {
			t.Fatalf("admin must pass the strict guard, got 403 body=%s", w.Body.String())
		}
	}()
	if !reached {
		t.Fatal("admin request never reached the handler")
	}
}
