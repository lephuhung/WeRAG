package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Task 6 integration finding: tenantless real humans pass the owner-aware
// read/download guards on platform-public rows (per-KB authorization
// decides), stay unauthorized on private rows, and stay unauthorized on
// manage routes unless explicitly SuperAdmin.

func tenantlessPublicGuardCtx(user string) context.Context {
	return types.WithCaller(context.Background(), types.Caller{
		TenantID: 0, UserID: user, Role: types.TenantRoleMember,
	})
}

func serveTenantlessGuard(
	t *testing.T, ctx context.Context, guard func(c *gin.Context), pattern, url string,
) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	// Full engine with the production error renderer so aborts surface
	// their mapped HTTP status (401/403/404), not just c.IsAborted().
	r := gin.New()
	r.Use(ErrorHandler())
	var gotCtx *gin.Context
	r.GET(pattern, func(c *gin.Context) {
		c.Request = c.Request.WithContext(ctx)
		gotCtx = c
		guard(c)
		if !c.IsAborted() {
			c.Status(http.StatusNoContent)
		}
	})
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.NotNil(t, gotCtx)
	return gotCtx, rec
}

func TestRequireKBAccess_TenantlessHumanPublicPasses(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	c, rec := serveTenantlessGuard(t, tenantlessPublicGuardCtx("plain-user"),
		RequireKBAccess(KBIDFromParam("id"), types.KBPermissionViewer, kbsvc, nil, cfgRBAC(true)),
		"/x/:id", "/x/kb-pub")
	require.False(t, c.IsAborted(), "tenantless human public read must pass, body=%s", rec.Body.String())
}

func TestRequireKBDownload_TenantlessHumanPublicPasses(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	c, rec := serveTenantlessGuard(t, tenantlessPublicGuardCtx("plain-user"),
		RequireKBDownload(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true)),
		"/x/:id", "/x/kb-pub")
	require.False(t, c.IsAborted(), "tenantless human public download must pass, body=%s", rec.Body.String())
}

func TestRequireKBAccess_TenantlessHumanPrivateDenied(t *testing.T) {
	kb := tenantTestKB("kb-priv", 2)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-priv": kb}}
	c, rec := serveTenantlessGuard(t, tenantlessPublicGuardCtx("plain-user"),
		RequireKBAccess(KBIDFromParam("id"), types.KBPermissionViewer, kbsvc, nil, cfgRBAC(true)),
		"/x/:id", "/x/kb-priv")
	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
}

func TestRequireKBManage_TenantlessNonSuperAdminDenied(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	c, rec := serveTenantlessGuard(t, tenantlessPublicGuardCtx("plain-user"),
		RequireKBManage(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true)),
		"/x/:id", "/x/kb-pub")
	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
}

func TestResolveKBManageAccess_TenantlessHumanRejectedBeforeKBLookup(t *testing.T) {
	ctx := tenantlessPublicGuardCtx("plain-user")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPut, "/", nil).WithContext(ctx)
	kbsvc := &stubKBLookup{} // A lookup would return not-found and reveal existence.
	_, err := resolveKBManageAccess(ctx, c, "missing", kbsvc)
	require.ErrorIs(t, err, access.ErrUnauthorized)
}

func TestRequireKBAccess_TenantlessSuperAdminStillPasses(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	ctx := context.WithValue(tenantlessPublicGuardCtx("super-1"), types.SystemAdminContextKey, true)
	c, rec := serveTenantlessGuard(t, ctx,
		RequireKBManage(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true)),
		"/x/:id", "/x/kb-pub")
	require.False(t, c.IsAborted(), "explicit SuperAdmin tenantless manage must keep passing, body=%s", rec.Body.String())
}

func platformKeyGuardCtx(tenant uint64) context.Context {
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		KeyID: 9, Name: "platform-key", ScopeType: types.APIKeyScopePlatform,
		Capabilities:     types.StringArray{string(types.APIKeyCapabilityRetrieve)},
		KnowledgeBaseIDs: types.StringArray{"kb-pub"},
	})
	ctx = types.WithCaller(ctx, types.Caller{
		TenantID: tenant, UserID: "api_platform:9", Role: types.TenantRoleMember,
	})
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPIPlatform, ID: "9"})
}

// Task 6 fix round 1: platform API-key principals are denied at the guard
// even when allowlisted to the public row. With-tenant keys map to 403
// (authenticated-but-forbidden, same as synthetic keys); tenantless keys
// fail the precheck with 401.
func TestRequireKBAccess_PlatformKeyDenied(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	for _, tc := range []struct {
		tenant uint64
		status int
	}{
		{3, http.StatusForbidden},
		{0, http.StatusUnauthorized},
	} {
		c, rec := serveTenantlessGuard(t, platformKeyGuardCtx(tc.tenant),
			RequireKBAccess(KBIDFromParam("id"), types.KBPermissionViewer, kbsvc, nil, cfgRBAC(true)),
			"/x/:id", "/x/kb-pub")
		require.True(t, c.IsAborted(), "tenant=%d", tc.tenant)
		require.Equal(t, tc.status, rec.Code, "tenant=%d body=%s", tc.tenant, rec.Body.String())
	}
}

func TestRequireKBDownload_PlatformKeyDenied(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	// With-tenant platform key: 403 from the access layer (same status
	// family as synthetic keys); deny is what matters.
	c, rec := serveTenantlessGuard(t, platformKeyGuardCtx(3),
		RequireKBDownload(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true)),
		"/x/:id", "/x/kb-pub")
	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}
