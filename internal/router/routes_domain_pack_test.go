package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestDomainPackReconcileRouteNeedsPlatformSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	g := &rbacGuards{cfg: &config.Config{}}
	RegisterDomainPackReconcileRoutes(engine.Group("/api/v1"), handler.NewDomainPackReconcileHandler(nil), g)

	want := map[string]bool{
		"POST /api/v1/system/domain-packs/:name/reconcile":      false,
		"GET /api/v1/system/domain-packs":                       false,
		"GET /api/v1/system/domain-packs/:name":                 false,
		"PUT /api/v1/system/domain-packs/:name/formulas/:id":    false,
		"DELETE /api/v1/system/domain-packs/:name/formulas/:id": false,
	}
	for _, r := range engine.Routes() {
		if _, ok := want[r.Method+" "+r.Path]; ok {
			want[r.Method+" "+r.Path] = true
		}
	}
	for route, ok := range want {
		if !ok {
			t.Fatalf("%s is not registered", route)
		}
	}

	for _, probe := range []struct {
		role         types.TenantRole
		method, path string
	}{
		{types.TenantRoleMember, http.MethodPost, "/api/v1/system/domain-packs/opc-vn-business-law/reconcile"},
		{types.TenantRoleOwner, http.MethodPost, "/api/v1/system/domain-packs/opc-vn-business-law/reconcile"},
		{types.TenantRoleOwner, http.MethodGet, "/api/v1/system/domain-packs"},
		{types.TenantRoleOwner, http.MethodGet, "/api/v1/system/domain-packs/opc-vn-business-law"},
		{types.TenantRoleOwner, http.MethodPut, "/api/v1/system/domain-packs/opc-vn-business-law/formulas/bien_hoa_von"},
		{types.TenantRoleOwner, http.MethodDelete, "/api/v1/system/domain-packs/opc-vn-business-law/formulas/bien_hoa_von"},
	} {
		role := probe.role
		req := httptest.NewRequest(probe.method, probe.path, nil)
		ctx := types.WithCaller(req.Context(), types.Caller{TenantID: 1, UserID: "u", Role: role})
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req.WithContext(ctx))
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: got %d, want 401/403", role, rec.Code)
		}
	}
}
