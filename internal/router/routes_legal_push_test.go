package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

func legalPushTestHandler(t *testing.T) *handler.LegalPushHandler {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.LegalProvisionLinkCandidate{}, &types.LegalDocument{}, &types.LegalDocumentRequest{},
		&types.LegalProvisionLink{}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, metadata TEXT, created_at DATETIME, deleted_at DATETIME)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	return handler.NewLegalPushHandler(service.NewLegalPushService(db, nil, nil, nil, nil), nil)
}

func legalPushTestEngine(t *testing.T, caller types.Caller, systemAdmin, legalUploader bool) *gin.Engine {
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
	RegisterLegalPushRoutes(r.Group("/api/v1"), legalPushTestHandler(t), &rbacGuards{cfg: &config.Config{}})
	return r
}

var legalLinkReviewRoutes = []struct{ method, path, body string }{
	{http.MethodGet, "/api/v1/system/legal-link-candidates/documents", ""},
	{http.MethodGet, "/api/v1/system/legal-link-candidates/provision-text?doc_id=x&path=dieu:1", ""},
	{http.MethodGet, "/api/v1/system/legal-link-candidates/provisions?doc_id=x", ""},
	{http.MethodPost, "/api/v1/system/legal-link-candidates/recheck", `{}`},
	{http.MethodPost, "/api/v1/system/legal-link-candidates/decide-batch", `{"ids":[1],"action":"reject"}`},
}

// The link review stays human SuperAdmin only: not the feed's operator
// (who may push), not an organization admin, not an API key.
func TestLegalLinkReviewRoutesSuperAdminOnly(t *testing.T) {
	admin := legalPushTestEngine(t, types.Caller{TenantID: types.PlatformTenantID, UserID: "u-admin"}, true, false)
	operator := legalPushTestEngine(t, types.Caller{TenantID: types.PlatformTenantID, UserID: "u-operator"}, false, true)
	member := legalPushTestEngine(t, types.Caller{TenantID: 7, UserID: "u-member", Role: types.TenantRoleAdmin}, false, false)
	for _, rt := range legalLinkReviewRoutes {
		if code := serve(admin, rt.method, rt.path, rt.body); code != http.StatusOK {
			t.Fatalf("SuperAdmin %s %s = %d, want 200", rt.method, rt.path, code)
		}
		if code := serve(operator, rt.method, rt.path, rt.body); code != http.StatusForbidden {
			t.Fatalf("operator %s %s = %d, want 403", rt.method, rt.path, code)
		}
		if code := serve(member, rt.method, rt.path, rt.body); code != http.StatusForbidden {
			t.Fatalf("tenant admin %s %s = %d, want 403", rt.method, rt.path, code)
		}
	}

	gin.SetMode(gin.TestMode)
	g := &rbacGuards{cfg: &config.Config{}}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := types.WithTenantAPIKeyScope(c.Request.Context(), types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	RegisterLegalPushRoutes(engine.Group("/api/v1", g.ensureAPIKeyAuthorizer().Middleware()), legalPushTestHandler(t), g)
	for _, rt := range legalLinkReviewRoutes {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body)))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s with an API key = %d, want 403", rt.method, rt.path, w.Code)
		}
	}
}
