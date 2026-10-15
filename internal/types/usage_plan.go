package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Usage packages (phase 2): plans, subscriptions, seats.
//
// A plan is a named bundle of limits that SuperAdmin edits at runtime. A
// subscription attaches a plan to a scope — a workspace or a person — for
// a period. Workspace subscriptions run in one of two modes: "pool"
// (everyone draws from the workspace's counters) or "seat" (the
// workspace buys N seats, each seat holder has their own counters and a
// Tenant Admin assigns the seats). Without an active subscription a turn
// is refused once enforcement is switched on.

const (
	UsagePeriodDay   = "day"
	UsagePeriodMonth = "month"

	UsageModePool = "pool"
	UsageModeSeat = "seat"

	UsagePlanScopeTenant = "tenant"
	UsagePlanScopeUser   = "user"

	// UsageSystemAdminPlanCode is the personal, unlimited package every
	// SuperAdmin holds (seeded by migration 000140, editable like any plan).
	// A subscription to it counts only while its holder is a SuperAdmin.
	UsageSystemAdminPlanCode = "system-admin"

	UsageSubscriptionActive    = "active"
	UsageSubscriptionCancelled = "cancelled"

	// UsageEnforceSettingKey is the system_settings switch that turns the
	// pre-flight check on. Off by default so a deployment can assign
	// subscriptions to its workspaces before anyone is refused.
	UsageEnforceSettingKey = "usage.enforce"
)

// UsageLimit caps one metric over one period. Limit <= 0 means unlimited.
type UsageLimit struct {
	Metric string `json:"metric"`
	Period string `json:"period"`
	Limit  int64  `json:"limit"`
}

// UsageLimits is the JSONB list of limits of a plan or override.
type UsageLimits []UsageLimit

// Scan implements sql.Scanner.
func (l *UsageLimits) Scan(value interface{}) error {
	if value == nil {
		*l = nil
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return errors.New("usage limits: unsupported scan source")
	}
	if len(raw) == 0 {
		*l = nil
		return nil
	}
	var out []UsageLimit
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*l = out
	return nil
}

// Value implements driver.Valuer.
func (l UsageLimits) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Validate rejects unknown metrics and periods.
func (l UsageLimits) Validate() error {
	for i, lim := range l {
		switch lim.Metric {
		case UsageMetricQuestions, UsageMetricTokens:
		default:
			return fmt.Errorf("limits[%d]: unknown metric %q", i, lim.Metric)
		}
		switch lim.Period {
		case UsagePeriodDay, UsagePeriodMonth:
		default:
			return fmt.Errorf("limits[%d]: unknown period %q", i, lim.Period)
		}
		if lim.Limit < 0 {
			return fmt.Errorf("limits[%d]: limit must be >= 0", i)
		}
	}
	return nil
}

