package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// addonFixture adds a specialist agent "thue" to the scope fixture, sold to
// workspace 1 and to the personal subscription of "u-personal".
func addonFixture(t *testing.T) *customAgentService {
	t.Helper()
	svc, _ := addonFixtureDB(t)
	return svc
}

func addonFixtureDB(t *testing.T) (*customAgentService, *gorm.DB) {
	t.Helper()
	svc, db := newAgentScopeService(t)
	ctx := context.Background()
	require.NoError(t, svc.repo.CreateAgent(ctx, &types.CustomAgent{
		ID: "thue", Name: "Thuế HKD", TenantID: 9, Distribution: types.AgentDistributionAddon,
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning, DomainPacks: []string{"thue-hkd"}},
	}))
	past := time.Now().AddDate(0, -1, 0)
	for _, s := range []*types.UsageSubscription{
		{ID: "sub-1", ScopeType: types.UsageScopeTenant, ScopeID: "1", PlanID: "p", StartsAt: past, Status: "active"},
		{ID: "sub-u", ScopeType: types.UsageScopeUser, ScopeID: "u-personal", PlanID: "p", StartsAt: past, Status: "active"},
	} {
		require.NoError(t, db.Omit("Plan").Create(s).Error)
	}
	plans := repository.NewUsagePlanRepository(db)
	for _, sub := range []string{"sub-1", "sub-u"} {
		require.NoError(t, plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
			SubscriptionID: sub, AgentID: "thue", StartsAt: past,
		}))
	}
	return svc, db
}

func userCtx(tenantID uint64, userID string) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	return types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: userID, Role: types.TenantRoleMember})
}

func TestAddonAgentVisibilityThroughService(t *testing.T) {
	svc := addonFixture(t)

	// member of the buying Tổ chức
	_, err := svc.GetAgentByID(userCtx(1, "u-member"), "thue")
	require.NoError(t, err)
	list, err := svc.ListAgents(userCtx(1, "u-member"))
	require.NoError(t, err)
	require.Contains(t, customAgentIDs(list), "thue")

	// member of another Tổ chức: hidden, the same not-found as a bad id
	_, err = svc.GetAgentByID(userCtx(2, "u-other"), "thue")
	require.ErrorIs(t, err, ErrAgentNotFound)
	list, err = svc.ListAgents(userCtx(2, "u-other"))
	require.NoError(t, err)
	require.NotContains(t, customAgentIDs(list), "thue")

	// the person's own subscription bought it, whatever Tổ chức they are in
	_, err = svc.GetAgentByID(userCtx(2, "u-personal"), "thue")
	require.NoError(t, err)

	// an API key carries no person: its Tổ chức decides
	_, err = svc.GetAgentByID(userCtx(2, "system-2"), "thue")
	require.ErrorIs(t, err, ErrAgentNotFound)
	_, err = svc.GetAgentByID(userCtx(1, "system-1"), "thue")
	require.NoError(t, err)

	// GetAgentByIDAndTenant (channels, shared agents) answers for the Tổ chức
	_, err = svc.GetAgentByIDAndTenant(userCtx(2, "u-personal"), "thue", 2)
	require.ErrorIs(t, err, ErrAgentNotFound)

	// a SuperAdmin sees every agent
	_, err = svc.GetAgentByID(scopeSuperAdminCtx(2), "thue")
	require.NoError(t, err)
}

func TestAgentDistributionOnCreate(t *testing.T) {
	svc, _ := newAgentScopeService(t)
	profile := scopeSuperAdminCtx(9)

	plain, err := svc.CreateAgent(profile, &types.CustomAgent{Name: "Chung"})
	require.NoError(t, err)
	require.Equal(t, types.AgentDistributionBasic, plain.Distribution)

	packed, err := svc.CreateAgent(profile, &types.CustomAgent{
		Name: "Thuế", Config: types.CustomAgentConfig{DomainPacks: []string{"thue-hkd"}},
	})
	require.NoError(t, err)
	require.Equal(t, types.AgentDistributionAddon, packed.Distribution,
		"an agent with a skill set starts as an unsold add-on")
	_, err = svc.GetAgentByID(scopeMemberCtx(1), packed.ID)
	require.ErrorIs(t, err, ErrAgentNotFound, "nobody bought it yet")

	listed, err := svc.CreateAgent(profile, &types.CustomAgent{Name: "Nhóm", AppliesToTenantIDs: types.AgentTenantScope{1}})
	require.NoError(t, err)
	require.Equal(t, types.AgentDistributionInternal, listed.Distribution)

	chosen, err := svc.CreateAgent(profile, &types.CustomAgent{
		Name: "Chọn", Distribution: "basic", Config: types.CustomAgentConfig{DomainPacks: []string{"thue-hkd"}},
	})
	require.NoError(t, err)
	require.Equal(t, types.AgentDistributionBasic, chosen.Distribution, "an explicit choice wins")

	_, err = svc.CreateAgent(profile, &types.CustomAgent{
		Name: "Sai", Distribution: types.AgentDistributionBasic, AppliesToTenantIDs: types.AgentTenantScope{1},
	})
	require.ErrorIs(t, err, ErrAgentScopeNeedsInternal)
	_, err = svc.CreateAgent(profile, &types.CustomAgent{Name: "Sai", Distribution: "everyone"})
	require.ErrorIs(t, err, ErrAgentDistributionInvalid)
	require.True(t, IsAgentTenantScopeError(ErrAgentDistributionInvalid))
}

