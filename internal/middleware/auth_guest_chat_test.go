package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

const testPortalID uint64 = 10050

// portalTenantService serves tenants by id and resolves the public portal
// (interfaces.PublicPortalResolver), as the real tenant service does.
type portalTenantService struct {
	fakeTenantService
	tenants  map[uint64]*types.Tenant
	noPortal bool
}

func (f *portalTenantService) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if t, ok := f.tenants[id]; ok {
		return t, nil
	}
	return nil, nil
}

func (f *portalTenantService) GetTenantsByIDs(_ context.Context, ids []uint64) (map[uint64]*types.Tenant, error) {
	out := map[uint64]*types.Tenant{}
	for _, id := range ids {
		if t, ok := f.tenants[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

func (f *portalTenantService) GetPublicPortal(context.Context) (*types.Tenant, error) {
	if f.noPortal {
		return nil, nil
	}
	return f.tenants[testPortalID], nil
}

type guestChatResult struct {
	code     int
	tenantID uint64
	role     string
	errCode  string
}

// guestChatHarness runs authenticateJWTUser for user (JWT claim = its
// TenantID) against tenants 7 (workspace) and the portal, with the member
// rows given, and echoes the session the request resolved to.
func guestChatHarness(t *testing.T, user *types.User, members *fakeMemberService, method, path, tenantHeader string,
	noPortal bool,
) guestChatResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	tenants := &portalTenantService{noPortal: noPortal, tenants: map[uint64]*types.Tenant{
		7:            {ID: 7, Name: "Sở Tư pháp", Kind: types.TenantKindWorkspace},
		9:            {ID: 9, Name: "Phòng khác", Kind: types.TenantKindWorkspace},
		testPortalID: {ID: testPortalID, Name: "Tra cứu công khai", Kind: types.TenantKindPublicPortal},
		// tenant 11 was deleted: GetTenantByID/GetTenantsByIDs no longer find it
	}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if authenticateJWTUser(c, tenants, members, cfgWithRBAC(true), user, user.TenantID) {
			c.Next()
		}
	})
	r.Handle(method, path, func(c *gin.Context) {
		ctx := c.Request.Context()
		tid, _ := types.TenantIDFromContext(ctx)
		c.JSON(http.StatusOK, gin.H{"tenant_id": tid, "role": string(types.CallerFromContext(ctx).Role)})
	})
	req := httptest.NewRequest(method, path, nil)
	if tenantHeader != "" {
		req.Header.Set("X-Tenant-ID", tenantHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body struct {
		TenantID uint64 `json:"tenant_id"`
		Role     string `json:"role"`
		Code     string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return guestChatResult{code: w.Code, tenantID: body.TenantID, role: body.Role, errCode: body.Code}
}

var guestChatPaths = []struct{ method, path string }{
	{http.MethodPost, "/api/v1/sessions"},
	{http.MethodGet, "/api/v1/sessions"},
	{http.MethodGet, "/api/v1/sessions/s1"},
	{http.MethodPost, "/api/v1/agent-chat/s1"},
	{http.MethodPost, "/api/v1/knowledge-chat/s1"},
	{http.MethodGet, "/api/v1/messages/s1/load"},
	{http.MethodGet, "/api/v1/agents"},
	{http.MethodGet, "/api/v1/me/usage"},
	{http.MethodGet, "/api/v1/billing/plans"},
	{http.MethodPost, "/api/v1/billing/orders"},
	{http.MethodGet, "/api/v1/billing/orders/WR123"},
}

// A Guest with no tenant at all chats in the public portal, role-less; the
// rest of the tenant surface still answers TENANT_REQUIRED.
func TestGuestChatTenantlessGuestServedInPortal(t *testing.T) {
	user := &types.User{ID: "guest"}
	for _, rt := range guestChatPaths {
		got := guestChatHarness(t, user, newFakeMemberService(), rt.method, rt.path, "", false)
		if got.code != http.StatusOK || got.tenantID != testPortalID || got.role != "" {
			t.Errorf("%s %s = %+v, want 200 in portal with no role", rt.method, rt.path, got)
		}
	}
	for _, rt := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/sessions/s1/attachments"},
		{http.MethodPost, "/api/v1/sessions/s1/documents"},
		{http.MethodPost, "/api/v1/sessions/s1/fork"},
		{http.MethodGet, "/api/v1/knowledge-bases"},
		{http.MethodDelete, "/api/v1/sessions/batch"},
		{http.MethodGet, "/api/v1/agents/builtin-public-lookup"},
	} {
		got := guestChatHarness(t, user, newFakeMemberService(), rt.method, rt.path, "", false)
		if got.code != http.StatusConflict || got.errCode != "TENANT_REQUIRED" {
			t.Errorf("%s %s = %+v, want 409 TENANT_REQUIRED", rt.method, rt.path, got)
		}
	}
}

// Without a portal row (migration not applied) nothing changes.
func TestGuestChatNoPortalKeepsTenantRequired(t *testing.T) {
	got := guestChatHarness(t, &types.User{ID: "guest"}, newFakeMemberService(), http.MethodPost, "/api/v1/sessions", "", true)
	if got.code != http.StatusConflict || got.errCode != "TENANT_REQUIRED" {
		t.Fatalf("got %+v, want 409 TENANT_REQUIRED", got)
	}
}

// A Guest whose stale claim names a tenant they were removed from chats in
// the portal; a stale X-Tenant-ID is ignored on the guest chat routes.
func TestGuestChatStaleTenantAndHeader(t *testing.T) {
	members := newFakeMemberService()
	members.seedActive("tenant-7-admin", 7, types.TenantRoleAdmin)
	user := &types.User{ID: "guest", TenantID: 7}
	got := guestChatHarness(t, user, members, http.MethodPost, "/api/v1/agent-chat/s1", "", false)
	if got.code != http.StatusOK || got.tenantID != testPortalID {
		t.Fatalf("stale claim: got %+v, want portal", got)
	}
	// stale header naming the old tenant (home, accessible) and another one (inaccessible)
	for _, header := range []string{"7", "8"} {
		got = guestChatHarness(t, &types.User{ID: "guest"}, members, http.MethodGet, "/api/v1/sessions", header, false)
		if got.code != http.StatusOK || got.tenantID != testPortalID {
			t.Errorf("stale X-Tenant-ID %s: got %+v, want portal", header, got)
		}
	}
	// off the guest chat routes the header is still refused
	got = guestChatHarness(t, &types.User{ID: "guest"}, members, http.MethodGet, "/api/v1/knowledge-bases", "8", false)
	if got.code != http.StatusForbidden {
		t.Fatalf("header off guest chat routes: got %+v, want 403", got)
	}
}

// A member of any tenant is never a Guest: with a stale claim on a guest
// chat route they get the membership error, not the portal.
func TestGuestChatMemberElsewhereNotGuest(t *testing.T) {
	members := newFakeMemberService()
	members.seedActive("tenant-7-admin", 7, types.TenantRoleAdmin)
	members.seedActive("user-1", 9, types.TenantRoleMember)
	got := guestChatHarness(t, &types.User{ID: "user-1", TenantID: 7}, members, http.MethodPost, "/api/v1/sessions", "", false)
	if got.code != http.StatusForbidden || got.errCode != "TENANT_MEMBERSHIP_REQUIRED" {
		t.Fatalf("got %+v, want 403 TENANT_MEMBERSHIP_REQUIRED", got)
	}
}

// Nobody targets the portal by name: not a member, not a SuperAdmin.
func TestGuestChatPortalNeverNamedByHeader(t *testing.T) {
	members := newFakeMemberService()
	members.seedActive("user-1", 7, types.TenantRoleMember)
	got := guestChatHarness(t, &types.User{ID: "user-1", TenantID: 7}, members, http.MethodGet, "/api/v1/sessions", "10050", false)
	if got.code != http.StatusForbidden {
		t.Errorf("member naming the portal: got %+v, want 403", got)
	}
	admin := &types.User{ID: "admin-1", IsSystemAdmin: true}
	got = guestChatHarness(t, admin, newFakeMemberService(), http.MethodGet, "/api/v1/sessions", "10050", false)
	if got.code != http.StatusForbidden {
		t.Errorf("SuperAdmin naming the portal: got %+v, want 403", got)
	}
	// a tenantless SuperAdmin is not a Guest either
	got = guestChatHarness(t, admin, newFakeMemberService(), http.MethodPost, "/api/v1/sessions", "", false)
	if got.code != http.StatusConflict {
		t.Errorf("tenantless SuperAdmin: got %+v, want 409", got)
	}
}

// A member keeps their own tenant and role on the chat routes.
func TestGuestChatMemberUnchanged(t *testing.T) {
	members := newFakeMemberService()
	members.seedActive("user-1", 7, types.TenantRoleMember)
	got := guestChatHarness(t, &types.User{ID: "user-1", TenantID: 7}, members, http.MethodPost, "/api/v1/agent-chat/s1", "", false)
	if got.code != http.StatusOK || got.tenantID != 7 || got.role != string(types.TenantRoleMember) {
		t.Fatalf("got %+v, want tenant 7 as member", got)
	}
}

// An active membership left in a deleted tenant does not make a member
// (GET /auth/me drops it too): the account chats in the portal.
func TestGuestChatDeletedTenantMembershipStillGuest(t *testing.T) {
	members := newFakeMemberService()
	members.seedActive("user-1", 11, types.TenantRoleMember)
	got := guestChatHarness(t, &types.User{ID: "user-1"}, members, http.MethodPost, "/api/v1/agent-chat/s1", "", false)
	if got.code != http.StatusOK || got.tenantID != testPortalID {
		t.Fatalf("got %+v, want portal", got)
	}
}

func TestIsGuestChatAPI(t *testing.T) {
	yes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/sessions/continue-stream/s1"},
		{http.MethodPut, "/api/v1/messages/s1/m1/feedback"},
		{http.MethodPost, "/api/v1/sessions/s1/generate_title"},
	}
	no := []struct{ method, path string }{
		{http.MethodDelete, "/api/v1/sessions/batch"},
		{http.MethodGet, "/api/v1/sessions/"},
		{http.MethodGet, "/api/v1/sessions//context-usage"},
		{http.MethodPost, "/api/v1/agents"},
		{http.MethodGet, "/api/v1/sessions/s1/documents"},
		{http.MethodPost, "/api/v1/agent-chat"},
		{http.MethodGet, "/api/v2/sessions"},
	}
	for _, c := range yes {
		if !IsGuestChatAPI(c.path, c.method) {
			t.Errorf("%s %s should be a guest chat route", c.method, c.path)
		}
	}
	for _, c := range no {
		if IsGuestChatAPI(c.path, c.method) {
			t.Errorf("%s %s should not be a guest chat route", c.method, c.path)
		}
	}
}
