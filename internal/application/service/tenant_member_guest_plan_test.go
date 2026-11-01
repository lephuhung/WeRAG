package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// Joining an organization (an added member, an accepted invitation, a
// bootstrapped admin) removes the person's guest plan: outside the portal
// it counts for nothing and the usage page showed it frozen. A bought
// package stays.
func TestNewMembershipDropsTheGuestPlan(t *testing.T) {
	own := &types.UsagePlan{ID: "plan-own", Code: "ca-nhan", ScopeKind: "user", Mode: "pool"}
	plans := &replacingPlanRepo{adminPlanRepo: adminPlanRepo{
		fakeUsagePlanRepo: fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
			"user/guest-1": {ID: "sub-guest-1", ScopeType: "user", ScopeID: "guest-1", PlanID: types.UsageGuestPlanID, Plan: guestPlan(), Mode: "pool", Status: "active"},
			"user/guest-2": {ID: "sub-guest-2", ScopeType: "user", ScopeID: "guest-2", PlanID: types.UsageGuestPlanID, Plan: guestPlan(), Mode: "pool", Status: "active"},
			"user/buyer":   {ID: "sub-buyer", ScopeType: "user", ScopeID: "buyer", PlanID: own.ID, Plan: own, Mode: "pool", Status: "active"},
		}},
	}}
	svc := NewTenantMemberService(newFakeRepo(), nil, nil, nil, plans)
	ctx := context.Background()

	_, err := svc.AddMember(ctx, "guest-1", 7, types.TenantRoleMember, nil)
	require.NoError(t, err)
	_, err = svc.EnsureAdmin(ctx, "guest-2", 8)
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, "buyer", 7, types.TenantRoleMember, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"sub-guest-1", "sub-guest-2"}, plans.deleted)
	require.Empty(t, plans.updated)
}
