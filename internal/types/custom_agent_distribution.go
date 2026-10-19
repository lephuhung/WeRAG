package types

import (
	"strings"
	"time"
)

// Agent distribution ("Cách phân phối", docs/agent-addon-plans.md §3): who
// may use a custom agent inherited from a configuration profile.
const (
	// AgentDistributionBasic ("Cơ bản"): every Tổ chức. The value of every
	// agent saved before the column existed, so nothing changed for them.
	AgentDistributionBasic = "basic"
	// AgentDistributionAddon ("Chuyên ngành – bán kèm"): only a Tổ chức or a
	// person whose current subscription holds a current add-on row for the
	// agent (usage_subscription_addons). Default for a new agent with a
	// skill set, so creating one never hands it to every Tổ chức.
	AgentDistributionAddon = "addon"
	// AgentDistributionInternal ("Nội bộ"): the Tổ chức in the agent's
	// "Tổ chức áp dụng" list; an empty list = every Tổ chức (the pre-addon
	// behaviour of the list).
	AgentDistributionInternal = "internal"
)

// IsValidAgentDistribution reports a known distribution value.
func IsValidAgentDistribution(d string) bool {
	switch d {
	case AgentDistributionBasic, AgentDistributionAddon, AgentDistributionInternal:
		return true
	}
	return false
}

// NormalizeAgentDistribution trims and lower-cases a distribution value;
// "" stays "" (not sent).
func NormalizeAgentDistribution(d string) string {
	return strings.ToLower(strings.TrimSpace(d))
}

// DefaultAgentDistribution is the distribution of a new custom agent whose
// request named none: "internal" when it carries a "Tổ chức áp dụng" list
// (the list only means something there, and API clients written before
// distribution existed send just the list), "addon" when it has a skill set
// (domain packs), else "basic".
func DefaultAgentDistribution(a *CustomAgent) string {
	switch {
	case a == nil:
		return AgentDistributionBasic
	case len(a.AppliesToTenantIDs.Normalized()) > 0:
		return AgentDistributionInternal
	case len(a.Config.DomainPacks) > 0:
		return AgentDistributionAddon
	default:
		return AgentDistributionBasic
	}
}

// EffectiveDistribution is the agent's distribution for readers: built-ins
// are always basic, and a row read without the column (a stub, an old test
// schema) counts as basic like the column default.
func (a *CustomAgent) EffectiveDistribution() string {
	if a == nil || a.IsBuiltin || IsBuiltinAgentID(a.ID) {
		return AgentDistributionBasic
	}
	if d := NormalizeAgentDistribution(a.Distribution); IsValidAgentDistribution(d) {
		return d
	}
	return AgentDistributionBasic
}

// AgentViewer is who an agent lookup answers for: the Tổ chức of the call
// and, for a human, the person (whose personal subscription can hold
// add-ons). API keys, IM bots, embed widgets and MCP endpoints carry no
// person: they see what their Tổ chức sees.
type AgentViewer struct {
	TenantID uint64
	// UserID is the human caller; "" for machine principals.
	UserID string
	// AllAddons shows "addon" agents without a purchase (see
	// service.addonsVisibleWithoutPurchase, decision 7.4: never today).
	AllAddons bool
	// Now is the instant subscriptions and add-ons are checked at; zero =
	// time.Now().
	Now time.Time
}

// At returns the instant the viewer checks validity at.
func (v AgentViewer) At() time.Time {
	if v.Now.IsZero() {
		return time.Now()
	}
	return v.Now
}

// UsageSubscriptionAddon is one row of usage_subscription_addons: the right
// of a subscription (an organization's or a person's) to use one "addon"
// agent. It counts while the subscription is current and now is inside
// [StartsAt, EndsAt); a nil EndsAt follows the subscription's own end.
type UsageSubscriptionAddon struct {
	SubscriptionID string     `json:"subscription_id" gorm:"primaryKey;type:varchar(36)"`
	AgentID        string     `json:"agent_id"        gorm:"primaryKey;type:varchar(36)"`
	StartsAt       time.Time  `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at"`
	// PriceVND is the monthly price agreed for this add-on (0 = granted
	// free, e.g. a SuperAdmin-negotiated deal).
	PriceVND int64 `json:"price_vnd"`
	// OrderID is the order that bought it; nil for a manual grant.
	OrderID   *string   `json:"order_id"`
	GrantedBy string    `json:"granted_by"      gorm:"type:varchar(36)"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// AgentName is filled by the listing for the admin page; not stored.
	AgentName string `json:"agent_name,omitempty" gorm:"-"`
}

// TableName returns the table name.
func (UsageSubscriptionAddon) TableName() string { return "usage_subscription_addons" }

// IsCurrentAt reports whether the add-on row itself is inside its period at
// t (the subscription is checked separately).
func (a *UsageSubscriptionAddon) IsCurrentAt(t time.Time) bool {
	if a == nil {
		return false
	}
	if !a.StartsAt.IsZero() && t.Before(a.StartsAt) {
		return false
	}
	return a.EndsAt == nil || t.Before(*a.EndsAt)
}

// UsageSubscriptionAddonGrant is the body of a manual add-on grant
// (POST /system/admin/usage/subscriptions/{id}/addons).
type UsageSubscriptionAddonGrant struct {
	AgentID string `json:"agent_id" binding:"required"`
	// StartsAt defaults to now.
	StartsAt *time.Time `json:"starts_at"`
	// EndsAt nil = until the subscription ends.
	EndsAt *time.Time `json:"ends_at"`
	// PriceVND is the agreed monthly price (0 = free).
	PriceVND int64 `json:"price_vnd"`
}

// EffectiveEnabled is the agent's on/off state: the SuperAdmin's stored
// switch when there is one, else builtin_agents.yaml `enabled` for a
// built-in, else on. A switched-off built-in is left out of every picker but
// stays resolvable by id; a switched-off custom agent is hidden from
// everyone but a SuperAdmin (the visibility rule), so its sessions get 2104.
func (a *CustomAgent) EffectiveEnabled() bool {
	if a == nil {
		return false
	}
	if a.Switch != nil {
		return *a.Switch
	}
	if a.IsBuiltin || IsBuiltinAgentID(a.ID) {
		return IsBuiltinAgentEnabled(a.ID)
	}
	return true
}

// MergeSubscriptionAddon is the never-shorten rule for writing a right that
// may already exist: the later end wins (nil = "with the subscription"
// counts as the latest), the earlier start is kept, and price, order and
// grantor come from the new write. A right is only ever shortened on
// purpose (PinSubscriptionAddonEnd) or revoked.
func MergeSubscriptionAddon(existing, incoming *UsageSubscriptionAddon) *UsageSubscriptionAddon {
	if existing == nil {
		return incoming
	}
	out := *incoming
	out.CreatedAt = existing.CreatedAt
	if !existing.StartsAt.IsZero() && (out.StartsAt.IsZero() || existing.StartsAt.Before(out.StartsAt)) {
		out.StartsAt = existing.StartsAt
	}
	switch {
	case existing.EndsAt == nil || incoming.EndsAt == nil:
		out.EndsAt = nil
	case existing.EndsAt.After(*incoming.EndsAt):
		end := *existing.EndsAt
		out.EndsAt = &end
	}
	return &out
}

// CoversUntil reports whether a right still runs at now and at least to
// end (a nil end follows the subscription and covers any end within it).
func (a *UsageSubscriptionAddon) CoversUntil(now, end time.Time) bool {
	if a == nil || !a.IsCurrentAt(now) {
		return false
	}
	return a.EndsAt == nil || !a.EndsAt.Before(end)
}
