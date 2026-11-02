package tools

import (
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// PlatformPublicKBIDs keeps the platform public knowledge bases (owner 0,
// visibility public) that satisfy an agent's tool requirements: the whole
// scope of KBSelectionModePublic. The chat service resolves the scope with
// it and the chat handler refuses a Guest's turn when it comes out empty,
// so both read the same rule.
func PlatformPublicKBIDs(kbs []*types.KnowledgeBase, agentMode string, allowedTools []string) []string {
	capFilter := DeriveKBFilterForAgent(agentMode, allowedTools)
	kbIDs := make([]string, 0, len(kbs))
	for _, kb := range kbs {
		if !access.IsPlatformPublicKB(kb) {
			continue
		}
		if !capFilter.IsEmpty() && !KBSatisfiesAgentRequirements(kb.Capabilities(), agentMode, allowedTools) {
			continue
		}
		kbIDs = append(kbIDs, kb.ID)
	}
	return kbIDs
}
