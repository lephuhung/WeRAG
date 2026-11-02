package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// apiKeyOwnerUserService answers GetUserByTenantID with a fixed owner row,
// the user an organization API key acts as.
type apiKeyOwnerUserService struct {
	interfaces.UserService
	owner *types.User
	calls int
}

func (s *apiKeyOwnerUserService) GetUserByTenantID(context.Context, uint64) (*types.User, error) {
	s.calls++
	return s.owner, nil
}

func attachTenantKeyForTest(t *testing.T, owner *types.User) (*gin.Context, *httptest.ResponseRecorder, *apiKeyOwnerUserService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	tenantID := uint64(42)
	key := &types.TenantAPIKey{ID: 5, TenantID: &tenantID, ScopeType: types.APIKeyScopeTenant, FullAccess: true}
	users := &apiKeyOwnerUserService{owner: owner}
	attachAPIKeyAuthContext(c, &fakeTenantService{tenant: &types.Tenant{ID: tenantID}}, users, tenantID, key)
	return c, rec, users
}

// A locked owner locks the organization's keys: 401 ACCOUNT_DISABLED with a
// message that names the key owner, and no auth context attached.
func TestTenantAPIKeyRefusedWhenOwnerDisabled(t *testing.T) {
	c, rec, users := attachTenantKeyForTest(t, &types.User{ID: "u-owner", TenantID: 42, IsActive: false})

	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, service.ErrAPIKeyOwnerDisabled.Error(), body["error"])
	require.Equal(t, "ACCOUNT_DISABLED", body["code"])
	_, ok := types.TenantIDFromContext(c.Request.Context())
	require.False(t, ok, "a refused key must not attach a tenant")
	require.Equal(t, 1, users.calls, "the existing owner load is reused, no extra query")
}

func TestTenantAPIKeyAllowedWhenOwnerActive(t *testing.T) {
	c, _, users := attachTenantKeyForTest(t, &types.User{ID: "u-owner", TenantID: 42, IsActive: true})

	require.False(t, c.IsAborted())
	userID, ok := types.UserIDFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, "u-owner", userID)
	tenantID, ok := types.TenantIDFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, uint64(42), tenantID)
	require.Equal(t, 1, users.calls)
}

// No user row: the synthetic system user stays (it has no account to lock).
func TestTenantAPIKeyWithoutOwnerUsesSyntheticUser(t *testing.T) {
	c, _, _ := attachTenantKeyForTest(t, nil)

	require.False(t, c.IsAborted())
	userID, _ := types.UserIDFromContext(c.Request.Context())
	require.True(t, types.IsSyntheticUserID(userID), userID)
}

type touchCountingKeyService struct {
	interfaces.TenantAPIKeyService
	key     *types.TenantAPIKey
	touched []uint64
}

func (s *touchCountingKeyService) AuthenticateAPIKey(context.Context, string) (*types.TenantAPIKey, error) {
	return s.key, nil
}

func (s *touchCountingKeyService) TouchAPIKeyLastUsed(id uint64) { s.touched = append(s.touched, id) }

// last_used_at moves only for an accepted request, not for a key refused
// because its acting user is locked.
func TestAPIKeyLastUsedTouchedOnlyWhenAccepted(t *testing.T) {
	for _, tc := range []struct {
		name     string
		active   bool
		wantCode int
		touched  int
	}{
		{"locked owner", false, http.StatusUnauthorized, 0},
		{"active owner", true, http.StatusOK, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			tenantID := uint64(42)
			keys := &touchCountingKeyService{key: &types.TenantAPIKey{
				ID: 5, TenantID: &tenantID, ScopeType: types.APIKeyScopeTenant, FullAccess: true,
			}}
			users := &apiKeyOwnerUserService{owner: &types.User{ID: "u-owner", TenantID: tenantID, IsActive: tc.active}}
			r := gin.New()
			r.Use(Auth(&fakeTenantService{tenant: &types.Tenant{ID: tenantID}}, users, nil, keys, nil))
			r.GET("/api/v1/knowledge-bases", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
			req.Header.Set("X-API-Key", "sk-test")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.Equal(t, tc.wantCode, rec.Code, rec.Body.String())
			require.Len(t, keys.touched, tc.touched)
		})
	}
}
