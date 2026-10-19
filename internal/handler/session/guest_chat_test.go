package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// A Guest's request keeps the question only: the public-lookup agent
// whatever was named, and none of the scope or tool choices.
func TestRestrictPublicPortalRequest(t *testing.T) {
	req := &CreateKnowledgeQARequest{
		Query:               "Mức phạt vượt đèn đỏ?",
		AgentID:             types.BuiltinDocumentAssistantID,
		KnowledgeBaseIDs:    []string{"kb-tenant"},
		KnowledgeIds:        []string{"doc-1"},
		MentionedItems:      []MentionedItemRequest{{ID: "kb-invited", Type: "kb"}},
		TagIDs:              []string{"tag-1"},
		MCPServiceIDs:       []string{"mcp-1"},
		SkillNames:          []string{"skill-1"},
		WebSearchEnabled:    true,
		LocalBrowserEnabled: true,
		SummaryModelID:      "model-x",
		Images:              []ImageAttachment{{Data: "x"}},
		AttachmentUploads:   []AttachmentUpload{{Data: "x", FileName: "a.docx"}},
		AttachmentIDs:       []string{"att-1"},
		DocumentSelection:   &types.DocumentSelection{Text: "đoạn"},
		Channel:             "word_addin",
	}
	restrictPublicPortalRequest(context.Background(), req)
	require.Equal(t, "Mức phạt vượt đèn đỏ?", req.Query)
	require.Equal(t, "word_addin", req.Channel)
	require.Equal(t, types.BuiltinPublicLookupID, req.AgentID)
	require.True(t, req.AgentEnabled)
	require.Nil(t, req.KnowledgeBaseIDs)
	require.Nil(t, req.KnowledgeIds)
	require.Nil(t, req.MentionedItems)
	require.Nil(t, req.TagIDs)
	require.Nil(t, req.MCPServiceIDs)
	require.Nil(t, req.SkillNames)
	require.False(t, req.WebSearchEnabled)
	require.False(t, req.LocalBrowserEnabled)
	require.Empty(t, req.SummaryModelID)
	require.Nil(t, req.Images)
	require.Nil(t, req.AttachmentUploads)
	require.Nil(t, req.AttachmentIDs)
	require.Nil(t, req.DocumentSelection)
	require.Nil(t, req.DocumentTemplateRequest)
}

func TestPublicLookupUnavailable(t *testing.T) {
	on, off := true, false
	require.Error(t, publicLookupUnavailable(nil), "a built-in that did not resolve must not fall back to plain chat")
	require.Error(t, publicLookupUnavailable(&types.CustomAgent{ID: types.BuiltinSmartReasoningID, Switch: &on}))
	require.Error(t, publicLookupUnavailable(&types.CustomAgent{ID: types.BuiltinPublicLookupID, Switch: &off}))
	require.NoError(t, publicLookupUnavailable(&types.CustomAgent{ID: types.BuiltinPublicLookupID, Switch: &on}))
}

// A SuperAdmin save from the agent editor cannot widen a Guest's turn: the
// scope and the tool boundary are reapplied, models and prompt are kept.
func TestEnforcePublicLookupConfig(t *testing.T) {
	stored := &types.CustomAgent{ID: types.BuiltinPublicLookupID, Config: types.CustomAgentConfig{
		ModelID: "chat-1", SystemPrompt: "riêng", KBSelectionMode: "all", KnowledgeBases: []string{"kb-tenant"},
		RetrieveKBOnlyWhenMentioned: true, WebSearchEnabled: true, WebSearchDefaultOn: true, WebFetchEnabled: true,
		MCPSelectionMode: "all", MCPServices: []string{"mcp-1"}, SkillsSelectionMode: "all",
		SelectedSkills: []string{"s"}, SandboxConfigID: "sb", DomainPacks: []string{"tax"},
		ImageUploadEnabled: true, AudioUploadEnabled: true, DataAnalysisEnabled: true,
		AllowedTools: []string{"search_knowledge", "rewrite_paragraphs", "data_analysis", "check_legal_status"},
	}}
	got := enforcePublicLookupConfig(stored)
	require.Equal(t, "chat-1", got.Config.ModelID)
	require.Equal(t, "riêng", got.Config.SystemPrompt)
	require.Equal(t, types.KBSelectionModePublic, got.Config.KBSelectionMode)
	require.Nil(t, got.Config.KnowledgeBases)
	require.False(t, got.Config.RetrieveKBOnlyWhenMentioned)
	require.False(t, got.Config.WebSearchEnabled || got.Config.WebSearchDefaultOn || got.Config.WebFetchEnabled)
	require.Equal(t, "none", got.Config.MCPSelectionMode)
	require.Nil(t, got.Config.MCPServices)
	require.Equal(t, "none", got.Config.SkillsSelectionMode)
	require.Empty(t, got.Config.SandboxConfigID)
	require.Nil(t, got.Config.DomainPacks)
	require.False(t, got.Config.ImageUploadEnabled || got.Config.AudioUploadEnabled || got.Config.DataAnalysisEnabled)
	require.NotNil(t, got.Config.MemoryEnabled)
	require.False(t, *got.Config.MemoryEnabled)
	require.Equal(t, []string{"search_knowledge", "check_legal_status"}, got.Config.AllowedTools)
	// the stored row is untouched
	require.Equal(t, "all", stored.Config.KBSelectionMode)

	// an allow-list with nothing allowed falls back to the read tools, never
	// to the runtime default set
	stored.Config.AllowedTools = []string{"rewrite_paragraphs"}
	require.Equal(t, []string{"search_knowledge", "read_document", "list_documents"},
		enforcePublicLookupConfig(stored).Config.AllowedTools)
}
