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

var documentLabelPaths = []string{
	"/api/v1/document-labels",
	"/api/v1/document-labels/facets",
	"/api/v1/document-labels/export",
}

// The label routes hold users' paragraph text: no API-key policy is
// declared, so the gate refuses every key before the handler.
func TestDocumentLabelRoutesRefuseAPIKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g := &rbacGuards{}
	gate := g.ensureAPIKeyAuthorizer()
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := types.WithTenantAPIKeyScope(c.Request.Context(), types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	v1 := engine.Group("/api/v1", gate.Middleware())
	// nil repository: the handler must never be reached
	RegisterDocumentLabelRoutes(v1, handler.NewDocumentLabelHandler(nil, nil), g)
	for _, p := range documentLabelPaths {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusForbidden {
			t.Errorf("GET %s with an API key = %d, want 403", p, w.Code)
		}
	}
}

// A Member is refused by the strict Tenant Admin guard.
func TestDocumentLabelRoutesRefuseMembers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enabled := true
	g := &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: 7, UserID: "u1", Role: types.TenantRoleMember})
		ctx = context.WithValue(ctx, types.UserIDContextKey, "u1")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	RegisterDocumentLabelRoutes(engine.Group("/api/v1"), handler.NewDocumentLabelHandler(nil, nil), g)
	for _, p := range documentLabelPaths {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusForbidden {
			t.Errorf("GET %s as a Member = %d, want 403", p, w.Code)
		}
	}
}
