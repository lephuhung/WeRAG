package types

import (
	"strconv"
	"time"
)

// Usage metering (usage packages, phase 1).
//
// Every chat turn is counted twice: once for the organization that owns the
// session (scope "tenant") and once for the person who asked (scope
// "user"). Counters are bucketed by Vietnam calendar day and month so a
// package can later say "N questions per day" or "M tokens per month"
// without re-aggregating the messages table. Rows only ever grow; the
// day rows are the ground truth and the month rows a convenience.

const (
	UsageScopeTenant = "tenant"
	UsageScopeUser   = "user"
	// UsageScopeSession counts one chat session (scope_id = session id)
	// over its whole life, in the single period UsagePeriodAll. Only the
	// per-session caps read it (types.UsageFeatureSessionMaxTurns/Tokens);
	// every aggregate over usage_counters filters by scope_type, so these
	// rows never add into organization or person totals.
	UsageScopeSession = "session"
	// UsageScopeGuest counts a person's guest-chat turns in the public portal
	// (scope_id = user id) apart from their "user" counters, so the guest
	// plan and a seat or personal package never draw on each other's
	// questions: a member removed from a Tổ chức starts the guest day at 0,
	// and guest turns do not eat a later seat cap.
	UsageScopeGuest = "guest"
	// UsagePeriodAll is the period key of lifetime counters (session scope).
	UsagePeriodAll = "all"

	// UsageMetricQuestions counts role='user' messages (one per turn).
	UsageMetricQuestions = "questions"
	// UsageMetricTokens is the provider's total_tokens of the turn.
	UsageMetricTokens           = "tokens"
	UsageMetricPromptTokens     = "prompt_tokens"
	UsageMetricCompletionTokens = "completion_tokens"
	// UsageMetricOCRTokens is the part of "tokens" spent reading chat
	// attachments with the vision/OCR model (images and scanned pages).
	// Those tokens are also added to "tokens" (and its prompt/completion
	// parts): an OCR call costs the deployment like any other model call,
	// so it counts against the token quota, and the text it produced is
	// paid for again when the next question carries it in its prompt.
	// Uploads into a knowledge base are not counted here.
	UsageMetricOCRTokens = "ocr_tokens"
	// UsageMetricOCRPages counts the images / scanned pages those calls read.
	UsageMetricOCRPages = "ocr_pages"
	// UsageMetricCacheReadTokens is the part of prompt_tokens the provider
	// served from its prompt cache (DeepSeek prompt_cache_hit_tokens,
	// OpenAI/Anthropic cache reads). A subset of prompt_tokens, like the
	// OCR counters are a subset of tokens: never add it to the total.
	//
	// It is counted because nothing else explains the bill: a cached input
	// token costs up to 1/100 of an uncached one, so the same token count
	// can cost ten times more from one month to the next.
	UsageMetricCacheReadTokens = "cache_read_tokens"
	// UsageMetricCacheMissTokens is the rest of prompt_tokens: what the
	// provider had to read from scratch. DeepSeek reports it directly and
	// other providers derive it as prompt_tokens - cache reads, but only
	// when they report cache figures at all — a provider that reports
	// nothing leaves both counters at zero instead of pretending the whole
	// prompt missed.
	UsageMetricCacheMissTokens = "cache_miss_tokens"
	// UsageMetricCostMicroVND is what the calls cost the deployment, in
	// millionths of a đồng, priced at the moment of the call with the
	// model's tariff and the hour it ran (see models/pricing). Millionths
	// because one answer costs a fraction of a đồng: rounding each turn
	// to a whole đồng would drift by up to a đồng per turn.
	//
	// Only the operator sees this. It is what a package's price has to
	// cover, and it is kept apart from the token counters so a price
	// change never rewrites history: each turn keeps the cost it was
	// stamped with.
	UsageMetricCostMicroVND = "cost_micro_vnd"

	// UsageTimezone is the calendar used for period keys. Deployments are
	// provincial Vietnamese units, so "today" means today in Vietnam
	// regardless of the server's clock zone.
	UsageTimezone = "Asia/Ho_Chi_Minh"
)

var usageLocation = func() *time.Location {
	loc, err := time.LoadLocation(UsageTimezone)
	if err != nil {
		// No tzdata in the image: Vietnam has no DST, so a fixed offset is exact.
		return time.FixedZone("ICT", 7*3600)
	}
	return loc
}()

// UsageLocation returns the location period keys are computed in.
func UsageLocation() *time.Location { return usageLocation }

