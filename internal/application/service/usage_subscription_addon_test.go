package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

func newAddonPlanService(t *testing.T) *usagePlanService {
	svc, _ := newAddonPlanServiceDB(t)
	return svc
}

func newAddonPlanServiceDB(t *testing.T) (*usagePlanService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.CustomAgent{}, &types.UsagePlan{}, &types.UsageSubscription{},
		&types.UsageSubscriptionAddon{}, &types.UsageAgentAddon{}))
	ctx := context.Background()
	require.NoError(t, db.Create(&types.UsagePlan{ID: "p", Code: "go", Name: "Go", IsActive: true,
		ScopeKind: "tenant", Mode: types.UsageModeSeat, PriceVND: 49000}).Error)
	require.NoError(t, db.Create(&types.UsagePlan{ID: "plus", Code: "plus", Name: "Plus", IsActive: true,
		ScopeKind: "tenant", Mode: types.UsageModePool, PriceVND: 990000}).Error)
	require.NoError(t, db.Create(&types.UsagePlan{ID: "me", Code: "me", Name: "Cá nhân", IsActive: true,
		ScopeKind: "user", Mode: types.UsageModePool, PriceVND: 29000}).Error)
	require.NoError(t, db.Omit("Plan").Create(&types.UsageSubscription{
		ID: "sub-1", ScopeType: types.UsageScopeTenant, ScopeID: "1", PlanID: "p",
		StartsAt: time.Now().AddDate(0, -1, 0), Status: "active",
	}).Error)
	agents := repository.NewCustomAgentRepository(db)
	for _, a := range []*types.CustomAgent{
		{ID: "thue", Name: "Thuế", TenantID: 9, Distribution: types.AgentDistributionAddon},
		{ID: "chung", Name: "Chung", TenantID: 9, Distribution: types.AgentDistributionBasic},
	} {
		require.NoError(t, agents.CreateAgent(ctx, a))
	}
	return &usagePlanService{repo: repository.NewUsagePlanRepository(db), now: time.Now}, db
}

func appErrCode(t *testing.T, err error) int {
	t.Helper()
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok, "%v", err)
	return appErr.HTTPCode
}

func TestGrantAndRevokeSubscriptionAddon(t *testing.T) {
	svc := newAddonPlanService(t)
	ctx := context.Background()

	addon, err := svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "thue", PriceVND: 99000}, "super-1")
	require.NoError(t, err)
	require.Equal(t, "super-1", addon.GrantedBy)
	require.Nil(t, addon.EndsAt, "no end = follows the subscription")
	list, err := svc.ListSubscriptionAddons(ctx, "sub-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "Thuế", list[0].AgentName)

	// refused grants
	_, err = svc.GrantSubscriptionAddon(ctx, "missing", types.UsageSubscriptionAddonGrant{AgentID: "thue"}, "")
	require.Equal(t, 404, appErrCode(t, err))
	_, err = svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "nope"}, "")
	require.Equal(t, 404, appErrCode(t, err))
	_, err = svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "chung"}, "")
	require.Equal(t, 400, appErrCode(t, err), "a basic agent is already everyone's")
	past := time.Now().AddDate(0, 0, -2) // ends snap to 24:00 VN: an earlier date
	_, err = svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "thue", EndsAt: &past}, "")
	require.Equal(t, 400, appErrCode(t, err))
	_, err = svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "thue", PriceVND: -1}, "")
	require.Equal(t, 400, appErrCode(t, err))

	require.NoError(t, svc.RevokeSubscriptionAddon(ctx, "sub-1", "thue"))
	require.Equal(t, 404, appErrCode(t, svc.RevokeSubscriptionAddon(ctx, "sub-1", "thue")))
	_, err = svc.ListSubscriptionAddons(ctx, "missing")
	require.Equal(t, 404, appErrCode(t, err))
}

