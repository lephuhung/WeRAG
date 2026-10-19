package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// AgentTenantScope is the "Tổ chức áp dụng" list of a custom agent: the
// workspaces (Tổ chức) allowed to use an agent they inherit from a
// configuration profile. Empty means every workspace on the profile — the
// behaviour of every agent saved before the list existed.
//
// It is stored as a JSON array in custom_agents.applies_to_tenant_ids (jsonb
// on Postgres, JSON text in the SQLite test schema), so the visibility check
// is one SQL condition on the row itself and travels with the row wherever
// it is copied or rebound (see repository inheritAgent).
type AgentTenantScope []uint64

// Value implements driver.Valuer. A nil list is stored as [] so the column
// never holds SQL NULL.
func (s AgentTenantScope) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]uint64(s))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (s *AgentTenantScope) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("unsupported applies_to_tenant_ids value %T", value)
	}
	if len(b) == 0 {
		*s = nil
		return nil
	}
	var ids []uint64
	if err := json.Unmarshal(b, &ids); err != nil {
		return err
	}
	*s = ids
	return nil
}

// MarshalJSON answers [] rather than null for an empty list, so API clients
// see one shape for "every Tổ chức".
func (s AgentTenantScope) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]uint64(s))
}

// Normalized returns the list sorted, without zero or duplicate ids, and
// never nil, so the API always answers with an array.
func (s AgentTenantScope) Normalized() AgentTenantScope {
	out := make(AgentTenantScope, 0, len(s))
	seen := make(map[uint64]bool, len(s))
	for _, id := range s {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AppliesToTenant reports whether a workspace may use the agent. An empty
// list applies to every workspace.
func (s AgentTenantScope) AppliesToTenant(tenantID uint64) bool {
	if len(s) == 0 {
		return true
	}
	for _, id := range s {
		if id == tenantID {
			return true
		}
	}
	return false
}

// AppliesToTenant reports whether the agent may be used by a workspace.
// Built-in agents are never scoped (see AgentTenantScope).
func (a *CustomAgent) AppliesToTenant(tenantID uint64) bool {
	if a == nil {
		return false
	}
	if a.IsBuiltin || IsBuiltinAgentID(a.ID) {
		return true
	}
	return a.AppliesToTenantIDs.AppliesToTenant(tenantID)
}

// IsReservedBuiltinAgentID reports a built-in agent id: registered from
// builtin_agents.yaml, or carrying the "builtin-" prefix of one that this
// deployment does not register (custom agents get UUIDs). Chat entry points
// use it to keep their "fall back to plain chat" path for built-ins only.
func IsReservedBuiltinAgentID(id string) bool {
	return IsBuiltinAgentID(id) || strings.HasPrefix(id, "builtin-")
}