// UsagePeriodKeys are the day and month buckets one instant falls into.
type UsagePeriodKeys struct {
	Day   string // YYYY-MM-DD
	Month string // YYYY-MM
}

// UsagePeriodKeysAt buckets t in the usage calendar.
func UsagePeriodKeysAt(t time.Time) UsagePeriodKeys {
	local := t.In(usageLocation)
	return UsagePeriodKeys{Day: local.Format("2006-01-02"), Month: local.Format("2006-01")}
}

// UsageCounter is one row of usage_counters.
type UsageCounter struct {
	ScopeType string    `json:"scope_type" gorm:"primaryKey;type:varchar(16)"`
	ScopeID   string    `json:"scope_id"   gorm:"primaryKey;type:varchar(64)"`
	Metric    string    `json:"metric"     gorm:"primaryKey;type:varchar(32)"`
	PeriodKey string    `json:"period_key" gorm:"primaryKey;type:varchar(16)"`
	Value     int64     `json:"value"      gorm:"not null;default:0"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name.
func (UsageCounter) TableName() string { return "usage_counters" }

// UsageIncrement adds Delta to one counter (created on first use).
type UsageIncrement struct {
	ScopeType string
	ScopeID   string
	Metric    string
	PeriodKey string
	Delta     int64
}

// UsageTenantScopeID is the scope_id of an organization counter.
func UsageTenantScopeID(tenantID uint64) string { return strconv.FormatUint(tenantID, 10) }

// UsagePeriodTotals are the metrics of one scope in one period.
type UsagePeriodTotals struct {
	PeriodKey        string `json:"period_key"`
	Questions        int64  `json:"questions"`
	Tokens           int64  `json:"tokens"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	// OCRTokens is the share of Tokens spent on chat-attachment OCR;
	// OCRPages the images and scanned pages read.
	OCRTokens int64 `json:"ocr_tokens"`
	OCRPages  int64 `json:"ocr_pages"`
	// CacheReadTokens is the share of PromptTokens a provider's prompt
	// cache served, CacheMissTokens the rest. Both are zero when the
	// provider reports no cache figures; CacheReadTokens/PromptTokens is
	// the hit rate quoted on the dashboard.
	CacheReadTokens int64 `json:"cache_read_tokens"`
	CacheMissTokens int64 `json:"cache_miss_tokens"`
	// CostMicroVND is what these calls cost, in millionths of a đồng. Only
	// a SuperAdmin report shows it: it is the operator's spend, not the
	// tenant's consumption.
	CostMicroVND int64 `json:"cost_micro_vnd"`
}

// AddMetric folds one counter row into the totals.
func (t *UsagePeriodTotals) AddMetric(metric string, value int64) {
	switch metric {
	case UsageMetricQuestions:
		t.Questions += value
	case UsageMetricTokens:
		t.Tokens += value
	case UsageMetricPromptTokens:
		t.PromptTokens += value
	case UsageMetricCompletionTokens:
		t.CompletionTokens += value
	case UsageMetricOCRTokens:
		t.OCRTokens += value
	case UsageMetricOCRPages:
		t.OCRPages += value
	case UsageMetricCacheReadTokens:
		t.CacheReadTokens += value
	case UsageMetricCacheMissTokens:
		t.CacheMissTokens += value
	case UsageMetricCostMicroVND:
		t.CostMicroVND += value
	}
}

// UsageScopeSummary is what one scope used today and this month.
type UsageScopeSummary struct {
	ScopeType string            `json:"scope_type"`
	ScopeID   string            `json:"scope_id"`
	Today     UsagePeriodTotals `json:"today"`
	Month     UsagePeriodTotals `json:"month"`
}

// UsageSummary answers GET /me/usage: the caller's own counters and their
// current organization's. Either part is nil when the caller has no such scope
// (machine principals have no user scope; tenantless humans no tenant).
type UsageSummary struct {
	Timezone string             `json:"timezone"`
	User     *UsageScopeSummary `json:"user,omitempty"`
	Tenant   *UsageScopeSummary `json:"tenant,omitempty"`
	// Enforced mirrors the usage.enforce setting: whether a missing or
	// exhausted entitlement refuses turns.
	Enforced bool `json:"enforced"`
	// Entitlement is the subscription governing the caller's turns, nil
	// when there is none.
	Entitlement *UsageEntitlement `json:"entitlement,omitempty"`
}

// UsageStats is the platform-wide slice of the SystemAdmin dashboard:
// every organization's counters summed for today and this month.
type UsageStats struct {
	Timezone string            `json:"timezone"`
	Today    UsagePeriodTotals `json:"today"`
	Month    UsagePeriodTotals `json:"month"`
}
