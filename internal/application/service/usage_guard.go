package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

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

// Enforced reports usage.enforce, ON unless a SuperAdmin saved false (the
// switch exists for testing only). A guard without a settings service is
// only built by unit tests, which expect no enforcement; production wiring
// (container, NewUsagePlanService) always passes one.
func (g *usageGuard) Enforced(ctx context.Context) bool {
	if g.settings == nil {
		return false
	}
	return g.settings.GetBool(ctx, types.UsageEnforceSettingKey, "", types.UsageEnforceDefault)
}

func (g *usageGuard) CheckTurn(
	ctx context.Context, tenantID uint64, userID, sessionID string, capMode types.SessionCapMode,
) (*types.UsageTurnGrant, error) {
	// No enforcement, no caps: the plan features ride on the same switch as
	// the limits, so turning usage.enforce off restores the old behaviour.
	if !g.Enforced(ctx) || types.IsSystemAdminFromContext(ctx) {
		return &types.UsageTurnGrant{}, nil
	}
	ent, err := g.Entitlement(ctx, tenantID, userID)
	if err != nil {
		logger.Warnf(ctx, "usage guard: entitlement lookup failed for tenant %d user %s, letting the turn through: %v",
			tenantID, userID, err)
		return &types.UsageTurnGrant{}, nil
	}
	if err := usageDenial(ent, userID); err != nil {
		return nil, err
	}
	grant := &types.UsageTurnGrant{Features: ent.Features}
	if sessionID == "" {
		return grant, nil
	}
	session, err := g.sessionStatus(ctx, ent.Features, sessionID)
	if err != nil {
		logger.Warnf(ctx, "usage guard: session %s counters unavailable, letting the turn through: %v", sessionID, err)
		return grant, nil
	}
	if session != nil && session.Reached {
		if capMode == types.SessionCapSoft {
			session.Soft = true
		} else {
			metric, limit, used := session.ExhaustedMetric()
			return nil, apperrors.NewTooManyRequestsError(
				fmt.Sprintf("session limit reached: %d %s per session", limit, metric)).
				WithDetails(types.UsageDenial{
					Reason: types.UsageDenialSessionLimit, Metric: metric, Period: types.UsagePeriodSession,
					Limit: limit, Used: used, SessionID: sessionID,
				})
		}
	}
	grant.Session = session
	return grant, nil
}

