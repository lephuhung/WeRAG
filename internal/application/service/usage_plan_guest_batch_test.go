package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

type listCountingPlanRepo struct {
	*listingPlanRepo
	lists int
}

func (r *listCountingPlanRepo) ListSubscriptions(ctx context.Context, f types.UsageSubscriptionFilter) ([]*types.UsageSubscription, error) {
	r.lists++
	return r.listingPlanRepo.ListSubscriptions(ctx, f)
}

type getRecordingCounters struct {
	*countingRepo
	gets []string
}

func (c *getRecordingCounters) Get(ctx context.Context, scopeType, scopeID string, keys []string) ([]*types.UsageCounter, error) {
	c.gets = append(c.gets, scopeType+"/"+scopeID)
	return c.countingRepo.Get(ctx, scopeType, scopeID, keys)
}

// The usage page's personal listing reads the counters of every Guest
// without a package in one GetMany, never one Get per Guest, and a scope
// filter alone does not list the subscriptions a second time.
func TestUsagePlanServiceGuestViewsReadCountersOnce(t *testing.T) {
	plans := &listCountingPlanRepo{listingPlanRepo: &listingPlanRepo{adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{}},
		plans:             []*types.UsagePlan{guestPlan()},
	}}}
	day := types.UsagePeriodKeysAt(guardNow).Day
	counters := &getRecordingCounters{countingRepo: &countingRepo{rows: map[string][]*types.UsageCounter{
		"guest/g1": {{Metric: types.UsageMetricQuestions, PeriodKey: day, Value: 4}},
		"guest/g3": {{Metric: types.UsageMetricQuestions, PeriodKey: day, Value: 9}},
	}}}
	users := &guestUserRepo{guests: []*types.User{{ID: "g1", Username: "g1"}, {ID: "g2", Email: "g2@x"}, {ID: "g3", Username: "g3"}}}
	clock := func() time.Time { return guardNow }
	s := &usagePlanService{repo: plans, counters: counters, users: users, now: clock,
		guard: &usageGuard{plans: plans, counters: counters, settings: enforcingSettings{on: true}, now: clock}}

	views, err := s.ListSubscriptions(context.Background(), types.UsageSubscriptionFilter{ScopeType: types.UsageScopeUser})
	require.NoError(t, err)
	used := map[string]int64{}
	for _, v := range views {
		require.True(t, v.Implicit)
		used[v.ScopeID] = v.Limits[0].Used
	}
	require.Equal(t, map[string]int64{"g1": 4, "g2": 0, "g3": 9}, used)
	require.Equal(t, 1, counters.getMany)
	require.Empty(t, counters.gets, "no per-Guest counters read")
	require.Equal(t, 1, plans.lists, "a scope filter alone lists the subscriptions once")

	// the guest-plan filter still asks for every active personal row
	_, err = s.ListSubscriptions(context.Background(), types.UsageSubscriptionFilter{PlanID: types.UsageGuestPlanID})
	require.NoError(t, err)
	require.Equal(t, 3, plans.lists)
}
