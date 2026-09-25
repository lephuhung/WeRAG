package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Task 6 integration finding: handler-level re-resolution admits tenantless
// real humans on platform-public rows (read + original download) and keeps
// denying private rows, anonymous callers, API-key principals, and
// tenantless non-SuperAdmin mutations.

func tenantlessHandlerCtx(user string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: 0, UserID: user, Role: types.TenantRoleMember,
	})
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	return c, rec
}

func tenantlessKBService(kb *types.KnowledgeBase) *stubKBOnlyService {
	return &stubKBOnlyService{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
		return kb, nil
	}}
}

func publicTenantlessKB() *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: "kb-pub", TenantID: 9, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}
}

func TestResolveHandlerAccess_TenantlessHumanPublicRead(t *testing.T) {
	c, _ := tenantlessHandlerCtx("plain-user")
	grant, err := resolveHandlerKBAccessFor(c, "kb-pub", tenantlessKBService(publicTenantlessKB()), nil, types.KBPermissionViewer)
	require.NoError(t, err)
	require.NotNil(t, grant)
	require.Equal(t, uint64(9), grant.EffectiveTenantID)
}

func TestResolveHandlerDownload_TenantlessHumanPublic(t *testing.T) {
	c, _ := tenantlessHandlerCtx("plain-user")
	grant, err := resolveHandlerKBDownloadAccessFor(c, "kb-pub", tenantlessKBService(publicTenantlessKB()))
	require.NoError(t, err)
	require.NotNil(t, grant)
}

func TestResolveHandlerAccess_TenantlessHumanPrivateDenied(t *testing.T) {
	priv := &types.KnowledgeBase{ID: "kb-priv", TenantID: 2, OwnerTenantID: 2, Visibility: types.KBVisibilityTenant}
	c, _ := tenantlessHandlerCtx("plain-user")
	_, err := resolveHandlerKBAccessFor(c, "kb-priv", tenantlessKBService(priv), nil, types.KBPermissionViewer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Unauthorized")
	_, err = resolveHandlerKBDownloadAccessFor(c, "kb-priv", tenantlessKBService(priv))
	require.Error(t, err)
	require.Contains(t, err.Error(), "Unauthorized")
}

func TestResolveHandlerAccess_AnonymousDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.Background())
	_, err := resolveHandlerKBAccessFor(c, "kb-pub", tenantlessKBService(publicTenantlessKB()), nil, types.KBPermissionViewer)
	require.Error(t, err)
}

func TestResolveHandlerManage_TenantlessNonSuperAdminDenied(t *testing.T) {
	c, _ := tenantlessHandlerCtx("plain-user")
	_, err := resolveHandlerKBManageAccessFor(c, "kb-pub", tenantlessKBService(publicTenantlessKB()), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Unauthorized")
}

func TestResolveHandlerManage_TenantlessHumanRejectedBeforeKBLookup(t *testing.T) {
	c, _ := tenantlessHandlerCtx("plain-user")
	loaded := false
	kbsvc := &stubKBOnlyService{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
		loaded = true
		return publicTenantlessKB(), nil
	}}
	_, err := resolveHandlerKBManageAccessFor(c, "kb-pub", kbsvc, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Unauthorized")
	require.False(t, loaded, "a tenantless non-SuperAdmin must be denied before KB lookup")
}

func platformKeyHandlerCtx(tenant uint64) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		KeyID: 9, Name: "platform-key", ScopeType: types.APIKeyScopePlatform,
		Capabilities:     types.StringArray{string(types.APIKeyCapabilityRetrieve)},
		KnowledgeBaseIDs: types.StringArray{"kb-pub"},
	})
	ctx = types.WithCaller(ctx, types.Caller{
		TenantID: tenant, UserID: "api_platform:9", Role: types.TenantRoleMember,
	})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPIPlatform, ID: "9"})
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	return c, rec
}

// Task 6 fix round 1: platform API-key principals are denied at handler
// re-resolution even when allowlisted to the public row.
func TestResolveHandlerAccess_PlatformKeyDenied(t *testing.T) {
	for _, tenant := range []uint64{3, 0} {
		c, _ := platformKeyHandlerCtx(tenant)
		_, err := resolveHandlerKBAccessFor(c, "kb-pub", tenantlessKBService(publicTenantlessKB()), nil, types.KBPermissionViewer)
		require.Error(t, err, "tenant=%d", tenant)
		_, err = resolveHandlerKBDownloadAccessFor(c, "kb-pub", tenantlessKBService(publicTenantlessKB()))
		require.Error(t, err, "tenant=%d", tenant)
	}
}
