package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"PATCH /api/v1/document-labels/spelling:1",
}

// labelRequest builds the request for a documentLabelPaths entry ("PATCH
// /path" or a GET path).
func labelRequest(p string) (string, *http.Request) {
	method := http.MethodGet
	if m, rest, ok := strings.Cut(p, " "); ok {
		method, p = m, rest
	}
	var body io.Reader
	if method != http.MethodGet {
		body = strings.NewReader(`{"review_verdict":"skip"}`)
	}
	req := httptest.NewRequest(method, p, body)
	req.Header.Set("Content-Type", "application/json")
	return method + " " + p, req
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
		name, req := labelRequest(p)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s with an API key = %d, want 403", name, w.Code)
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
		name, req := labelRequest(p)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s as a Member = %d, want 403", name, w.Code)
		}
	}
}
