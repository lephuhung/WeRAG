package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// liveCounters is a questions counter turns write while others read it.
type liveCounters struct {
	fakeUsageCounterRepo
	mu        sync.Mutex
	questions int64
}

func (c *liveCounters) Get(context.Context, string, string, []string) ([]*types.UsageCounter, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return []*types.UsageCounter{{Metric: types.UsageMetricQuestions, PeriodKey: "2026-10-13", Value: c.questions}}, nil
}

func (c *liveCounters) record() {
	c.mu.Lock()
	c.questions++
	c.mu.Unlock()
}

func reservingGuard(counters *liveCounters, limit int64, clock func() time.Time) *usageGuard {
	plans := &fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
		"tenant/10003": {ID: "sub-1", PlanID: "plan-1", Mode: "pool", Status: "active", StartsAt: guardNow.Add(-time.Hour),
			Plan: poolPlan(types.UsageLimit{Metric: types.UsageMetricQuestions, Period: types.UsagePeriodDay, Limit: limit})},
	}}
	return &usageGuard{plans: plans, counters: counters, settings: enforcingSettings{on: true}, now: clock,
		reservations: newQuestionReservations(questionReservationTTL)}
}

// A burst of turns checked together lets through only as many questions as
// the cap has left: each check counts the questions let through and not
// yet saved. Without the reservation every turn of the burst passed.
func TestUsageGuardQuestionsCapHoldsUnderABurst(t *testing.T) {
	counters := &liveCounters{questions: 7}
	g := reservingGuard(counters, 10, func() time.Time { return guardNow })

	var granted atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			grant, err := g.CheckTurn(context.Background(), 10003, "user-1", "", types.SessionCapHard)
			if err != nil {
				require.Equal(t, types.UsageDenialLimit, denialReason(t, err))
				return
			}
			granted.Add(1)
			time.Sleep(5 * time.Millisecond) // the turn saves its question later
			counters.record()
			grant.ReleaseReservation()
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 3, granted.Load())
	require.EqualValues(t, 10, counters.questions, "the cap is never overshot")
}

// A released reservation is not counted twice once the question is in the
// counters, and a turn that never saved its question frees its place when
// the reservation expires.
func TestUsageGuardQuestionReservationReleaseAndExpiry(t *testing.T) {
	now := guardNow
	counters := &liveCounters{questions: 8}
	g := reservingGuard(counters, 10, func() time.Time { return now })
	ctx := context.Background()

	first, err := g.CheckTurn(ctx, 10003, "user-1", "", types.SessionCapHard)
	require.NoError(t, err)
	counters.record()
	first.ReleaseReservation()
	first.ReleaseReservation() // idempotent

	// 9 counted, nothing pending: one more fits
	lost, err := g.CheckTurn(ctx, 10003, "user-1", "", types.SessionCapHard)
	require.NoError(t, err)
	require.NotNil(t, lost)
	// its question never reached the counters: the cap is held for it...
	_, err = g.CheckTurn(ctx, 10003, "user-1", "", types.SessionCapHard)
	require.Equal(t, types.UsageDenialLimit, denialReason(t, err))
	// ...until the reservation expires
	now = now.Add(questionReservationTTL + time.Second)
	_, err = g.CheckTurn(ctx, 10003, "user-1", "", types.SessionCapHard)
	require.NoError(t, err)
}

// A turn refused for its session cap gives its reserved question back.
func TestUsageGuardSessionCapRefusalReleasesTheQuestion(t *testing.T) {
	counters := &sessionCountingLive{liveCounters: liveCounters{questions: 9}, sessionTurns: 5}
	plans := &fakeUsagePlanRepo{active: map[string]*types.UsageSubscription{
		"tenant/10003": {ID: "sub-1", PlanID: "plan-1", Mode: "pool", Status: "active", StartsAt: guardNow.Add(-time.Hour),
			Plan: &types.UsagePlan{ID: "plan-1", Code: "basic", ScopeKind: "tenant", Mode: "pool",
				Limits:   types.UsageLimits{{Metric: types.UsageMetricQuestions, Period: types.UsagePeriodDay, Limit: 10}},
				Features: types.JSON(`{"session.max_turns": 5}`)}},
	}}
	g := &usageGuard{plans: plans, counters: counters, settings: enforcingSettings{on: true},
		now: func() time.Time { return guardNow }, reservations: newQuestionReservations(questionReservationTTL)}
	_, err := g.CheckTurn(context.Background(), 10003, "user-1", "sess-full", types.SessionCapHard)
	require.Equal(t, types.UsageDenialSessionLimit, denialReason(t, err))
	// the last question of the day is still free for another session
	_, err = g.CheckTurn(context.Background(), 10003, "user-1", "", types.SessionCapHard)
	require.NoError(t, err)
}

type sessionCountingLive struct {
	liveCounters
	sessionTurns int64
}

func (c *sessionCountingLive) Get(ctx context.Context, scopeType, scopeID string, keys []string) ([]*types.UsageCounter, error) {
	if scopeType == types.UsageScopeSession {
		return []*types.UsageCounter{{Metric: types.UsageMetricQuestions, PeriodKey: types.UsagePeriodAll, Value: c.sessionTurns}}, nil
	}
	return c.liveCounters.Get(ctx, scopeType, scopeID, keys)
}

type questionCountingMeter struct {
	interfaces.UsageMeter
	questions int
}

func (m *questionCountingMeter) RecordQuestion(context.Context, uint64, string, string) {
	m.questions++
}

// Saving a turn's user message counts the question and then frees the place
// the usage check reserved for it.
func TestMeterMessageReleasesTheReservedQuestion(t *testing.T) {
	meter := &questionCountingMeter{}
	s := &messageService{usageMeter: meter}
	released := 0
	ctx := types.WithUsageTurnGrant(context.Background(), &types.UsageTurnGrant{Release: func() { released++ }})
	s.meterMessage(ctx, 10003, &types.Message{Role: "user", SessionID: "s1"}, true)
	require.Equal(t, 1, meter.questions)
	require.Equal(t, 1, released)
	// an assistant row or a turn without a grant releases nothing
	s.meterMessage(context.Background(), 10003, &types.Message{Role: "user", SessionID: "s1"}, true)
	require.Equal(t, 1, released)
}
