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
