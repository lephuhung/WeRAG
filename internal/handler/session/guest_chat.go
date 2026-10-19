package session

import (
	"context"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// publicLookupDisabledMessage answers a Guest's turn while a SuperAdmin has
// switched the public-lookup agent off: there is no other agent to run.
const publicLookupDisabledMessage = "Tra cứu văn bản công khai đang tạm tắt. " +
	"Hãy thử lại sau, hoặc tham gia một Tổ chức để dùng trợ lý đầy đủ."

// restrictPublicPortalRequest turns a Guest's chat request into a public
// lookup: the public-lookup agent whatever the client named, and none of
// the choices a Guest may not make (knowledge bases, @mentions, files,
// images, MCP, skills, web search, local browser, model, documents). The
// public-lookup agent's "public" knowledge scope then decides what is read.
// It runs only in the public portal (types.InPublicPortal).
func restrictPublicPortalRequest(ctx context.Context, request *CreateKnowledgeQARequest) {
	if request.AgentID != "" && request.AgentID != types.BuiltinPublicLookupID {
		logger.Infof(ctx, "Public portal: agent %q replaced by %s", request.AgentID, types.BuiltinPublicLookupID)
	}
	request.AgentID = types.BuiltinPublicLookupID
	request.AgentEnabled = true
	request.KnowledgeBaseIDs = nil
	request.KnowledgeIds = nil
	request.MentionedItems = nil
	request.TagIDs = nil
	request.MCPServiceIDs = nil
	request.SkillNames = nil
	request.WebSearchEnabled = false
	request.LocalBrowserEnabled = false
	request.SummaryModelID = ""
	request.Images = nil
	request.AttachmentUploads = nil
	request.AttachmentIDs = nil
	request.DocumentSelection = nil
	request.DocumentTemplateRequest = nil
}

// publicLookupUnavailable refuses a Guest's turn when the public-lookup
// agent is switched off or did not resolve: a built-in that does not
// resolve elsewhere falls back to plain chat, which a Guest must not get.
func publicLookupUnavailable(agent *types.CustomAgent) error {
	if agent == nil || agent.ID != types.BuiltinPublicLookupID || !agent.EffectiveEnabled() {
		return errors.NewAgentUnavailableError(publicLookupDisabledMessage)
	}
	return nil
}
