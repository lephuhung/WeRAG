package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func publicTestKB(id string, dataTenant uint64) *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: id, TenantID: dataTenant,
		OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}
}

func tenantTestKB(id string, owner uint64) *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: id, TenantID: owner,
		OwnerTenantID: owner, Visibility: types.KBVisibilityTenant,
	}
}

type stubInviteLookup struct {
	accepted map[string]bool
}

func (s *stubInviteLookup) HasAcceptedInvite(_ context.Context, kbID, userID string) (bool, error) {
	return s.accepted[kbID+"\x00"+userID], nil
}

func TestRequireKBAccess_PublicReadAcrossTenants(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "kb-pub"}}
	req := httptest.NewRequest("GET", "/", nil)
	ctx := types.WithCaller(req.Context(), types.Caller{
		TenantID: 3, UserID: "user-a", Role: types.TenantRoleMember,
	})
	c.Request = req.WithContext(ctx)
	RequireKBAccess(KBIDFromParam("id"), types.KBPermissionViewer, kbsvc, nil, cfgRBAC(true))(c)
	require.False(t, c.IsAborted(), "authenticated cross-tenant public read must pass")
	got, ok := KBAccessFromContext(c)
	require.True(t, ok)
	require.Equal(t, types.KBPermissionViewer, got.Permission)
	require.Equal(t, uint64(9), got.EffectiveTenantID)
	execTenant, ok := types.TenantIDFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, uint64(9), execTenant)
}

func TestRequireKBAccess_PublicReadDeniesAnonymous(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "kb-pub"}}
	c.Request = httptest.NewRequest("GET", "/", nil)
	RequireKBAccess(KBIDFromParam("id"), types.KBPermissionViewer, kbsvc, nil, cfgRBAC(true))(c)
	require.True(t, c.IsAborted(), "anonymous public read must abort")
}

func TestRequireKBDownload_PublicViewerMayDownload(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "kb-pub"}}
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req.WithContext(types.WithCaller(req.Context(), types.Caller{
		TenantID: 3, UserID: "user-a", Role: types.TenantRoleMember,
	}))
	RequireKBDownload(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true))(c)
	require.False(t, c.IsAborted(), "public authenticated viewers may download originals")
	got, ok := KBAccessFromContext(c)
	require.True(t, ok)
	require.Equal(t, uint64(9), got.EffectiveTenantID)
}

func TestRequireKBDownload_InviteViewerStillDenied(t *testing.T) {
	kb := tenantTestKB("kb-priv", 2)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-priv": kb}}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "kb-priv"}}
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req.WithContext(types.WithCaller(req.Context(), types.Caller{
		TenantID: 3, UserID: "user-b", Role: types.TenantRoleMember,
	}))
	invites := &stubInviteLookup{accepted: map[string]bool{"kb-priv\x00user-b": true}}
	// Read access via invite succeeds...
	RequireKBAccessWithInvite(KBIDFromParam("id"), types.KBPermissionViewer,
		kbsvc, nil, invites, cfgRBAC(true))(c)
	require.False(t, c.IsAborted(), "invite read must still pass")
	// ...but the dedicated download guard denies the same caller.
	rec2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rec2)
	c2.Params = gin.Params{{Key: "id", Value: "kb-priv"}}
	c2.Request = req.WithContext(types.WithCaller(req.Context(), types.Caller{
		TenantID: 3, UserID: "user-b", Role: types.TenantRoleMember,
	}))
	RequireKBDownload(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true))(c2)
	require.True(t, c2.IsAborted(), "invitation viewers keep no-download behavior")
}

func TestRequireKBManage_PublicSuperAdminOnly(t *testing.T) {
	kb := publicTestKB("kb-pub", 9)
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb-pub": kb}}
	newCtx := func(tenant uint64, user string, role types.TenantRole, sysAdmin bool) *gin.Context {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Params = gin.Params{{Key: "id", Value: "kb-pub"}}
		req := httptest.NewRequest("GET", "/", nil)
		ctx := types.WithCaller(req.Context(), types.Caller{TenantID: tenant, UserID: user, Role: role})
		if sysAdmin {
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
		}
		c.Request = req.WithContext(ctx)
		return c
	}
	c := newCtx(3, "super-1", types.TenantRoleMember, true)
	RequireKBManage(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true))(c)
	require.False(t, c.IsAborted(), "explicit human SuperAdmin manages platform-owned content")

	c = newCtx(0, "super-1", types.TenantRoleMember, true)
	RequireKBManage(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true))(c)
	require.False(t, c.IsAborted(), "SuperAdmin with no active tenant reaches public KB")

	c = newCtx(9, "admin-1", types.TenantRoleAdmin, false)
	RequireKBManage(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true))(c)
	require.True(t, c.IsAborted(), "owning-data-scope Tenant Admin is not the owner of a platform KB")

	c = newCtx(3, "user-a", types.TenantRoleMember, false)
	RequireKBManage(KBIDFromParam("id"), kbsvc, nil, cfgRBAC(true))(c)
	require.True(t, c.IsAborted(), "cross-tenant member cannot manage public content")
}
