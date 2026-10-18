package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// staleTenantGuestHarness runs authenticateJWTUser for a non-SuperAdmin
// whose users.tenant_id (and JWT claim) is tenant 7, where the member
// service holds the given row (nil = no membership at all). The handler
// echoes the tenant the session resolved to.
func staleTenantGuestHarness(t *testing.T, row *types.TenantMember, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	user := &types.User{ID: "guest", TenantID: 7}
	members := newFakeMemberService()
	// Tenant 7 already has an Admin, so no orphan-tenant path applies.
	members.seedActive("tenant-7-admin", 7, types.TenantRoleAdmin)
	if row != nil {
		members.members[memberKey(row.UserID, row.TenantID)] = row
	}
	tenants := &fakeTenantService{tenant: &types.Tenant{ID: 7}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if authenticateJWTUser(c, tenants, members, cfgWithRBAC(true), user, user.TenantID) {
			c.Next()
		}
	})
	r.Handle(method, path, func(c *gin.Context) {
		tid, _ := types.TenantIDFromContext(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"tenant_id": tid})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// Live-test finding: a Guest whose home tenant holds no active membership
// (removed, suspended or stale claim) must still sign in: identity routes
// and public KB reads are served tenantless; tenant routes answer 403 with
// TENANT_MEMBERSHIP_REQUIRED.
func TestStaleTenantGuest(t *testing.T) {
	rows := map[string]*types.TenantMember{
		"no membership": nil,
		"suspended membership": {
			UserID: "guest", TenantID: 7, Role: types.TenantRoleMember,
			Status: types.TenantMemberStatusSuspended,
		},
	}
	tenantless := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodGet, "/api/v1/knowledge-bases/public"},
		{http.MethodPost, "/api/v1/auth/switch-tenant"},
		{http.MethodGet, "/api/v1/me/invitations"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1"},
	}
	closed := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/knowledge-bases"},
		{http.MethodPost, "/api/v1/knowledge-bases"},
		{http.MethodGet, "/api/v1/sessions"},
	}
	for name, row := range rows {
		for _, rt := range tenantless {
			w := staleTenantGuestHarness(t, row, rt.method, rt.path)
			if w.Code != http.StatusOK {
				t.Errorf("%s: %s %s = %d %s, want 200", name, rt.method, rt.path, w.Code, w.Body.String())
				continue
			}
			var body struct {
				TenantID uint64 `json:"tenant_id"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if body.TenantID != 0 {
				t.Errorf("%s: %s %s ran in tenant %d, want tenantless", name, rt.method, rt.path, body.TenantID)
			}
		}
		for _, rt := range closed {
			w := staleTenantGuestHarness(t, row, rt.method, rt.path)
			var body struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != http.StatusForbidden || body.Code != "TENANT_MEMBERSHIP_REQUIRED" ||
				body.Error != "Forbidden: not a member of the target organization" {
				t.Errorf("%s: %s %s = %d %s, want 403 TENANT_MEMBERSHIP_REQUIRED", name, rt.method, rt.path, w.Code, w.Body.String())
			}
		}
	}
}

// An active member of the home tenant still runs in it.
func TestStaleTenantGuest_ActiveMemberUnaffected(t *testing.T) {
	row := &types.TenantMember{UserID: "guest", TenantID: 7, Role: types.TenantRoleMember, Status: types.TenantMemberStatusActive}
	w := staleTenantGuestHarness(t, row, http.MethodGet, "/api/v1/auth/me")
	if w.Code != http.StatusOK || w.Body.String() != `{"tenant_id":7}` {
		t.Fatalf("active member /auth/me = %d %s, want tenant 7", w.Code, w.Body.String())
	}
}
