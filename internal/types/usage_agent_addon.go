package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// UsageIncludedAgentIDs are the "agent cơ bản" every service plan includes
// (docs/agent-addon-plans.md decision 7.1): the document assistant and the
// legal Q&A (smart reasoning) built-ins. Listed on the plan cards; they are
// basic, so visibility needs nothing more.
var UsageIncludedAgentIDs = []string{BuiltinDocumentAssistantID, BuiltinSmartReasoningID}

// UsagePlanIDList is the plan_ids jsonb of a catalogue row: the service
// plans an add-on is sold with; empty = every plan.
type UsagePlanIDList []string

// Value implements driver.Valuer; nil is stored as [].
func (l UsagePlanIDList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(l))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (l *UsagePlanIDList) Scan(value interface{}) error {
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
		return fmt.Errorf("unsupported plan_ids value %T", value)
	}
	if len(raw) == 0 {
		*l = nil
		return nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return err
	}
	*l = ids
	return nil
}

// MarshalJSON answers [] for an empty list.
func (l UsagePlanIDList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(l))
}

// Normalized trims, drops blanks and duplicates, sorts; never nil.
func (l UsagePlanIDList) Normalized() UsagePlanIDList {
	out := make(UsagePlanIDList, 0, len(l))
	seen := map[string]bool{}
	for _, id := range l {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Includes reports whether the add-on is sold with planID (empty = all).
func (l UsagePlanIDList) Includes(planID string) bool {
	if len(l) == 0 {
		return true
	}
	for _, id := range l {
		if id == planID {
			return true
		}
	}
	return false
}

// UsageAgentAddon is one row of usage_agent_addons, the catalogue of
// specialist ("addon") agents sold with service plans. Runtime data a
// SuperAdmin edits; nothing is seeded.
type UsageAgentAddon struct {
	AgentID string `json:"agent_id" gorm:"primaryKey;type:varchar(36)"`
	// PriceVND per month: for a seat-mode plan per seat per month.
	PriceVND int64 `json:"price_vnd"`
	// PlanIDs are the plans it is sold with; [] = every plan.
	PlanIDs     UsagePlanIDList `json:"plan_ids" gorm:"type:jsonb"`
	Description string          `json:"description" gorm:"type:text"`
	IsActive    bool            `json:"is_active"`
	SortOrder   int             `json:"sort_order"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`

	// Filled by listings, not stored: the agent's name, whether it is still
	// a live addon agent (deleted or converted agents are never sold), and
	// how many current subscriptions hold it.
	AgentName      string `json:"agent_name,omitempty" gorm:"-"`
	AgentAvailable bool   `json:"agent_available" gorm:"-"`
	Holders        int64  `json:"holders" gorm:"-"`
}

// TableName returns the table name.
func (UsageAgentAddon) TableName() string { return "usage_agent_addons" }

// UsagePlanAgent names an agent on a plan card.
type UsagePlanAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UsagePlanAddonOffer is a specialist agent a buyer may add to a plan.
type UsagePlanAddonOffer struct {
	AgentID     string `json:"agent_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// PriceVND per month; × seats when PerSeat.
	PriceVND int64 `json:"price_vnd"`
	PerSeat  bool  `json:"per_seat"`
}

// UsagePlanOffer is a plan as GET /billing/plans answers it: the plan's own
// fields, the basic agents it includes and the add-ons sold with it.
type UsagePlanOffer struct {
	*UsagePlan
	IncludedAgents []UsagePlanAgent      `json:"included_agents"`
	Addons         []UsagePlanAddonOffer `json:"addons"`
}

// UsageAgentAddonCatalogue is the admin listing: the catalogue rows and the
// basic agents every plan includes (for the plan cards).
type UsageAgentAddonCatalogue struct {
	Addons         []*UsageAgentAddon `json:"addons"`
	IncludedAgents []UsagePlanAgent   `json:"included_agents"`
}
