// Package pricing turns a model call's token usage into the money it cost.
//
// Why this exists: a token is not worth one price. A provider bills an input
// token that hit its prompt cache at up to 1/100 of one that missed, bills
// reasoning tokens as output, and (DeepSeek) charges half at night. A package
// price derived from "tokens × one rate" is therefore off by a factor of two
// to ten, which is exactly the error that makes a paid plan unprofitable.
//
// Every call stamps its own cost at the moment the response arrives
// (models/chat stamps it in logUsage, and the usage meter folds it into the
// cost_micro_vnd counter), so the price table below is applied once, with the
// real peak/off-peak hour and the real cache split of that call, and a later
// price change never rewrites history.
//
// Money is VND, kept in millionths (micro-VND) because one cheap call is worth
// a fraction of a đồng: rounding each call to a whole đồng would drift by up
// to a đồng per call, which is real money at a hundred thousand calls a month.
package pricing

import (
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
)

// defaultUSDVND is the conversion used when PRICING_USD_VND is not set.
// Published provider prices are USD; the plans this deployment sells are VND.
const defaultUSDVND = 26300.0

// Rates are USD per 1,000,000 tokens of one kind.
type Rates struct {
	// InputCacheHit is the input a provider served from its prompt cache.
	InputCacheHit float64
	// InputCacheMiss is every other input token.
	InputCacheMiss float64
	// Output is every generated token, reasoning tokens included: a
	// provider bills thinking as output, and thinking mode can multiply a
	// visible answer by two to five.
	Output float64
}

// Price is one model's published rates at peak and off peak. Providers that
// do not vary by hour set both to the same value.
type Price struct {
	// Name is the table key this price was found under, for logs.
	Name    string
	Peak    Rates
	OffPeak Rates
}

var usdVND = sync.OnceValue(func() float64 {
	raw := strings.TrimSpace(os.Getenv("PRICING_USD_VND"))
	if raw == "" {
		return defaultUSDVND
	}
	rate, err := strconv.ParseFloat(raw, 64)
	if err != nil || rate <= 0 || math.IsInf(rate, 0) || math.IsNaN(rate) {
		return defaultUSDVND
	}
	return rate
})

// USDVND is the USD→VND rate costs are stamped with. It is read once from
// PRICING_USD_VND so a running deployment keeps one rate: two rates inside one
// month would add up to a number no report can explain.
func USDVND() float64 { return usdVND() }

// deepseekPrices is the table read from the provider's pricing page. It is
// deliberately kept in code, not in the database: it changes a few times a
// year, it must be reviewed against the source page when it does, and a wrong
// row here silently reprices every package.
//
// Source: https://api-docs.deepseek.com/quick_start/pricing (per 1M tokens).
var deepseekPrices = map[string]Price{
	"deepseek-flash": {
		Name:    "deepseek-flash",
		OffPeak: Rates{InputCacheHit: 0.003, InputCacheMiss: 0.15, Output: 0.60},
		Peak:    Rates{InputCacheHit: 0.006, InputCacheMiss: 0.30, Output: 1.20},
	},
	"deepseek-v4-pro": {
		Name:    "deepseek-v4-pro",
		OffPeak: Rates{InputCacheHit: 0.022, InputCacheMiss: 0.66, Output: 1.98},
		Peak:    Rates{InputCacheHit: 0.044, InputCacheMiss: 1.32, Output: 3.96},
	},
}

// modelAliases maps the names a deployment may still have configured — and the
// legacy names a provider serves with a current model — onto a table key.
// Every alias is billed at the price of the model that actually serves it.
var modelAliases = map[string]string{
	// DeepSeek retired V4-Flash's names; requests are served by V4.1-Flash
	// and billed at the Flash price.
	"deepseek-v4-flash":            "deepseek-flash",
	"deepseek-v4-flash-vision-exp": "deepseek-flash",
	"deepseek-v4.1-flash":          "deepseek-flash",
	"deepseek-v4.1-flash-1017":     "deepseek-flash",
	// deepseek-chat was the V3 name and is served by the current Flash.
	"deepseek-chat": "deepseek-flash",
	"deepseek-v3":   "deepseek-flash",
	"deepseek-v3.2": "deepseek-flash",
	// The reasoner name is billed as the expensive tier: it is the only
	// safe assumption when a deployment still points at it.
	"deepseek-reasoner":    "deepseek-v4-pro",
	"deepseek-v4-pro-0813": "deepseek-v4-pro",
}

// normaliseModelName lowercases and drops a vendor prefix, so a binding named
// "DeepSeek/deepseek-v4-pro" or "deepseek/deepseek-v4-pro" still prices.
func normaliseModelName(modelName string) string {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if i := strings.LastIndexAny(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, name)
}

