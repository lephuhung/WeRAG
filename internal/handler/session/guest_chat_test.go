package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// A Guest's request keeps the agent they picked and their own material
// (images, uploads, documents, the editor selection, a template request);
// Tổ chức knowledge, MCP, skills, web, the local browser and the model go.
func TestRestrictPublicPortalRequest(t *testing.T) {
	tpl := &types.DocumentTemplateRequest{}
	req := &CreateKnowledgeQARequest{
		Query:            "Mức phạt vượt đèn đỏ?",
		AgentID:          types.BuiltinDocumentAssistantID,
		KnowledgeBaseIDs: []string{"kb-tenant"},
		KnowledgeIds:     []string{"doc-1"},
		MentionedItems: []MentionedItemRequest{
			{ID: "kb-invited", Type: "kb"}, {ID: "file-1", Type: "file"}, {ID: "tag-1", Type: "tag"},
			{ID: "mcp-1", Type: "mcp"}, {ID: "skill-1", Type: "skill"}, {ID: "ws-1", Type: "document"},
		},
		TagIDs:                  []string{"tag-1"},
		MCPServiceIDs:           []string{"mcp-1"},
		SkillNames:              []string{"skill-1"},
		WebSearchEnabled:        true,
		LocalBrowserEnabled:     true,
		SummaryModelID:          "model-x",
		Images:                  []ImageAttachment{{Data: "x"}},
		AttachmentUploads:       []AttachmentUpload{{Data: "x", FileName: "a.docx"}},
		AttachmentIDs:           []string{"att-1"},
		DocumentSelection:       &types.DocumentSelection{Text: "đoạn"},
		DocumentTemplateRequest: tpl,
		Channel:                 "word_addin",
	}
	restrictPublicPortalRequest(context.Background(), req)
	require.Equal(t, "Mức phạt vượt đèn đỏ?", req.Query)
	require.Equal(t, "word_addin", req.Channel)
	require.Equal(t, types.BuiltinDocumentAssistantID, req.AgentID)
	require.True(t, req.AgentEnabled)
	require.Nil(t, req.KnowledgeBaseIDs)
	require.Nil(t, req.KnowledgeIds)
	require.Equal(t, []MentionedItemRequest{{ID: "ws-1", Type: "document"}}, req.MentionedItems)
	require.Nil(t, req.TagIDs)
	require.Nil(t, req.MCPServiceIDs)
	require.Nil(t, req.SkillNames)
	require.False(t, req.WebSearchEnabled)
	require.False(t, req.LocalBrowserEnabled)
	require.Empty(t, req.SummaryModelID)
	require.Len(t, req.Images, 1)
	require.Len(t, req.AttachmentUploads, 1)
	require.Equal(t, []string{"att-1"}, req.AttachmentIDs)
	require.NotNil(t, req.DocumentSelection)
	require.Same(t, tpl, req.DocumentTemplateRequest)
}

// No agent, or the retired public-lookup agent of an old session, runs as
// smart reasoning; a bought add-on or an unknown id is left as named (the
// ordinary unavailable-agent refusal answers the unknown one).
func TestRestrictPublicPortalRequestAgent(t *testing.T) {
	for in, want := range map[string]string{
		"":                               types.BuiltinSmartReasoningID,
		types.BuiltinPublicLookupID:      types.BuiltinSmartReasoningID,
		types.BuiltinSmartReasoningID:    types.BuiltinSmartReasoningID,
		types.BuiltinDocumentAssistantID: types.BuiltinDocumentAssistantID,
		"addon-tax":                      "addon-tax",
	} {
		req := &CreateKnowledgeQARequest{AgentID: in}
		restrictPublicPortalRequest(context.Background(), req)
		require.Equalf(t, want, req.AgentID, "agent %q", in)
		require.Nil(t, req.MentionedItems)
	}
}