// UsagePlan is one row of usage_plans.
type UsagePlan struct {
	ID          string      `json:"id"          gorm:"primaryKey;type:varchar(36)"`
	Code        string      `json:"code"        gorm:"type:varchar(64);uniqueIndex"`
	Name        string      `json:"name"        gorm:"type:varchar(128)"`
	Description string      `json:"description" gorm:"type:text"`
	ScopeKind   string      `json:"scope_kind"  gorm:"type:varchar(16);default:'tenant'"`
	Mode        string      `json:"mode"        gorm:"type:varchar(16);default:'pool'"`
	Limits      UsageLimits `json:"limits"      gorm:"type:jsonb"`
	Features    JSON        `json:"features"    gorm:"type:jsonb"`
	PriceLabel  string      `json:"price_label" gorm:"type:varchar(128)"`
	// PriceVND is the list price per month in Vietnamese đồng; in seat
	// mode it is per seat per month. 0 = free / not sold online.
	PriceVND  int64     `json:"price_vnd"`
	SortOrder int       `json:"sort_order"`
	IsActive  bool      `json:"is_active"   gorm:"default:true"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name.
func (UsagePlan) TableName() string { return "usage_plans" }

// UsageSubscription is one row of usage_subscriptions.
type UsageSubscription struct {
	ID             string       `json:"id"              gorm:"primaryKey;type:varchar(36)"`
	ScopeType      string       `json:"scope_type"      gorm:"type:varchar(16)"`
	ScopeID        string       `json:"scope_id"        gorm:"type:varchar(64)"`
	PlanID         string       `json:"plan_id"         gorm:"type:varchar(36)"`
	Plan           *UsagePlan   `json:"plan,omitempty"  gorm:"foreignKey:PlanID"`
	Mode           string       `json:"mode"            gorm:"type:varchar(16);default:'pool'"`
	Seats          int          `json:"seats"`
	LimitsOverride *UsageLimits `json:"limits_override" gorm:"type:jsonb"`
	StartsAt       time.Time    `json:"starts_at"`
	EndsAt         *time.Time   `json:"ends_at"`
	Status         string       `json:"status"          gorm:"type:varchar(16);default:'active'"`
	Note           string       `json:"note"            gorm:"type:text"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// TableName returns the table name.
func (UsageSubscription) TableName() string { return "usage_subscriptions" }

// EffectiveLimits are the subscription's override when set, else the plan's.
func (s *UsageSubscription) EffectiveLimits() UsageLimits {
	if s.LimitsOverride != nil {
		return *s.LimitsOverride
	}
	if s.Plan != nil {
		return s.Plan.Limits
	}
	return nil
}

// IsCurrentAt reports whether the subscription is active and inside its
// period at t.
func (s *UsageSubscription) IsCurrentAt(t time.Time) bool {
	if s == nil || s.Status != UsageSubscriptionActive {
		return false
	}
	if !s.StartsAt.IsZero() && t.Before(s.StartsAt) {
		return false
	}
	if s.EndsAt != nil && !t.Before(*s.EndsAt) {
		return false
	}
	return true
}

// UsageSeat is one row of usage_seats.
type UsageSeat struct {
	SubscriptionID string    `json:"subscription_id" gorm:"primaryKey;type:varchar(36)"`
	UserID         string    `json:"user_id"         gorm:"primaryKey;type:varchar(36)"`
	AssignedBy     string    `json:"assigned_by"     gorm:"type:varchar(36)"`
	AssignedAt     time.Time `json:"assigned_at"`
}

// TableName returns the table name.
func (UsageSeat) TableName() string { return "usage_seats" }

// UsageLimitStatus is one limit of an entitlement next to what has been
// used in the current period.
type UsageLimitStatus struct {
	Metric    string `json:"metric"`
	Period    string `json:"period"`
	PeriodKey string `json:"period_key"`
	Limit     int64  `json:"limit"`
	Used      int64  `json:"used"`
	Remaining int64  `json:"remaining"`
	Exhausted bool   `json:"exhausted"`
}

// UsageEntitlement is what governs a caller's turns right now: the
// subscription found for the workspace (source "tenant") or, failing
// that, for the person (source "user"), with its limits and consumption.
type UsageEntitlement struct {
	Source         string `json:"source"`
	SubscriptionID string `json:"subscription_id"`
	PlanID         string `json:"plan_id"`
	PlanCode       string `json:"plan_code"`
	PlanName       string `json:"plan_name"`
	Mode           string `json:"mode"`
	Seats          int    `json:"seats,omitempty"`
	// HasSeat is meaningful in seat mode only: whether the caller holds one.
	HasSeat bool `json:"has_seat"`
	// CountedScope is the scope whose counters the limits are checked
	// against ("tenant" in pool mode, "user" in seat mode or for a
	// personal subscription).
	CountedScope string             `json:"counted_scope"`
	EndsAt       *time.Time         `json:"ends_at,omitempty"`
	Expired      bool               `json:"expired"`
	Limits       []UsageLimitStatus `json:"limits"`
}

// UsageDenialReason labels why a turn was refused (error details).
const (
	UsageDenialNoSubscription = "no_subscription"
	UsageDenialExpired        = "expired"
	UsageDenialNoSeat         = "no_seat"
	UsageDenialLimit          = "limit_exhausted"
)

// UsageDenial is the details payload of the 429 returned by the guard.
type UsageDenial struct {
	Reason string `json:"reason"`
	Metric string `json:"metric,omitempty"`
	Period string `json:"period,omitempty"`
	Limit  int64  `json:"limit,omitempty"`
	Used   int64  `json:"used,omitempty"`
}

// UsageSubscriptionFilter narrows a subscription listing.
type UsageSubscriptionFilter struct {
	ScopeType string
	ScopeID   string
	PlanID    string
	Status    string
}

// UsageSubscriptionView is a subscription as the admin page shows it: with
// its plan, the name of what it covers and this month's consumption of the
// scope whose counters it governs.
type UsageSubscriptionView struct {
	*UsageSubscription
	ScopeName string             `json:"scope_name"`
	Current   bool               `json:"current"`
	Limits    []UsageLimitStatus `json:"limit_status"`
	SeatsUsed int64              `json:"seats_used"`
}

// UsageTenantOverview lists one workspace next to its subscription so the
// admin page can assign plans to workspaces that have none.
type UsageTenantOverview struct {
	TenantID     uint64                 `json:"tenant_id"`
	TenantName   string                 `json:"tenant_name"`
	Subscription *UsageSubscriptionView `json:"subscription,omitempty"`
	Month        UsagePeriodTotals      `json:"month"`
	Today        UsagePeriodTotals      `json:"today"`
}

// UsageTenantUsage is what a workspace's members see of its package: the
// subscription with consumption, and in seat mode who holds a seat.
type UsageTenantUsage struct {
	Enforced     bool                   `json:"enforced"`
	Subscription *UsageSubscriptionView `json:"subscription,omitempty"`
	Seats        []*UsageSeat           `json:"seats"`
}
