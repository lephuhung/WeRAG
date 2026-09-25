package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Task 4 fix round 1: this file exercises the ACTUAL production
// RegisterKnowledgeBaseRoutes registration for GET /knowledge-bases/public
// (the previous helper registered g.Member() and proved nothing about the
// production no-guard wiring). It asserts the route exists in the table,
// declares no API-key policy, admits tenantless authenticated humans to the
// real handler, and denies anonymous/API-key callers end to end.

// stubPublicCatalogService serves only the catalog endpoint; the embedded
// interface keeps every other KnowledgeBaseService method out of scope.
type stubPublicCatalogService struct {
	interfaces.KnowledgeBaseService
	items []*types.KnowledgeBase
	total int64
}

func (s *stubPublicCatalogService) ListPublicCatalog(
	_ context.Context, _, _ int, _ string,
) ([]*types.KnowledgeBase, int64, error) {
	return s.items, s.total, nil
}

// newProductionCatalogEngine wires the real RegisterKnowledgeBaseRoutes
// with a real handler backed by the stub service. Guard closures for the
// sibling :id routes are registered but never hit on /public paths.
func newProductionCatalogEngine(
	t *testing.T, svc *stubPublicCatalogService, ctx context.Context,
) (*gin.Engine, *rbacGuards) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enabled := true
	guards := &rbacGuards{
		cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	h := handler.NewKnowledgeBaseHandler(nil, svc, nil, nil, nil, nil, nil, nil, nil, nil)
	RegisterKnowledgeBaseRoutes(r.Group("/api/v1"), h, guards)
	return r, guards
}

func tenantlessHumanCtx() context.Context {
	// Role mirrors attachTenantlessUserContext reality: no tenant role
	// (zero value, level 0), so any Member+ floor rejects this caller
	// when RBAC is enforced — the sensitivity control below proves it.
	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: 0, UserID: "super-1",
	})
	return context.WithValue(ctx, types.SystemAdminContextKey, true)
}

func TestProductionPublicCatalogRoute_TableAndPolicy(t *testing.T) {
	svc := &stubPublicCatalogService{}
	engine, guards := newProductionCatalogEngine(t, svc, tenantlessHumanCtx())

	found := false
	for _, ri := range engine.Routes() {
		if ri.Method == http.MethodGet && ri.Path == "/api/v1/knowledge-bases/public" {
			found = true
		}
	}
	require.True(t, found, "production route table must contain GET /knowledge-bases/public")

	for method, paths := range guards.ensureAPIKeyAuthorizer().RegisteredRoutes() {
		for _, p := range paths {
			require.NotEqual(t, "GET /api/v1/knowledge-bases/public", method+" "+p,
				"public catalog must not be API-key reachable; visibility is never an implicit key grant")
		}
	}
}

func TestProductionPublicCatalogRoute_TenantlessHumanReachesHandler(t *testing.T) {
	svc := &stubPublicCatalogService{
		items: []*types.KnowledgeBase{
			{ID: "pub-a", Name: "pub-a", TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
		},
		total: 1,
	}
	engine, _ := newProductionCatalogEngine(t, svc, tenantlessHumanCtx())
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases/public", nil))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "pub-a")
	require.Contains(t, rec.Body.String(), `"total":1`)
}

func TestProductionPublicCatalogRoute_DeniesNonHumans(t *testing.T) {
	svc := &stubPublicCatalogService{}
	// Anonymous: no identity at all.
	engine, _ := newProductionCatalogEngine(t, svc, context.Background())
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases/public", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())

	// API-key principal.
	keyCtx := types.WithCaller(context.Background(),
		types.Caller{TenantID: 1, UserID: "system-1", Role: types.TenantRoleAdmin})
	keyCtx = types.WithTenantAPIKeyScope(keyCtx, types.TenantAPIKeyScope{
		KeyID: 7, Name: "k",
		Capabilities: types.StringArray{string(types.APIKeyCapabilityRetrieve)},
	})
	engine, _ = newProductionCatalogEngine(t, svc, keyCtx)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases/public", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

// TestProductionPublicCatalogRoute_GuardSensitivity is the control proving
// the tests above detect an over-strict floor: the same path behind an
// Admin floor rejects the tenantless human, which is why production must
// not gate the catalog on tenant roles. (Tenantless sessions normalize to
// Member, so a Member floor would pass — production still uses no role
// guard by POST /tenants precedent, keeping one enforcement layer in the
// handler/service instead of two.)
func TestProductionPublicCatalogRoute_GuardSensitivity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enabled := true
	guards := &rbacGuards{
		cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenantlessHumanCtx())
		c.Next()
	})
	h := handler.NewKnowledgeBaseHandler(nil, &stubPublicCatalogService{}, nil, nil, nil, nil, nil, nil, nil, nil)
	r.Group("/api/v1").GET("/knowledge-bases/public", guards.Admin(), h.ListPublicCatalog)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases/public", nil))
	require.Equal(t, http.StatusForbidden, rec.Code,
		"control: an Admin floor rejects tenantless humans; production must not impose this floor")
}
