package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type provisionUserService struct {
	createUserService
	byID    map[string]*types.User
	byEmail map[string]*types.User
}

func (s *provisionUserService) GetUserByID(_ context.Context, id string) (*types.User, error) {
	if u := s.byID[id]; u != nil {
		return u, nil
	}
	return nil, errors.New("not found")
}

func (s *provisionUserService) GetUserByEmail(_ context.Context, email string) (*types.User, error) {
	if u := s.byEmail[email]; u != nil {
		return u, nil
	}
	return nil, errors.New("not found")
}

type provisionTenantService struct {
	interfaces.TenantService
	created []*types.Tenant
	deleted []uint64
}

func (s *provisionTenantService) CreateTenant(_ context.Context, tenant *types.Tenant) (*types.Tenant, error) {
	tenant.ID = uint64(100 + len(s.created))
	s.created = append(s.created, tenant)
	return tenant, nil
}

func (s *provisionTenantService) DeleteTenant(_ context.Context, id uint64) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *provisionTenantService) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if id == 7 {
		return &types.Tenant{ID: 7, Name: "So B"}, nil
	}
	return nil, errors.New("not found")
}

type provisionMemberService struct {
	interfaces.TenantMemberService
	addErr  error
	added   []types.TenantMember
	removed []string
}

func (s *provisionMemberService) AddMember(
	_ context.Context, userID string, tenantID uint64, role types.TenantRole, _ *string,
) (*types.TenantMember, error) {
	if s.addErr != nil {
		return nil, s.addErr
	}
	m := types.TenantMember{UserID: userID, TenantID: tenantID, Role: role}
	s.added = append(s.added, m)
	return &m, nil
}

func (s *provisionMemberService) RemoveMember(_ context.Context, userID string, _ uint64) error {
	s.removed = append(s.removed, userID)
	return nil
}

func systemTenantRouter(h *SystemHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(tenantPolicyErrorCapture())
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, "super-admin")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.POST("/system/admin/tenants", h.ProvisionTenant)
	r.POST("/system/admin/tenants/:tenant_id/members", h.AddSystemTenantMember)
	r.DELETE("/system/admin/tenants/:tenant_id/members/:user_id", h.RemoveSystemTenantMember)
	return r
}

