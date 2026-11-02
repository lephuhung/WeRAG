package types

import (
	"encoding/json"
	"testing"
	"time"

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
	scoped := &CustomAgent{ID: "a", Distribution: AgentDistributionInternal, AppliesToTenantIDs: AgentTenantScope{1, 3}}
	require.True(t, scoped.AppliesToTenant(3))
	require.False(t, scoped.AppliesToTenant(2))
	// the list only counts for internal; an addon agent's purchase is
	// subscription data the repository checks, not this in-memory test
	scoped.Distribution = AgentDistributionBasic
	require.True(t, scoped.AppliesToTenant(2))
	scoped.Distribution = AgentDistributionAddon
	require.True(t, scoped.AppliesToTenant(2))
	require.True(t, (&CustomAgent{ID: "x", IsBuiltin: true, AppliesToTenantIDs: AgentTenantScope{1}}).AppliesToTenant(2),
		"built-ins are never scoped")
	require.False(t, (*CustomAgent)(nil).AppliesToTenant(1))
	require.True(t, IsReservedBuiltinAgentID("builtin-anything"))
	require.False(t, IsReservedBuiltinAgentID("6f1c0c3e-0000-4000-8000-000000000000"))
}

func TestMCPEndpointAgentAllowedChecksTenantScope(t *testing.T) {
	agent := &CustomAgent{ID: "group", TenantID: 2, Distribution: AgentDistributionInternal, AppliesToTenantIDs: AgentTenantScope{1}}
	require.False(t, MCPEndpointAgentAllowed(agent, 2))
	agent.AppliesToTenantIDs = AgentTenantScope{2}
	require.True(t, MCPEndpointAgentAllowed(agent, 2))
}

func TestAgentDistributionDefaults(t *testing.T) {
	require.Equal(t, AgentDistributionBasic, DefaultAgentDistribution(&CustomAgent{}))
	require.Equal(t, AgentDistributionAddon, DefaultAgentDistribution(&CustomAgent{
		Config: CustomAgentConfig{DomainPacks: []string{"thue-hkd"}}}))
	require.Equal(t, AgentDistributionInternal, DefaultAgentDistribution(&CustomAgent{
		AppliesToTenantIDs: AgentTenantScope{3}, Config: CustomAgentConfig{DomainPacks: []string{"thue-hkd"}}}),
		"a list sent without a distribution keeps meaning what it meant before")
	require.Equal(t, AgentDistributionBasic, (&CustomAgent{ID: BuiltinSmartReasoningID, IsBuiltin: true, Distribution: "addon"}).EffectiveDistribution())
	require.Equal(t, AgentDistributionBasic, (&CustomAgent{ID: "x"}).EffectiveDistribution())
	require.Equal(t, AgentDistributionAddon, (&CustomAgent{ID: "x", Distribution: " Addon "}).EffectiveDistribution())
	require.False(t, IsValidAgentDistribution("everyone"))
}

func TestUsageSubscriptionAddonIsCurrentAt(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	a := &UsageSubscriptionAddon{StartsAt: start, EndsAt: &end}
	require.False(t, a.IsCurrentAt(start.Add(-time.Second)))
	require.True(t, a.IsCurrentAt(start))
	require.False(t, a.IsCurrentAt(end), "ends_at is exclusive")
	a.EndsAt = nil
	require.True(t, a.IsCurrentAt(end.AddDate(1, 0, 0)), "no end = follows the subscription")
}
