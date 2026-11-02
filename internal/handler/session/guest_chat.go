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

// publicLookupTools are the tools a Guest's turn may run: reading public
// knowledge bases and the public legal catalogue. Anything a SuperAdmin adds
// to the stored agent beyond these (document editing, sandbox, drawing...)
// is dropped in the portal.
var publicLookupTools = map[string]bool{
	"search_knowledge":             true,
	"read_document":                true,
	"list_documents":               true,
	"search_document_section":      true,
	"query_knowledge_graph":        true,
	"check_legal_status":           true,
	"find_related_legal_documents": true,
}

// publicLookupDefaultTools replace an allow-list left empty by the filter:
// an empty list would mean the runtime's default tool set.
var publicLookupDefaultTools = []string{"search_knowledge", "read_document", "list_documents"}

// enforcePublicLookupConfig returns a copy of the public-lookup agent with
// the Guest boundary applied whatever the stored row says: a SuperAdmin may
// tune its models, prompt and retrieval numbers, but a save from the agent
// editor must never widen a Guest's scope. Platform public knowledge bases
// only; no web search or fetch, MCP, skills, sandbox, domain packs, memory,
// uploads or data analysis; read-only tools.
func enforcePublicLookupConfig(agent *types.CustomAgent) *types.CustomAgent {
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
	cfg.DataAnalysisEnabled = false
	cfg.ImageUploadEnabled = false
	cfg.AudioUploadEnabled = false
	cfg.SupportedFileTypes = nil
	disabled := false
	cfg.MemoryEnabled = &disabled
	tools := make([]string, 0, len(agent.Config.AllowedTools))
	for _, t := range agent.Config.AllowedTools {
		if publicLookupTools[t] {
			tools = append(tools, t)
		}
	}
	if len(tools) == 0 {
		tools = append(tools, publicLookupDefaultTools...)
	}
	cfg.AllowedTools = tools
	enforced.Config = cfg
	return &enforced
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