func TestAgentAddonCatalogueValidation(t *testing.T) {
	svc := newAddonPlanService(t)
	ctx := context.Background()

	row, err := svc.CreateAgentAddon(ctx, &types.UsageAgentAddon{
		AgentID: "thue", PriceVND: 99000, PlanIDs: types.UsagePlanIDList{" p ", "p"}, Description: " Thuế HKD ", IsActive: true,
	})
	require.NoError(t, err)
	require.Equal(t, types.UsagePlanIDList{"p"}, row.PlanIDs, "trimmed, deduplicated")
	require.Equal(t, "Thuế HKD", row.Description)
	require.Equal(t, "Thuế", row.AgentName)
	require.True(t, row.AgentAvailable)

	_, err = svc.CreateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "thue"})
	require.Equal(t, 409, appErrCode(t, err), "one row per agent")
	_, err = svc.CreateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "chung"})
	require.Equal(t, 400, appErrCode(t, err), "only addon agents are sold")
	_, err = svc.CreateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "nope"})
	require.Equal(t, 404, appErrCode(t, err))

	_, err = svc.UpdateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "thue", PriceVND: -1})
	require.Equal(t, 400, appErrCode(t, err))
	_, err = svc.UpdateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "thue", PlanIDs: types.UsagePlanIDList{"ghost"}})
	require.Equal(t, 400, appErrCode(t, err), "plans must exist")
	_, err = svc.UpdateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "chung"})
	require.Equal(t, 404, appErrCode(t, err))
	updated, err := svc.UpdateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "thue", PriceVND: 0, IsActive: false})
	require.NoError(t, err)
	require.Zero(t, updated.PriceVND)
	require.Empty(t, updated.PlanIDs, "[] = every plan")

	catalogue, err := svc.ListAgentAddons(ctx, true)
	require.NoError(t, err)
	require.Len(t, catalogue.Addons, 1)

	require.NoError(t, svc.DeleteAgentAddon(ctx, "thue"))
	require.Equal(t, 404, appErrCode(t, svc.DeleteAgentAddon(ctx, "thue")))
}

func TestPlanIncludedAgentsValidation(t *testing.T) {
	svc, db := newAddonPlanServiceDB(t)
	ctx := context.Background()
	reason := func(err error) string {
		t.Helper()
		appErr, ok := err.(*apperrors.AppError)
		require.True(t, ok, "%v", err)
		require.Equal(t, 400, appErr.HTTPCode)
		return appErr.Details.(map[string]string)["reason"]
	}
	plan := func(ids ...string) *types.UsagePlan {
		return &types.UsagePlan{Code: "adv", Name: "Nâng cao", IncludedAgentIDs: ids}
	}

	created, err := svc.CreatePlan(ctx, plan(" thue ", "thue"))
	require.NoError(t, err)
	require.Equal(t, types.UsageAgentIDList{"thue"}, created.IncludedAgentIDs, "trimmed, deduplicated")
	stored, err := svc.GetPlan(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, types.UsageAgentIDList{"thue"}, stored.IncludedAgentIDs)

	_, err = svc.CreatePlan(ctx, &types.UsagePlan{Code: "b", Name: "B", IncludedAgentIDs: []string{"chung"}})
	require.Equal(t, "included_agent_not_addon", reason(err), "a basic agent needs no inclusion")
	_, err = svc.CreatePlan(ctx, &types.UsagePlan{Code: "c", Name: "C", IncludedAgentIDs: []string{"nope"}})
	require.Equal(t, "included_agent_not_found", reason(err))

	// an update without the field keeps the list; [] clears it
	kept, err := svc.UpdatePlan(ctx, &types.UsagePlan{ID: created.ID, Code: "adv", Name: "Nâng cao 2"})
	require.NoError(t, err)
	require.Equal(t, types.UsageAgentIDList{"thue"}, kept.IncludedAgentIDs)
	// the agent became basic since: an unrelated edit still saves, without it
	require.NoError(t, db.Exec("UPDATE custom_agents SET distribution = 'basic' WHERE id = 'thue'").Error)
	dropped, err := svc.UpdatePlan(ctx, &types.UsagePlan{ID: created.ID, Code: "adv", Name: "Nâng cao 3",
		IncludedAgentIDs: types.UsageAgentIDList{"thue"}})
	require.NoError(t, err)
	require.Empty(t, dropped.IncludedAgentIDs)
	_, err = svc.UpdatePlan(ctx, &types.UsagePlan{ID: created.ID, Code: "adv", Name: "Nâng cao 3",
		IncludedAgentIDs: types.UsageAgentIDList{"chung"}})
	require.Equal(t, "included_agent_not_addon", reason(err), "newly added ones are checked")
	require.NoError(t, db.Exec("UPDATE custom_agents SET distribution = 'addon' WHERE id = 'thue'").Error)

	// the admin catalogue lists every live addon agent as a choice
	catalogue, err := svc.ListAgentAddons(ctx, true)
	require.NoError(t, err)
	require.Equal(t, []types.UsagePlanAgent{{ID: "thue", Name: "Thuế"}}, catalogue.AddonAgents)
}

