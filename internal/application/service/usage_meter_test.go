package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeUsageCounterRepo struct {
	added   []types.UsageIncrement
	claimed map[string]bool
	// failAdds makes the next Add calls fail
	failAdds int
	addCalls int
}

func (f *fakeUsageCounterRepo) Add(_ context.Context, incs []types.UsageIncrement) error {
	f.addCalls++
	if f.failAdds > 0 {
		f.failAdds--
		return errors.New("db down")
	}
	f.added = append(f.added, incs...)
	return nil
}

func (f *fakeUsageCounterRepo) Get(context.Context, string, string, []string) ([]*types.UsageCounter, error) {
	return nil, nil
}

func (f *fakeUsageCounterRepo) GetMany(context.Context, string, []string, []string) ([]*types.UsageCounter, error) {
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

func TestUsageMeterRecordOCRUsageJoinsTokenQuota(t *testing.T) {
	repo := &fakeUsageCounterRepo{}
	m := &usageMeter{repo: repo, now: time.Now}
	m.RecordOCRUsage(context.Background(), 10003, "user-1", "sess-1", 2,
		types.VLMUsage{PromptTokens: 1800, CompletionTokens: 120, TotalTokens: 1920})
	sum := map[string]int64{}
	for _, inc := range repo.added {
		if inc.ScopeType == types.UsageScopeSession {
			sum[inc.Metric] += inc.Delta
		}
	}
	require.Equal(t, int64(1920), sum[types.UsageMetricTokens], "OCR tokens count against the token quota")
	require.Equal(t, int64(1920), sum[types.UsageMetricOCRTokens])
	require.Equal(t, int64(1800), sum[types.UsageMetricPromptTokens])
	require.Equal(t, int64(2), sum[types.UsageMetricOCRPages])
	// {tenant,user} x {day,month} + session, for 5 metrics
	require.Len(t, repo.added, 5*5)

	repo.added = nil
	m.RecordOCRUsage(context.Background(), 10003, "user-1", "", 0, types.VLMUsage{})
	require.Empty(t, repo.added, "nothing read, nothing counted")
}

// A message carries the prompt-cache split and the cost its call was stamped
// with; both land in the counters, because the token total alone cannot
// explain a bill (a cached input token costs 1/100 of an uncached one) nor a
// package's margin.
func TestUsageMeterCountsCacheSplitAndCost(t *testing.T) {
	repo := &fakeUsageCounterRepo{}
	m := &usageMeter{repo: repo, now: time.Now}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	m.RecordMessageUsage(ctx, &types.Message{ID: "m1", Role: "assistant", Usage: &types.TokenUsage{
		PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100,
		CacheReadTokens: 800, CacheMissTokens: 200, CacheReported: true,
		CostMicroVND: 4_000_000,
	}})
	sum := map[string]int64{}
	for _, inc := range repo.added {
		if inc.ScopeType == types.UsageScopeTenant && len(inc.PeriodKey) == 7 {
			sum[inc.Metric] += inc.Delta
		}
	}
	require.Equal(t, int64(1100), sum[types.UsageMetricTokens])
	require.Equal(t, int64(800), sum[types.UsageMetricCacheReadTokens])
	require.Equal(t, int64(200), sum[types.UsageMetricCacheMissTokens])
	require.Equal(t, int64(4_000_000), sum[types.UsageMetricCostMicroVND], "micro-VND")
}

// A provider that reports nothing about its cache leaves both counters absent
// rather than claiming a 100% miss rate: "no row" is the signal that the
// deployment is blind here, and a zero row would look like a real measurement.
func TestUsageMeterOmitsCacheCountersWhenUnreported(t *testing.T) {
	repo := &fakeUsageCounterRepo{}
	m := &usageMeter{repo: repo, now: time.Now}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	m.RecordMessageUsage(ctx, &types.Message{ID: "m1", Role: "assistant", Usage: &types.TokenUsage{
		PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100,
	}})
	for _, inc := range repo.added {
		require.NotEqual(t, types.UsageMetricCacheReadTokens, inc.Metric)
		require.NotEqual(t, types.UsageMetricCacheMissTokens, inc.Metric)
		require.NotEqual(t, types.UsageMetricCostMicroVND, inc.Metric)
	}
}

// A full cache miss is reported as such: the miss counter is written, so a
// report can tell "caching never hit" from "the provider says nothing".
func TestUsageMeterRecordsAFullMiss(t *testing.T) {
	repo := &fakeUsageCounterRepo{}
	m := &usageMeter{repo: repo, now: time.Now}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	m.RecordMessageUsage(ctx, &types.Message{ID: "m1", Role: "assistant", Usage: &types.TokenUsage{
		PromptTokens: 500, CompletionTokens: 50, TotalTokens: 550,
		CacheMissTokens: 500, CacheReported: true,
	}})
	var misses, reads int64
	for _, inc := range repo.added {
		// The month row only: the day row carries the same delta.
		if inc.ScopeType != types.UsageScopeTenant || len(inc.PeriodKey) != 7 {
			continue
		}
		switch inc.Metric {
		case types.UsageMetricCacheMissTokens:
			misses += inc.Delta
		case types.UsageMetricCacheReadTokens:
			reads += inc.Delta
		}
	}
	require.Equal(t, int64(500), misses)
	require.Zero(t, reads)
}

// The dashboard totals fold the new metrics, so a period shows its hit rate
// and its money without a second query.
func TestUsagePeriodTotalsFoldCacheAndCost(t *testing.T) {
	totals := types.UsagePeriodTotals{}
	totals.AddMetric(types.UsageMetricPromptTokens, 1000)
	totals.AddMetric(types.UsageMetricCacheReadTokens, 750)
	totals.AddMetric(types.UsageMetricCacheMissTokens, 250)
	totals.AddMetric(types.UsageMetricCostMicroVND, 2_500_000)

	require.Equal(t, int64(1000), totals.PromptTokens)
	require.Equal(t, int64(750), totals.CacheReadTokens)
	require.Equal(t, int64(250), totals.CacheMissTokens)
	require.Equal(t, int64(2_500_000), totals.CostMicroVND)
}

// A failed question count is retried: the first write fails, the second
// lands, nothing is counted twice.
func TestUsageMeterRetriesFailedCounterWrite(t *testing.T) {
	repo := &fakeUsageCounterRepo{failAdds: 1}
	var async []func()
	m := &usageMeter{repo: repo, now: time.Now, retryDelays: []time.Duration{0, 0},
		goAsync: func(f func()) { async = append(async, f) }}

	m.RecordQuestion(context.Background(), 10003, "", "")
	require.Equal(t, 1, repo.addCalls, "only the first attempt runs on the caller's path")
	require.Len(t, async, 1)
	async[0]()
	require.Equal(t, 2, repo.addCalls)
	require.Len(t, repo.added, 2, "tenant day + month, once")
}

// Every attempt fails: the meter gives up after the configured retries and
// does not panic or block forever.
func TestUsageMeterGivesUpAfterRetries(t *testing.T) {
	repo := &fakeUsageCounterRepo{failAdds: 10}
	var async []func()
	m := &usageMeter{repo: repo, now: time.Now, retryDelays: []time.Duration{0, 0},
		goAsync: func(f func()) { async = append(async, f) }}

	m.RecordQuestion(context.Background(), 10003, "", "")
	require.Len(t, async, 1)
	async[0]()
	require.Equal(t, 3, repo.addCalls)
	require.Empty(t, repo.added)
}
