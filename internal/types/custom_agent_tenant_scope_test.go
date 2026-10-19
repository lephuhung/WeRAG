package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentTenantScopeRoundTrip(t *testing.T) {
	v, err := AgentTenantScope(nil).Value()
	require.NoError(t, err)
	require.Equal(t, "[]", v, "nil is stored as [] so the column never holds NULL")

	var s AgentTenantScope
	require.NoError(t, s.Scan([]byte("[3,1]")))
	require.Equal(t, AgentTenantScope{3, 1}, s)
	require.NoError(t, s.Scan(nil))
	require.Nil(t, s)
	require.Error(t, s.Scan(42))

	b, err := json.Marshal(struct {
		S AgentTenantScope `json:"s"`
	}{})
	require.NoError(t, err)
	require.JSONEq(t, `{"s":[]}`, string(b))

	require.Equal(t, AgentTenantScope{1, 3}, AgentTenantScope{3, 0, 1, 3}.Normalized())
	require.NotNil(t, AgentTenantScope(nil).Normalized())
}

func TestCustomAgentAppliesToTenant(t *testing.T) {
	require.True(t, (&CustomAgent{ID: "a"}).AppliesToTenant(5), "empty list = every Tổ chức")
	scoped := &CustomAgent{ID: "a", AppliesToTenantIDs: AgentTenantScope{1, 3}}
	require.True(t, scoped.AppliesToTenant(3))
	require.False(t, scoped.AppliesToTenant(2))
	require.True(t, (&CustomAgent{ID: "x", IsBuiltin: true, AppliesToTenantIDs: AgentTenantScope{1}}).AppliesToTenant(2),
		"built-ins are never scoped")
	require.False(t, (*CustomAgent)(nil).AppliesToTenant(1))
	require.True(t, IsReservedBuiltinAgentID("builtin-anything"))
	require.False(t, IsReservedBuiltinAgentID("6f1c0c3e-0000-4000-8000-000000000000"))
}

func TestMCPEndpointAgentAllowedChecksTenantScope(t *testing.T) {
	agent := &CustomAgent{ID: "group", TenantID: 2, AppliesToTenantIDs: AgentTenantScope{1}}
	require.False(t, MCPEndpointAgentAllowed(agent, 2))
	agent.AppliesToTenantIDs = AgentTenantScope{2}
	require.True(t, MCPEndpointAgentAllowed(agent, 2))
}
