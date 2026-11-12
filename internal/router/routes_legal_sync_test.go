package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

func legalSyncTestHandler(t *testing.T) *handler.LegalSyncHandler {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.LegalSyncRun{}, &types.LegalDocumentRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, metadata TEXT, parse_status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE legal_documents (id TEXT PRIMARY KEY, source TEXT, status TEXT)`,
		`CREATE TABLE legal_feed_log (id INTEGER PRIMARY KEY, action TEXT, vbpl_id TEXT, detail TEXT, created_at DATETIME)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	return handler.NewLegalSyncHandler(service.NewLegalSyncMonitor(db, repository.NewLegalSyncRunRepository(db), nil))
}

// legalSyncTestEngine seeds a caller like legalRequestTestEngine; the
// real guards decide.
func legalSyncTestEngine(t *testing.T, caller types.Caller, systemAdmin, legalUploader bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CallerContextKey, caller.Normalize())
		ctx = context.WithValue(ctx, types.UserIDContextKey, caller.UserID)
		ctx = context.WithValue(ctx, types.TenantIDContextKey, caller.TenantID)
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, systemAdmin)
		ctx = context.WithValue(ctx, types.LegalUploaderContextKey, legalUploader)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	RegisterLegalSyncRoutes(r.Group("/api/v1"), legalSyncTestHandler(t), &rbacGuards{cfg: &config.Config{}})
	return r
}

// The legal sync monitor is SuperAdmin only: not the legal-document
// operators, not organization members.
func TestLegalSyncRoutesSuperAdminOnly(t *testing.T) {
	paths := []string{"/api/v1/system/legal-sync/status", "/api/v1/system/legal-sync/runs"}
	admin := legalSyncTestEngine(t, types.Caller{TenantID: types.PlatformTenantID, UserID: "u-admin"}, true, false)
	operator := legalSyncTestEngine(t, types.Caller{TenantID: types.PlatformTenantID, UserID: "u-operator"}, false, true)
	member := legalSyncTestEngine(t, types.Caller{TenantID: 7, UserID: "u-member", Role: types.TenantRoleAdmin}, false, false)
	for _, p := range paths {
		if code := serve(admin, http.MethodGet, p, ""); code != http.StatusOK {
			t.Fatalf("SuperAdmin GET %s = %d, want 200", p, code)
		}
		if code := serve(operator, http.MethodGet, p, ""); code != http.StatusForbidden {
			t.Fatalf("operator GET %s = %d, want 403", p, code)
		}
		if code := serve(member, http.MethodGet, p, ""); code != http.StatusForbidden {
			t.Fatalf("tenant admin GET %s = %d, want 403", p, code)
		}
	}
}

// The routes are declared to no API-key policy: the gate on /api/v1
// refuses every API key before the SuperAdmin guard and handler.
func TestLegalSyncRoutesRefuseAPIKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g := &rbacGuards{cfg: &config.Config{}}
	gate := g.ensureAPIKeyAuthorizer()
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := types.WithTenantAPIKeyScope(c.Request.Context(), types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	RegisterLegalSyncRoutes(engine.Group("/api/v1", gate.Middleware()), legalSyncTestHandler(t), g)
	for _, p := range []string{"/api/v1/system/legal-sync/status", "/api/v1/system/legal-sync/runs"} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("GET %s with an API key = %d, want 403", p, w.Code)
		}
	}
	// nil handler: no route
	RegisterLegalSyncRoutes(engine.Group("/x"), nil, g)
}
