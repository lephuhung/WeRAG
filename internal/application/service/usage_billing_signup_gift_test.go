package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// SignupGift follows GrantSignupPlan's rules, so the pricing page never
// promises a gift sign-up would not give.
func TestSignupGiftFollowsGrantRules(t *testing.T) {
	ctx := context.Background()
	f := newBillingFixture(t)
	settings := f.svc.settings.(billingSettings)
	f.plans.planByID["welcome"] = &types.UsagePlan{
		ID: "welcome", Name: "Gói chào mừng", ScopeKind: types.UsagePlanScopeUser, IsActive: true,
		Limits:           types.UsageLimits{{Metric: "tokens", Period: "month", Limit: 1_500_000}},
		IncludedAgentIDs: types.UsageAgentIDList{"deep"},
	}
	f.plans.extraAgents = []types.UsagePlanAgent{{ID: "deep", Name: "Suy luận nâng cao"}}

	// none set
	gift, err := f.svc.SignupGift(ctx)
	require.NoError(t, err)
	require.Nil(t, gift)

	settings.strings[types.UsageSignupPlanSettingKey] = "welcome"
	gift, err = f.svc.SignupGift(ctx)
	require.NoError(t, err)
	require.NotNil(t, gift)
	require.Equal(t, "Gói chào mừng", gift.Name)
	require.Equal(t, int64(types.UsageSignupPlanDaysDefault), gift.Days)
	require.Equal(t, int64(1_500_000), gift.Limits[0].Limit)
	last := gift.IncludedAgents[len(gift.IncludedAgents)-1]
	require.Equal(t, "Suy luận nâng cao", last.Name)
	require.True(t, last.Included)

	// a negative day count falls back like GrantSignupPlan; 0 = no end
	settings.ints[types.UsageSignupPlanDaysSettingKey] = -5
	gift, _ = f.svc.SignupGift(ctx)
	require.Equal(t, int64(types.UsageSignupPlanDaysDefault), gift.Days)
	settings.ints[types.UsageSignupPlanDaysSettingKey] = 0
	gift, _ = f.svc.SignupGift(ctx)
	require.Equal(t, int64(0), gift.Days)

	// a Tổ chức plan, an inactive plan or a missing plan is no gift
	settings.strings[types.UsageSignupPlanSettingKey] = "pool"
	gift, err = f.svc.SignupGift(ctx)
	require.NoError(t, err)
	require.Nil(t, gift)
	f.plans.planByID["welcome"].IsActive = false
	settings.strings[types.UsageSignupPlanSettingKey] = "welcome"
	gift, _ = f.svc.SignupGift(ctx)
	require.Nil(t, gift)
	settings.strings[types.UsageSignupPlanSettingKey] = "gone"
	gift, err = f.svc.SignupGift(ctx)
	require.NoError(t, err)
	require.Nil(t, gift)
}
