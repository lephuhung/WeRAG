package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeUsageCounterRepo struct {
	added   []types.UsageIncrement
	claimed map[string]bool
}

func (f *fakeUsageCounterRepo) Add(_ context.Context, incs []types.UsageIncrement) error {
	f.added = append(f.added, incs...)
	return nil
}

func (f *fakeUsageCounterRepo) Get(context.Context, string, string, []string) ([]*types.UsageCounter, error) {
	return nil, nil
}

func (f *fakeUsageCounterRepo) SumByPeriod(context.Context, string, string) ([]*types.UsageCounter, error) {
	return nil, nil
}

func (f *fakeUsageCounterRepo) DeleteScope(context.Context, string, string) error { return nil }

func (f *fakeUsageCounterRepo) AddMessageUsage(_ context.Context, id string, incs []types.UsageIncrement) (bool, error) {
	if f.claimed == nil {
		f.claimed = map[string]bool{}
	}
	if f.claimed[id] {
		return false, nil
	}
	f.claimed[id] = true
	f.added = append(f.added, incs...)
	return true, nil
}

func TestUsagePeriodKeysUseVietnamCalendar(t *testing.T) {
	// 2026-10-13 18:30 UTC is already 2026-10-14 01:30 in Vietnam.
	at := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
	keys := types.UsagePeriodKeysAt(at)
	require.Equal(t, "2026-10-14", keys.Day)
	require.Equal(t, "2026-10", keys.Month)
}

func TestUsageMeterRecordQuestionCountsTenantAndRealUserOnly(t *testing.T) {
	repo := &fakeUsageCounterRepo{}
	m := &usageMeter{repo: repo, now: func() time.Time { return time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC) }}

	m.RecordQuestion(context.Background(), 10003, "user-1", "")
	require.Len(t, repo.added, 4) // {tenant,user} x {day,month}
	for _, inc := range repo.added {
		require.Equal(t, types.UsageMetricQuestions, inc.Metric)
		require.Equal(t, int64(1), inc.Delta)
	}

	repo.added = nil
	m.RecordQuestion(context.Background(), 10003, "system-10003", "") // IM/API synthetic user
	require.Len(t, repo.added, 2)
	for _, inc := range repo.added {
		require.Equal(t, types.UsageScopeTenant, inc.ScopeType)
		require.Equal(t, "10003", inc.ScopeID)
	}
}

// A session's turns and tokens also land in its lifetime counters, the one
// row per metric the per-session caps read; workspace and person rows are
// unchanged by it.
func TestUsageMeterCountsTheSessionOnce(t *testing.T) {
	repo := &fakeUsageCounterRepo{}
	m := &usageMeter{repo: repo, now: func() time.Time { return time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC) }}

	m.RecordQuestion(context.Background(), 10003, "user-1", "sess-1")
	require.Len(t, repo.added, 5)
	var session []types.UsageIncrement
	for _, inc := range repo.added {
		if inc.ScopeType == types.UsageScopeSession {
			session = append(session, inc)
		}
	}
	require.Equal(t, []types.UsageIncrement{{
		ScopeType: types.UsageScopeSession, ScopeID: "sess-1", Metric: types.UsageMetricQuestions,
		PeriodKey: types.UsagePeriodAll, Delta: 1,
	}}, session)

	repo.added = nil
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(10003))
	m.RecordMessageUsage(ctx, &types.Message{ID: "a1", SessionID: "sess-1", Role: "assistant",
		Usage: &types.TokenUsage{PromptTokens: 90, CompletionTokens: 10, TotalTokens: 100}})
	// 3 token metrics x ({tenant} x {day,month} + session)
	require.Len(t, repo.added, 9)
	for _, inc := range repo.added {
		if inc.ScopeType == types.UsageScopeSession {
			require.Equal(t, types.UsagePeriodAll, inc.PeriodKey)
		} else {
			require.Equal(t, types.UsageScopeTenant, inc.ScopeType)
		}
	}
}

func TestUsageMeterRecordMessageUsageOncePerMessage(t *testing.T) {
	repo := &fakeUsageCounterRepo{}
	m := &usageMeter{repo: repo, now: time.Now}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "u7")

	msg := &types.Message{ID: "m1", Role: "assistant", Usage: &types.TokenUsage{PromptTokens: 90, CompletionTokens: 10, TotalTokens: 100}}
	m.RecordMessageUsage(ctx, msg)
	// 3 token metrics x {tenant,user} x {day,month}
	require.Len(t, repo.added, 12)
	var tokens int64
	for _, inc := range repo.added {
		if inc.Metric == types.UsageMetricTokens && inc.ScopeType == types.UsageScopeTenant && len(inc.PeriodKey) == 7 {
			tokens = inc.Delta
		}
	}
	require.Equal(t, int64(100), tokens)

	m.RecordMessageUsage(ctx, msg) // saved again (artifacts, checkpoint...)
	require.Len(t, repo.added, 12, "second save must not re-count")

	// Nothing to count: no usage, user role, or zero tokens.
	m.RecordMessageUsage(ctx, &types.Message{ID: "m2", Role: "assistant"})
	m.RecordMessageUsage(ctx, &types.Message{ID: "m3", Role: "user", Usage: &types.TokenUsage{TotalTokens: 5}})
	m.RecordMessageUsage(ctx, &types.Message{ID: "m4", Role: "assistant", Usage: &types.TokenUsage{}})
	require.Len(t, repo.added, 12)
	require.False(t, repo.claimed["m2"])
}
