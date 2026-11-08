package pricing

import (
	"math"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// peak is 09:00 UTC on a Tuesday: inside DeepSeek's 06:00-10:00 peak window.
var peak = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

// offPeak is 22:00 UTC on the same Tuesday.
var offPeak = time.Date(2026, 10, 13, 22, 0, 0, 0, time.UTC)

// assertMicroVND compares with a one-micro-VND tolerance: the arithmetic goes
// through a float, and a đồng has a million of them.
func assertMicroVND(t *testing.T, want, got int64) {
	t.Helper()
	assert.InDelta(t, float64(want), float64(got), 1, "micro-VND")
}

// One million output tokens at the published DeepSeek table. These numbers are
// the ones a package price is derived from, so they are pinned literally: a
// typo in the table (or a stray factor of a million) fails here.
func TestFromUsageMillionOutputTokens(t *testing.T) {
	rate := USDVND()
	tests := []struct {
		model string
		at    time.Time
		usd   float64
	}{
		{"deepseek-flash", offPeak, 0.60},
		{"deepseek-flash", peak, 1.20},
		{"deepseek-v4-pro", offPeak, 1.98},
		{"deepseek-v4-pro", peak, 3.96},
	}
	for _, tc := range tests {
		cost := FromUsage(tc.model, types.TokenUsage{CompletionTokens: 1_000_000}, tc.at)
		require.True(t, cost.Priced, tc.model)
		// micro-VND = tokens × USD per million × VND per USD
		assertMicroVND(t, int64(math.Round(tc.usd*rate*1_000_000)), cost.MicroVND)
		assert.Equal(t, tc.at == peak, cost.Peak)
	}
}

// A cached input token costs 1/100 of an uncached one on DeepSeek, which is
// the whole reason the counters and this package exist: the same prompt_tokens
// number must price very differently depending on the split.
func TestFromUsageCacheHitIsFarCheaper(t *testing.T) {
	missed := FromUsage("deepseek-v4-pro", types.TokenUsage{
		PromptTokens: 1_000_000, CacheMissTokens: 1_000_000, CacheReported: true,
	}, peak)
	hit := FromUsage("deepseek-v4-pro", types.TokenUsage{
		PromptTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheReported: true,
	}, peak)

	require.True(t, missed.Priced)
	require.True(t, hit.Priced)
	assert.Equal(t, 1_000_000, missed.CacheMissTokens)
	assert.Equal(t, 0, missed.CacheHitTokens)
	assert.Equal(t, 1_000_000, hit.CacheHitTokens)
	assert.Equal(t, 0, hit.CacheMissTokens)

	// $1.32 vs $0.044 per million: the hit is exactly 30x cheaper.
	assertMicroVND(t, int64(math.Round(1.32*USDVND()*1_000_000)), missed.MicroVND)
	assertMicroVND(t, int64(math.Round(0.044*USDVND()*1_000_000)), hit.MicroVND)
	assert.Greater(t, missed.MicroVND, hit.MicroVND*29)
}

// A provider that reports no cache figures is charged as if every input token
// missed: that is what the provider bills, and assuming hits would hide the
// case where caching silently stopped working.
func TestFromUsageUnreportedCacheChargesTheWholePromptAsMissed(t *testing.T) {
	cost := FromUsage("deepseek-flash", types.TokenUsage{PromptTokens: 400}, peak)
	assert.Equal(t, 0, cost.CacheHitTokens)
	assert.Equal(t, 400, cost.CacheMissTokens)
}

// Miss is derived from the prompt when a provider reports reads but not
// misses, so a partial report still splits the input correctly.
func TestFromUsageDerivesMissFromPrompt(t *testing.T) {
	cost := FromUsage("deepseek-flash", types.TokenUsage{
		PromptTokens: 1000, CacheReadTokens: 900, CacheReported: true,
	}, peak)
	assert.Equal(t, 900, cost.CacheHitTokens)
	assert.Equal(t, 100, cost.CacheMissTokens)
}

// A read count larger than the prompt (a provider bug, or a stale counter) must
// not produce a negative cost.
func TestFromUsageClampsAnImpossibleCacheSplit(t *testing.T) {
	cost := FromUsage("deepseek-flash", types.TokenUsage{
		PromptTokens: 100, CacheReadTokens: 500, CacheMissTokens: -20, CacheReported: true,
	}, peak)
	assert.Equal(t, 100, cost.CacheHitTokens)
	assert.Equal(t, 0, cost.CacheMissTokens)
	assert.GreaterOrEqual(t, cost.MicroVND, int64(0))
}

// An unpriced model still reports its cache split so a report can show the
// call, but contributes no money rather than a made-up one.
func TestFromUsageUnpricedModel(t *testing.T) {
	cost := FromUsage("some-self-hosted-model", types.TokenUsage{
		PromptTokens: 500, CompletionTokens: 500, CacheReadTokens: 200, CacheReported: true,
	}, peak)
	assert.False(t, cost.Priced)
	assert.Zero(t, cost.MicroVND)
	assert.Empty(t, cost.Model)
	assert.Equal(t, 200, cost.CacheHitTokens)
	assert.Equal(t, 300, cost.CacheMissTokens)
}

// DeepSeek's peak windows are 01:00-04:00 and 06:00-10:00 UTC on weekdays,
// which in Vietnam is 08:00-11:00 and 13:00-17:00 — the working day, so almost
// all real traffic is billed at peak. This is why package prices are computed
// on peak rates.
func TestAtPeakWindowsInVietnamHours(t *testing.T) {
	// A Monday, so the weekday check does not interfere.
	monday := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	at := func(hour, minute int) time.Time {
		return monday.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	tests := []struct {
		hour, minute int
		want         bool
		why          string
	}{
		{0, 59, false, "before the first window"},
		{1, 0, true, "window edge opens (08:00 VN)"},
		{3, 59, true, "inside the first window"},
		{4, 0, false, "window edge closes (11:00 VN)"},
		{5, 30, false, "lunch gap"},
		{6, 0, true, "second window opens (13:00 VN)"},
		{9, 59, true, "inside the second window"},
		{10, 0, false, "second window closes (17:00 VN)"},
		{23, 0, false, "late evening"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, AtPeak(at(tc.hour, tc.minute)), "%02d:%02d %s", tc.hour, tc.minute, tc.why)
	}
	// Weekends are off-peak all day, including inside a peak window.
	saturday := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	assert.False(t, AtPeak(saturday), "weekend is off-peak")
	assert.False(t, AtPeak(saturday.Add(24*time.Hour)), "sunday is off-peak")
}

// The names a deployment may still have configured must price as the model
// that actually serves them: DeepSeek retired the V4-Flash names and bills
// their requests at the Flash price.
func TestLookupResolvesLegacyAndDatedNames(t *testing.T) {
	tests := []struct{ name, want string }{
		{"deepseek-flash", "deepseek-flash"},
		{"deepseek-v4-flash", "deepseek-flash"},
		{"deepseek-v4.1-flash", "deepseek-flash"},
		{"deepseek-chat", "deepseek-flash"},
		{"DeepSeek/deepseek-v4-pro", "deepseek-v4-pro"},
		{"deepseek-v4-pro-0813", "deepseek-v4-pro"},
		{"deepseek-reasoner", "deepseek-v4-pro"},
		{"deepseek-v4-pro-1017-preview", "deepseek-v4-pro"},
	}
	for _, tc := range tests {
		price, ok := Lookup(tc.name)
		require.True(t, ok, tc.name)
		assert.Equal(t, tc.want, price.Name, tc.name)
	}
	_, ok := Lookup("")
	assert.False(t, ok)
	_, ok = Lookup("qwen-max")
	assert.False(t, ok)
}

// The Dashboard needs the number in đồng, not millionths.
func TestVNDRendersMicroVND(t *testing.T) {
	assert.InDelta(t, 31_560.0, VND(31_560_000_000), 0.001)
}
