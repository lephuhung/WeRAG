package dto

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func superAdminContext() context.Context {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 5, UserID: "super-1"})
	return context.WithValue(ctx, types.SystemAdminContextKey, true)
}

// Phase 2 leftover: an explicit human SuperAdmin (no membership in the
// tenant) sees integration secrets like the tenant's admin; a synthetic or
// API-key identity carrying the flag does not.
func TestSuperAdminViewsIntegrationSecrets(t *testing.T) {
	require.True(t, CanViewIntegrationSecrets(superAdminContext()))
	require.False(t, CanViewIntegrationSecrets(viewerContext()))

	synthetic := types.WithCaller(context.Background(), types.Caller{TenantID: 5, UserID: "system-5"})
	require.False(t, CanViewIntegrationSecrets(context.WithValue(synthetic, types.SystemAdminContextKey, true)))

	key := types.WithTenantAPIKeyScope(superAdminContext(), types.TenantAPIKeyScope{
		Capabilities: types.StringArray{string(types.APIKeyCapabilityChat)},
	})
	require.False(t, CanViewIntegrationSecrets(key), "a scoped key never rides on the SuperAdmin flag")

	tenant := &types.Tenant{ID: 5, Name: "t", Credentials: &types.CredentialsConfig{}}
	require.NotNil(t, NewTenantResponse(superAdminContext(), tenant).Credentials,
		"the tenant response is not redacted for a SuperAdmin")
	require.Nil(t, NewTenantResponse(viewerContext(), tenant).Credentials)
}

func TestSuperAdminViewsTenantAPIKey(t *testing.T) {
	require.True(t, CanViewTenantAPIKey(superAdminContext()))
	require.False(t, CanViewTenantAPIKey(viewerContext()))
	synthetic := types.WithCaller(context.Background(), types.Caller{TenantID: 5, UserID: "system-5"})
	require.False(t, CanViewTenantAPIKey(context.WithValue(synthetic, types.SystemAdminContextKey, true)))
	key := types.WithTenantAPIKeyScope(superAdminContext(), types.TenantAPIKeyScope{KeyID: 1})
	require.False(t, CanViewTenantAPIKey(key), "a key never rides on the SuperAdmin flag")
}

// Login / OIDC responses render the active tenant for an explicit
// SuperAdmin as its admin would see it, like /auth/me (NewTenantResponse).
func TestLoginResponsesTreatSuperAdminAsTenantAdmin(t *testing.T) {
	tenant := &types.Tenant{ID: 5, Name: "t", Credentials: &types.CredentialsConfig{}}
	super := &types.User{ID: "super-1", IsSystemAdmin: true}
	plain := &types.User{ID: "u-1"}
	member := []types.Membership{{TenantID: 5, Role: types.TenantRoleMember}}

	login := NewAuthLoginResponse(&types.LoginResponse{User: super, ActiveTenant: tenant})
	require.NotNil(t, login.ActiveTenant.Credentials, "a SuperAdmin without membership sees the admin view")
	login = NewAuthLoginResponse(&types.LoginResponse{User: super, ActiveTenant: tenant, Memberships: member})
	require.NotNil(t, login.ActiveTenant.Credentials, "a SuperAdmin with a Member row sees the admin view")
	login = NewAuthLoginResponse(&types.LoginResponse{User: plain, ActiveTenant: tenant, Memberships: member})
	require.Nil(t, login.ActiveTenant.Credentials)

	oidc := NewAuthOIDCCallbackResponse(&types.OIDCCallbackResponse{User: super, Tenant: tenant})
	require.NotNil(t, oidc.Tenant.Credentials)
	oidc = NewAuthOIDCCallbackResponse(&types.OIDCCallbackResponse{User: plain, Tenant: tenant, Memberships: member})
	require.Nil(t, oidc.Tenant.Credentials)
}
