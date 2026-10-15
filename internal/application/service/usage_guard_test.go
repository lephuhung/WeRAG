package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// fakeUsagePlanRepo serves active subscriptions and seats from memory.
type fakeUsagePlanRepo struct {
	interfaces.UsagePlanRepository
	active map[string]*types.UsageSubscription // key scopeType/scopeID
	seats  map[string]bool                     // key subID/userID
}

func (f *fakeUsagePlanRepo) GetActiveSubscription(_ context.Context, scopeType, scopeID string) (*types.UsageSubscription, error) {
	return f.active[scopeType+"/"+scopeID], nil
}

func (f *fakeUsagePlanRepo) HasSeat(_ context.Context, subID, userID string) (bool, error) {
	return f.seats[subID+"/"+userID], nil
}

// countingRepo returns fixed counter rows for one scope.
type countingRepo struct {
	fakeUsageCounterRepo
	rows map[string][]*types.UsageCounter // key scopeType/scopeID
}

func (c *countingRepo) Get(_ context.Context, scopeType, scopeID string, _ []string) ([]*types.UsageCounter, error) {
	return c.rows[scopeType+"/"+scopeID], nil
}

type enforcingSettings struct {
	interfaces.SystemSettingService
	on bool
}

func (s enforcingSettings) GetBool(context.Context, string, string, bool) bool { return s.on }

var guardNow = time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC) // 2026-10-13 in Vietnam

func poolPlan(limits ...types.UsageLimit) *types.UsagePlan {
	return &types.UsagePlan{ID: "plan-1", Code: "basic", Name: "Basic", ScopeKind: "tenant", Mode: "pool", Limits: limits}
}

func newGuard(plans *fakeUsagePlanRepo, counters *countingRepo, on bool) *usageGuard {
	return &usageGuard{plans: plans, counters: counters, settings: enforcingSettings{on: on}, now: func() time.Time { return guardNow }}
}

func denialReason(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok)
	require.Equal(t, apperrors.ErrTooManyRequests, appErr.Code)
	d, ok := appErr.Details.(types.UsageDenial)
	require.True(t, ok)
	return d.Reason
}

func TestUsageGuardOffLetsEveryoneThrough(t *testing.T) {
	g := newGuard(&fakeUsagePlanRepo{}, &countingRepo{}, false)
	require.NoError(t, checkTurn(g, context.Background(), 10003, "user-1"))
}

func TestUsageGuardSystemAdminExempt(t *testing.T) {
	g := newGuard(&fakeUsagePlanRepo{}, &countingRepo{}, true)
	ctx := context.WithValue(context.Background(), types.SystemAdminContextKey, true)
	require.NoError(t, checkTurn(g, ctx, 10003, "user-1"))
}

func TestUsageGuardNoSubscriptionBlocks(t *testing.T) {
	g := newGuard(&fakeUsagePlanRepo{}, &countingRepo{}, true)
	err := checkTurn(g, context.Background(), 10003, "user-1")
	require.Equal(t, types.UsageDenialNoSubscription, denialReason(t, err))
}

func TestUsageGuardPoolLimitAgainstTenantCounters(t *testing.T) {
	plans := &fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
		"tenant/10003": {ID: "sub-1", PlanID: "plan-1", Mode: "pool", Status: "active", StartsAt: guardNow.Add(-time.Hour),
			Plan: poolPlan(types.UsageLimit{Metric: "questions", Period: "day", Limit: 10})},
	}}
	counters := &countingRepo{rows: map[string][]*types.UsageCounter{
		"tenant/10003": {{Metric: "questions", PeriodKey: "2026-10-13", Value: 9}},
		"user/user-1":  {{Metric: "questions", PeriodKey: "2026-10-13", Value: 50}}, // ignored in pool mode
	}}
	g := newGuard(plans, counters, true)
	require.NoError(t, checkTurn(g, context.Background(), 10003, "user-1"))

	counters.rows["tenant/10003"][0].Value = 10
	err := checkTurn(g, context.Background(), 10003, "user-1")
	require.Equal(t, types.UsageDenialLimit, denialReason(t, err))

	ent, err := g.Entitlement(context.Background(), 10003, "user-1")
	require.NoError(t, err)
	require.Equal(t, "tenant", ent.Source)
	require.Equal(t, "tenant", ent.CountedScope)
	require.Len(t, ent.Limits, 1)
	require.True(t, ent.Limits[0].Exhausted)
	require.Equal(t, int64(0), ent.Limits[0].Remaining)
}

func TestUsageGuardOverrideBeatsPlanLimits(t *testing.T) {
	override := types.UsageLimits{{Metric: "tokens", Period: "month", Limit: 1000}}
	plans := &fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
		"tenant/10003": {ID: "sub-1", Mode: "pool", Status: "active", LimitsOverride: &override,
			Plan: poolPlan(types.UsageLimit{Metric: "questions", Period: "day", Limit: 1})},
	}}
	counters := &countingRepo{rows: map[string][]*types.UsageCounter{
		"tenant/10003": {
			{Metric: "questions", PeriodKey: "2026-10-13", Value: 99},
			{Metric: "tokens", PeriodKey: "2026-10", Value: 999},
		},
	}}
	g := newGuard(plans, counters, true)
	require.NoError(t, checkTurn(g, context.Background(), 10003, "user-1"))
}