func TestPortalAgentUnavailable(t *testing.T) {
	on, off := true, false
	require.Error(t, portalAgentUnavailable(nil), "a built-in that did not resolve must not fall back to plain chat")
	require.Error(t, portalAgentUnavailable(&types.CustomAgent{ID: types.BuiltinSmartReasoningID, Switch: &off}))
	require.NoError(t, portalAgentUnavailable(&types.CustomAgent{ID: types.BuiltinSmartReasoningID, Switch: &on}))
	require.NoError(t, portalAgentUnavailable(&types.CustomAgent{ID: types.BuiltinDocumentAssistantID, Switch: &on}))
	// its own 503 code, so the refused turn is not metered as a missing agent
	appErr := portalAgentUnavailable(nil).(*errors.AppError)
	require.Equal(t, errors.ErrPublicLookupUnavailable, appErr.Code)
	require.Equal(t, map[string]string{"reason": errors.PublicLookupReasonOff}, appErr.Details)
}

// A SuperAdmin save from the agent editor cannot widen a Guest's knowledge
// scope or capabilities; what the agent itself does (tools, file types,
// images, memory, data analysis, models, prompt) is kept.
func TestEnforcePortalAgentConfig(t *testing.T) {
	memoryOn := true
	stored := &types.CustomAgent{ID: types.BuiltinDocumentAssistantID, Config: types.CustomAgentConfig{
		ModelID: "chat-1", SystemPrompt: "riêng", KBSelectionMode: "all", KnowledgeBases: []string{"kb-tenant"},
		RetrieveKBOnlyWhenMentioned: true, WebSearchEnabled: true, WebSearchDefaultOn: true, WebFetchEnabled: true,
		WebSearchProviderID: "p", MCPSelectionMode: "all", MCPServices: []string{"mcp-1"},
		IllustrationMCPServiceID: "img", SkillsSelectionMode: "all",
		SelectedSkills: []string{"s"}, SandboxConfigID: "sb", DomainPacks: []string{"tax"},
		ImageUploadEnabled: true, DataAnalysisEnabled: true, MemoryEnabled: &memoryOn,
		SupportedFileTypes: []string{"docx", "pdf"},
		AllowedTools:       []string{"search_knowledge", "rewrite_paragraphs", "check_document_legal_basis"},
	}}
	got := enforcePortalAgentConfig(stored)
	require.Equal(t, "chat-1", got.Config.ModelID)
	require.Equal(t, "riêng", got.Config.SystemPrompt)
	require.Equal(t, types.KBSelectionModePublic, got.Config.KBSelectionMode)
	require.Nil(t, got.Config.KnowledgeBases)
	require.False(t, got.Config.RetrieveKBOnlyWhenMentioned)
	require.False(t, got.Config.WebSearchEnabled || got.Config.WebSearchDefaultOn || got.Config.WebFetchEnabled)
	require.Empty(t, got.Config.WebSearchProviderID)
	require.Equal(t, "none", got.Config.MCPSelectionMode)
	require.Nil(t, got.Config.MCPServices)
	require.Empty(t, got.Config.IllustrationMCPServiceID)
	require.Equal(t, "none", got.Config.SkillsSelectionMode)
	require.Nil(t, got.Config.SelectedSkills)
	require.Empty(t, got.Config.SandboxConfigID)
	require.Nil(t, got.Config.DomainPacks)
	// the agent's own abilities stay
	require.True(t, got.Config.ImageUploadEnabled)
	require.True(t, got.Config.DataAnalysisEnabled)
	require.Same(t, &memoryOn, got.Config.MemoryEnabled)
	require.Equal(t, []string{"docx", "pdf"}, got.Config.SupportedFileTypes)
	require.Equal(t, stored.Config.AllowedTools, got.Config.AllowedTools)
	// the stored row is untouched
	require.Equal(t, "all", stored.Config.KBSelectionMode)
	require.Nil(t, enforcePortalAgentConfig(nil))
}
