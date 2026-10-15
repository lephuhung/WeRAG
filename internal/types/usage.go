package types

import (
	"strconv"
	"time"
)

// Usage metering (usage packages, phase 1).
//
// Every chat turn is counted twice: once for the workspace that owns the
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
	// rows never add into workspace or person totals.
	UsageScopeSession = "session"
	// UsagePeriodAll is the period key of lifetime counters (session scope).
	UsagePeriodAll = "all"

	// UsageMetricQuestions counts role='user' messages (one per turn).
	UsageMetricQuestions = "questions"
	// UsageMetricTokens is the provider's total_tokens of the turn.
	UsageMetricTokens           = "tokens"
	UsageMetricPromptTokens     = "prompt_tokens"
	UsageMetricCompletionTokens = "completion_tokens"

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

// UsageTenantScopeID is the scope_id of a workspace counter.
func UsageTenantScopeID(tenantID uint64) string { return strconv.FormatUint(tenantID, 10) }

// UsagePeriodTotals are the metrics of one scope in one period.
type UsagePeriodTotals struct {
	PeriodKey        string `json:"period_key"`
	Questions        int64  `json:"questions"`
	Tokens           int64  `json:"tokens"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
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
// current workspace's. Either part is nil when the caller has no such scope
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
// every workspace's counters summed for today and this month.
type UsageStats struct {
	Timezone string            `json:"timezone"`
	Today    UsagePeriodTotals `json:"today"`
	Month    UsagePeriodTotals `json:"month"`
}
