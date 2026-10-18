package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDomainPacks(t *testing.T) {
	lookup := func(name string) ([]string, bool) {
		switch name {
		case "opc":
			return []string{"search_knowledge", "compare_tax_methods"}, true
		case "plain":
			return nil, true
		}
		return nil, false
	}
	defaults := []string{"search_knowledge", "read_document"}
	valid := func() *CustomAgentConfig {
		return &CustomAgentConfig{
			AgentMode:       AgentModeSmartReasoning,
			KBSelectionMode: "selected",
			AllowedTools:    []string{"search_knowledge", "compare_tax_methods"},
			DomainPacks:     []string{"opc"},
		}
	}
	require.NoError(t, valid().ValidateDomainPacks(lookup, defaults))
	require.NoError(t, (&CustomAgentConfig{}).ValidateDomainPacks(lookup, defaults), "no packs, nothing to check")

	cases := map[string]struct {
		mutate func(c *CustomAgentConfig)
		want   string
	}{
		"unknown name":   {func(c *CustomAgentConfig) { c.DomainPacks = []string{"opc", "gone"} }, `"gone" không tồn tại`},
		"duplicate name": {func(c *CustomAgentConfig) { c.DomainPacks = []string{"opc", " opc"} }, "hai lần"},
		"quick answer":   {func(c *CustomAgentConfig) { c.AgentMode = AgentModeQuickAnswer }, "smart-reasoning"},
		"missing tool": {func(c *CustomAgentConfig) { c.AllowedTools = []string{"search_knowledge"} },
			`cần công cụ "compare_tax_methods"`},
		"defaults lack tool": {func(c *CustomAgentConfig) { c.AllowedTools = nil }, `"compare_tax_methods"`},
		"no knowledge base":  {func(c *CustomAgentConfig) { c.KBSelectionMode = "none" }, "kho tri thức"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := valid()
			tc.mutate(cfg)
			err := cfg.ValidateDomainPacks(lookup, defaults)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	// empty allowed_tools falls back to the defaults
	plain := &CustomAgentConfig{AgentMode: AgentModeSmartReasoning, DomainPacks: []string{"plain"}}
	require.NoError(t, plain.ValidateDomainPacks(lookup, defaults))
}
