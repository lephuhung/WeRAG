package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// replacingPlanRepo records how CreateSubscription closes the previous row.
type replacingPlanRepo struct {
	adminPlanRepo
	updated []*types.UsageSubscription
	deleted []string
}

func (f *replacingPlanRepo) GetPlan(_ context.Context, id string) (*types.UsagePlan, error) {
	for _, p := range f.plans {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}

func (f *replacingPlanRepo) UpdateSubscription(_ context.Context, sub *types.UsageSubscription) error {
	f.updated = append(f.updated, sub)
	return nil
}

func (f *replacingPlanRepo) DeleteSubscription(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

// Moving off the guest plan leaves no "cancelled" guest row in the person's
// history; moving off a bought package still closes it as cancelled.
func TestCreateSubscriptionRemovesTheReplacedGuestPlan(t *testing.T) {
	own := &types.UsagePlan{ID: "plan-own", Code: "ca-nhan", ScopeKind: "user", Mode: "pool"}
	other := &types.UsagePlan{ID: "plan-other", Code: "nang-cao", ScopeKind: "user", Mode: "pool"}
	repo := &replacingPlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			"user/guest-1": {ID: "sub-guest", ScopeType: "user", ScopeID: "guest-1", PlanID: types.UsageGuestPlanID, Plan: guestPlan(), Mode: "pool", Status: "active"},
		}},
		plans: []*types.UsagePlan{own, other, guestPlan()},
	}}
	s := &usagePlanService{repo: repo, counters: &countingRepo{}, now: func() time.Time { return guardNow }}

	_, err := s.CreateSubscription(context.Background(), &types.UsageSubscription{
		ScopeType: "user", ScopeID: "guest-1", PlanID: own.ID, Status: types.UsageSubscriptionActive})
	require.NoError(t, err)
	require.Equal(t, []string{"sub-guest"}, repo.deleted)
	require.Empty(t, repo.updated)

	_, err = s.CreateSubscription(context.Background(), &types.UsageSubscription{
		ScopeType: "user", ScopeID: "guest-1", PlanID: other.ID, Status: types.UsageSubscriptionActive})
	require.NoError(t, err)
	require.Equal(t, []string{"sub-guest"}, repo.deleted)
	require.Len(t, repo.updated, 1)
	require.Equal(t, types.UsageSubscriptionCancelled, repo.updated[0].Status)
	require.Equal(t, own.ID, repo.updated[0].PlanID)
}
