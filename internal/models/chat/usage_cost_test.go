package chat

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/pricing"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logUsage is the choke point where a provider response becomes a priced
// usage: every path (SDK chat, raw chat, both stream readers) calls it, so the
// stamp has to happen here or the money is missing from the counters.
func TestLogUsageStampsTheCallsCost(t *testing.T) {
	usage := &types.TokenUsage{
		PromptTokens: 1_000_000, TotalTokens: 1_000_000,
		CacheMissTokens: 1_000_000, CacheReported: true,
	}
	logUsage(context.Background(), "deepseek-v4-pro", usage)

	rate := pricing.USDVND()
	offPeak := int64(math.Round(0.66 * rate * 1_000_000))
	peak := int64(math.Round(1.32 * rate * 1_000_000))
	// The stamp reads the wall clock, so the assertion accepts whichever half
	// of the tariff the test happens to run in.
	assert.Contains(t, []int64{offPeak, peak}, usage.CostMicroVND)
}

// An unpriced model is logged as unpriced and contributes no money: the token
// counters still count it, so the gap shows up in a report instead of being
// priced at a guess.
func TestLogUsageLeavesUnpricedModelsFree(t *testing.T) {
	usage := &types.TokenUsage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20}
	logUsage(context.Background(), "totally-unknown-model", usage)
	assert.Zero(t, usage.CostMicroVND)
}

// The cost must never reach a client: it is the operator's spend, and usage is
// serialised into every message and every stream event. This test is the guard
// on the json:"-" tag (see types.TokenUsage.CostMicroVND).
func TestUsageCostIsNotSerialised(t *testing.T) {
	usage := &types.TokenUsage{
		PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CostMicroVND: 123_456,
	}
	raw, err := json.Marshal(usage)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "cost")
	assert.NotContains(t, string(raw), "123456")
}

// A turn of several rounds costs the sum of its rounds: cost adds in
// Accumulate like the token counters do.
func TestAccumulateSumsCost(t *testing.T) {
	total := types.TokenUsage{PromptTokens: 10, CostMicroVND: 1000}
	total.Accumulate(types.TokenUsage{PromptTokens: 20, CostMicroVND: 2500})
	assert.Equal(t, int64(3500), total.CostMicroVND)
	assert.Equal(t, 30, total.PromptTokens)
}

// DeepSeek reports prompt_cache_hit_tokens / prompt_cache_miss_tokens in
// fields the pinned OpenAI SDK drops. The raw HTTP path is what keeps them, so
// a DeepSeek-shaped endpoint must take it even when the body is a plain
// standard request (thinking unset, no cache key to inject).
func TestBuildOutbound_DeepSeekEndpointTakesTheRawPath(t *testing.T) {
	c, err := NewRemoteAPIChat(&ChatConfig{
		Source:    types.ModelSourceRemote,
		BaseURL:   "https://api.deepseek.com/v1",
		ModelName: "deepseek-v4-pro",
		ModelID:   "deepseek-v4-pro",
		APIKey:    "k",
		Provider:  "generic",
	})
	require.NoError(t, err)

	_, _, useRaw, err := c.buildOutbound(
		context.Background(), []Message{{Role: "user", Content: "hi"}}, &ChatOptions{}, false)
	require.NoError(t, err)
	require.True(t, useRaw, "raw path keeps usage.prompt_cache_hit_tokens")
}

func TestCacheAccountingOutOfBand(t *testing.T) {
	assert.True(t, cacheAccountingOutOfBand("https://api.deepseek.com/v1"))
	assert.True(t, cacheAccountingOutOfBand("https://API.DeepSeek.com"))
	assert.True(t, cacheAccountingOutOfBand("https://api.moonshot.cn/v1"))
	assert.False(t, cacheAccountingOutOfBand("https://api.openai.com/v1"))
	assert.False(t, cacheAccountingOutOfBand("http://localhost:11434/v1"))
	assert.False(t, cacheAccountingOutOfBand(""))
}

