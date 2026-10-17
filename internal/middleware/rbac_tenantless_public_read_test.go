package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestTenantlessPublicReadPassesRoleFloor(t *testing.T) {
	guest := types.Caller{UserID: "guest-1"}
	human := context.Background()
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		caller types.Caller
		min    types.TenantRole
		method string
		path   string
		want   bool
	}{
		{"guest KB detail", human, guest, types.TenantRoleMember, http.MethodGet, "/api/v1/knowledge-bases/kb-1", true},
		{"guest hybrid search", human, guest, types.TenantRoleMember, http.MethodPost, "/api/v1/knowledge-bases/kb-1/hybrid-search", true},
		{"guest document preview", human, guest, types.TenantRoleMember, http.MethodGet, "/api/v1/knowledge/doc-1/preview", true},
		{"guest chunk list", human, guest, types.TenantRoleMember, http.MethodGet, "/api/v1/chunks/doc-1", true},
		{"guest wiki pages", human, guest, types.TenantRoleMember, http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/pages", true},
		{"guest KB list stays tenant-scoped", human, guest, types.TenantRoleMember, http.MethodGet, "/api/v1/knowledge-bases", false},
		{"guest upload stays closed", human, guest, types.TenantRoleMember, http.MethodPost, "/api/v1/knowledge-bases/kb-1/knowledge/file", false},
		{"guest KB update stays closed", human, guest, types.TenantRoleMember, http.MethodPut, "/api/v1/knowledge-bases/kb-1", false},
		{"guest cross-KB search stays closed", human, guest, types.TenantRoleMember, http.MethodGet, "/api/v1/knowledge/search", false},
		{"admin floor never bypassed", human, guest, types.TenantRoleAdmin, http.MethodGet, "/api/v1/knowledge-bases/kb-1", false},
		{"tenant without role keeps the floor", human, types.Caller{TenantID: 7, UserID: "u-1"},
			types.TenantRoleMember, http.MethodGet, "/api/v1/knowledge-bases/kb-1", false},
		{"anonymous", human, types.Caller{}, types.TenantRoleMember, http.MethodGet, "/api/v1/knowledge-bases/kb-1", false},
		{"synthetic user", human, types.Caller{UserID: "system-7"}, types.TenantRoleMember,
			http.MethodGet, "/api/v1/knowledge-bases/kb-1", false},
		{"api key", types.WithTenantAPIKeyScope(human, types.TenantAPIKeyScope{}), guest,
			types.TenantRoleMember, http.MethodGet, "/api/v1/knowledge-bases/kb-1", false},
		{"non-web principal", types.WithPrincipal(human, types.Principal{Type: types.PrincipalAPITenant, ID: "7"}), guest,
			types.TenantRoleMember, http.MethodGet, "/api/v1/knowledge-bases/kb-1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tenantlessPublicReadPassesRoleFloor(tc.ctx, tc.caller, tc.min, tc.method, tc.path)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestRequireRole_TenantlessGuestPublicRead drives RequireRole end to end:
// the Member floor lets a tenantless human through on a public read shape
// and still rejects the tenant KB list.
func TestRequireRole_TenantlessGuestPublicRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	serve := func(method, path string) int {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			ctx := types.WithCaller(c.Request.Context(), types.Caller{UserID: "guest-1"})
			c.Request = c.Request.WithContext(ctx)
			c.Next()
		})
		r.Handle(method, "/*any", RequireRole(types.TenantRoleMember, cfgRBAC(true)),
			func(c *gin.Context) { c.Status(http.StatusNoContent) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w.Code
	}
	assert.Equal(t, http.StatusNoContent, serve(http.MethodGet, "/api/v1/knowledge-bases/kb-1"))
	assert.Equal(t, http.StatusNoContent, serve(http.MethodPost, "/api/v1/knowledge-bases/kb-1/hybrid-search"))
	assert.Equal(t, http.StatusForbidden, serve(http.MethodGet, "/api/v1/knowledge-bases"))
	assert.Equal(t, http.StatusForbidden, serve(http.MethodPost, "/api/v1/knowledge-bases/kb-1/knowledge/file"))
}