// Lookup returns the price table row for a model name. A deployment that
// configured a name nobody priced (a self-hosted model, a new release) gets
// false, and its calls are recorded with no cost rather than with a made-up
// one — an unpriced model shows up as a gap in the report, which is how it
// gets noticed.
func Lookup(modelName string) (Price, bool) {
	name := normaliseModelName(modelName)
	if name == "" {
		return Price{}, false
	}
	if price, ok := deepseekPrices[name]; ok {
		return price, true
	}
	if key, ok := modelAliases[name]; ok {
		price := deepseekPrices[key]
		return price, true
	}
	// A dated or preview build of a priced model ("deepseek-v4-pro-1017")
	// is billed as that model: longest key match wins so -pro beats -flash.
	best, bestLen := "", 0
	for key := range deepseekPrices {
		if strings.HasPrefix(name, key) && len(key) > bestLen {
			best, bestLen = key, len(key)
		}
	}
	if best != "" {
		return deepseekPrices[best], true
	}
	if strings.Contains(name, "deepseek") {
		// Known provider, unknown tier: price it as the cheaper of its
		// tiers only if the name does not say "pro"/"reasoner", which are
		// the expensive letters in every DeepSeek name so far.
		if strings.Contains(name, "pro") || strings.Contains(name, "reason") || strings.Contains(name, "r1") {
			return deepseekPrices["deepseek-v4-pro"], true
		}
		return deepseekPrices["deepseek-flash"], true
	}
	return Price{}, false
}

// peakWindowsUTC are the provider's peak hours in UTC, as half-open ranges.
// DeepSeek: 01:00-04:00 and 06:00-10:00 UTC, Monday to Friday, excluding
// Chinese public holidays; off-peak is billed at half. The holidays are not
// modelled, so a holiday weekday is priced as peak: that overstates cost,
// which is the safe direction for a package price.
var peakWindowsUTC = [][2]int{{1 * 3600, 4 * 3600}, {6 * 3600, 10 * 3600}}

// AtPeak reports whether at falls in a peak window. It is exported so reports
// can explain a cost without re-deriving the rule.
func AtPeak(at time.Time) bool {
	utc := at.UTC()
	if weekday := utc.Weekday(); weekday == time.Saturday || weekday == time.Sunday {
		return false
	}
	second := utc.Hour()*3600 + utc.Minute()*60 + utc.Second()
	for _, window := range peakWindowsUTC {
		if second >= window[0] && second < window[1] {
			return true
		}
	}
	return false
}

// RatesAt returns the rates in force at at, and whether they are peak rates.
func (p Price) RatesAt(at time.Time) (Rates, bool) {
	if AtPeak(at) {
		return p.Peak, true
	}
	return p.OffPeak, false
}

// Cost is what one call cost.
type Cost struct {
	// MicroVND is the cost in millionths of a đồng.
	MicroVND int64
	// Priced is false when the model has no price row; MicroVND is then 0
	// and the call is counted in tokens but contributes no money.
	Priced bool
	// Peak records which half of the provider's tariff was applied.
	Peak bool
	// Model is the price row used, empty when the model is unpriced.
	Model string
	// CacheHitTokens / CacheMissTokens are the split the cost was computed
	// from, kept so a report can show the arithmetic.
	CacheHitTokens  int
	CacheMissTokens int
}

// inputSplit splits the prompt tokens of u into cached and uncached. A caller
// that did not report cache figures is charged as if none of its input hit the
// cache: that is what the provider charges, and assuming a hit would hide the
// case where caching silently stopped working.
func inputSplit(u types.TokenUsage) (hit, miss int) {
	hit = u.CacheReadTokens
	if hit < 0 {
		hit = 0
	}
	if hit > u.PromptTokens {
		hit = u.PromptTokens
	}
	if u.CacheReported && u.CacheMissTokens > 0 {
		miss = u.CacheMissTokens
	}
	if miss == 0 {
		miss = u.PromptTokens - hit
	}
	if miss < 0 {
		miss = 0
	}
	return hit, miss
}

// FromUsage prices one call's usage. at must be the time the provider served
// the call: peak and off-peak are half the tariff apart. An unpriced model
// returns Priced=false with a zero cost and the usage split still filled in,
// so a report can tell "free" from "not priced yet".
func FromUsage(modelName string, u types.TokenUsage, at time.Time) Cost {
	hit, miss := inputSplit(u)
	cost := Cost{CacheHitTokens: hit, CacheMissTokens: miss, Peak: AtPeak(at)}
	price, ok := Lookup(modelName)
	if !ok {
		return cost
	}
	cost.Priced = true
	cost.Model = price.Name
	rates, peak := price.RatesAt(at)
	cost.Peak = peak
	// micro-VND = tokens × USD per 1M tokens × VND per USD: the per-million
	// and per-millionth factors cancel, so no division is needed.
	usd := float64(hit)*rates.InputCacheHit +
		float64(miss)*rates.InputCacheMiss +
		float64(u.CompletionTokens)*rates.Output
	cost.MicroVND = int64(math.Round(usd * USDVND()))
	return cost
}

// VND renders micro-VND as a đồng amount for logs.
func VND(microVND int64) float64 { return float64(microVND) / 1e6 }
