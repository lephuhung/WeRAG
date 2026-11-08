package chat

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/pricing"
	"github.com/Tencent/WeKnora/internal/types"
)

// logUsage prices and logs one call's usage. It is the single choke point
// between a provider response and everything downstream (the message's stored
// usage, the stream event the client sees, the usage counters), which is why
// the cost is stamped here: this is the last place that knows the model name
// and the hour the provider served the call, and the first place after the
// usage is final. Callers pass the same pointer they return, so the stamp
// reaches the caller's usage.
func logUsage(ctx context.Context, model string, u *types.TokenUsage) {
	if u == nil {
		return
	}
	cost := pricing.FromUsage(model, *u, time.Now())
	u.CostMicroVND = cost.MicroVND
	purpose, prefixFingerprint := types.LLMCallMetadataFromContext(ctx)
	logger.Infof(ctx,
		"[LLM Usage] model=%s, purpose=%s, prompt_prefix=%s, prompt_tokens=%d, completion_tokens=%d, "+
			"total_tokens=%d, cached_tokens=%d, cache_read_tokens=%d, cache_write_tokens=%d, "+
			"cache_miss_tokens=%d, cache_hit_rate=%.1f%%, cache_reported=%t, cache_status=%s%s%s",
		model, purpose, prefixFingerprint, u.PromptTokens, u.CompletionTokens, u.TotalTokens,
		u.CachedTokens, u.CacheReadTokens, u.CacheWriteTokens, u.CacheMissTokens,
		u.PromptCacheHitRate(), u.CacheReported, u.CacheStatus,
		pricingLogSuffix(cost), usageAttribution(ctx))
}

// pricingLogSuffix renders the cost fields, so one grep over the log answers
// "what did today cost" without joining it against the counters.
func pricingLogSuffix(cost pricing.Cost) string {
	if !cost.Priced {
		return ", price=unpriced, cost_micro_vnd=0"
	}
	tier := "off_peak"
	if cost.Peak {
		tier = "peak"
	}
	return ", price=" + cost.Model + ", price_tier=" + tier +
		", cost_micro_vnd=" + strconv.FormatInt(cost.MicroVND, 10)
}

// usageAttribution renders the ", session_id=…, principal=…" suffix that
// attributes a usage line to the session and terminal principal that
// triggered the call. Calls that run outside a session or without a resolved
// principal (document parsing, title generation, background jobs) render an
// empty string, keeping those lines byte-identical to before.
func usageAttribution(ctx context.Context) string {
	var b strings.Builder
	if sessionID, ok := types.SessionIDFromContext(ctx); ok && sessionID != "" {
		b.WriteString(", session_id=")
		b.WriteString(sessionID)
	}
	if principal, ok := types.PrincipalFromContext(ctx); ok {
		b.WriteString(", principal=")
		b.WriteString(principal.Type)
		b.WriteString(":")
		b.WriteString(principal.ID)
	}
	return b.String()
}