// The raw usage fields DeepSeek sends are folded into the shared model, which
// is what makes the cache counters (and therefore the cost) correct.
func TestApplyRawPromptCacheUsageDeepSeekFields(t *testing.T) {
	usage := types.TokenUsage{PromptTokens: 1000, CompletionTokens: 20, TotalTokens: 1020}
	applyRawPromptCacheUsage([]byte(`{"usage":{"prompt_tokens":1000,"prompt_cache_hit_tokens":800,`+
		`"prompt_cache_miss_tokens":200}}`), &usage)

	assert.Equal(t, 800, usage.CacheReadTokens)
	assert.Equal(t, 200, usage.CacheMissTokens)
	assert.True(t, usage.CacheReported)
	assert.Equal(t, types.PromptCacheStatusHit, usage.CacheStatus)
	assert.InDelta(t, 80.0, usage.PromptCacheHitRate(), 0.01)

	// And the split is what the cost is computed from.
	cost := pricing.FromUsage("deepseek-flash", usage, time.Date(2026, 10, 13, 22, 0, 0, 0, time.UTC))
	assert.True(t, cost.Priced)
	assert.Equal(t, 800, cost.CacheHitTokens)
	assert.Equal(t, 200, cost.CacheMissTokens)
}

// A gateway that reports the cached count at the root of usage (Moonshot/Kimi)
// is accounted the same way as DeepSeek's hit/miss pair, which is why such an
// endpoint also takes the raw path.
func TestApplyRawPromptCacheUsageRootCachedTokens(t *testing.T) {
	usage := types.TokenUsage{PromptTokens: 1000, CompletionTokens: 10, TotalTokens: 1010}
	applyRawPromptCacheUsage([]byte(`{"usage":{"prompt_tokens":1000,"completion_tokens":10,`+
		`"total_tokens":1010,"cached_tokens":640}}`), &usage)

	assert.Equal(t, 640, usage.CacheReadTokens)
	assert.Equal(t, 360, usage.CacheMissTokens)
	assert.True(t, usage.CacheReported)
	assert.Equal(t, types.PromptCacheStatusHit, usage.CacheStatus)
}

// Forcing the raw path for DeepSeek moved a fallback away from the code that
// used to provide it: the SDK branch retries a multimodal rejection without
// images, so an image attached to a non-vision model degrades to the text
// description instead of failing the round. DeepSeek v4-pro does not accept
// images, so the raw path has to keep that promise.
func TestChatOnTheRawPathStillFallsBackWithoutImages(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(func() { secutils.SetSSRFWhitelistFromRaw("") })

	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		first := len(bodies) == 1
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if first {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"This model does not support image input"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"1","object":"chat.completion","created":1,"model":"deepseek-v4-pro",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,`+
			`"prompt_cache_hit_tokens":8,"prompt_cache_miss_tokens":2}}`)
	}))
	defer server.Close()

	// The path segment is what makes cacheAccountingOutOfBand recognise a
	// DeepSeek-shaped endpoint here; production matches the host.
	c, err := NewRemoteAPIChat(&ChatConfig{
		Source:    types.ModelSourceRemote,
		BaseURL:   server.URL + "/deepseek/v1",
		ModelName: "deepseek-v4-pro",
		ModelID:   "deepseek-v4-pro",
		APIKey:    "k",
		Provider:  "generic",
	})
	require.NoError(t, err)

	messages := []Message{{Role: "user", Content: "mô tả ảnh này", Images: []string{"data:image/png;base64,AAAA"}}}
	resp, err := c.Chat(context.Background(), messages, &ChatOptions{})
	require.NoError(t, err)
	require.Equal(t, "ok", resp.Content)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2, "one rejection, one retry")
	assert.Contains(t, bodies[0], "image/png", "the first attempt carries the image")
	assert.NotContains(t, bodies[1], "image/png", "the retry drops it")
	assert.Contains(t, bodies[1], "mô tả ảnh này", "the text question survives")

	// The retry is still accounted: cache counters and cost come from the raw body.
	assert.Equal(t, 8, resp.Usage.CacheReadTokens)
	assert.Equal(t, 2, resp.Usage.CacheMissTokens)
	assert.Greater(t, resp.Usage.CostMicroVND, int64(0))
}
