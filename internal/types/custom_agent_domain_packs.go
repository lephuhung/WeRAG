package types

import (
	"fmt"
	"slices"
	"strings"
)

// domainPackSearchTool is tools.ToolSearchKnowledge (types cannot import the
// tools package). It is stripped at run time when the agent has no
// knowledge base, so it only counts when kb_selection_mode is not "none".
const domainPackSearchTool = "search_knowledge"

// DomainPackLookup returns a shipped pack's required tools, ok=false when no
// pack of that name ships in this build.
type DomainPackLookup func(name string) (requiredTools []string, ok bool)

// ValidateDomainPacks checks the agent's domain_packs at save time
// (docs/domain-pack-standard.md §6): every name ships and appears once, the
// agent is smart-reasoning, and its effective tools — allowed_tools, or
// defaultTools when that is empty — include each pack's required tools.
// The run-time check repeats this against the tools a turn really
// registered; this one makes the mistake visible where it can be fixed.
func (c *CustomAgentConfig) ValidateDomainPacks(lookup DomainPackLookup, defaultTools []string) error {
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
			if tool == domainPackSearchTool && c.KBSelectionMode == "none" {
				return fmt.Errorf("gói chuyên môn %q cần tìm trong kho tri thức; hãy chọn kho tri thức cho trợ lý", name)
			}
		}
	}
	return nil
}
