package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func signupPlanService(plans []*types.UsagePlan, settings billingSettings) (*usagePlanService, *replacingPlanRepo) {
	repo := &replacingPlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{}},
		plans:             plans,
	}}
	return &usagePlanService{repo: repo, counters: &countingRepo{}, settings: settings,
		now: func() time.Time { return guardNow }}, repo
}

// A new account gets the personal plan SuperAdmin chose, for the set days
// (ending at the end of that Vietnamese day).
func TestGrantSignupPlanSubscribesNewAccount(t *testing.T) {
	trial := &types.UsagePlan{ID: "plan-trial", Code: "dung-thu", ScopeKind: "user", Mode: "pool", IsActive: true}
	s, repo := signupPlanService([]*types.UsagePlan{trial}, billingSettings{
		strings: map[string]string{types.UsageSignupPlanSettingKey: " plan-trial "},
		ints:    map[string]int64{types.UsageSignupPlanDaysSettingKey: 14},
	})
	view, err := s.GrantSignupPlan(context.Background(), "user-new")
	require.NoError(t, err)
	require.NotNil(t, view)
	sub := repo.active["user/user-new"]
	require.NotNil(t, sub)
	require.Equal(t, "plan-trial", sub.PlanID)
	require.Equal(t, types.UsageSubscriptionActive, sub.Status)
	require.NotNil(t, sub.EndsAt)
	// guardNow = 2026-10-13 10:00 VN; +14 days → end of 2026-10-27 VN
	require.Equal(t, time.Date(2026, 10, 27, 17, 0, 0, 0, time.UTC), sub.EndsAt.UTC())

	// 0 days = no end date
	s, repo = signupPlanService([]*types.UsagePlan{trial}, billingSettings{
		strings: map[string]string{types.UsageSignupPlanSettingKey: "plan-trial"},
		ints:    map[string]int64{types.UsageSignupPlanDaysSettingKey: 0},
	})
	_, err = s.GrantSignupPlan(context.Background(), "user-2")
	require.NoError(t, err)
	require.Nil(t, repo.active["user/user-2"].EndsAt)

	// a negative value is the default, never "no end date"
	s, repo = signupPlanService([]*types.UsagePlan{trial}, billingSettings{
		strings: map[string]string{types.UsageSignupPlanSettingKey: "plan-trial"},
		ints:    map[string]int64{types.UsageSignupPlanDaysSettingKey: -1},
	})
	_, err = s.GrantSignupPlan(context.Background(), "user-3")
	require.NoError(t, err)
	require.NotNil(t, repo.active["user/user-3"].EndsAt)
	require.Equal(t, time.Date(2026, 11, 12, 17, 0, 0, 0, time.UTC), repo.active["user/user-3"].EndsAt.UTC())
}

// No setting, a missing/inactive/organization plan, or an account that
// already holds a package: nothing is granted and registration goes on.
func TestGrantSignupPlanSkips(t *testing.T) {
	off := &types.UsagePlan{ID: "plan-off", Code: "off", ScopeKind: "user", Mode: "pool", IsActive: false}
	org := &types.UsagePlan{ID: "plan-org", Code: "org", ScopeKind: "tenant", Mode: "pool", IsActive: true}
	trial := &types.UsagePlan{ID: "plan-trial", Code: "trial", ScopeKind: "user", Mode: "pool", IsActive: true}
	plans := []*types.UsagePlan{off, org, trial}
	for _, id := range []string{"", "plan-missing", "plan-off", "plan-org"} {
		s, repo := signupPlanService(plans, billingSettings{strings: map[string]string{types.UsageSignupPlanSettingKey: id}})
		view, err := s.GrantSignupPlan(context.Background(), "user-x")
		require.NoError(t, err, id)
		require.Nil(t, view, id)
		require.Empty(t, repo.active, id)
	}

	s, repo := signupPlanService(plans, billingSettings{strings: map[string]string{types.UsageSignupPlanSettingKey: "plan-trial"}})
	held := &types.UsageSubscription{ID: "sub-bought", ScopeType: "user", ScopeID: "user-x", PlanID: "plan-org", Status: "active"}
	repo.active["user/user-x"] = held
	view, err := s.GrantSignupPlan(context.Background(), "user-x")
	require.NoError(t, err)
	require.Nil(t, view)
	require.Same(t, held, repo.active["user/user-x"])
}
