package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type overviewInviteStub struct {
	interfaces.KBInvitationService
	live bool
}

func (s overviewInviteStub) HasLiveInvite(context.Context, string, string) (bool, error) {
	return s.live, nil
}

func (overviewInviteStub) ListByKB(context.Context, types.Caller, string) ([]*types.KBInviteResponse, error) {
	return []*types.KBInviteResponse{{
		ID: "inv-1", KBID: "kb", RecipientUserID: "u-9", RecipientTenantID: 1,
		Status: types.KBInvitationStatusRevoked, RevokedReason: types.KBInviteRevokedReasonOwnerChanged,
	}}, nil
}

type overviewGrantStub struct {
	interfaces.KBAccessGrantService
}

func (overviewGrantStub) ApprovedKBPermission(context.Context, string, uint64) (types.KBPermission, bool, error) {
	return "", false, nil
}

func (overviewGrantStub) ListByKB(context.Context, types.Caller, string) ([]*types.KBAccessGrantResponse, error) {
	return []*types.KBAccessGrantResponse{{
		ID: "g-1", KBID: "kb", GranteeTenantID: 2, GranteeName: "Province office",
		Permission: types.KBPermissionViewer, Status: types.GrantStatusApproved,
	}}, nil
}

func runAccessOverview(t *testing.T, grant *access.KBAccess, invites overviewInviteStub) KBAccessOverview {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewKBAccessOverviewHandler(overviewGrantStub{}, invites, nil, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), grant.Caller))
		c.Set(middleware.KBAccessContextKey, grant)
		c.Next()
	})
	r.GET("/knowledge-bases/:id/access", h.GetAccess)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/knowledge-bases/kb/access", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Data KBAccessOverview `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Data
}

func TestKBAccessOverviewManagerSeesGrantees(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityRestricted,
		MemberDefault: types.KBMemberDefaultView}
	admin := types.Caller{TenantID: 1, UserID: "admin-1", Role: types.TenantRoleAdmin}
	got := runAccessOverview(t, &access.KBAccess{
		KnowledgeBase: kb, Caller: admin, EffectiveTenantID: 1, Permission: types.KBPermissionAdmin,
		Source: access.SourceTenantAdmin, CanDownloadOriginal: true,
	}, overviewInviteStub{})
	require.Equal(t, types.KBVisibilityRestricted, got.Visibility)
	require.Equal(t, types.KBMemberDefaultView, got.MemberDefault)
	require.Equal(t, KBEffectivePermissions{View: true, Contribute: true, Manage: true, DownloadOriginal: true},
		got.EffectivePermissions)
	require.Equal(t, []access.AccessSource{access.SourceTenantAdmin}, got.Sources)
	require.Len(t, got.Grantees, 2)
	require.Equal(t, "user", got.Grantees[0].Kind)
	require.Equal(t, "u-9", got.Grantees[0].Name, "names fall back to the id")
	require.Equal(t, types.KBInviteRevokedReasonOwnerChanged, got.Grantees[0].RevokedReason)
	require.Equal(t, "tenant", got.Grantees[1].Kind)
	require.Equal(t, "Province office", got.Grantees[1].Name)
}

func TestKBAccessOverviewReaderSeesOnlyOwnRights(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityRestricted}
	reader := types.Caller{TenantID: 2, UserID: "reader", Role: types.TenantRoleMember}
	got := runAccessOverview(t, &access.KBAccess{
		KnowledgeBase: kb, Caller: reader, EffectiveTenantID: 1, Permission: types.KBPermissionViewer,
		Source: access.SourceACLUser,
	}, overviewInviteStub{live: true})
	require.Equal(t, KBEffectivePermissions{View: true}, got.EffectivePermissions)
	require.Equal(t, []access.AccessSource{access.SourceACLUser}, got.Sources)
	require.Empty(t, got.Grantees, "readers never see who else has access")
}
