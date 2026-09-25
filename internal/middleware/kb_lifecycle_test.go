package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Task 3: the explicit-human-SuperAdmin route guard and the tenantless
// lifecycle path allowlist.
//
// Operation order: route middleware must not reject explicit human
// SuperAdmins without tenant context, while owner-aware guards and service
// checks keep them from mutating tenant-owned KBs.
func lifecycleGuardCtx(tenant uint64, user string, sysAdmin bool, apiKey bool) context.Context {
	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: tenant, UserID: user, Role: types.TenantRoleMember,
	})
	if sysAdmin {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	if apiKey {
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{
			KeyID: 7, Name: "k",
			Capabilities: types.StringArray{string(types.APIKeyCapabilityManageKnowledgeBases)},
		})
	}
	return ctx
}

func serveLifecycleGuard(t *testing.T, ctx context.Context) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
	RequireExplicitHumanSuperAdmin(cfgRBAC(true))(c)
	if !c.IsAborted() {
		return http.StatusOK
	}
	return rec.Code
}

func TestRequireExplicitHumanSuperAdmin(t *testing.T) {
	// Explicit human SuperAdmin passes with and without tenant context.
	require.Equal(t, http.StatusOK, serveLifecycleGuard(t, lifecycleGuardCtx(3, "super-1", true, false)))
	require.Equal(t, http.StatusOK, serveLifecycleGuard(t, lifecycleGuardCtx(0, "super-1", true, false)),
		"tenantless explicit human SuperAdmin must reach platform routes")
	// Tenant Admin without the flag is denied.
	require.Equal(t, http.StatusForbidden,
		serveLifecycleGuard(t, types.WithCaller(context.Background(), types.Caller{
			TenantID: 3, UserID: "admin-1", Role: types.TenantRoleAdmin,
		})))
	// CanAccessAllTenants alone (flag absent) is denied.
	crossOnly := context.WithValue(lifecycleGuardCtx(3, "user-a", false, false),
		types.UserContextKey, &types.User{ID: "user-a", TenantID: 3, CanAccessAllTenants: true})
	require.Equal(t, http.StatusForbidden, serveLifecycleGuard(t, crossOnly))
	// API-key manage_kbs with the raw flag set is still denied: machine
	// principals are never explicit human SuperAdmins.
	require.Equal(t, http.StatusForbidden, serveLifecycleGuard(t, lifecycleGuardCtx(1, "system-1", true, true)))
	// Synthetic identity with the flag is denied.
	require.Equal(t, http.StatusForbidden, serveLifecycleGuard(t, lifecycleGuardCtx(3, "system-3", true, false)))
	// Tenantless anonymous stays unauthorized, not forbidden.
	require.Equal(t, http.StatusUnauthorized, serveLifecycleGuard(t, context.Background()))
}

func TestIsTenantlessKBLifecyclePath(t *testing.T) {
	cases := []struct {
		path   string
		method string
		want   bool
	}{
		{"/api/v1/knowledge-bases/public", http.MethodPost, true},
		// Task 4: human-only public catalog discovery (GET) is tenant-
		// optional; the handler + service still deny anonymous/API-key.
		{"/api/v1/knowledge-bases/public", http.MethodGet, true},
		{"/api/v1/knowledge-bases/kb-1", http.MethodPut, true},
		{"/api/v1/knowledge-bases/kb-1", http.MethodDelete, true},
		{"/api/v1/knowledge-bases/kb-1/visibility", http.MethodPut, true},
		{"/api/v1/knowledge-bases/kb-1/profile/generate", http.MethodPost, true},
		// Task 6: per-KB-guarded public reads no longer require a
		// workspace (auth admits; KBAccessRead/Download decides on the
		// loaded row). Reads outside the Task 6 whitelist still do.
		{"/api/v1/knowledge-bases/kb-1", http.MethodGet, true},
		{"/api/v1/knowledge-bases", http.MethodGet, false},
		// Nested content writes stay excluded; whitelisted reads admitted.
		{"/api/v1/knowledge-bases/kb-1/knowledge/file", http.MethodPost, false},
		{"/api/v1/knowledge-bases/kb-1/faq/entries", http.MethodGet, true},
		{"/api/v1/knowledge-bases/kb-1/tags", http.MethodGet, true},
		{"/api/v1/knowledge-bases/kb-1/wiki/pages", http.MethodGet, false},
		{"/api/v1/knowledge-bases/kb-1/knowledge/batch-download", http.MethodPost, true},
		// Wrong methods on lifecycle shapes stay tenant-scoped.
		{"/api/v1/knowledge-bases/kb-1/visibility", http.MethodGet, false},
		{"/api/v1/knowledge-bases/kb-1", http.MethodPost, false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, isTenantOptionalAPI(tc.path, tc.method),
			"path=%s method=%s", tc.path, tc.method)
	}
}
