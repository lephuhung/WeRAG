package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Plan change (user decisions 03/11/2026, Fable billing UX review H2/H3/H7):
//   - An order for another plan than the scope's current one replaces it
//     when confirmed; the days the scope has paid for and not used yet are
//     credited against the order (CreditVND), valued at the price the last
//     paid order of that subscription charged.
//   - The specialist agents (add-ons) it holds are carried over only when
//     the buyer chooses to; by default those that cost nothing to carry are
//     kept and those that would cost more (added seats) are dropped. A
//     dropped add-on's unused paid days are credited too.
//   - A seat plan bought again keeps the seats already assigned on the
//     previous seat subscription of the scope, up to the seats bought.

// Credit line kinds.
const (
	UsageCreditPlan  = "plan"
	UsageCreditAddon = "addon"
)

// UsageCreditLine is one part of a plan-change credit: the unused days of
// the replaced plan, or of an add-on the order drops.
type UsageCreditLine struct {
	Kind    string `json:"kind"`
	AgentID string `json:"agent_id,omitempty"`
	Name    string `json:"name"`
	Units   int    `json:"units"`
	// Days are whole Vietnamese calendar days left after today.
	Days      int   `json:"days"`
	AmountVND int64 `json:"amount_vnd"`
	// Paid is false when no paid order is found for the time (a SuperAdmin
	// grant, a sign-up gift): such time is not credited (AmountVND 0).
	Paid bool `json:"paid"`
}

// UsageCreditLines is stored as jsonb on usage_orders.credit_lines.
type UsageCreditLines []UsageCreditLine

// Value implements driver.Valuer.
func (l UsageCreditLines) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]UsageCreditLine(l))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (l *UsageCreditLines) Scan(value interface{}) error {
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
		return fmt.Errorf("unsupported type for UsageCreditLines: %T", value)
	}
	if len(raw) == 0 {
		*l = nil
		return nil
	}
	var out []UsageCreditLine
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*l = out
	return nil
}

// MarshalJSON writes nil as [].
func (l UsageCreditLines) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]UsageCreditLine(l))
}

// Total is the sum of the lines.
func (l UsageCreditLines) Total() int64 {
	var t int64
	for _, x := range l {
		t += x.AmountVND
	}
	return t
}

// DroppedAddons is the set of add-ons the order drops.
func (l UsageCreditLines) DroppedAddons() map[string]bool {
	out := map[string]bool{}
	for _, x := range l {
		if x.Kind == UsageCreditAddon && x.AgentID != "" {
			out[x.AgentID] = true
		}
	}
	return out
}

// UsageReplacedSubscription tells the buyer which package an order replaces.
type UsageReplacedSubscription struct {
	SubscriptionID string     `json:"subscription_id"`
	PlanID         string     `json:"plan_id"`
	PlanName       string     `json:"plan_name"`
	EndsAt         *time.Time `json:"ends_at,omitempty"`
}

// UsageHeldAddonChoice is an add-on the scope holds, offered on a plan
// change: carried over (CarryVND for the seats the order adds) or dropped
// (CreditVND for its unused paid days).
type UsageHeldAddonChoice struct {
	AgentID   string     `json:"agent_id"`
	Name      string     `json:"name"`
	EndsAt    *time.Time `json:"ends_at,omitempty"`
	Kept      bool       `json:"kept"`
	CarryVND  int64      `json:"carry_vnd"`
	CreditVND int64      `json:"credit_vnd"`
	// BoughtAgain: the order buys the agent again, so it is always kept.
	BoughtAgain bool `json:"bought_again,omitempty"`
}
