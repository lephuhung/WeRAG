package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// listingPlanRepo lists the active subscriptions it holds.
type listingPlanRepo struct {
	adminPlanRepo
}

func (f *listingPlanRepo) ListSubscriptions(_ context.Context, filter types.UsageSubscriptionFilter) ([]*types.UsageSubscription, error) {
	var out []*types.UsageSubscription
	for _, sub := range f.active {
		if (filter.ScopeType == "" || sub.ScopeType == filter.ScopeType) &&
			(filter.PlanID == "" || sub.PlanID == filter.PlanID) {
			out = append(out, sub)
		}
	}
	return out, nil
}

type guestUserRepo struct {
	interfaces.UserRepository
	guests []*types.User
}

func (r *guestUserRepo) ListGuestUsers(context.Context) ([]*types.User, error) { return r.guests, nil }

func (r *guestUserRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	for _, u := range r.guests {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, nil
}

// The usage page lists the guest plan of a Guest who has not asked anything
// yet (no row), and counts a guest plan against the guest counters.
func TestUsagePlanServiceListsGuestPlans(t *testing.T) {
	own := &types.UsagePlan{ID: "plan-own", Code: "ca-nhan", ScopeKind: "user", Mode: "pool",
		Limits: types.UsageLimits{{Metric: types.UsageMetricQuestions, Period: types.UsagePeriodDay, Limit: 50}}}
	plans := &listingPlanRepo{adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			"user/asked":  {ID: "sub-asked", ScopeType: "user", ScopeID: "asked", PlanID: types.UsageGuestPlanID, Plan: guestPlan(), Mode: "pool", Status: "active"},
			"user/bought": {ID: "sub-bought", ScopeType: "user", ScopeID: "bought", PlanID: own.ID, Plan: own, Mode: "pool", Status: "active"},
		}},
		plans: []*types.UsagePlan{own, guestPlan()},
	}}
	counters := &countingRepo{rows: map[string][]*types.UsageCounter{
		"guest/asked":  {{Metric: "questions", PeriodKey: "2026-10-13", Value: 3}},
		"user/asked":   {{Metric: "questions", PeriodKey: "2026-10-13", Value: 40}},
		"guest/new":    {{Metric: "questions", PeriodKey: "2026-10-13", Value: 0}},
		"user/bought":  {{Metric: "questions", PeriodKey: "2026-10-13", Value: 7}},
		"guest/bought": {{Metric: "questions", PeriodKey: "2026-10-13", Value: 1}},
	}}
	users := &guestUserRepo{guests: []*types.User{
		{ID: "asked", Username: "asked"}, {ID: "bought", Username: "bought"}, {ID: "new", Username: "new"},
	}}
	// a member's own package keeps the personal counters
	plans.active["user/member"] = &types.UsageSubscription{ID: "sub-member", ScopeType: "user", ScopeID: "member",
		PlanID: own.ID, Plan: own, Mode: "pool", Status: "active"}
	counters.rows["user/member"] = []*types.UsageCounter{{Metric: "questions", PeriodKey: "2026-10-13", Value: 5}}
	clock := func() time.Time { return guardNow }
	s := &usagePlanService{repo: plans, counters: counters, users: users, now: clock,
		guard: &usageGuard{plans: plans, counters: counters, settings: enforcingSettings{on: true}, now: clock}}

	views, err := s.ListSubscriptions(context.Background(), types.UsageSubscriptionFilter{ScopeType: "user"})
	require.NoError(t, err)
	byScope := map[string]*types.UsageSubscriptionView{}
	for _, v := range views {
		byScope[v.ScopeID] = v
	}
	require.Len(t, byScope, 4)
	require.EqualValues(t, 5, byScope["member"].Limits[0].Used)
	require.False(t, byScope["asked"].Implicit)
	require.EqualValues(t, 3, byScope["asked"].Limits[0].Used, "guest plan reads the guest counters")
	require.EqualValues(t, 1, byScope["bought"].Limits[0].Used, "a Guest's bought plan reads the guest counters, as the guard does")
	require.True(t, byScope["new"].Implicit)
	require.Empty(t, byScope["new"].ID)
	require.Equal(t, types.UsageGuestPlanID, byScope["new"].PlanID)
	require.Equal(t, "new", byScope["new"].ScopeName)

	// tenant listings and cancelled listings get no implicit rows
	views, err = s.ListSubscriptions(context.Background(), types.UsageSubscriptionFilter{ScopeType: "tenant"})
	require.NoError(t, err)
	require.Empty(t, views)
	views, err = s.ListSubscriptions(context.Background(), types.UsageSubscriptionFilter{Status: "cancelled"})
	require.NoError(t, err)
	for _, v := range views {
		require.False(t, v.Implicit)
	}

	// filtered on the guest plan: the Guest holding a bought plan is not added
	views, err = s.ListSubscriptions(context.Background(), types.UsageSubscriptionFilter{PlanID: types.UsageGuestPlanID})
	require.NoError(t, err)
	got := map[string]bool{}
	for _, v := range views {
		got[v.ScopeID] = true
	}
	require.Equal(t, map[string]bool{"asked": true, "new": true}, got)
}

// A Guest whose bought plan expired, or who kept a system-admin package,
// is under the guest plan (the guard applies it): the page shows that row
// next to the old one.
func TestUsagePlanServiceListsGuestPlanBesideInapplicablePackage(t *testing.T) {
	ended := guardNow.Add(-time.Hour)
	own := &types.UsagePlan{ID: "plan-own", Code: "ca-nhan", ScopeKind: "user", Mode: "pool"}
	admin := &types.UsagePlan{ID: "plan-admin", Code: types.UsageSystemAdminPlanCode, ScopeKind: "user", Mode: "pool"}
	plans := &listingPlanRepo{adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			"user/expired": {ID: "sub-expired", ScopeType: "user", ScopeID: "expired", PlanID: own.ID, Plan: own, Mode: "pool", Status: "active", EndsAt: &ended},
			"user/demoted": {ID: "sub-admin", ScopeType: "user", ScopeID: "demoted", PlanID: admin.ID, Plan: admin, Mode: "pool", Status: "active"},
		}},
		plans: []*types.UsagePlan{own, admin, guestPlan()},
	}}
	counters := &countingRepo{rows: map[string][]*types.UsageCounter{}}
	users := &guestUserRepo{guests: []*types.User{{ID: "expired", Username: "expired"}, {ID: "demoted", Username: "demoted"}}}
	clock := func() time.Time { return guardNow }
	s := &usagePlanService{repo: plans, counters: counters, users: users, now: clock,
		guard: &usageGuard{plans: plans, counters: counters, settings: enforcingSettings{on: true}, now: clock}}

	views, err := s.ListSubscriptions(context.Background(), types.UsageSubscriptionFilter{ScopeType: "user"})
	require.NoError(t, err)
	implicit := map[string]bool{}
	for _, v := range views {
		if v.Implicit {
			implicit[v.ScopeID] = true
		}
	}
	require.Len(t, views, 4)
	require.Equal(t, map[string]bool{"expired": true, "demoted": true}, implicit)
}
