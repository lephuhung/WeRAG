package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestAgentAddonCatalogue(t *testing.T) {
	_, repo := distributionFixture(t)
	ctx := context.Background()
	plans := NewUsagePlanRepository(repo.db)

	require.NoError(t, plans.CreateAgentAddon(ctx, &types.UsageAgentAddon{
		AgentID: "addon", PriceVND: 49000, PlanIDs: types.UsagePlanIDList{"plan-go"}, Description: "Thuế HKD",
		IsActive: true, SortOrder: 2,
	}))
	// a row whose agent is basic is kept but reported unavailable
	require.NoError(t, plans.CreateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "basic", IsActive: true, SortOrder: 1}))

	rows, err := plans.ListAgentAddons(ctx, true)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "basic", rows[0].AgentID, "sort_order first")
	require.False(t, rows[0].AgentAvailable)
	got := rows[1]
	require.Equal(t, "addon", got.AgentName)
	require.True(t, got.AgentAvailable)
	require.Equal(t, int64(2), got.Holders, "workspace 1 and the personal subscription hold it")
	require.Equal(t, types.UsagePlanIDList{"plan-go"}, got.PlanIDs)

	got.PriceVND = 59000
	got.PlanIDs = nil
	got.IsActive = false
	require.NoError(t, plans.UpdateAgentAddon(ctx, got))
	again, err := plans.GetAgentAddon(ctx, "addon")
	require.NoError(t, err)
	require.Equal(t, int64(59000), again.PriceVND)
	require.Empty(t, again.PlanIDs)
	require.False(t, again.IsActive)
	active, err := plans.ListAgentAddons(ctx, false)
	require.NoError(t, err)
	require.Len(t, active, 1, "inactive rows are left out")

	deleted, err := plans.DeleteAgentAddon(ctx, "addon")
	require.NoError(t, err)
	require.True(t, deleted)
	_, err = plans.GetAgentAddon(ctx, "addon")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestDeactivateUnsellableAgentAddon(t *testing.T) {
	db, repo := distributionFixture(t)
	ctx := context.Background()
	plans := NewUsagePlanRepository(db)
	require.NoError(t, plans.CreateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "addon", PriceVND: 1, IsActive: true}))

	// still an addon agent: untouched
	require.NoError(t, plans.DeactivateUnsellableAgentAddon(ctx, "addon"))
	row, err := plans.GetAgentAddon(ctx, "addon")
	require.NoError(t, err)
	require.True(t, row.IsActive)

	// converted to basic: switched off, and not back on by converting again
	agent, err := repo.GetAgentByIDIgnoringTenantScope(ctx, "addon", 9)
	require.NoError(t, err)
	agent.Distribution = types.AgentDistributionBasic
	require.NoError(t, repo.UpdateAgent(ctx, agent))
	require.NoError(t, plans.DeactivateUnsellableAgentAddon(ctx, "addon"))
	row, err = plans.GetAgentAddon(ctx, "addon")
	require.NoError(t, err)
	require.False(t, row.IsActive)
	require.False(t, row.AgentAvailable)

	// deleted agent: same
	agent.Distribution = types.AgentDistributionAddon
	require.NoError(t, repo.UpdateAgent(ctx, agent))
	row.IsActive = true
	require.NoError(t, plans.UpdateAgentAddon(ctx, row))
	require.NoError(t, repo.DeleteAgent(ctx, "addon", 9))
	require.NoError(t, plans.DeactivateUnsellableAgentAddon(ctx, "addon"))
	row, err = plans.GetAgentAddon(ctx, "addon")
	require.NoError(t, err)
	require.False(t, row.IsActive)
}
