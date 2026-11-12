package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
)

// The backup status route is declared to no API-key policy: the gate on
// /api/v1 refuses every API key before the SuperAdmin guard and handler.
func TestBackupStatusRouteRefusesAPIKeys(t *testing.T) {
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
	RegisterBackupStatusRoutes(v1, handler.NewBackupStatusHandler(nil), g)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/admin/backups", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("GET /system/admin/backups with an API key = %d, want 403", w.Code)
	}
	// nil handler: no route
	RegisterBackupStatusRoutes(engine.Group("/x"), nil, g)
}
