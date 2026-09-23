package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
)

func serveAbbreviationGuardRoute(
	t *testing.T, method, path string,
	role types.TenantRole, systemAdmin bool,
) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	g := &rbacGuards{cfg: &config.Config{}}
	engine.Handle(http.MethodGet, "/member", g.MemberOrSystemAdmin(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	engine.Handle(http.MethodPatch, "/owner", g.OwnerOrSystemAdmin(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(method, path, nil)
	ctx := types.WithCaller(req.Context(), types.Caller{
		TenantID: 1, UserID: "u", Role: role,
	})
	if systemAdmin {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req.WithContext(ctx))
	return recorder.Code
}

func TestAbbreviationGuardsMemberCannotMutate(t *testing.T) {
	if code := serveAbbreviationGuardRoute(t, http.MethodGet, "/member", types.TenantRoleMember, false); code != http.StatusOK {
		t.Fatalf("member GET = %d", code)
	}
	if code := serveAbbreviationGuardRoute(t, http.MethodPatch, "/owner", types.TenantRoleMember, false); code != http.StatusForbidden {
		t.Fatalf("member PATCH = %d, want 403", code)
	}
}

func TestAbbreviationGuardsOwnerCanMutate(t *testing.T) {
	if code := serveAbbreviationGuardRoute(t, http.MethodPatch, "/owner", types.TenantRoleOwner, false); code != http.StatusOK {
		t.Fatalf("owner PATCH = %d", code)
	}
}

func TestAbbreviationGuardsSystemAdminBypasses(t *testing.T) {
	if code := serveAbbreviationGuardRoute(t, http.MethodGet, "/member", types.TenantRoleMember, true); code != http.StatusOK {
		t.Fatalf("system admin GET = %d", code)
	}
	if code := serveAbbreviationGuardRoute(t, http.MethodPatch, "/owner", types.TenantRoleMember, true); code != http.StatusOK {
		t.Fatalf("system admin PATCH = %d", code)
	}
}

func TestRegisterAbbreviationRoutesRegistersAllMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	g := &rbacGuards{cfg: &config.Config{}}
	RegisterAbbreviationRoutes(engine.Group("/api/v1"), handler.NewAbbreviationHandler(nil), g)

	want := map[string]bool{
		"GET /api/v1/abbreviations":        false,
		"GET /api/v1/abbreviations/:id":    false,
		"POST /api/v1/abbreviations":       false,
		"PATCH /api/v1/abbreviations/:id":  false,
		"DELETE /api/v1/abbreviations/:id": false,
	}
	for _, r := range engine.Routes() {
		key := r.Method + " " + r.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Fatalf("route missing: %s", key)
		}
	}
}
