package service

import (
	"context"
	"fmt"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// usageGuard implements interfaces.UsageGuard: the pre-flight check that
// refuses a chat turn when the caller has no current subscription or has
// used up one of its limits. The check reads the same usage_counters the
// meter writes, so a turn that reaches a cap is refused on the next one.
type usageGuard struct {
	plans    interfaces.UsagePlanRepository
	counters interfaces.UsageCounterRepository
	settings interfaces.SystemSettingService
	now      func() time.Time
}

// NewUsageGuard creates the guard.
func NewUsageGuard(
	plans interfaces.UsagePlanRepository,
	counters interfaces.UsageCounterRepository,
	settings interfaces.SystemSettingService,
) interfaces.UsageGuard {
	return &usageGuard{plans: plans, counters: counters, settings: settings, now: time.Now}
}

func (g *usageGuard) Enforced(ctx context.Context) bool {
	if g.settings == nil {
		return false
	}
	return g.settings.GetBool(ctx, types.UsageEnforceSettingKey, "", false)
}

func (g *usageGuard) CheckTurn(ctx context.Context, tenantID uint64, userID string) error {
	if !g.Enforced(ctx) || types.IsSystemAdminFromContext(ctx) {
		return nil
	}
	ent, err := g.Entitlement(ctx, tenantID, userID)
	if err != nil {
		logger.Warnf(ctx, "usage guard: entitlement lookup failed for tenant %d user %s, letting the turn through: %v",
			tenantID, userID, err)
		return nil
	}
	return usageDenial(ent, userID)
}

// usageDenial turns an entitlement into the 429 the caller gets, or nil.
func usageDenial(ent *types.UsageEntitlement, userID string) error {
	if ent == nil {
		return apperrors.NewTooManyRequestsError("no usage package covers this workspace or account").
			WithDetails(types.UsageDenial{Reason: types.UsageDenialNoSubscription})
	}
	if ent.Expired {
		return apperrors.NewTooManyRequestsError("the usage package has expired").
			WithDetails(types.UsageDenial{Reason: types.UsageDenialExpired})
	}
	realUser := userID != "" && !types.IsSyntheticUserID(userID)
	if ent.Mode == types.UsageModeSeat && realUser && !ent.HasSeat {
		return apperrors.NewTooManyRequestsError("no seat of the workspace package is assigned to this account").
			WithDetails(types.UsageDenial{Reason: types.UsageDenialNoSeat})
	}
	for _, lim := range ent.Limits {
		if lim.Exhausted {
			return apperrors.NewTooManyRequestsError(
				fmt.Sprintf("usage limit reached: %d %s per %s", lim.Limit, lim.Metric, lim.Period)).
				WithDetails(types.UsageDenial{
					Reason: types.UsageDenialLimit, Metric: lim.Metric, Period: lim.Period,
					Limit: lim.Limit, Used: lim.Used,
				})
		}
	}
	return nil
}

// Entitlement looks for the workspace's subscription first, then the
// person's own. Pool subscriptions are checked against the workspace
// counters; seat subscriptions and personal ones against the person's.
func (g *usageGuard) Entitlement(ctx context.Context, tenantID uint64, userID string) (*types.UsageEntitlement, error) {
	now := g.now()
	realUser := userID != "" && !types.IsSyntheticUserID(userID)
	if tenantID != 0 {
		sub, err := g.plans.GetActiveSubscription(ctx, types.UsageScopeTenant, types.UsageTenantScopeID(tenantID))
		if err != nil {
			return nil, err
		}
		if sub != nil {
			ent := entitlementFrom(sub, types.UsageScopeTenant, now)
			countedScope, countedID := types.UsageScopeTenant, types.UsageTenantScopeID(tenantID)
			if sub.Mode == types.UsageModeSeat {
				if realUser {
					has, err := g.plans.HasSeat(ctx, sub.ID, userID)
					if err != nil {
						return nil, err
					}
					ent.HasSeat = has
					countedScope, countedID = types.UsageScopeUser, userID
				} else {
					// Machine principals (IM, API keys) have no seat; they draw
					// on the workspace counters without a per-seat cap.
					ent.HasSeat = true
					countedScope, countedID = "", ""
				}
			}
			ent.CountedScope = countedScope
			if countedID != "" {
				if err := g.fillLimitStatus(ctx, ent, sub.EffectiveLimits(), countedScope, countedID, now); err != nil {
					return nil, err
				}
			}
			return ent, nil
		}
	}
	if realUser {
		sub, err := g.plans.GetActiveSubscription(ctx, types.UsageScopeUser, userID)
		if err != nil {
			return nil, err
		}
		if sub != nil {
			ent := entitlementFrom(sub, types.UsageScopeUser, now)
			ent.HasSeat = true
			ent.CountedScope = types.UsageScopeUser
			if err := g.fillLimitStatus(ctx, ent, sub.EffectiveLimits(), types.UsageScopeUser, userID, now); err != nil {
				return nil, err
			}
			return ent, nil
		}
	}
	return nil, nil
}

func entitlementFrom(sub *types.UsageSubscription, source string, now time.Time) *types.UsageEntitlement {
	ent := &types.UsageEntitlement{
		Source:         source,
		SubscriptionID: sub.ID,
		PlanID:         sub.PlanID,
		Mode:           sub.Mode,
		Seats:          sub.Seats,
		EndsAt:         sub.EndsAt,
		Expired:        !sub.IsCurrentAt(now),
		Limits:         []types.UsageLimitStatus{},
	}
	if sub.Plan != nil {
		ent.PlanCode = sub.Plan.Code
		ent.PlanName = sub.Plan.Name
	}
	return ent
}

// fillLimitStatus reads the counters of one scope and marks each limit
// with its consumption.
func (g *usageGuard) fillLimitStatus(
	ctx context.Context, ent *types.UsageEntitlement, limits types.UsageLimits,
	scopeType, scopeID string, now time.Time,
) error {
	statuses, err := g.limitStatus(ctx, limits, scopeType, scopeID, now)
	if err != nil {
		return err
	}
	ent.Limits = statuses
	return nil
}

// limitStatus is shared with the admin views.
func (g *usageGuard) limitStatus(
	ctx context.Context, limits types.UsageLimits, scopeType, scopeID string, now time.Time,
) ([]types.UsageLimitStatus, error) {
	out := make([]types.UsageLimitStatus, 0, len(limits))
	if len(limits) == 0 {
		return out, nil
	}
	keys := types.UsagePeriodKeysAt(now)
	rows, err := g.counters.Get(ctx, scopeType, scopeID, []string{keys.Day, keys.Month})
	if err != nil {
		return nil, err
	}
	used := make(map[[2]string]int64, len(rows))
	for _, row := range rows {
		used[[2]string{row.Metric, row.PeriodKey}] = row.Value
	}
	for _, lim := range limits {
		periodKey := keys.Day
		if lim.Period == types.UsagePeriodMonth {
			periodKey = keys.Month
		}
		st := types.UsageLimitStatus{
			Metric: lim.Metric, Period: lim.Period, PeriodKey: periodKey,
			Limit: lim.Limit, Used: used[[2]string{lim.Metric, periodKey}],
		}
		if lim.Limit > 0 {
			st.Remaining = lim.Limit - st.Used
			if st.Remaining < 0 {
				st.Remaining = 0
			}
			st.Exhausted = st.Used >= lim.Limit
		}
		out = append(out, st)
	}
	return out, nil
}
