package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tenantlessAssetUserService struct {
	interfaces.UserService
	user *types.User
}

func (s *tenantlessAssetUserService) ValidateToken(context.Context, string) (*types.User, uint64, error) {
	return s.user, 0, nil
}

// Task 6 integration finding: tenantless authenticated humans must reach
// per-KB-guarded public read/download routes (auth admits; per-KB
// KBAccessRead/Download decides on the loaded owner+visibility). The table
// pins exact allow/deny: public-read shapes true; broad, mutating, and
// non-KB-guarded shapes false.
func TestTenantOptionalPublicReadSurface(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		// KB detail + content reads.
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/knowledge", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/knowledge/folders", true},
		{http.MethodGet, "/api/v1/knowledge/abc", true},
		{http.MethodGet, "/api/v1/knowledge/abc/stages", true},
		{http.MethodGet, "/api/v1/knowledge/abc/spans", true},
		{http.MethodGet, "/api/v1/knowledge/abc/preview", true},
		// Original downloads (per-KB download guard downstream).
		{http.MethodGet, "/api/v1/knowledge/abc/download", true},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/knowledge/batch-download", true},
		// Per-KB search (read-only, per-KB guard downstream).
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/hybrid-search", true},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/hybrid-search", true},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/faq/search", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/faq/entries", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/faq/entries/export", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/faq/entries/e-1", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/tags", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/access", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/files", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/entity-graph", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/legal-graph", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/pages", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/pages/some-slug", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/revisions/some-slug", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/folders", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/index", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/graph", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/stats", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/search", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/lint", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/issues", true},
		{http.MethodGet, "/api/v1/chunks/doc-1", true},
		{http.MethodGet, "/api/v1/chunks/by-id/ch-1", true},
		{http.MethodGet, "/api/v1/chunks/doc-1/ch-1/revisions", true},

		// Deny: mixed tenant list, cross-KB search/batch, activity,
		// progress, invites, copy, pins, move-targets, files serve.
		{http.MethodGet, "/api/v1/knowledge-bases", false},
		{http.MethodGet, "/api/v1/knowledge/search", false},
		{http.MethodGet, "/api/v1/knowledge/batch", false},
		{http.MethodPost, "/api/v1/knowledge/move", false},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/activity", false},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/move-targets", false},
		{http.MethodPut, "/api/v1/knowledge-bases/kb-1/pin", false},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/duplicate", false},
		{http.MethodPost, "/api/v1/knowledge-bases/copy", false},
		{http.MethodGet, "/api/v1/faq/import/progress/t-1", false},
		// Deny: every write under an allowlisted prefix.
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/knowledge/file", false},
		{http.MethodDelete, "/api/v1/knowledge-bases/kb-1/knowledge", false},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/faq/entries", false},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/tags", false},
		{http.MethodPost, "/api/v1/knowledgebase/kb-1/wiki/pages", false},
		{http.MethodPut, "/api/v1/knowledgebase/kb-1/wiki/pages/some-slug", false},
		{http.MethodDelete, "/api/v1/knowledge/doc-1", false},
		{http.MethodDelete, "/api/v1/chunks/doc-1/ch-1", false},
		// Deny: unknown shapes and trailing-slash variants fail closed.
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/unknown", false},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/knowledge/", false},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/other", false},
		{http.MethodPost, "/api/v1/knowledge/abc/download", false},
	}
	for _, tt := range tests {
		if got := isTenantOptionalAPI(tt.path, tt.method); got != tt.want {
			t.Errorf("isTenantOptionalAPI(%s %s) = %v, want %v", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestTenantlessKBAssetRoutesReachAndRemainBoundByKBGuards(t *testing.T) {
	routes := []string{
		"/api/v1/knowledge-bases/:id/files",
		"/api/v1/knowledge-bases/:id/entity-graph",
		"/api/v1/knowledge-bases/:id/legal-graph",
	}
	public := publicTestKB("kb", 9)
	published := tenantTestKB("kb", 9)
	published.Visibility = types.KBVisibilityPublished
	private := tenantTestKB("kb", 9)
	restricted := tenantTestKB("kb", 9)
	restricted.Visibility = types.KBVisibilityRestricted

	for _, route := range routes {
		for _, tc := range []struct {
			name          string
			authenticated bool
			kb            *types.KnowledgeBase
			want          int
		}{
			{"published public", true, public, http.StatusNoContent},
			{"tenant published", true, published, http.StatusNoContent},
			{"private", true, private, http.StatusUnauthorized},
			{"restricted", true, restricted, http.StatusUnauthorized},
			{"anonymous", false, public, http.StatusUnauthorized},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				cfg := cfgRBAC(true)
				members := newFakeMemberService()
				tenants := &fakeTenantService{}
				r := gin.New()
				r.Use(ErrorHandler())
				r.Use(Auth(tenants, &tenantlessAssetUserService{user: &types.User{ID: "guest"}}, members, nil, cfg))
				r.GET(route,
					RequireRole(types.TenantRoleMember, cfg),
					RequireKBAccess(KBIDFromParam("id"), types.KBPermissionViewer,
						&stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": tc.kb}}, nil, cfg),
					func(c *gin.Context) { c.Status(http.StatusNoContent) },
				)
				rec := httptest.NewRecorder()
				path := route
				path = strings.Replace(path, ":id", "kb", 1)
				req := httptest.NewRequest(http.MethodGet, path, nil)
				if tc.authenticated {
					req.Header.Set("Authorization", "Bearer valid")
				}
				r.ServeHTTP(rec, req)
				require.Equal(t, tc.want, rec.Code, rec.Body.String())
			})
		}
	}
}
