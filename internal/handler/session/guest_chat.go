package session

import (
	"context"
	"slices"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// portalAgentDisabledMessage answers a Guest's turn while a SuperAdmin has
// switched the agent off: a built-in that does not resolve would otherwise
// fall back to plain chat outside the Guest's knowledge scope.
const portalAgentDisabledMessage = "Trợ lý này đang tạm tắt. " +
	"Hãy thử lại sau, hoặc chọn trợ lý khác để tiếp tục."

// restrictPublicPortalRequest drops the choices a Guest of the public
// portal may not make: Tổ chức knowledge bases, documents and tags (and the
// @mentions naming them), MCP, skills, web search, the local browser and
// the model. The Guest's own material stays: images, attachment uploads and
// ids, the editor selection and a template request. The agent is the
// Guest's pick (smart reasoning, document assistant, or a bought add-on);
// none, or the retired public-lookup agent of an old session, runs as
// smart reasoning. An unknown id is left for the ordinary unavailable-agent
// refusal. It runs only in the public portal (types.InPublicPortal).
func restrictPublicPortalRequest(ctx context.Context, request *CreateKnowledgeQARequest) {
	if request.AgentID == "" || request.AgentID == types.BuiltinPublicLookupID {
		if request.AgentID != "" {
			logger.Infof(ctx, "Public portal: retired agent %s runs as %s", request.AgentID, types.BuiltinSmartReasoningID)
		}
		request.AgentID = types.BuiltinSmartReasoningID
	}
	request.AgentEnabled = true
	request.KnowledgeBaseIDs = nil
	request.KnowledgeIds = nil
	request.MentionedItems = portalMentions(request.MentionedItems)
	request.TagIDs = nil
	request.MCPServiceIDs = nil
	request.SkillNames = nil
	request.WebSearchEnabled = false
	request.LocalBrowserEnabled = false
	request.SummaryModelID = ""
}

// portalMentions keeps only the @mentions of the session's own editable
// documents ("document"): a knowledge base, file or tag mention would name
// Tổ chức knowledge (a Guest reads platform public bases through the
// agent's public scope), an MCP or skill mention a capability a Guest does
// not get.
func portalMentions(items []MentionedItemRequest) []MentionedItemRequest {
	if len(items) == 0 {
		return nil
	}
	kept := slices.DeleteFunc(slices.Clone(items), func(m MentionedItemRequest) bool {
		return m.Type != "document"
	})
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// enforcePortalAgentConfig returns a copy of agent with the Guest boundary
// applied whatever the stored row says: platform public knowledge bases
// only (never an agent's own, a Tổ chức's, published or invited one), no web
// search or fetch, MCP, skills, sandbox or domain packs. Tools, file types,
// image upload, memory and data analysis are the agent's own: what limits a
// Guest is the usage plan, not the agent.
func enforcePortalAgentConfig(agent *types.CustomAgent) *types.CustomAgent {
	if agent == nil {
		return nil
	}
	enforced := *agent
	cfg := agent.Config
	cfg.KBSelectionMode = types.KBSelectionModePublic
	cfg.KnowledgeBases = nil
	cfg.RetrieveKBOnlyWhenMentioned = false
	cfg.WebSearchEnabled = false
	cfg.WebSearchDefaultOn = false
	cfg.WebSearchProviderID = ""
	cfg.WebFetchEnabled = false
	cfg.MCPSelectionMode = "none"
	cfg.MCPServices = nil
	cfg.IllustrationMCPServiceID = ""
	cfg.SkillsSelectionMode = "none"
	cfg.SelectedSkills = nil
	cfg.SandboxConfigID = ""
	cfg.DomainPacks = nil
	enforced.Config = cfg
	return &enforced
}

// portalAgentUnavailable refuses a Guest's turn when the agent resolved but
// is switched off, or a built-in did not resolve: outside the portal such a
// built-in answers as plain chat, which would leave the Guest's scope.
func portalAgentUnavailable(agent *types.CustomAgent) error {
	if agent == nil || !agent.EffectiveEnabled() {
		return errors.NewPublicLookupUnavailableError(errors.PublicLookupReasonOff, portalAgentDisabledMessage)
	}
	return nil
}
