package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerInviteGrants struct {
	interfaces.KBAccessGrantService
	live bool
}

func (s *handlerInviteGrants) GetKBScope(context.Context, string) (*types.KBScope, error) {
	return nil, nil
}

func (s *handlerInviteGrants) ApprovedKBPermission(context.Context, string, uint64) (types.KBPermission, bool, error) {
	return "", false, nil
}

func (s *handlerInviteGrants) HasAcceptedInvite(context.Context, string, string) (bool, error) {
	return s.live, nil
}

func (s *handlerInviteGrants) HasLiveInvite(context.Context, string, string) (bool, error) {
	return s.live, nil
}

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

func TestResolveHandlerAccess_RestrictedInviteIsReadOnly(t *testing.T) {
	kb := &types.KnowledgeBase{
		ID: "kb-restricted", TenantID: 7, OwnerTenantID: 7,
		Visibility: types.KBVisibilityRestricted,
	}
	kbsvc := tenantlessKBService(kb)
	grants := &handlerInviteGrants{live: true}
	c, _ := tenantlessHandlerCtx("invited-user")
	ctx := types.WithCaller(c.Request.Context(), types.Caller{
		TenantID: 3, UserID: "invited-user", Role: types.TenantRoleMember,
	})
	c.Request = c.Request.WithContext(ctx)

	grant, err := resolveHandlerKBAccessFor(c, kb.ID, kbsvc, grants, types.KBPermissionViewer)
	require.NoError(t, err)
	require.Equal(t, types.KBPermissionViewer, grant.Permission)

	_, err = resolveHandlerKBAccessFor(c, kb.ID, kbsvc, grants, types.KBPermissionEditor)
	require.Error(t, err, "an invitation must not grant write access")

	_, err = resolveHandlerKBDownloadAccessFor(c, kb.ID, kbsvc)
	require.Error(t, err, "an invitation must not grant original-file download")
}

func TestGetKnowledgeBatchRouteHonorsLiveInvite(t *testing.T) {
	kb := &types.KnowledgeBase{
		ID: "kb-restricted", TenantID: 7, OwnerTenantID: 7,
		Visibility: types.KBVisibilityRestricted,
	}
	kg := &mutationAdmissionKnowledge{rows: []*types.Knowledge{{
		ID: "doc", TenantID: 7, KnowledgeBaseID: kb.ID,
	}}}
	h := &KnowledgeHandler{
		kgService:            kg,
		kbService:            tenantlessKBService(kb),
		kbAccessGrantService: &handlerInviteGrants{live: true},
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: 3, UserID: "invited-user", Role: types.TenantRoleMember,
		}))
		c.Next()
	})
	r.GET("/api/v1/knowledge/batch", h.GetKnowledgeBatch)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/knowledge/batch?ids=doc&kb_id=kb-restricted", nil)
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"id":"doc"`)

	h.kbAccessGrantService.(*handlerInviteGrants).live = false
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req.Clone(req.Context()))
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestResolveKnowledgeReadHonorsLiveInvite(t *testing.T) {
	kb := &types.KnowledgeBase{
		ID: "kb-restricted", TenantID: 7, OwnerTenantID: 7,
		Visibility: types.KBVisibilityRestricted,
	}
	kg := &mutationAdmissionKnowledge{rows: []*types.Knowledge{{
		ID: "doc", TenantID: 7, KnowledgeBaseID: kb.ID,
	}}}
	grants := &handlerInviteGrants{live: true}
	h := &KnowledgeHandler{
		kgService: kg, kbService: tenantlessKBService(kb), kbAccessGrantService: grants,
	}
	c, _ := tenantlessHandlerCtx("invited-user")
	c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), types.Caller{
		TenantID: 3, UserID: "invited-user", Role: types.TenantRoleMember,
	}))

	knowledge, effective, err := h.resolveKnowledgeAndValidateKBAccess(c, "doc", types.KBPermissionViewer)
	require.NoError(t, err)
	require.Equal(t, "doc", knowledge.ID)
	tenantID, ok := types.TenantIDFromContext(effective)
	require.True(t, ok)
	require.Equal(t, uint64(7), tenantID)

	grants.live = false
	_, _, err = h.resolveKnowledgeAndValidateKBAccess(c, "doc", types.KBPermissionViewer)
	require.Error(t, err, "revoking the invite must close document reads immediately")
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
