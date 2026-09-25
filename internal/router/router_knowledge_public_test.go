package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func publicRouteKB(id string, dataTenant uint64) *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: id, TenantID: dataTenant,
		OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}
}

func ownedRouteKB(id string, owner uint64) *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: id, TenantID: owner,
		OwnerTenantID: owner, Visibility: types.KBVisibilityTenant,
	}
}

// newPublicRouteEngine wires guard chains the same way RegisterKnowledgeRoutes
// does, but terminates in a stub handler so allow-cases can assert
// pass-through without executing real services.
func newPublicRouteEngine(
	t *testing.T,
	tenantID uint64,
	userID string,
	role types.TenantRole,
	sysAdmin bool,
	knowledge *types.Knowledge,
	kb *types.KnowledgeBase,
	register func(r *gin.RouterGroup, g *rbacGuards),
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enabled := true
	guards := &rbacGuards{
		cfg:              &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
		knowledgeService: &downloadKnowledgeLookup{knowledge: knowledge},
		kbService:        &stubWikiKBLookup{kbs: map[string]*types.KnowledgeBase{kb.ID: kb}},
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: tenantID, UserID: userID, Role: role,
		})
		if sysAdmin {
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
		}
		ctx = types.WithExecutionTenant(ctx, tenantID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	register(r.Group("/api/v1"), guards)
	return r
}

func okStub(c *gin.Context) { c.Status(http.StatusNoContent) }

func TestPublicDownloadRouteAdmitsCrossTenantViewer(t *testing.T) {
	kb := publicRouteKB("kb-pub", 9)
	knowledge := &types.Knowledge{ID: "doc-1", KnowledgeBaseID: "kb-pub", TenantID: 9}
	engine := newPublicRouteEngine(t, 3, "user-a", types.TenantRoleMember, false, knowledge, kb,
		func(r *gin.RouterGroup, g *rbacGuards) {
			k := g.apiKeyGroup(r.Group("/knowledge"), apiKeyRetrieve(apiKeyFullAccess()))
			kRead := k.With(apiKeyRetrieve(apiKeyFullAccess()))
			kRead.GET("/:id/download", g.Member(), g.KBAccessDownloadFromKnowledgeIDParam("id"), okStub)
		})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/doc-1/download", nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())
}

func TestPublicBatchDownloadRouteAdmitsCrossTenantViewer(t *testing.T) {
	kb := publicRouteKB("kb-pub", 9)
	engine := newPublicRouteEngine(t, 3, "user-a", types.TenantRoleMember, false, nil, kb,
		func(r *gin.RouterGroup, g *rbacGuards) {
			kbGroup := g.apiKeyGroup(r.Group("/knowledge-bases/:id/knowledge"), apiKeyRetrieve(apiKeyFullAccess()))
			kbGroup.POST("/batch-download", g.Member(), g.KBAccessDownload("id"), okStub)
		})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/kb-pub/knowledge/batch-download", nil)
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())
}

func TestDownloadRouteStillDeniesInviteViewer(t *testing.T) {
	kb := ownedRouteKB("kb-priv", 2)
	knowledge := &types.Knowledge{ID: "doc-1", KnowledgeBaseID: "kb-priv", TenantID: 2}
	enabled := true
	_ = enabled
	engine := newPublicRouteEngine(t, 3, "user-b", types.TenantRoleMember, false, knowledge, kb,
		func(r *gin.RouterGroup, g *rbacGuards) {
			k := g.apiKeyGroup(r.Group("/knowledge"), apiKeyRetrieve(apiKeyFullAccess()))
			kRead := k.With(apiKeyRetrieve(apiKeyFullAccess()))
			kRead.GET("/:id/download", g.Member(), g.KBAccessDownloadFromKnowledgeIDParam("id"), okStub)
		})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/doc-1/download", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

func TestManageRouteDeniesNonSuperAdminOnPublic(t *testing.T) {
	kb := publicRouteKB("kb-pub", 9)
	knowledge := &types.Knowledge{ID: "doc-1", KnowledgeBaseID: "kb-pub", TenantID: 9}
	register := func(r *gin.RouterGroup, g *rbacGuards) {
		k := g.apiKeyGroup(r.Group("/knowledge"), apiKeyIngest(apiKeyFullAccess()))
		k.DELETE("/:id", g.TenantAdmin(), g.KBAccessManageFromKnowledgeIDParam("id"), okStub)
	}
	engine := newPublicRouteEngine(t, 9, "admin-1", types.TenantRoleAdmin, false, knowledge, kb, register)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge/doc-1", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())

	superEngine := newPublicRouteEngine(t, 0, "super-1", types.TenantRoleMember, true, knowledge, kb, register)
	rec = httptest.NewRecorder()
	superEngine.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge/doc-1", nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())
}

func TestManageRouteKeepsOwningTenantPolicy(t *testing.T) {
	kb := ownedRouteKB("kb-own", 3)
	knowledge := &types.Knowledge{ID: "doc-1", KnowledgeBaseID: "kb-own", TenantID: 3}
	register := func(r *gin.RouterGroup, g *rbacGuards) {
		k := g.apiKeyGroup(r.Group("/knowledge"), apiKeyIngest(apiKeyFullAccess()))
		k.DELETE("/:id", g.TenantAdmin(), g.KBAccessManageFromKnowledgeIDParam("id"), okStub)
	}
	engine := newPublicRouteEngine(t, 3, "admin-1", types.TenantRoleAdmin, false, knowledge, kb, register)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge/doc-1", nil))
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())

	foreign := newPublicRouteEngine(t, 4, "admin-2", types.TenantRoleAdmin, false, knowledge, kb, register)
	rec = httptest.NewRecorder()
	foreign.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge/doc-1", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}