// PreviewSession mirrors CheckTurn's plan resolution without its effects:
// a SuperAdmin or enforcement off applies no cap (no package lookup, which
// for a SuperAdmin could create a subscription), an exhausted or missing
// package is reported rather than refused, and the session counters are
// read even when the plan sets no per-session cap.
func (g *usageGuard) PreviewSession(
	ctx context.Context, tenantID uint64, userID, sessionID string, capMode types.SessionCapMode,
) (*types.UsageSessionPreview, error) {
	preview := &types.UsageSessionPreview{Session: types.UsageSessionStatus{SessionID: sessionID}}
	if g.Enforced(ctx) && !types.IsSystemAdminFromContext(ctx) {
		ent, err := g.Entitlement(ctx, tenantID, userID)
		if err != nil {
			return nil, err
		}
		preview.Enforced = true
		if ent != nil {
			preview.PlanName = ent.PlanName
			preview.Features = ent.Features
		}
		// what CheckTurn would refuse, reported instead of raised
		if err := usageDenial(ent, userID); err != nil {
			preview.Denied = true
			if appErr, ok := err.(*apperrors.AppError); ok {
				if d, ok := appErr.Details.(types.UsageDenial); ok {
					preview.DenialReason = d.Reason
				}
			}
		}
	}
	if sessionID == "" {
		return preview, nil
	}
	turns, tokens, err := g.sessionCounters(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if st := types.NewUsageSessionStatus(preview.Features, sessionID, turns, tokens); st != nil {
		st.Soft = st.Reached && capMode == types.SessionCapSoft
		preview.Session = *st
	} else {
		preview.Session.Turns, preview.Session.Tokens = turns, tokens
	}
	return preview, nil
}

// sessionCounters reads a session's lifetime question and token counters.
func (g *usageGuard) sessionCounters(ctx context.Context, sessionID string) (turns, tokens int64, err error) {
	rows, err := g.counters.Get(ctx, types.UsageScopeSession, sessionID, []string{types.UsagePeriodAll})
	if err != nil {
		return 0, 0, err
	}
	for _, row := range rows {
		switch row.Metric {
		case types.UsageMetricQuestions:
			turns = row.Value
		case types.UsageMetricTokens:
			tokens = row.Value
		}
	}
	return turns, tokens, nil
}

// sessionStatus reads a session's lifetime counters against the plan's
// per-session caps; nil when the plan sets none.
func (g *usageGuard) sessionStatus(
	ctx context.Context, features types.UsagePlanFeatures, sessionID string,
) (*types.UsageSessionStatus, error) {
	if features.SessionMaxTurns <= 0 && features.SessionMaxTokens <= 0 {
		return nil, nil
	}
	turns, tokens, err := g.sessionCounters(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return types.NewUsageSessionStatus(features, sessionID, turns, tokens), nil
}

// usageDenial turns an entitlement into the 429 the caller gets, or nil.
func usageDenial(ent *types.UsageEntitlement, userID string) error {
	if ent == nil {
		return apperrors.NewTooManyRequestsError("no usage package covers this organization or account").
			WithDetails(types.UsageDenial{Reason: types.UsageDenialNoSubscription})
	}
	if ent.Expired {
		return apperrors.NewTooManyRequestsError("the usage package has expired").
			WithDetails(types.UsageDenial{Reason: types.UsageDenialExpired})
	}
	realUser := userID != "" && !types.IsSyntheticUserID(userID)
	if ent.Mode == types.UsageModeSeat && realUser && !ent.HasSeat {
		return apperrors.NewTooManyRequestsError("no seat of the organization package is assigned to this account").
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
//
// A SuperAdmin is governed by their own package (the system-admin plan)
// whatever workspace they are in; one promoted after migration 000140 gets
// that subscription on the first lookup.
//
// A Guest in the public portal is governed by their own package alone: the
// portal holds no subscription, and one without a package of their own gets
// a subscription to the guest plan on the first lookup. Outside the portal
// a guest-plan subscription counts for nothing (a Guest who joined a Tổ chức
// draws on its package).
func (g *usageGuard) Entitlement(ctx context.Context, tenantID uint64, userID string) (*types.UsageEntitlement, error) {
	now := g.now()
	realUser := userID != "" && !types.IsSyntheticUserID(userID)
	systemAdmin := realUser && types.IsSystemAdminFromContext(ctx)
	if systemAdmin {
		ent, err := g.personalPlanEntitlement(ctx, userID, types.UsageSystemAdminPlanCode, "SuperAdmin", now)
		if err != nil || ent != nil {
			return ent, err
		}
	}
	portal := portalTenant(ctx, tenantID)
	if portal {
		if !realUser {
			return nil, nil
		}
		return g.personalPlanEntitlement(ctx, userID, types.UsageGuestPlanCode, "Khách", now)
	}
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
		// a SuperAdmin package outlives a revoked SuperAdmin role, and a
		// guest package the account's Guest days: ignore both
		role := rolePlan(subPlan(sub))
		if sub != nil && !(role == types.UsageSystemAdminPlanCode && !systemAdmin) && role != types.UsageGuestPlanCode {
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

// portalTenant reports whether tenantID is the public portal the request
// runs in (types.InPublicPortal for the same tenant).
func portalTenant(ctx context.Context, tenantID uint64) bool {
	t, ok := types.TenantInfoFromContext(ctx)
	return ok && tenantID != 0 && t.ID == tenantID && t.IsPublicPortal()
}

// personalPlanEntitlement is a person's own package, creating the
// subscription to the plan named by planCode when they have none: the
// system-admin plan for a SuperAdmin, the guest plan for a Guest. Nil when
// the plan was deleted: a SuperAdmin then falls back to the ordinary lookup,
// a Guest is refused (no package).
//
// A package the person already holds wins (one they bought), except the
// other role package — a system-admin one kept after losing the SuperAdmin
// role, a guest one kept after becoming SuperAdmin — which is closed and
// replaced (a scope holds one active subscription).
func (g *usageGuard) personalPlanEntitlement(
	ctx context.Context, userID, planCode, note string, now time.Time,
) (*types.UsageEntitlement, error) {
	sub, err := g.plans.GetActiveSubscription(ctx, types.UsageScopeUser, userID)
	if err != nil {
		return nil, err
	}
	if role := rolePlan(subPlan(sub)); role != "" && role != planCode {
		sub.Status = types.UsageSubscriptionCancelled
		if err := g.plans.UpdateSubscription(ctx, sub); err != nil {
			return nil, err
		}
		logger.Infof(ctx, "usage guard: %s package of %s closed for %s", role, userID, planCode)
		sub = nil
	}
	if sub == nil {
		plan, err := g.planByCode(ctx, planCode)
		if err != nil || plan == nil {
			return nil, err
		}
		created := &types.UsageSubscription{
			ID:        uuid.New().String(),
			ScopeType: types.UsageScopeUser,
			ScopeID:   userID,
			PlanID:    plan.ID,
			Mode:      types.UsageModePool,
			StartsAt:  now,
			Status:    types.UsageSubscriptionActive,
			Note:      note,
		}
		if err := g.plans.CreateSubscription(ctx, created); err != nil {
			// a concurrent lookup may have created it first
			logger.Warnf(ctx, "usage guard: %s subscription for %s: %v", planCode, userID, err)
		}
		if sub, err = g.plans.GetActiveSubscription(ctx, types.UsageScopeUser, userID); err != nil || sub == nil {
			return nil, err
		}
	}
	// A Guest whose bought package ran out still has the guest plan: its
	// expired subscription stays the scope's one active row (billing renews
	// it), so the guest plan applies without a subscription of its own.
	if planCode == types.UsageGuestPlanCode && !sub.IsCurrentAt(now) &&
		rolePlan(sub.Plan) != types.UsageGuestPlanCode {
		plan, err := g.planByCode(ctx, planCode)
		if err != nil {
			return nil, err
		}
		if plan != nil {
			sub = &types.UsageSubscription{
				ScopeType: types.UsageScopeUser, ScopeID: userID, PlanID: plan.ID, Plan: plan,
				Mode: types.UsageModePool, StartsAt: now, Status: types.UsageSubscriptionActive,
			}
		}
	}
	ent := entitlementFrom(sub, types.UsageScopeUser, now)
	ent.HasSeat = true
	ent.CountedScope = types.UsageScopeUser
	// in the public portal the person's turns are counted apart (guest scope)
	counterScope := types.UsageScopeUser
	if planCode == types.UsageGuestPlanCode {
		counterScope = types.UsageScopeGuest
	}
	if err := g.fillLimitStatus(ctx, ent, sub.EffectiveLimits(), counterScope, userID, now); err != nil {
		return nil, err
	}
	return ent, nil
}

// rolePlan names the role package p is — the packages that follow an
// account's kind rather than a purchase: UsageSystemAdminPlanCode
// (SuperAdmin, found by code) or UsageGuestPlanCode (Guest, found by the
// fixed id migration 000172 seeds, so a plan someone else named "guest" is
// never taken for it). "" for any other plan.
func rolePlan(p *types.UsagePlan) string {
	switch {
	case p == nil:
		return ""
	case p.ID == types.UsageGuestPlanID:
		return types.UsageGuestPlanCode
	case p.Code == types.UsageSystemAdminPlanCode:
		return types.UsageSystemAdminPlanCode
	}
	return ""
}

// subPlan is sub's plan, nil for no subscription.
func subPlan(sub *types.UsageSubscription) *types.UsagePlan {
	if sub == nil {
		return nil
	}
	return sub.Plan
}

// planByCode finds a role package (see rolePlan).
func (g *usageGuard) planByCode(ctx context.Context, code string) (*types.UsagePlan, error) {
	plans, err := g.plans.ListPlans(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		if rolePlan(p) == code {
			return p, nil
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
		// normalizePlan validates features on write; a row edited by hand
		// that no longer parses applies no cap rather than refusing chat.
		ent.Features, _ = types.ParseUsagePlanFeatures(sub.Plan.Features)
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
