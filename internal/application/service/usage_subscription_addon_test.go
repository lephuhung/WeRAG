package service

import (
	"context"
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
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.CustomAgent{}, &types.UsagePlan{}, &types.UsageSubscription{},
		&types.UsageSubscriptionAddon{}))
	ctx := context.Background()
	require.NoError(t, db.Create(&types.UsagePlan{ID: "p", Code: "go", Name: "Go", IsActive: true}).Error)
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
	return &usagePlanService{repo: repository.NewUsagePlanRepository(db), now: time.Now}
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
	past := time.Now().Add(-time.Hour)
	_, err = svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "thue", EndsAt: &past}, "")
	require.Equal(t, 400, appErrCode(t, err))
	_, err = svc.GrantSubscriptionAddon(ctx, "sub-1", types.UsageSubscriptionAddonGrant{AgentID: "thue", PriceVND: -1}, "")
	require.Equal(t, 400, appErrCode(t, err))

	require.NoError(t, svc.RevokeSubscriptionAddon(ctx, "sub-1", "thue"))
	require.Equal(t, 404, appErrCode(t, svc.RevokeSubscriptionAddon(ctx, "sub-1", "thue")))
	_, err = svc.ListSubscriptionAddons(ctx, "missing")
	require.Equal(t, 404, appErrCode(t, err))
}
