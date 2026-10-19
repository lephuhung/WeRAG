package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// A SuperAdmin reads every agent, so an embed channel may only bind an agent
// whose "Tổ chức áp dụng" list includes the channel's workspace.
func TestEnsureAgentOwnedChecksTenantScope(t *testing.T) {
	scoped := &types.CustomAgent{ID: "group", TenantID: 42, Distribution: types.AgentDistributionInternal,
		AppliesToTenantIDs: types.AgentTenantScope{7}}
	svc := &embedChannelService{agentService: &stubAgentForEmbed{agent: scoped}}
	_, err := svc.ensureAgentOwned(context.Background(), 42, "group")
	require.Error(t, err)

	scoped.AppliesToTenantIDs = types.AgentTenantScope{42}
	_, err = svc.ensureAgentOwned(context.Background(), 42, "group")
	require.NoError(t, err)
}
