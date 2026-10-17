package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// handlerReachedStatus is the sentinel the recovery middleware writes when
// a real handler (wired with nil services) panics: every guard ran and
// let the request through, so the handler itself was reached.
const handlerReachedStatus = 599

// newTenantlessGuestEngine wires the real KB/knowledge/chunk/wiki route
// registrations behind a signed-in human with no tenant context (a Guest
// served tenantless by auth): TenantID 0, no tenant role.
func newTenantlessGuestEngine(t *testing.T, apiKey bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enabled := true
	kbs := map[string]*types.KnowledgeBase{
		"kb-pub": {ID: "kb-pub", TenantID: 9, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
		"kb-published": {ID: "kb-published", TenantID: 5, OwnerTenantID: 5,
			Visibility: types.KBVisibilityPublished},
		"kb-tenant": ownedRouteKB("kb-tenant", 5),
		"kb-restricted": {ID: "kb-restricted", TenantID: 5, OwnerTenantID: 5,
			Visibility: types.KBVisibilityRestricted, MemberDefault: types.KBMemberDefaultContribute},
	}
	guards := &rbacGuards{
		cfg:       &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
		kbService: &stubWikiKBLookup{kbs: kbs},
		knowledgeService: &downloadKnowledgeLookup{knowledge: &types.Knowledge{
			ID: "doc-pub", KnowledgeBaseID: "kb-pub", TenantID: 9,
		}},
		chunkService: &restrictedChunkLookup{chunk: &types.Chunk{
			ID: "chunk-pub", KnowledgeID: "doc-pub", KnowledgeBaseID: "kb-pub", TenantID: 9,
		}},
		kbInviteService: &restrictedInviteStub{},
	}
	r := gin.New()
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		c.AbortWithStatus(handlerReachedStatus)
	}))
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{UserID: "guest-1"})
		ctx = context.WithValue(ctx, types.UserIDContextKey, "guest-1")
		if apiKey {
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	g := r.Group("/api/v1")
	RegisterKnowledgeBaseRoutes(g, &handler.KnowledgeBaseHandler{}, guards)
	RegisterKnowledgeRoutes(g, &handler.KnowledgeHandler{}, guards)
	RegisterChunkRoutes(g, &handler.ChunkHandler{}, guards)
	RegisterWikiPageRoutes(g, &handler.WikiPageHandler{}, nil, guards)
	return r
}

func serveGuest(engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	engine.ServeHTTP(rec, req)
	return rec
}

// TestTenantlessGuestReadsPublicAndPublishedKBs: the Member role floor on
// public read routes steps aside for a tenantless human so the per-KB
// guard decides; public and published KBs reach the handler.
func TestTenantlessGuestReadsPublicAndPublishedKBs(t *testing.T) {
	engine := newTenantlessGuestEngine(t, false)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/knowledge-bases/kb-pub", ""},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-pub/hybrid-search", `{"query_text":"x"}`},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-published", ""},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-pub/knowledge", ""},
		{http.MethodGet, "/api/v1/knowledge/doc-pub", ""},
		{http.MethodGet, "/api/v1/knowledge/doc-pub/preview", ""},
		{http.MethodGet, "/api/v1/knowledge/doc-pub/download", ""},
		{http.MethodGet, "/api/v1/chunks/doc-pub", ""},
		{http.MethodGet, "/api/v1/chunks/by-id/chunk-pub", ""},
		{http.MethodGet, "/api/v1/knowledgebase/kb-pub/wiki/pages", ""},
		{http.MethodGet, "/api/v1/knowledgebase/kb-pub/wiki/search?q=x", ""},
	} {
		rec := serveGuest(engine, tc.method, tc.path, tc.body)
		require.NotContainsf(t, []int{http.StatusUnauthorized, http.StatusForbidden}, rec.Code,
			"%s %s must reach the handler, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
	}
}

// TestTenantlessGuestStaysClosedOnTenantRoutes: tenant-owned and
// restricted KBs, the tenant KB list and writes stay closed. Tenant and
// restricted KB reads now reach the per-KB guard, which denies a
// tenantless caller with 401 (DecideKB step 7: no tenant -> unauthorized);
// routes outside the public read shapes keep the 403 role floor.
func TestTenantlessGuestStaysClosedOnTenantRoutes(t *testing.T) {
	engine := newTenantlessGuestEngine(t, false)
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/v1/knowledge-bases/kb-tenant", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-restricted", "", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-tenant/hybrid-search", `{"query_text":"x"}`, http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/knowledge-bases", "", http.StatusForbidden},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-pub/knowledge/file", "", http.StatusForbidden},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-pub/knowledge/manual", `{}`, http.StatusForbidden},
		{http.MethodGet, "/api/v1/knowledge/search?query=x", "", http.StatusForbidden},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-pub/move-targets", "", http.StatusForbidden},
	} {
		rec := serveGuest(engine, tc.method, tc.path, tc.body)
		require.Equalf(t, tc.want, rec.Code,
			"%s %s must stay closed, body=%s", tc.method, tc.path, rec.Body.String())
	}
}

// TestTenantlessAPIKeyPrincipalUnchanged: an API-key principal never takes
// the tenantless human path. RequireRole still defers it to the APIKeyGate
// as before, and the per-KB guard grants machine principals nothing on a
// public KB they do not own, so the handler is never reached.
func TestTenantlessAPIKeyPrincipalUnchanged(t *testing.T) {
	engine := newTenantlessGuestEngine(t, true)
	for _, path := range []string{
		"/api/v1/knowledge-bases/kb-pub",
		"/api/v1/knowledge-bases/kb-published",
		"/api/v1/knowledge-bases/kb-tenant",
	} {
		rec := serveGuest(engine, http.MethodGet, path, "")
		require.Equalf(t, http.StatusUnauthorized, rec.Code, "%s body=%s", path, rec.Body.String())
	}
}
