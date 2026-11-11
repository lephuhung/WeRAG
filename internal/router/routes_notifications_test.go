package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
)

// The notification routes are declared to no API-key policy, so the gate on
// /api/v1 refuses every API key (even a full-access one) before the handler.
func TestNotificationRoutesRefuseAPIKeys(t *testing.T) {
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
	RegisterUserNotificationRoutes(v1, handler.NewUserNotificationHandler(nil))

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/notifications"},
		{http.MethodGet, "/api/v1/notifications/unread-count"},
		{http.MethodPost, "/api/v1/notifications/seen"},
		{http.MethodPost, "/api/v1/notifications/read-all"},
		{http.MethodPost, "/api/v1/notifications/abc/read"},
	} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"ids":[]}`)))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s with an API key = %d, want 403", tc.method, tc.path, w.Code)
		}
	}
}
