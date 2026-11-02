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

func TestSubscriptionAddonUpsertNeverShortens(t *testing.T) {
	_, repo := distributionFixture(t)
	ctx := context.Background()
	plans := NewUsagePlanRepository(repo.db)
	long, short := distNow.AddDate(0, 3, 0), distNow.AddDate(0, 1, 0)
	require.NoError(t, plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-t2", AgentID: "addon", StartsAt: distNow, EndsAt: &long}))
	require.NoError(t, plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-t2", AgentID: "addon", StartsAt: distNow.AddDate(0, 0, 5), EndsAt: &short, PriceVND: 7}))
	rows, err := plans.ListSubscriptionAddons(ctx, "sub-t2")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, long.Equal(*rows[0].EndsAt), "the later end is kept")
	require.True(t, distNow.Equal(rows[0].StartsAt), "the earlier start is kept")
	require.Equal(t, int64(7), rows[0].PriceVND)

	// pinning only gives an open right an end
	require.NoError(t, plans.PinSubscriptionAddonEnd(ctx, "sub-t2", "addon", short))
	rows, _ = plans.ListSubscriptionAddons(ctx, "sub-t2")
	require.True(t, long.Equal(*rows[0].EndsAt))
	rows, _ = plans.ListSubscriptionAddons(ctx, "sub-t1")
	require.Nil(t, rows[0].EndsAt)
	require.NoError(t, plans.PinSubscriptionAddonEnd(ctx, "sub-t1", "addon", short))
	rows, _ = plans.ListSubscriptionAddons(ctx, "sub-t1")
	require.True(t, short.Equal(*rows[0].EndsAt))
}

// A switched-off specialist agent is not sold (review item 3).
func TestSwitchedOffAgentIsNotAvailableForSale(t *testing.T) {
	db, repo := distributionFixture(t)
	ctx := context.Background()
	plans := NewUsagePlanRepository(repo.db)
	require.NoError(t, plans.CreateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "addon", IsActive: true}))
	row, err := plans.GetAgentAddon(ctx, "addon")
	require.NoError(t, err)
	require.True(t, row.AgentAvailable)
	require.NoError(t, db.Exec("UPDATE custom_agents SET enabled = ? WHERE id = ?", false, "addon").Error)
	row, err = plans.GetAgentAddon(ctx, "addon")
	require.NoError(t, err)
	require.False(t, row.AgentAvailable)
	require.True(t, row.IsActive, "switching it back on resumes sales")
}

func TestProfileBuiltinAgentsAndPendingOrderAddons(t *testing.T) {
	db, repo := distributionFixture(t)
	ctx := context.Background()
	plans := NewUsagePlanRepository(repo.db)
	off := false
	require.NoError(t, repo.CreateAgent(ctx, &types.CustomAgent{ID: types.BuiltinSmartReasoningID, Name: "Suy luận",
		TenantID: 9, IsBuiltin: true, Switch: &off}))
	require.NoError(t, repo.CreateAgent(ctx, &types.CustomAgent{ID: types.BuiltinSmartReasoningID, Name: "riêng",
		TenantID: 1, IsBuiltin: true}))
	rows, err := plans.ProfileBuiltinAgents(ctx, types.UsageIncludedAgentIDs)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the profile's row only")
	require.Equal(t, "Suy luận", rows[0].Name)
	require.False(t, rows[0].EffectiveEnabled())

	require.NoError(t, db.AutoMigrate(&types.UsageOrder{}))
	for _, o := range []*types.UsageOrder{
		{ID: "o1", OrderCode: "WRA", ScopeType: "tenant", ScopeID: "1", Status: types.UsageOrderPending,
			Addons: types.UsageOrderAddonLines{{AgentID: "addon"}}},
		{ID: "o2", OrderCode: "WRB", ScopeType: "tenant", ScopeID: "1", Status: types.UsageOrderPaid,
			Addons: types.UsageOrderAddonLines{{AgentID: "x"}}},
		{ID: "o3", OrderCode: "WRC", ScopeType: "tenant", ScopeID: "2", Status: types.UsageOrderPending,
			Addons: types.UsageOrderAddonLines{{AgentID: "y"}}},
	} {
		require.NoError(t, db.Create(o).Error)
	}
	pending, err := plans.PendingOrderAddons(ctx, "tenant", "1")
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"o1": {"addon"}}, pending)
}
