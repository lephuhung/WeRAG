package repository

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

var distNow = time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC)

// distributionFixture: profile 9 with workspaces 1 (bought "addon") and 2
// (did not). Agents on the profile: one per distribution, plus an internal
// agent with an empty list. Subscriptions: workspace 1 and 2 pool plans, a
// personal plan of "u-personal" (who works in workspace 2) holding "addon".
func distributionFixture(t *testing.T) (*gorm.DB, *customAgentRepository) {
	t.Helper()
	return distributionFixtureOn(t, setupModelUsageTestDB(t))
}

func distributionFixtureOn(t *testing.T, db *gorm.DB) (*gorm.DB, *customAgentRepository) {
	t.Helper()
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, kind, config_profile_id) VALUES
		(9, 'config_profile', NULL), (1, 'workspace', 9), (2, 'workspace', 9)`).Error)
	repo := NewCustomAgentRepository(db).(*customAgentRepository)
	ctx := context.Background()
	for _, a := range []*types.CustomAgent{
		{ID: "basic", Name: "basic", TenantID: 9, Distribution: types.AgentDistributionBasic},
		{ID: "addon", Name: "addon", TenantID: 9, Distribution: types.AgentDistributionAddon},
		{ID: "internal-one", Name: "internal-one", TenantID: 9, Distribution: types.AgentDistributionInternal,
			AppliesToTenantIDs: types.AgentTenantScope{1}},
		{ID: "internal-all", Name: "internal-all", TenantID: 9, Distribution: types.AgentDistributionInternal},
	} {
		require.NoError(t, repo.CreateAgent(ctx, a))
	}
	month := distNow.AddDate(0, 1, 0)
	for _, s := range []*types.UsageSubscription{
		{ID: "sub-t1", ScopeType: types.UsageScopeTenant, ScopeID: "1", PlanID: "plan", StartsAt: distNow.AddDate(0, -1, 0), EndsAt: &month, Status: "active"},
		{ID: "sub-t2", ScopeType: types.UsageScopeTenant, ScopeID: "2", PlanID: "plan", StartsAt: distNow.AddDate(0, -1, 0), Status: "active"},
		{ID: "sub-u", ScopeType: types.UsageScopeUser, ScopeID: "u-personal", PlanID: "plan", StartsAt: distNow.AddDate(0, -1, 0), Status: "active"},
	} {
		require.NoError(t, db.Omit("Plan").Create(s).Error)
	}
	plans := NewUsagePlanRepository(db)
	for _, sub := range []string{"sub-t1", "sub-u"} {
		require.NoError(t, plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
			SubscriptionID: sub, AgentID: "addon", StartsAt: distNow.AddDate(0, 0, -1),
		}))
	}
	return db, repo
}

func visibleIDs(t *testing.T, repo *customAgentRepository, viewer types.AgentViewer) []string {
	t.Helper()
	list, err := repo.ListVisibleAgents(context.Background(), viewer)
	require.NoError(t, err)
	ids := make([]string, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.ID)
		require.Equal(t, viewer.TenantID, a.TenantID)
	}
	sort.Strings(ids)
	// Get answers exactly like the list
	for _, id := range []string{"basic", "addon", "internal-one", "internal-all"} {
		_, err := repo.GetVisibleAgentByID(context.Background(), id, viewer)
		if contains(ids, id) {
			require.NoError(t, err, "get %s", id)
		} else {
			require.ErrorIs(t, err, ErrCustomAgentNotFound, "get %s", id)
		}
	}
	return ids
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestAgentDistributionVisibilityByCaller(t *testing.T) {
	_, repo := distributionFixture(t)
	checkVisibilityByCaller(t, repo)
}

// TestAgentDistributionVisibilityPostgres runs the same matrix on a real
// PostgreSQL (the jsonb branch and the EXISTS with timestamptz). Skipped
// unless WEKNORA_REPO_TEST_POSTGRES_DSN points at a disposable database
// holding at least tenants, custom_agents (000165 + 000166),
// usage_subscriptions and usage_subscription_addons; every row of those
// tables is deleted first.
func TestAgentDistributionVisibilityPostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_REPO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_REPO_TEST_POSTGRES_DSN to run against PostgreSQL")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	for _, table := range []string{"usage_agent_addons", "usage_subscription_addons", "usage_subscriptions", "custom_agents", "tenants"} {
		require.NoError(t, db.Exec("DELETE FROM "+table).Error)
	}
	if err := db.Exec(`INSERT INTO usage_plans (id, code, name) VALUES ('plan', 'plan', 'Plan') ON CONFLICT DO NOTHING`).Error; err != nil {
		t.Logf("usage_plans: %v", err)
	}
	_, repo := distributionFixtureOn(t, db)
	checkVisibilityByCaller(t, repo)

	// the catalogue queries on jsonb / timestamptz
	ctx := context.Background()
	plans := NewUsagePlanRepository(db)
	require.NoError(t, plans.CreateAgentAddon(ctx, &types.UsageAgentAddon{
		AgentID: "addon", PriceVND: 49000, PlanIDs: types.UsagePlanIDList{"plan"}, IsActive: true}))
	rows, err := plans.ListAgentAddons(ctx, false)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].AgentAvailable)
	require.Equal(t, types.UsagePlanIDList{"plan"}, rows[0].PlanIDs)
	require.NoError(t, db.Exec("UPDATE custom_agents SET distribution = 'basic' WHERE id = 'addon'").Error)
	require.NoError(t, plans.DeactivateUnsellableAgentAddon(ctx, "addon"))
	row, err := plans.GetAgentAddon(ctx, "addon")
	require.NoError(t, err)
	require.False(t, row.IsActive)
	agents, err := NewDomainPackAgentRepository(db).ListAgentsUsingPack(ctx, "none")
	require.NoError(t, err)
	require.Empty(t, agents)
}

func checkVisibilityByCaller(t *testing.T, repo *customAgentRepository) {
	t.Helper()
	cases := []struct {
		name   string
		viewer types.AgentViewer
		want   []string
	}{
		{"member of the buying Tổ chức", types.AgentViewer{TenantID: 1, UserID: "u-member1", Now: distNow},
			[]string{"addon", "basic", "internal-all", "internal-one"}},
		{"member of another Tổ chức", types.AgentViewer{TenantID: 2, UserID: "u-member2", Now: distNow},
			[]string{"basic", "internal-all"}},
		{"personal subscriber in another Tổ chức", types.AgentViewer{TenantID: 2, UserID: "u-personal", Now: distNow},
			[]string{"addon", "basic", "internal-all"}},
		{"API key of the buying Tổ chức", types.AgentViewer{TenantID: 1, UserID: "system-1", Now: distNow},
			[]string{"addon", "basic", "internal-all", "internal-one"}},
		{"API key of another Tổ chức", types.AgentViewer{TenantID: 2, UserID: "system-2", Now: distNow},
			[]string{"basic", "internal-all"}},
		{"machine principal with no person",
			types.AgentViewer{TenantID: 2, Now: distNow}, []string{"basic", "internal-all"}},
		{"addons shown without purchase (decision 7.4 flipped)",
			types.AgentViewer{TenantID: 2, AllAddons: true, Now: distNow},
			[]string{"addon", "basic", "internal-all"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, visibleIDs(t, repo, tc.viewer))
		})
	}

	// GetAgentByID / ListAgentsByTenantID answer for the Tổ chức alone
	_, err := repo.GetAgentByID(context.Background(), "addon", 2)
	require.ErrorIs(t, err, ErrCustomAgentNotFound)

	// the SuperAdmin read skips the rule
	all, err := repo.ListAgentsByTenantIDIgnoringTenantScope(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, all, 4)
}

func TestAgentDistributionAddonExpiry(t *testing.T) {
	db, repo := distributionFixture(t)
	member := types.AgentViewer{TenantID: 1, UserID: "u-member1"}
	at := func(ts time.Time) []string {
		v := member
		v.Now = ts
		return visibleIDs(t, repo, v)
	}
	require.Contains(t, at(distNow), "addon")
	// before the add-on starts, and once the subscription ended (ends_at is
	// exclusive) — expired add-on rights hide the agent, so the next turn
	// naming it gets 2104
	require.NotContains(t, at(distNow.AddDate(0, 0, -2)), "addon")
	require.NotContains(t, at(distNow.AddDate(0, 1, 0)), "addon")

	// the add-on row's own end
	end := distNow.Add(time.Hour)
	require.NoError(t, db.Model(&types.UsageSubscriptionAddon{}).
		Where("subscription_id = ? AND agent_id = ?", "sub-t1", "addon").Update("ends_at", end).Error)
	require.Contains(t, at(distNow), "addon")
	require.NotContains(t, at(end), "addon")

	// a cancelled subscription carries no add-on
	require.NoError(t, db.Model(&types.UsageSubscriptionAddon{}).
		Where("subscription_id = ?", "sub-t1").Update("ends_at", nil).Error)
	require.Contains(t, at(distNow), "addon")
	require.NoError(t, db.Model(&types.UsageSubscription{}).Where("id = ?", "sub-t1").
		Update("status", types.UsageSubscriptionCancelled).Error)
	require.NotContains(t, at(distNow), "addon")
}

func TestAgentDistributionOwnRowsAndRevoke(t *testing.T) {
	_, repo := distributionFixture(t)
	ctx := context.Background()
	// a Tổ chức's own row is always visible, whatever its distribution
	require.NoError(t, repo.CreateAgent(ctx, &types.CustomAgent{
		ID: "own-addon", Name: "own", TenantID: 2, Distribution: types.AgentDistributionAddon,
	}))
	require.Contains(t, visibleIDs(t, repo, types.AgentViewer{TenantID: 2, Now: distNow}), "own-addon")

	// revoking the add-on hides the agent at once
	plans := NewUsagePlanRepository(repo.db)
	deleted, err := plans.DeleteSubscriptionAddon(ctx, "sub-t1", "addon")
	require.NoError(t, err)
	require.True(t, deleted)
	require.NotContains(t, visibleIDs(t, repo, types.AgentViewer{TenantID: 1, Now: distNow}), "addon")
	deleted, err = plans.DeleteSubscriptionAddon(ctx, "sub-t1", "addon")
	require.NoError(t, err)
	require.False(t, deleted)
}

func TestSubscriptionAddonRepository(t *testing.T) {
	_, repo := distributionFixture(t)
	ctx := context.Background()
	plans := NewUsagePlanRepository(repo.db)

	addons, err := plans.ListSubscriptionAddons(ctx, "sub-t1")
	require.NoError(t, err)
	require.Len(t, addons, 1)
	require.Equal(t, "addon", addons[0].AgentName)
	created := addons[0].CreatedAt

	// re-granting replaces period and price, keeps one row and created_at
	end := distNow.AddDate(0, 2, 0)
	require.NoError(t, plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-t1", AgentID: "addon", StartsAt: distNow, EndsAt: &end, PriceVND: 150000,
	}))
	addons, err = plans.ListSubscriptionAddons(ctx, "sub-t1")
	require.NoError(t, err)
	require.Len(t, addons, 1)
	require.Equal(t, int64(150000), addons[0].PriceVND)
	require.NotNil(t, addons[0].EndsAt)
	require.WithinDuration(t, created, addons[0].CreatedAt, time.Second)

	d, found, err := plans.CustomAgentDistribution(ctx, "addon")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.AgentDistributionAddon, d)
	_, found, err = plans.CustomAgentDistribution(ctx, "missing")
	require.NoError(t, err)
	require.False(t, found)
}

func TestSwitchedOffCustomAgentIsHidden(t *testing.T) {
	db, repo := distributionFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Exec("UPDATE custom_agents SET enabled = ? WHERE id = ?", false, "basic").Error)
	viewer := types.AgentViewer{TenantID: 1, Now: distNow}
	require.NotContains(t, visibleIDs(t, repo, viewer), "basic")
	// NULL = on, as for every row saved before the column existed
	require.NoError(t, db.Exec("UPDATE custom_agents SET enabled = NULL WHERE id = ?", "basic").Error)
	require.Contains(t, visibleIDs(t, repo, viewer), "basic")

	// a built-in row stays resolvable when switched off; a tenant's own row
	// takes the profile's switch
	off := false
	require.NoError(t, repo.CreateAgent(ctx, &types.CustomAgent{ID: "builtin-x", Name: "x", TenantID: 9, IsBuiltin: true, Switch: &off}))
	require.NoError(t, repo.CreateAgent(ctx, &types.CustomAgent{ID: "builtin-x", Name: "x (1)", TenantID: 1, IsBuiltin: true}))
	got, err := repo.GetVisibleAgentByID(ctx, "builtin-x", viewer)
	require.NoError(t, err)
	require.Equal(t, "x (1)", got.Name)
	require.NotNil(t, got.Switch)
	require.False(t, *got.Switch)
	list, err := repo.ListVisibleAgents(ctx, viewer)
	require.NoError(t, err)
	for _, a := range list {
		if a.ID == "builtin-x" {
			require.False(t, a.EffectiveEnabled())
		}
	}
}

// TestUsageOrderAddonLinesPostgres stores an order's add-on lines (jsonb)
// and kind on a real PostgreSQL; skipped like the test above.
func TestUsageOrderAddonLinesPostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_REPO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_REPO_TEST_POSTGRES_DSN to run against PostgreSQL")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("DELETE FROM usage_orders WHERE id = 'o-pg'").Error)
	ctx := context.Background()
	repo := NewUsageBillingRepository(db)
	end := distNow.AddDate(0, 2, 0)
	target := "sub-x"
	order := &types.UsageOrder{ID: "o-pg", OrderCode: "WRPGTEST01", ScopeType: "tenant", ScopeID: "1",
		PlanID: "plan", Kind: types.UsageOrderKindAddon, TargetSubscriptionID: &target,
		Addons: types.UsageOrderAddonLines{{AgentID: "addon", Name: "A", UnitPriceVND: 99000, Units: 2,
			Months: 2, LeftoverDays: 10, LeftoverFactor: "half", AmountVND: 495000, PaidUntil: &end}},
		CreatedAt: distNow, UpdatedAt: distNow}
	require.NoError(t, repo.CreateOrder(ctx, order))
	got, err := repo.GetOrder(ctx, "o-pg")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderKindAddon, got.Kind)
	require.Equal(t, "sub-x", *got.TargetSubscriptionID)
	require.Len(t, got.Addons, 1)
	require.Equal(t, int64(495000), got.Addons[0].AmountVND)
	require.True(t, end.Equal(*got.Addons[0].PaidUntil))
	require.NoError(t, db.Exec("DELETE FROM usage_orders WHERE id = 'o-pg'").Error)
}
