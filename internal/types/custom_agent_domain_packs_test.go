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
		case "reader":
			return []string{"read_document"}, true
		}
		return nil, false
	}
	defaults := []string{"search_knowledge", "read_document"}
	scoped := []string{"search_knowledge", "read_document", "list_documents"}
	valid := func() *CustomAgentConfig {
		return &CustomAgentConfig{
			AgentMode:       AgentModeSmartReasoning,
			KBSelectionMode: "selected",
			KnowledgeBases:  []string{"kb-1"},
			AllowedTools:    []string{"search_knowledge", "compare_tax_methods"},
			DomainPacks:     []string{"opc"},
		}
	}
	require.NoError(t, valid().ValidateDomainPacks(lookup, defaults, scoped))
	require.NoError(t, (&CustomAgentConfig{}).ValidateDomainPacks(lookup, defaults, scoped), "no packs, nothing to check")

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
		"selected without a base": {func(c *CustomAgentConfig) { c.KnowledgeBases = nil },
			`cần công cụ "search_knowledge", công cụ này chỉ chạy khi trợ lý có kho tri thức`},
		"other knowledge tool without a base": {func(c *CustomAgentConfig) {
			c.KBSelectionMode = "none"
			c.DomainPacks = []string{"reader"}
			c.AllowedTools = []string{"read_document"}
		}, `"read_document"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := valid()
			tc.mutate(cfg)
			err := cfg.ValidateDomainPacks(lookup, defaults, scoped)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	// empty allowed_tools falls back to the defaults
	plain := &CustomAgentConfig{AgentMode: AgentModeSmartReasoning, DomainPacks: []string{"plain"}}
	require.NoError(t, plain.ValidateDomainPacks(lookup, defaults, scoped))

	// "all" cannot be checked against the organization's bases at save time
	all := valid()
	all.KBSelectionMode = "all"
	all.KnowledgeBases = nil
	require.NoError(t, all.ValidateDomainPacks(lookup, defaults, scoped))
}
