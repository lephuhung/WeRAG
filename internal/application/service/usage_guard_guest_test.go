package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

const testPortalID uint64 = 10050

// rolePlanRepo is adminPlanRepo that also records closed subscriptions.
type rolePlanRepo struct {
	adminPlanRepo
	closed []string
}

func (f *rolePlanRepo) UpdateSubscription(_ context.Context, sub *types.UsageSubscription) error {
	if sub.Status != types.UsageSubscriptionActive {
		delete(f.active, sub.ScopeType+"/"+sub.ScopeID)
		f.closed = append(f.closed, sub.ID)
	}
	return nil
}

func guestPlan() *types.UsagePlan {
	return &types.UsagePlan{ID: "plan-guest", Code: types.UsageGuestPlanCode, Name: "Khách", ScopeKind: "user", Mode: "pool",
		Limits: types.UsageLimits{{Metric: types.UsageMetricQuestions, Period: types.UsagePeriodDay, Limit: 10}}}
}

func portalCtx() context.Context {
	return context.WithValue(context.Background(), types.TenantInfoContextKey,
		&types.Tenant{ID: testPortalID, Kind: types.TenantKindPublicPortal})
}

func guestGuard(plans *rolePlanRepo, counters *countingRepo) *usageGuard {
	return &usageGuard{plans: plans, counters: counters, settings: enforcingSettings{on: true},
		now: func() time.Time { return guardNow }}
}

// A Guest in the portal gets the guest package on the first turn, counted
// against their own counters; the portal's own subscription is never read.
func TestUsageGuardGuestGetsGuestPackageInPortal(t *testing.T) {
	plans := &rolePlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			// a stray portal subscription must not apply
			"tenant/10050": {ID: "sub-portal", Mode: "pool", Status: "active", Plan: poolPlan()},
		}},
		plans: []*types.UsagePlan{poolPlan(), guestPlan()},
	}}
	counters := &countingRepo{rows: map[string][]*types.UsageCounter{
		"user/guest-1": {{Metric: "questions", PeriodKey: "2026-10-13", Value: 9}},
	}}
	g := guestGuard(plans, counters)

	require.NoError(t, checkTurn(g, portalCtx(), testPortalID, "guest-1"))
	sub := plans.active["user/guest-1"]
	require.NotNil(t, sub, "the guest subscription is created on the first lookup")
	require.Equal(t, "plan-guest", sub.PlanID)
	require.Equal(t, types.UsageScopeUser, sub.ScopeType)

	ent, err := g.Entitlement(portalCtx(), testPortalID, "guest-1")
	require.NoError(t, err)
	require.Equal(t, types.UsageGuestPlanCode, ent.PlanCode)
	require.Equal(t, types.UsageScopeUser, ent.CountedScope)

	counters.rows["user/guest-1"][0].Value = 10
	err = checkTurn(g, portalCtx(), testPortalID, "guest-1")
	require.Equal(t, types.UsageDenialLimit, denialReason(t, err))
}

// A package the Guest bought wins over the guest plan.
func TestUsageGuardGuestKeepsOwnPackage(t *testing.T) {
	own := &types.UsagePlan{ID: "plan-own", Code: "ca-nhan", ScopeKind: "user", Mode: "pool"}
	plans := &rolePlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			"user/guest-1": {ID: "sub-own", PlanID: "plan-own", Mode: "pool", Status: "active", Plan: own},
		}},
		plans: []*types.UsagePlan{own, guestPlan()},
	}}
	g := guestGuard(plans, &countingRepo{})
	ent, err := g.Entitlement(portalCtx(), testPortalID, "guest-1")
	require.NoError(t, err)
	require.Equal(t, "ca-nhan", ent.PlanCode)
	require.Empty(t, plans.closed)
}

// Outside the portal a guest-plan subscription counts for nothing: a Guest
// who joined a Tổ chức without a package is refused, not given 10 a day.
func TestUsageGuardGuestPackageIgnoredOutsidePortal(t *testing.T) {
	plans := &rolePlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			"user/user-1": {ID: "sub-g", PlanID: "plan-guest", Mode: "pool", Status: "active", Plan: guestPlan()},
		}},
		plans: []*types.UsagePlan{guestPlan()},
	}}
	g := guestGuard(plans, &countingRepo{})
	err := checkTurn(g, context.Background(), 10003, "user-1")
	require.Equal(t, types.UsageDenialNoSubscription, denialReason(t, err))
}

// Without the guest plan (deleted by a SuperAdmin) a Guest is refused.
func TestUsageGuardGuestWithoutPlanRefused(t *testing.T) {
	plans := &rolePlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{}},
	}}
	g := guestGuard(plans, &countingRepo{})
	err := checkTurn(g, portalCtx(), testPortalID, "guest-1")
	require.Equal(t, types.UsageDenialNoSubscription, denialReason(t, err))
}

// A former Guest promoted to SuperAdmin swaps the guest package for the
// system-admin one instead of keeping 10 questions a day.
func TestUsageGuardSystemAdminReplacesGuestPackage(t *testing.T) {
	adminPlan := &types.UsagePlan{ID: "plan-sa", Code: types.UsageSystemAdminPlanCode, ScopeKind: "user", Mode: "pool"}
	plans := &rolePlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			"user/admin-1": {ID: "sub-g", ScopeType: "user", ScopeID: "admin-1", PlanID: "plan-guest", Mode: "pool",
				Status: "active", Plan: guestPlan()},
		}},
		plans: []*types.UsagePlan{guestPlan(), adminPlan},
	}}
	g := guestGuard(plans, &countingRepo{})
	ctx := context.WithValue(context.Background(), types.SystemAdminContextKey, true)
	ent, err := g.Entitlement(ctx, 10003, "admin-1")
	require.NoError(t, err)
	require.Equal(t, types.UsageSystemAdminPlanCode, ent.PlanCode)
	require.Equal(t, []string{"sub-g"}, plans.closed)
}
