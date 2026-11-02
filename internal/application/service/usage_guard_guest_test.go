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
	return &types.UsagePlan{ID: types.UsageGuestPlanID, Code: types.UsageGuestPlanCode, Name: "Khách", ScopeKind: "user", Mode: "pool",
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
		"guest/guest-1": {{Metric: "questions", PeriodKey: "2026-10-13", Value: 9}},
		// questions asked as a member (seat or personal package) do not count here
		"user/guest-1": {{Metric: "questions", PeriodKey: "2026-10-13", Value: 50}},
	}}
	g := guestGuard(plans, counters)

	require.NoError(t, checkTurn(g, portalCtx(), testPortalID, "guest-1"))
	sub := plans.active["user/guest-1"]
	require.NotNil(t, sub, "the guest subscription is created on the first lookup")
	require.Equal(t, types.UsageGuestPlanID, sub.PlanID)
	require.Equal(t, types.UsageScopeUser, sub.ScopeType)

	ent, err := g.Entitlement(portalCtx(), testPortalID, "guest-1")
	require.NoError(t, err)
	require.Equal(t, types.UsageGuestPlanCode, ent.PlanCode)
	require.Equal(t, types.UsageScopeUser, ent.CountedScope)

	counters.rows["guest/guest-1"][0].Value = 10
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
			"user/user-1": {ID: "sub-g", PlanID: types.UsageGuestPlanID, Mode: "pool", Status: "active", Plan: guestPlan()},
		}},
		plans: []*types.UsagePlan{guestPlan()},
	}}
	g := guestGuard(plans, &countingRepo{})
	err := checkTurn(g, context.Background(), 10003, "user-1")
	require.Equal(t, types.UsageDenialNoSubscription, denialReason(t, err))
}

// A plan someone else coded "guest" is never taken for the guest plan: the
// guard finds it by the seeded id only.
func TestUsageGuardGuestPlanFoundByIDNotCode(t *testing.T) {
	other := &types.UsagePlan{ID: "plan-other", Code: types.UsageGuestPlanCode, ScopeKind: "user", Mode: "pool"}
	seeded := guestPlan()
	seeded.Code = "guest-chat"
	plans := &rolePlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{}},
		plans:             []*types.UsagePlan{other, seeded},
	}}
	g := guestGuard(plans, &countingRepo{})
	require.NoError(t, checkTurn(g, portalCtx(), testPortalID, "guest-1"))
	require.Equal(t, types.UsageGuestPlanID, plans.active["user/guest-1"].PlanID)

	// a personal subscription to the other "guest" plan is an ordinary package
	plans.active["user/user-2"] = &types.UsageSubscription{ID: "sub-o", PlanID: "plan-other", Mode: "pool",
		Status: "active", Plan: other}
	ent, err := g.Entitlement(context.Background(), 10003, "user-2")
	require.NoError(t, err)
	require.Equal(t, "plan-other", ent.PlanID)
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
			"user/admin-1": {ID: "sub-g", ScopeType: "user", ScopeID: "admin-1", PlanID: types.UsageGuestPlanID, Mode: "pool",
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

// A Guest whose bought package expired falls back to the guest plan instead
// of being stuck on "expired"; the bought subscription is left for renewal.
func TestUsageGuardGuestExpiredPackageFallsBackToGuestPlan(t *testing.T) {
	ended := guardNow.Add(-time.Hour)
	own := &types.UsagePlan{ID: "plan-own", Code: "ca-nhan", ScopeKind: "user", Mode: "pool"}
	plans := &rolePlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			"user/guest-1": {ID: "sub-own", PlanID: "plan-own", Mode: "pool", Status: "active", EndsAt: &ended, Plan: own},
		}},
		plans: []*types.UsagePlan{own, guestPlan()},
	}}
	counters := &countingRepo{rows: map[string][]*types.UsageCounter{
		"guest/guest-1": {{Metric: "questions", PeriodKey: "2026-10-13", Value: 3}},
	}}
	g := guestGuard(plans, counters)
	ent, err := g.Entitlement(portalCtx(), testPortalID, "guest-1")
	require.NoError(t, err)
	require.False(t, ent.Expired)
	require.Equal(t, types.UsageGuestPlanCode, ent.PlanCode)
	require.Len(t, ent.Limits, 1)
	require.Equal(t, int64(3), ent.Limits[0].Used)
	require.NoError(t, checkTurn(g, portalCtx(), testPortalID, "guest-1"))
	require.Equal(t, "sub-own", plans.active["user/guest-1"].ID, "the bought subscription is kept")
	require.Empty(t, plans.closed)

	// outside the portal the expired package still reads as expired
	err = checkTurn(g, context.Background(), 10003, "guest-1")
	require.Equal(t, types.UsageDenialExpired, denialReason(t, err))
}

// Portal turns are metered under the guest scope, never the user scope a
// seat or personal package reads; the portal's shared counters stay out of
// the Guest's own summary.
func TestUsageMeterPortalCountsGuestScope(t *testing.T) {
	scopes := usageScopes(portalCtx(), testPortalID, "guest-1")
	require.Equal(t, [][2]string{{types.UsageScopeTenant, types.UsageTenantScopeID(testPortalID)},
		{types.UsageScopeGuest, "guest-1"}}, scopes)
	require.Equal(t, [][2]string{{types.UsageScopeTenant, types.UsageTenantScopeID(10003)},
		{types.UsageScopeUser, "user-1"}}, usageScopes(context.Background(), 10003, "user-1"))

	repo := &countingRepo{rows: map[string][]*types.UsageCounter{
		"guest/guest-1": {{Metric: "questions", PeriodKey: "2026-10-13", Value: 4}},
		"tenant/" + types.UsageTenantScopeID(testPortalID): {{Metric: "questions", PeriodKey: "2026-10-13", Value: 900}},
	}}
	m := &usageMeter{repo: repo, now: func() time.Time { return guardNow }}
	sum, err := m.Summary(portalCtx(), testPortalID, "guest-1")
	require.NoError(t, err)
	require.Nil(t, sum.Tenant)
	require.NotNil(t, sum.User)
}
