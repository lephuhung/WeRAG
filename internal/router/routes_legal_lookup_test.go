package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

type stubLegalLookupService struct{ calls int }

func (s *stubLegalLookupService) LookupValidity(_ context.Context, q string, _ int) (*types.LegalValidityLookup, error) {
	s.calls++
	return &types.LegalValidityLookup{Query: q, Matches: []types.LegalValidityMatch{}}, nil
}

// newLegalLookupEngine wires the production RegisterLegalLookupRoutes behind
// the API-key gate, as NewRouter does, with ctx as the authenticated session.
func newLegalLookupEngine(t *testing.T, svc *stubLegalLookupService, ctx context.Context) (*gin.Engine, *rbacGuards) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enabled := true
	guards := &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	v1 := r.Group("/api/v1")
	v1.Use(guards.ensureAPIKeyAuthorizer().Middleware())
	RegisterLegalLookupRoutes(v1, handler.NewLegalLookupHandler(svc), nil)
	return r, guards
}

func TestLegalLookupRoute(t *testing.T) {
	svc := &stubLegalLookupService{}
	// a Guest: a signed-in human with no organization and no role
	guest := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "guest"})
	guest = types.WithCaller(guest, types.Caller{UserID: "guest"})
	engine, guards := newLegalLookupEngine(t, svc, guest)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/legal-lookup?q=53", nil))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Equal(t, 1, svc.calls)

	for method, paths := range guards.ensureAPIKeyAuthorizer().RegisteredRoutes() {
		for _, p := range paths {
			require.NotEqual(t, "GET /api/v1/legal-lookup", method+" "+p, "the lookup declares no API-key policy")
		}
	}

	// an API key is refused before the handler
	key := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "system-1", Role: types.TenantRoleAdmin})
	key = types.WithTenantAPIKeyScope(key, types.TenantAPIKeyScope{KeyID: 7, Name: "k", FullAccess: true})
	engine, _ = newLegalLookupEngine(t, svc, key)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/legal-lookup?q=53", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	require.Equal(t, 1, svc.calls)

	// anonymous too
	engine, _ = newLegalLookupEngine(t, svc, context.Background())
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/legal-lookup?q=53", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	require.Equal(t, 1, svc.calls)
}
