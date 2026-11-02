package handler

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type actingUserStub struct {
	interfaces.UserService
	user *types.User
}

func (s actingUserStub) GetUserByTenantID(context.Context, uint64) (*types.User, error) {
	return s.user, nil
}

// The settings page warns from is_active; it must reflect the same row the
// X-API-Key auth path acts as.
func TestAPIKeyActingUserReportsOwnerState(t *testing.T) {
	ctx := context.Background()

	locked := (&TenantHandler{userService: actingUserStub{user: &types.User{ID: "u1", Username: "owner", IsActive: false}}}).
		apiKeyActingUser(ctx, 7)
	require.NotNil(t, locked)
	require.Equal(t, "u1", locked.ID)
	require.False(t, locked.IsActive)

	active := (&TenantHandler{userService: actingUserStub{user: &types.User{ID: "u1", IsActive: true}}}).
		apiKeyActingUser(ctx, 7)
	require.True(t, active.IsActive)

	require.Nil(t, (&TenantHandler{userService: actingUserStub{}}).apiKeyActingUser(ctx, 7))
	require.Nil(t, (&TenantHandler{}).apiKeyActingUser(ctx, 7))
}