func TestAgentDistributionOnUpdate(t *testing.T) {
	svc, _ := newAgentScopeService(t)
	profile := scopeSuperAdminCtx(9)

	// not sent: kept
	got, err := svc.UpdateAgent(profile, &types.CustomAgent{ID: "group", Name: "Thuế"}, nil)
	require.NoError(t, err)
	require.Equal(t, types.AgentDistributionInternal, got.Distribution)
	require.Equal(t, types.AgentTenantScope{1}, got.AppliesToTenantIDs)

	// a list with another distribution is refused
	_, err = svc.UpdateAgent(profile, &types.CustomAgent{
		ID: "group", Name: "Thuế", Distribution: "basic", AppliesToTenantIDs: types.AgentTenantScope{1},
	}, nil)
	require.ErrorIs(t, err, ErrAgentScopeNeedsInternal)

	// leaving internal without a list drops the stored one
	got, err = svc.UpdateAgent(profile, &types.CustomAgent{ID: "group", Name: "Thuế", Distribution: "addon"}, nil)
	require.NoError(t, err)
	require.Equal(t, types.AgentDistributionAddon, got.Distribution)
	require.Empty(t, got.AppliesToTenantIDs)
	_, err = svc.GetAgentByID(scopeMemberCtx(1), "group")
	require.ErrorIs(t, err, ErrAgentNotFound, "workspace 1 has not bought it")

	// copies keep the distribution
	copied, err := svc.CopyAgent(profile, "group")
	require.NoError(t, err)
	require.Equal(t, types.AgentDistributionAddon, copied.Distribution)
}

func TestBuiltinAgentDistributionIsFixed(t *testing.T) {
	restore := withBuiltinModelAgent(t, "mode-chat")
	defer restore()
	svc, _ := newAgentScopeService(t)
	_, err := svc.UpdateAgent(scopeSuperAdminCtx(9), &types.CustomAgent{
		ID: "builtin-quick-answer", Config: types.CustomAgentConfig{ModelID: "mode-chat"}, Distribution: "addon",
	}, nil)
	require.ErrorIs(t, err, ErrBuiltinAgentDistribution)
	got, err := svc.UpdateAgent(scopeSuperAdminCtx(9), &types.CustomAgent{
		ID: "builtin-quick-answer", Config: types.CustomAgentConfig{ModelID: "mode-chat"}, Distribution: "basic",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, types.AgentDistributionBasic, got.Distribution)
}

func TestAddonsVisibleWithoutPurchaseIsOff(t *testing.T) {
	// decision 7.4: add-on agents only for buyers, even with usage.enforce off
	require.False(t, addonsVisibleWithoutPurchase(context.Background()))
	require.False(t, agentViewer(userCtx(1, "u"), 1).AllAddons)
	require.Empty(t, agentViewer(userCtx(1, "system-1"), 1).UserID)
}

// Converting an agent away from addon, or deleting it, switches its
// catalogue row off (it is never offered again until a SuperAdmin re-enables
// it).
func TestAgentChangesDeactivateCatalogueRow(t *testing.T) {
	svc, db := addonFixtureDB(t)
	plans := repository.NewUsagePlanRepository(db)
	svc.SetAgentAddonCatalogue(plans)
	ctx := context.Background()
	profile := scopeSuperAdminCtx(9)
	require.NoError(t, plans.CreateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "thue", PriceVND: 99000, IsActive: true}))

	// an edit that keeps addon leaves it on
	_, err := svc.UpdateAgent(profile, &types.CustomAgent{ID: "thue", Name: "Thuế HKD 2",
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning, DomainPacks: []string{"thue-hkd"}}}, nil)
	require.NoError(t, err)
	row, err := plans.GetAgentAddon(ctx, "thue")
	require.NoError(t, err)
	require.True(t, row.IsActive)

	_, err = svc.UpdateAgent(profile, &types.CustomAgent{ID: "thue", Name: "Thuế HKD 2", Distribution: "internal",
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning}}, nil)
	require.NoError(t, err)
	row, err = plans.GetAgentAddon(ctx, "thue")
	require.NoError(t, err)
	require.False(t, row.IsActive)

	// back to addon and on again, then deleted
	_, err = svc.UpdateAgent(profile, &types.CustomAgent{ID: "thue", Name: "Thuế HKD 2", Distribution: "addon",
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning}}, nil)
	require.NoError(t, err)
	row.IsActive = true
	require.NoError(t, plans.UpdateAgentAddon(ctx, row))
	require.NoError(t, svc.DeleteAgent(profile, "thue"))
	row, err = plans.GetAgentAddon(ctx, "thue")
	require.NoError(t, err)
	require.False(t, row.IsActive)
}