func TestPurchasablePlansCarryAgents(t *testing.T) {
	// the two basic agents, as builtin_agents.yaml would register them
	for id, name := range map[string]string{
		types.BuiltinDocumentAssistantID: "Soạn thảo văn bản", types.BuiltinSmartReasoningID: "Suy luận thông minh",
	} {
		id, name := id, name
		if _, ok := types.BuiltinAgentRegistry[id]; ok {
			continue
		}
		types.BuiltinAgentRegistry[id] = func(tenantID uint64) *types.CustomAgent {
			return &types.CustomAgent{ID: id, Name: name, IsBuiltin: true, TenantID: tenantID}
		}
		t.Cleanup(func() { delete(types.BuiltinAgentRegistry, id) })
	}
	svc, db := newAddonPlanServiceDB(t)
	ctx := context.Background()
	_, err := svc.CreateAgentAddon(ctx, &types.UsageAgentAddon{
		AgentID: "thue", PriceVND: 99000, Description: "Thuế HKD", IsActive: true,
	})
	require.NoError(t, err)
	billing := &usageBillingService{plans: svc, planRepo: svc.repo, now: time.Now}

	offers, err := billing.PurchasablePlans(ctx, "tenant")
	require.NoError(t, err)
	require.Len(t, offers, 2)
	byID := map[string]*types.UsagePlanOffer{}
	for _, o := range offers {
		byID[o.ID] = o
	}
	require.Len(t, byID["p"].Addons, 1)
	require.Equal(t, types.UsagePlanAddonOffer{AgentID: "thue", Name: "Thuế", Description: "Thuế HKD",
		PriceVND: 99000, PerSeat: true}, byID["p"].Addons[0], "seat plan: × seats")
	require.False(t, byID["plus"].Addons[0].PerSeat)
	ids := []string{}
	for _, a := range byID["p"].IncludedAgents {
		ids = append(ids, a.ID)
		require.NotEmpty(t, a.Name)
	}
	require.Equal(t, types.UsageIncludedAgentIDs, ids)

	// sold with one plan only
	_, err = svc.UpdateAgentAddon(ctx, &types.UsageAgentAddon{AgentID: "thue", PriceVND: 99000,
		PlanIDs: types.UsagePlanIDList{"plus"}, IsActive: true})
	require.NoError(t, err)
	offers, err = billing.PurchasablePlans(ctx, "tenant")
	require.NoError(t, err)
	for _, o := range offers {
		if o.ID == "p" {
			require.Empty(t, o.Addons)
			require.NotNil(t, o.Addons, "[] in the API")
		} else {
			require.Len(t, o.Addons, 1)
		}
	}

	// an agent converted away from addon is never offered, even before the
	// row is switched off
	require.NoError(t, db.Model(&types.CustomAgent{}).Where("id = ?", "thue").
		Update("distribution", types.AgentDistributionBasic).Error)
	offers, err = billing.PurchasablePlans(ctx, "")
	require.NoError(t, err)
	for _, o := range offers {
		require.Empty(t, o.Addons)
	}

	// the JSON keeps the plan's own fields at the top level
	b, err := json.Marshal(offers[0])
	require.NoError(t, err)
	require.Contains(t, string(b), `"price_vnd":`)
	require.Contains(t, string(b), `"included_agents":[`)
	require.Contains(t, string(b), `"addons":[]`)
}

