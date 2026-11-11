package types

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Per-member usage of one organization (report F9 "Báo cáo sử dụng theo
// thành viên").
//
// The "user" counters cannot answer it: they are keyed by the person
// alone, so a member of several Tổ chức (or one who also chats in a
// personal workspace) adds every workspace into one row, and seat caps read
// that row as it is. The meter therefore also counts each turn of a real
// person under scope "member" with scope_id "<tenant id>:<user id>", month
// rows only. Nothing enforces a limit on these rows; only the report reads
// them, so they change no quota.
const (
	UsageScopeMember = "member"
	// usageScopeIDMaxLen is usage_counters.scope_id's varchar width: a
	// longer member key is not counted rather than failing the whole
	// counter write.
	usageScopeIDMaxLen = 64
)

// UsageMemberScopeID is the scope_id of one person's counters inside one
// organization; "" when either part is missing or the key would not fit.
func UsageMemberScopeID(tenantID uint64, userID string) string {
	if tenantID == 0 || userID == "" {
		return ""
	}
	id := UsageMemberScopePrefix(tenantID) + userID
	if len(id) > usageScopeIDMaxLen {
		return ""
	}
	return id
}

// UsageMemberScopePrefix is the scope_id prefix shared by every member
// counter of one organization.
func UsageMemberScopePrefix(tenantID uint64) string {
	return strconv.FormatUint(tenantID, 10) + ":"
}

// UsageMemberIDFromScope returns the user id of a member scope_id of
// tenantID, "" when the scope belongs to another organization.
func UsageMemberIDFromScope(tenantID uint64, scopeID string) string {
	prefix := UsageMemberScopePrefix(tenantID)
	if !strings.HasPrefix(scopeID, prefix) {
		return ""
	}
	return scopeID[len(prefix):]
}

// ParseUsageMonth validates a "YYYY-MM" month key and returns the instants
// it spans in the usage calendar (Vietnam time): [start, end).
func ParseUsageMonth(month string) (start, end time.Time, err error) {
	t, err := time.ParseInLocation("2006-01", strings.TrimSpace(month), usageLocation)
	if err != nil || t.Format("2006-01") != strings.TrimSpace(month) {
		return time.Time{}, time.Time{}, fmt.Errorf("month must be YYYY-MM")
	}
	return t, t.AddDate(0, 1, 0), nil
}

// Sources of a member report.
const (
	// UsageMemberSourceCounters: the member counters (exact, with OCR and
	// cost).
	UsageMemberSourceCounters = "counters"
	// UsageMemberSourceMessages: the month predates member counting; the
	// figures are summed from the chat history of the organization's
	// sessions (questions and chat tokens only, no OCR, no cost).
	UsageMemberSourceMessages = "messages"
)

// UsageMemberUsage is one person's line in the member report.
type UsageMemberUsage struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	// Role is the member's role today; empty for someone who used the
	// organization this month but is no longer (or never was) a member,
	// e.g. a removed member or a SuperAdmin working in it.
	Role   TenantRole `json:"role,omitempty"`
	Member bool       `json:"member"`
	// HasSeat is set only when the organization's package is in seat mode.
	HasSeat *bool `json:"has_seat,omitempty"`
	// PersonalPlan names the member's own package, if any.
	PersonalPlan string            `json:"personal_plan,omitempty"`
	Usage        UsagePeriodTotals `json:"usage"`
}

// UsageMemberReport answers GET /tenants/:id/usage/members.
type UsageMemberReport struct {
	TenantID uint64 `json:"tenant_id"`
	Month    string `json:"month"`
	Timezone string `json:"timezone"`
	Source   string `json:"source"`
	// CostVisible: cost_micro_vnd is filled in (SuperAdmin only, it is the
	// operator's spend); otherwise every cost field is zero.
	CostVisible bool `json:"cost_visible"`
	// Subscription is the organization's current package with this
	// month's consumption against its limits (quota context); nil when it
	// has none.
	Subscription *UsageSubscriptionView `json:"subscription,omitempty"`
	// Members are sorted by tokens, then questions, descending.
	Members []*UsageMemberUsage `json:"members"`
	// Total is the organization counter for the month: every member plus
	// IM/API turns (machine principals) and anything counted before the
	// member counters existed.
	Total UsagePeriodTotals `json:"total"`
	// MembersTotal sums Members.
	MembersTotal UsagePeriodTotals `json:"members_total"`
	// Unattributed is Total minus MembersTotal (never negative): usage no
	// person can be named for.
	Unattributed UsagePeriodTotals `json:"unattributed"`
}

// Add folds other into t (every metric).
func (t *UsagePeriodTotals) Add(other UsagePeriodTotals) {
	t.Questions += other.Questions
	t.Tokens += other.Tokens
	t.PromptTokens += other.PromptTokens
	t.CompletionTokens += other.CompletionTokens
	t.OCRTokens += other.OCRTokens
	t.OCRPages += other.OCRPages
	t.CacheReadTokens += other.CacheReadTokens
	t.CacheMissTokens += other.CacheMissTokens
	t.CostMicroVND += other.CostMicroVND
}

// MinusFloor returns t - other per metric, floored at zero.
func (t UsagePeriodTotals) MinusFloor(other UsagePeriodTotals) UsagePeriodTotals {
	sub := func(a, b int64) int64 {
		if a > b {
			return a - b
		}
		return 0
	}
	return UsagePeriodTotals{
		PeriodKey:        t.PeriodKey,
		Questions:        sub(t.Questions, other.Questions),
		Tokens:           sub(t.Tokens, other.Tokens),
		PromptTokens:     sub(t.PromptTokens, other.PromptTokens),
		CompletionTokens: sub(t.CompletionTokens, other.CompletionTokens),
		OCRTokens:        sub(t.OCRTokens, other.OCRTokens),
		OCRPages:         sub(t.OCRPages, other.OCRPages),
		CacheReadTokens:  sub(t.CacheReadTokens, other.CacheReadTokens),
		CacheMissTokens:  sub(t.CacheMissTokens, other.CacheMissTokens),
		CostMicroVND:     sub(t.CostMicroVND, other.CostMicroVND),
	}
}
