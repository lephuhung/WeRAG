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

	registered := false
	for _, r := range engine.Routes() {
		if r.Method == http.MethodPost && r.Path == "/api/v1/system/domain-packs/:name/reconcile" {
			registered = true
		}
	}
	if !registered {
		t.Fatal("reconcile route is not registered")
	}

	for _, role := range []types.TenantRole{types.TenantRoleMember, types.TenantRoleOwner} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/domain-packs/opc-vn-business-law/reconcile", nil)
		ctx := types.WithCaller(req.Context(), types.Caller{TenantID: 1, UserID: "u", Role: role})
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req.WithContext(ctx))
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: got %d, want 401/403", role, rec.Code)
		}
	}
}