// The Tổ chức "Gói sử dụng" page lists the add-ons it holds with their end
// (a right without one shows the subscription's end).
func TestTenantUsageListsHeldAddons(t *testing.T) {
	svc, db := newAddonPlanServiceDB(t)
	svc.guard = &usageGuard{}
	ctx := context.Background()
	end := time.Now().AddDate(0, 2, 0)
	require.NoError(t, db.Model(&types.UsageSubscription{}).Where("id = ?", "sub-1").Update("ends_at", end).Error)
	_, err := svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "thue"}, "sa")
	require.NoError(t, err)
	past := time.Now().Add(-time.Hour)
	require.NoError(t, svc.repo.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "old", StartsAt: past.AddDate(0, -1, 0), EndsAt: &past}))

	usage, err := svc.TenantUsage(ctx, 1)
	require.NoError(t, err)
	require.Len(t, usage.Addons, 1, "expired rights are left out")
	require.Equal(t, "Thuế", usage.Addons[0].AgentName)
	require.WithinDuration(t, end, *usage.Addons[0].EndsAt, time.Second)
	// ended an hour ago while the package goes on: offered again
	require.Len(t, usage.EndedAddons, 1)
	require.Equal(t, "old", usage.EndedAddons[0].AgentID)
}

// The organization's usage view lists the agents its plan includes,
// flagged, with no end of their own; one also bought is shown once.
func TestTenantUsageListsPlanIncludedAgents(t *testing.T) {
	svc, db := newAddonPlanServiceDB(t)
	svc.guard = &usageGuard{}
	ctx := context.Background()
	agents := repository.NewCustomAgentRepository(db)
	require.NoError(t, agents.CreateAgent(ctx, &types.CustomAgent{
		ID: "ds", Name: "Suy luận nâng cao", TenantID: 9, Distribution: types.AgentDistributionAddon}))
	require.NoError(t, db.Model(&types.UsagePlan{}).Where("id = ?", "p").
		Update("included_agent_ids", types.UsageAgentIDList{"ds", "thue"}).Error)
	_, err := svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "thue"}, "sa")
	require.NoError(t, err)
	// a bought right of "ds" that ended an hour ago: the plan still gives
	// it, so no "buy it again" banner
	past := time.Now().Add(-time.Hour)
	require.NoError(t, svc.repo.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "ds", StartsAt: past.AddDate(0, -1, 0), EndsAt: &past}))

	usage, err := svc.TenantUsage(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, usage.EndedAddons)
	require.Len(t, usage.Addons, 2)
	byID := map[string]*types.UsageSubscriptionAddon{}
	for _, a := range usage.Addons {
		byID[a.AgentID] = a
	}
	require.True(t, byID["ds"].Included)
	require.Nil(t, byID["ds"].EndsAt)
	require.Equal(t, "Suy luận nâng cao", byID["ds"].AgentName)
	require.True(t, byID["thue"].Included, "bought and included: shown as included")
}

// An add-on that ended long ago is no longer reported as ended.
func TestTenantUsageEndedAddonsWindow(t *testing.T) {
	svc, db := newAddonPlanServiceDB(t)
	svc.guard = &usageGuard{}
	ctx := context.Background()
	require.NoError(t, db.Model(&types.UsageSubscription{}).Where("id = ?", "sub-1").
		Update("ends_at", time.Now().AddDate(0, 2, 0)).Error)
	long := time.Now().AddDate(0, 0, -(types.UsageAddonEndedNoticeDays + 1))
	require.NoError(t, svc.repo.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "thue", StartsAt: long.AddDate(0, -1, 0), EndsAt: &long}))
	usage, err := svc.TenantUsage(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, usage.Addons)
	require.Empty(t, usage.EndedAddons)
}

