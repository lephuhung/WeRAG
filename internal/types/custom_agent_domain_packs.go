package types

import (
	"fmt"
	"slices"
	"strings"
)

// DomainPackLookup returns a shipped pack's required tools, ok=false when no
// pack of that name ships in this build.
type DomainPackLookup func(name string) (requiredTools []string, ok bool)

// ValidateDomainPacks checks the agent's domain_packs at save time
// (docs/domain-pack-standard.md §6): every name ships and appears once, the
// agent is smart-reasoning, and its effective tools — allowed_tools
// (normalized by the caller), or defaultTools when that is empty — include
// each pack's required tools. knowledgeScoped are the tools a turn without
// a knowledge base loses (tools.KnowledgeScopedTools; types cannot import
// tools): an agent with no knowledge base does not count them.
// The run-time check repeats this against the tools a turn really
// registered; this one makes the mistake visible where it can be fixed.
func (c *CustomAgentConfig) ValidateDomainPacks(lookup DomainPackLookup, defaultTools, knowledgeScoped []string) error {
	if c == nil || len(c.DomainPacks) == 0 {
		return nil
	}
	if c.AgentMode != AgentModeSmartReasoning {
		return fmt.Errorf("gói chuyên môn chỉ dùng được cho trợ lý suy luận (smart-reasoning)")
	}
	effective := c.AllowedTools
	if len(effective) == 0 {
		effective = defaultTools
	}
	// "selected" with no base is as empty as "none". "all" over an
	// organization that has no knowledge base yet cannot be told here; the
	// run-time check drops the pack then.
	noKnowledge := c.KBSelectionMode == "none" || (c.KBSelectionMode == "selected" && len(c.KnowledgeBases) == 0)
	seen := map[string]bool{}
	for _, raw := range c.DomainPacks {
		name := strings.TrimSpace(raw)
		if name == "" {
			return fmt.Errorf("tên gói chuyên môn không được để trống")
		}
		if seen[name] {
			return fmt.Errorf("gói chuyên môn %q được chọn hai lần", name)
		}
		seen[name] = true
		required, ok := lookup(name)
		if !ok {
			return fmt.Errorf("gói chuyên môn %q không tồn tại trong phiên bản này", name)
		}
		for _, tool := range required {
			if !slices.Contains(effective, tool) {
				return fmt.Errorf("gói chuyên môn %q cần công cụ %q; hãy bật công cụ này cho trợ lý", name, tool)
			}
			if noKnowledge && slices.Contains(knowledgeScoped, tool) {
				return fmt.Errorf("gói chuyên môn %q cần công cụ %q, công cụ này chỉ chạy khi trợ lý có kho tri thức; hãy chọn kho tri thức cho trợ lý", name, tool)
			}
		}
	}
	return nil
}