func doSystemTenantJSON(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func newProvisionFixture() (*SystemHandler, *provisionUserService, *provisionTenantService, *provisionMemberService) {
	existing := &types.User{ID: "u-existing", Username: "canbo", Email: "canbo@hue.gov.vn"}
	users := &provisionUserService{
		createUserService: createUserService{
			createdUser: &types.User{ID: "u-new", Username: "admin-sob", Email: "admin@sob.gov.vn"},
			generated:   "Gen3rated!",
		},
		byID:    map[string]*types.User{existing.ID: existing},
		byEmail: map[string]*types.User{existing.Email: existing},
	}
	tenants := &provisionTenantService{}
	members := &provisionMemberService{}
	h := &SystemHandler{userSvc: users, tenantSvc: tenants, memberSvc: members, cfg: &config.Config{}}
	return h, users, tenants, members
}

func TestProvisionTenantAssignsExistingUserAsAdmin(t *testing.T) {
	h, _, tenants, members := newProvisionFixture()
	w := doSystemTenantJSON(t, systemTenantRouter(h), http.MethodPost, "/system/admin/tenants", map[string]any{
		"name": "Sở B", "admin": map[string]any{"email": "canbo@hue.gov.vn"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(tenants.created) != 1 || tenants.created[0].StorageQuota <= 0 {
		t.Fatalf("tenant not created with a quota: %+v", tenants.created)
	}
	if len(members.added) != 1 {
		t.Fatalf("want exactly one membership, got %+v", members.added)
	}
	m := members.added[0]
	if m.UserID != "u-existing" || m.Role != types.TenantRoleAdmin || m.TenantID != tenants.created[0].ID {
		t.Fatalf("unexpected membership %+v", m)
	}
	// The calling SuperAdmin must not join the workspace.
	for _, a := range members.added {
		if a.UserID == "super-admin" {
			t.Fatal("SuperAdmin was added to the provisioned workspace")
		}
	}
}

func TestProvisionTenantCreatesTenantlessAdminAccount(t *testing.T) {
	h, users, _, members := newProvisionFixture()
	w := doSystemTenantJSON(t, systemTenantRouter(h), http.MethodPost, "/system/admin/tenants", map[string]any{
		"name":  "Xã A",
		"admin": map[string]any{"new_user": map[string]any{"username": "admin-sob", "email": "admin@sob.gov.vn"}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.gotProvisioning != types.TenantProvisioningTenantless {
		t.Fatalf("new admin provisioned as %q, want tenantless", users.gotProvisioning)
	}
	var resp ProvisionTenantResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.AdminCreated || resp.GeneratedPassword != "Gen3rated!" {
		t.Fatalf("unexpected response %+v", resp)
	}
	if len(members.added) != 1 || members.added[0].UserID != "u-new" {
		t.Fatalf("unexpected memberships %+v", members.added)
	}
}

func TestProvisionTenantRejectsAmbiguousAdmin(t *testing.T) {
	h, _, tenants, _ := newProvisionFixture()
	for _, admin := range []map[string]any{
		{},
		{"user_id": "u-existing", "email": "canbo@hue.gov.vn"},
	} {
		w := doSystemTenantJSON(t, systemTenantRouter(h), http.MethodPost, "/system/admin/tenants", map[string]any{
			"name": "Sở C", "admin": admin,
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("admin=%v status=%d body=%s", admin, w.Code, w.Body.String())
		}
	}
	if len(tenants.created) != 0 {
		t.Fatalf("tenant created despite invalid admin: %+v", tenants.created)
	}
}

func TestProvisionTenantUnknownAdminCreatesNothing(t *testing.T) {
	h, _, tenants, _ := newProvisionFixture()
	w := doSystemTenantJSON(t, systemTenantRouter(h), http.MethodPost, "/system/admin/tenants", map[string]any{
		"name": "Sở D", "admin": map[string]any{"user_id": "missing"},
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(tenants.created) != 0 {
		t.Fatal("tenant created for an unknown admin")
	}
}

func TestProvisionTenantRollsBackWhenAdminAssignmentFails(t *testing.T) {
	h, _, tenants, members := newProvisionFixture()
	members.addErr = errors.New("db down")
	w := doSystemTenantJSON(t, systemTenantRouter(h), http.MethodPost, "/system/admin/tenants", map[string]any{
		"name": "Sở E", "admin": map[string]any{"user_id": "u-existing"},
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(tenants.deleted) != 1 || tenants.deleted[0] != tenants.created[0].ID {
		t.Fatalf("tenant not rolled back: created=%+v deleted=%v", tenants.created, tenants.deleted)
	}
}

func TestAddSystemTenantMember(t *testing.T) {
	h, _, _, members := newProvisionFixture()
	r := systemTenantRouter(h)

	w := doSystemTenantJSON(t, r, http.MethodPost, "/system/admin/tenants/7/members", map[string]any{
		"user_id": "u-existing", "role": "member",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(members.added) != 1 || members.added[0].TenantID != 7 || members.added[0].Role != types.TenantRoleMember {
		t.Fatalf("unexpected memberships %+v", members.added)
	}

	if w := doSystemTenantJSON(t, r, http.MethodPost, "/system/admin/tenants/7/members", map[string]any{
		"user_id": "u-existing", "role": "owner",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("owner role accepted: status=%d", w.Code)
	}
	if w := doSystemTenantJSON(t, r, http.MethodPost, "/system/admin/tenants/8/members", map[string]any{
		"user_id": "u-existing", "role": "member",
	}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown tenant: status=%d", w.Code)
	}

	members.addErr = service.ErrMembershipAlreadyExists
	if w := doSystemTenantJSON(t, r, http.MethodPost, "/system/admin/tenants/7/members", map[string]any{
		"user_id": "u-existing", "role": "admin",
	}); w.Code != http.StatusConflict {
		t.Fatalf("duplicate membership: status=%d", w.Code)
	}
}

func TestRemoveSystemTenantMember(t *testing.T) {
	h, _, _, members := newProvisionFixture()
	w := doSystemTenantJSON(t, systemTenantRouter(h), http.MethodDelete, "/system/admin/tenants/7/members/u-existing", nil)
	if w.Code != http.StatusOK || len(members.removed) != 1 {
		t.Fatalf("status=%d removed=%v", w.Code, members.removed)
	}
}

func TestCreateTenantAllowsSystemAdminWhenSelfServiceDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenants := &tenantPolicyTenantService{}
	h := &TenantHandler{
		service:          tenants,
		userService:      &tenantPolicyUserService{user: &types.User{ID: "super-admin", TenantID: 1, IsSystemAdmin: true}},
		config:           &config.Config{Tenant: &config.TenantConfig{}},
		systemSettingSvc: &tenantPolicySettingService{enabled: false},
	}
	r := gin.New()
	r.Use(tenantPolicyErrorCapture())
	r.POST("/tenants", h.CreateTenant)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tenants", bytes.NewBufferString(`{"name":"So F"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || tenants.createCalls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, tenants.createCalls, w.Body.String())
	}
}

func TestTenantSelfServiceCreationDefaultsOff(t *testing.T) {
	if resolveTenantSelfServiceCreationEnabled(context.Background(), nil, nil) {
		t.Fatal("self-service tenant creation must default to disabled")
	}
	if resolveTenantSelfServiceCreationEnabled(context.Background(), &config.Config{Tenant: &config.TenantConfig{}}, nil) {
		t.Fatal("unset tenant config must keep self-service disabled")
	}
}