// The guard names an add-on right of the caller's package that ran out,
// and nothing for an agent the package never held or still holds.
func TestUsageGuardEndedAddon(t *testing.T) {
	svc, db := newAddonPlanServiceDB(t)
	ctx := context.Background()
	require.NoError(t, db.Model(&types.UsageSubscription{}).Where("id = ?", "sub-1").
		Update("ends_at", time.Now().AddDate(0, 2, 0)).Error)
	past := time.Now().Add(-time.Hour)
	require.NoError(t, svc.repo.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "thue", StartsAt: past.AddDate(0, -1, 0), EndsAt: &past}))
	future := time.Now().AddDate(0, 1, 0)
	require.NoError(t, svc.repo.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "chung", StartsAt: past, EndsAt: &future}))
	g := &usageGuard{plans: svc.repo, now: time.Now}
	// a machine principal: no seat lookup
	got, err := g.EndedAddon(ctx, 1, "", "thue")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "Thuế", got.AgentName)
	got, err = g.EndedAddon(ctx, 1, "", "chung")
	require.NoError(t, err)
	require.Nil(t, got, "still current")
	got, err = g.EndedAddon(ctx, 1, "", "other")
	require.NoError(t, err)
	require.Nil(t, got)
	// the package itself ended: its refusal is the one to explain
	require.NoError(t, db.Model(&types.UsageSubscription{}).Where("id = ?", "sub-1").
		Update("ends_at", past).Error)
	got, err = g.EndedAddon(ctx, 1, "", "thue")
	require.NoError(t, err)
	require.Nil(t, got, "the package expired with it")
}

// Review item 9: plan cards name the basic agents as the profile stores
// them and leave out a switched-off one.
func TestIncludedPlanAgentsUseTheProfileRows(t *testing.T) {
	for id, name := range map[string]string{
		types.BuiltinDocumentAssistantID: "Soạn thảo văn bản", types.BuiltinSmartReasoningID: "Suy luận thông minh",
	} {
		id, name := id, name
		if _, ok := types.BuiltinAgentRegistry[id]; ok {
			continue
		}
		types.BuiltinAgentRegistry[id] = func(tenantID uint64) *types.CustomAgent {
			return &types.CustomAgent{ID: id, Name: name, IsBuiltin: true, TenantID: tenantID}
		}
		t.Cleanup(func() { delete(types.BuiltinAgentRegistry, id) })
	}
	svc, db := newAddonPlanServiceDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE tenants (id INTEGER PRIMARY KEY, kind TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, kind) VALUES (9, 'config_profile'), (1, 'workspace')`).Error)
	ctx := context.Background()
	agents := repository.NewCustomAgentRepository(db)
	require.NoError(t, agents.CreateAgent(ctx, &types.CustomAgent{ID: types.BuiltinSmartReasoningID, Name: "Suy luận",
		TenantID: 9, IsBuiltin: true}))
	// (the document assistant has no stored row: its name comes from the
	// registry, whatever another test loaded there)
	got := includedPlanAgents(ctx, svc.repo)
	require.Len(t, got, 2)
	require.Equal(t, types.BuiltinDocumentAssistantID, got[0].ID)
	require.Equal(t, types.UsagePlanAgent{ID: types.BuiltinSmartReasoningID, Name: "Suy luận"}, got[1])

	require.NoError(t, db.Exec("UPDATE custom_agents SET enabled = ? WHERE id = ?", false, types.BuiltinSmartReasoningID).Error)
	got = includedPlanAgents(ctx, svc.repo)
	require.Len(t, got, 1)
	require.Equal(t, types.BuiltinDocumentAssistantID, got[0].ID)
}