func TestUsageGuardExpiredSubscriptionBlocks(t *testing.T) {
	ended := guardNow.Add(-time.Minute)
	plans := &fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
		"tenant/10003": {ID: "sub-1", Mode: "pool", Status: "active", EndsAt: &ended, Plan: poolPlan()},
	}}
	g := newGuard(plans, &countingRepo{}, true)
	err := checkTurn(g, context.Background(), 10003, "user-1")
	require.Equal(t, types.UsageDenialExpired, denialReason(t, err))
}

func TestUsageGuardSeatModeNeedsSeatAndCountsUser(t *testing.T) {
	plans := &fakeUsagePlanRepo{
		active: map[string]*types.UsageSubscription{
			"tenant/10003": {ID: "sub-1", Mode: "seat", Seats: 2, Status: "active",
				Plan: poolPlan(types.UsageLimit{Metric: "questions", Period: "month", Limit: 100})},
		},
		seats: map[string]bool{"sub-1/user-1": true},
	}
	counters := &countingRepo{rows: map[string][]*types.UsageCounter{
		"tenant/10003": {{Metric: "questions", PeriodKey: "2026-10", Value: 1000}},
		"user/user-1":  {{Metric: "questions", PeriodKey: "2026-10", Value: 10}},
	}}
	g := newGuard(plans, counters, true)
	require.NoError(t, checkTurn(g, context.Background(), 10003, "user-1"))
	err := checkTurn(g, context.Background(), 10003, "user-2")
	require.Equal(t, types.UsageDenialNoSeat, denialReason(t, err))
	// Machine principals have no seat and are not capped per seat.
	require.NoError(t, checkTurn(g, context.Background(), 10003, "system-10003"))
}

func TestUsageGuardPersonalSubscriptionWhenWorkspaceHasNone(t *testing.T) {
	plans := &fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
		"user/user-1": {ID: "sub-u", Mode: "pool", Status: "active",
			Plan: &types.UsagePlan{Code: "guest", ScopeKind: "user", Limits: types.UsageLimits{{Metric: "questions", Period: "day", Limit: 5}}}},
	}}
	counters := &countingRepo{rows: map[string][]*types.UsageCounter{
		"user/user-1": {{Metric: "questions", PeriodKey: "2026-10-13", Value: 5}},
	}}
	g := newGuard(plans, counters, true)
	err := checkTurn(g, context.Background(), 10003, "user-1")
	require.Equal(t, types.UsageDenialLimit, denialReason(t, err))
	ent, err := g.Entitlement(context.Background(), 10003, "user-1")
	require.NoError(t, err)
	require.Equal(t, "user", ent.Source)
	require.Equal(t, "guest", ent.PlanCode)
}

func TestUsageLimitsValidate(t *testing.T) {
	require.NoError(t, types.UsageLimits{{Metric: "questions", Period: "day", Limit: 1}}.Validate())
	require.Error(t, types.UsageLimits{{Metric: "words", Period: "day", Limit: 1}}.Validate())
	require.Error(t, types.UsageLimits{{Metric: "tokens", Period: "week", Limit: 1}}.Validate())
	require.Error(t, types.UsageLimits{{Metric: "tokens", Period: "month", Limit: -1}}.Validate())
}

// checkTurn is CheckTurn without the grant, for tests about the denial.
func checkTurn(g *usageGuard, ctx context.Context, tenantID uint64, userID string) error {
	_, err := g.CheckTurn(ctx, tenantID, userID)
	return err
}

func TestUsageGuardGrantCarriesPlanFeatures(t *testing.T) {
	plan := poolPlan()
	plan.Features = types.JSON(`{"agent.turn_token_cap": 300000, "context.history_tokens": 32768,
		"session.max_turns": 15, "ui.theme": "dark"}`)
	plans := &fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
		"tenant/10003": {ID: "sub-1", Mode: "pool", Status: "active", Plan: plan},
	}}
	g := newGuard(plans, &countingRepo{}, true)
	grant, err := g.CheckTurn(context.Background(), 10003, "user-1")
	require.NoError(t, err)
	require.Equal(t, types.UsagePlanFeatures{TurnTokenCap: 300000, HistoryTokens: 32768, SessionMaxTurns: 15},
		grant.Features)

	// enforcement off and SuperAdmins: a grant without caps
	off := newGuard(plans, &countingRepo{}, false)
	grant, err = off.CheckTurn(context.Background(), 10003, "user-1")
	require.NoError(t, err)
	require.True(t, grant.Features.IsZero())
	admin := context.WithValue(context.Background(), types.SystemAdminContextKey, true)
	grant, err = g.CheckTurn(admin, 10003, "user-1")
	require.NoError(t, err)
	require.True(t, grant.Features.IsZero())
}

func TestNormalizePlanValidatesFeatures(t *testing.T) {
	ok := &types.UsagePlan{Code: "c", Name: "n",
		Features: types.JSON(`{"agent.max_iterations": 8, "other": [1, 2]}`)}
	require.NoError(t, normalizePlan(ok))
	require.JSONEq(t, `{"agent.max_iterations": 8, "other": [1, 2]}`, string(ok.Features))

	for _, bad := range []string{
		`{"agent.turn_token_cap": -1}`,
		`{"session.max_tokens": 1.5}`,
		`{"context.history_tokens": "lots"}`,
		`[1]`,
	} {
		plan := &types.UsagePlan{Code: "c", Name: "n", Features: types.JSON(bad)}
		require.Error(t, normalizePlan(plan), bad)
	}
	empty := &types.UsagePlan{Code: "c", Name: "n"}
	require.NoError(t, normalizePlan(empty))
	require.Equal(t, "{}", string(empty.Features))
}
