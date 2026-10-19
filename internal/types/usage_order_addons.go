package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Order kinds.
const (
	UsageOrderKindPlan  = "plan"
	UsageOrderKindAddon = "addon"
)

// Leftover factors of a mid-term add-on purchase (decision 7.3): the part
// of a month left after the whole months costs half when it is at most
// UsageAddonHalfMonthMaxDays days, else a full month.
const (
	UsageAddonLeftoverHalf     = "half"
	UsageAddonLeftoverFull     = "full"
	UsageAddonHalfMonthMaxDays = 15
)

// UsageOrderAddonLine is one add-on of an order, priced when the order was
// made: UnitPriceVND × Units (seats on a seat plan, else 1) × Months, plus
// the leftover part of a month for a mid-term purchase.
type UsageOrderAddonLine struct {
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	UnitPriceVND int64  `json:"unit_price_vnd"`
	Units        int    `json:"units"`
	Months       int    `json:"months"`
	// LeftoverDays/LeftoverFactor: mid-term purchase only ("" none,
	// "half", "full").
	LeftoverDays   int    `json:"leftover_days,omitempty"`
	LeftoverFactor string `json:"leftover_factor,omitempty"`
	AmountVND      int64  `json:"amount_vnd"`
	// PaidUntil: mid-term purchase only — the subscription end it was
	// priced to (the right ends there even if the subscription is extended
	// before the order is paid).
	PaidUntil *time.Time `json:"paid_until,omitempty"`
}

// UsageOrderAddonLines is the addons jsonb of usage_orders.
type UsageOrderAddonLines []UsageOrderAddonLine

// Value implements driver.Valuer; nil is stored as [].
func (l UsageOrderAddonLines) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]UsageOrderAddonLine(l))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (l *UsageOrderAddonLines) Scan(value interface{}) error {
	var raw []byte
	switch v := value.(type) {
	case nil:
		*l = nil
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("unsupported addons value %T", value)
	}
	if len(raw) == 0 {
		*l = nil
		return nil
	}
	var lines []UsageOrderAddonLine
	if err := json.Unmarshal(raw, &lines); err != nil {
		return err
	}
	*l = lines
	return nil
}

// MarshalJSON answers [] for no lines.
func (l UsageOrderAddonLines) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]UsageOrderAddonLine(l))
}

// Total is the sum of the lines.
func (l UsageOrderAddonLines) Total() int64 {
	var t int64
	for _, x := range l {
		t += x.AmountVND
	}
	return t
}

// PriceAddonRemainder prices one add-on bought on a running subscription
// from `from` to `until` (decision 7.3): every whole month left at the
// full price; the rest of a month at half price when it is at most 15
// days, else at the full price. Days are counted up (a started day counts).
// unit is the monthly price for one unit; units multiplies (seats).
func PriceAddonRemainder(unit int64, units int, from, until time.Time) (months, leftoverDays int, factor string, amount int64) {
	if units < 1 {
		units = 1
	}
	if !until.After(from) {
		return 0, 0, "", 0
	}
	for !from.AddDate(0, months+1, 0).After(until) {
		months++
	}
	rest := until.Sub(from.AddDate(0, months, 0))
	if rest > 0 {
		leftoverDays = int((rest + 24*time.Hour - 1) / (24 * time.Hour))
	}
	monthly := unit * int64(units)
	amount = monthly * int64(months)
	switch {
	case leftoverDays == 0:
	case leftoverDays <= UsageAddonHalfMonthMaxDays:
		factor = UsageAddonLeftoverHalf
		amount += (monthly + 1) / 2 // a half đồng is not given away
	default:
		factor = UsageAddonLeftoverFull
		amount += monthly
	}
	return months, leftoverDays, factor, amount
}

// UsageScopeAddons answers GET /billing/subscription: the scope's current
// subscription, the add-ons it holds and those it may still buy on it.
type UsageScopeAddons struct {
	Subscription *UsageSubscription        `json:"subscription,omitempty"`
	Held         []*UsageSubscriptionAddon `json:"held"`
	Purchasable  []UsagePlanAddonOffer     `json:"purchasable"`
}
