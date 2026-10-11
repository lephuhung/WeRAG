package service

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type fakeDocumentWorkspaces struct {
	interfaces.DocumentWorkspaceService
	ws *types.DocumentWorkspace
}

func (f *fakeDocumentWorkspaces) Enabled() bool          { return true }
func (f *fakeDocumentWorkspaces) DocumentsEnabled() bool { return true }

// OpenCurrent fails: the format check's background prewarm reads the
// document when a model is available.
func (f *fakeDocumentWorkspaces) OpenCurrent(context.Context, uint64, string, string) (io.ReadCloser, *types.DocumentWorkspace, error) {
	return nil, nil, errors.New("no document in this test")
}

func (f *fakeDocumentWorkspaces) GetBySession(context.Context, uint64, string) (*types.DocumentWorkspace, error) {
	if f.ws == nil {
		return nil, apperrors.NewNotFoundError("no workspace")
	}
	return f.ws, nil
}

func (f *fakeDocumentWorkspaces) List(context.Context, uint64, string) ([]*types.DocumentWorkspace, error) {
	if f.ws == nil {
		return nil, nil
	}
	return []*types.DocumentWorkspace{f.ws}, nil
}

func registerDocumentTools(t *testing.T, ws *types.DocumentWorkspace, allowed ...string) []string {
	t.Helper()
	kb := &types.KnowledgeBase{ID: "kb"}
	kb.IndexingStrategy.VectorEnabled = true
	svc := newToolSurfaceService(kb)
	svc.documentWorkspaces = &fakeDocumentWorkspaces{ws: ws}
	ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(7))
	registry := tools.NewToolRegistry()
	require.NoError(t, svc.registerTools(ctx, registry, toolSurfaceConfig(allowed...), nil, nil, "session"))
	return registry.ListTools()
}

func TestRegisterToolsOffersDocumentToolsWithAWorkspace(t *testing.T) {
	ws := &types.DocumentWorkspace{ID: "ws", SessionID: "session", FileName: "a.docx"}
	names := registerDocumentTools(t, ws,
		tools.ToolCheckDocumentFormat, tools.ToolReadDocumentOutline, tools.ToolApplyFormatFixes,
		tools.ToolSearchKnowledge)
	require.Contains(t, names, tools.ToolReadDocumentOutline)
	require.Contains(t, names, tools.ToolFindInDocuments, "find_in_documents rides on the outline")
	require.Contains(t, names, tools.ToolApplyFormatFixes)
	require.Contains(t, names, tools.ToolCheckDocumentFormat)
	require.Contains(t, names, tools.ToolSearchKnowledge)
	require.NotContains(t, names, tools.ToolRewriteParagraphs, "the allowlist still decides the writers")
	require.NotContains(t, names, tools.ToolInsertParagraphs)
	require.NotContains(t, names, tools.ToolMarkPassages)

	names = registerDocumentTools(t, ws, tools.ToolInsertParagraphs, tools.ToolMarkPassages)
	require.Contains(t, names, tools.ToolInsertParagraphs)
	require.Contains(t, names, tools.ToolMarkPassages)
	require.Equal(t, 1, countName(names, tools.ToolCheckDocumentFormat))

	// check_document_format follows the workspace, not the allowlist
	require.Contains(t, registerDocumentTools(t, ws, tools.ToolSearchKnowledge), tools.ToolCheckDocumentFormat)
}

func TestRegisterToolsHidesDocumentToolsWithoutAWorkspace(t *testing.T) {
	names := registerDocumentTools(t, nil,
		tools.ToolCheckDocumentFormat, tools.ToolReadDocumentOutline, tools.ToolApplyFormatFixes,
		tools.ToolRewriteParagraphs, tools.ToolInsertParagraphs, tools.ToolMarkPassages, tools.ToolSearchKnowledge)
	for _, name := range []string{tools.ToolCheckDocumentFormat, tools.ToolReadDocumentOutline, tools.ToolFindInDocuments,
		tools.ToolApplyFormatFixes, tools.ToolRewriteParagraphs, tools.ToolInsertParagraphs, tools.ToolMarkPassages} {
		require.NotContains(t, names, name)
	}
	require.Contains(t, names, tools.ToolSearchKnowledge)
}

type docChatBase interface{ chat.Chat }

type docModelChat struct {
	docChatBase
	id string
}

func (c docModelChat) GetModelID() string   { return c.id }
func (c docModelChat) GetModelName() string { return c.id }

type docModelService struct {
	interfaces.ModelService
	asked []string
}

func (m *docModelService) GetChatModel(_ context.Context, id string) (chat.Chat, error) {
	m.asked = append(m.asked, id)
	if id == "missing" {
		return nil, errors.New("not found")
	}
	return docModelChat{id: id}, nil
}

func TestDocumentToolModelResolution(t *testing.T) {
	models := &docModelService{}
	svc := &agentService{modelService: models}
	run := docModelChat{id: "run"}
	ctx := t.Context()
	require.Equal(t, run, svc.documentToolModel(ctx, "", run, "x"), "empty id → the run's model")
	require.Equal(t, run, svc.documentToolModel(ctx, "run", run, "x"), "same id → no second load")
	require.Equal(t, run, svc.documentToolModel(ctx, "missing", run, "x"), "unloadable → the run's model")
	require.Equal(t, docModelChat{id: "thinker"}, svc.documentToolModel(ctx, " thinker ", run, "x"))
	require.Equal(t, []string{"missing", "thinker"}, models.asked)
}

func TestRegisterToolsOffersCheckSpellingWithItsModel(t *testing.T) {
	ws := &types.DocumentWorkspace{ID: "ws", SessionID: "session", FileName: "a.docx"}
	kb := &types.KnowledgeBase{ID: "kb"}
	kb.IndexingStrategy.VectorEnabled = true
	svc := newToolSurfaceService(kb)
	svc.documentWorkspaces = &fakeDocumentWorkspaces{ws: ws}
	models := &docModelService{}
	svc.modelService = models
	ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(7))
	cfg := toolSurfaceConfig(tools.ToolCheckSpelling)
	cfg.SpellcheckModelID = "spell"
	cfg.FormatCheckModelID = "format"
	registry := tools.NewToolRegistry()
	require.NoError(t, svc.registerTools(ctx, registry, cfg, nil, nil, "session"))
	require.Contains(t, registry.ListTools(), tools.ToolCheckSpelling)
	require.ElementsMatch(t, []string{"format", "spell"}, models.asked)

	// without a model at all the spellcheck is not offered
	require.NotContains(t, registerDocumentTools(t, ws, tools.ToolCheckSpelling), tools.ToolCheckSpelling)
	// nor without a workspace
	require.NotContains(t, registerDocumentTools(t, nil, tools.ToolCheckSpelling), tools.ToolCheckSpelling)
}

func TestRegisterToolsReadOnlyDocumentsForReasoningInWord(t *testing.T) {
	ws := &types.DocumentWorkspace{ID: "ws", SessionID: "session", FileName: "a.docx",
		EditorKind: types.DocumentEditorKindWordAddin}
	kb := &types.KnowledgeBase{ID: "kb"}
	kb.IndexingStrategy.VectorEnabled = true
	svc := newToolSurfaceService(kb)
	svc.documentWorkspaces = &fakeDocumentWorkspaces{ws: ws}
	ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(7))
	cfg := toolSurfaceConfig(tools.ToolSearchKnowledge, tools.ToolRewriteParagraphs, tools.ToolMarkPassages,
		tools.ToolApplyFormatFixes, tools.ToolDrawDiagram)
	cfg.ReadOnlyDocuments = true
	registry := tools.NewToolRegistry()
	require.NoError(t, svc.registerTools(ctx, registry, cfg, nil, nil, "session"))
	names := registry.ListTools()

	require.Contains(t, names, tools.ToolSearchKnowledge)
	require.Contains(t, names, tools.ToolReadDocumentOutline, "reading the document is added")
	require.Contains(t, names, tools.ToolFindInDocuments)
	require.Contains(t, names, tools.ToolCheckDocumentFormat)
	for _, writer := range tools.ReadOnlyDocumentTools {
		require.NotContains(t, names, writer, "no tool that changes the document")
	}
	check, err := registry.GetTool(tools.ToolCheckDocumentFormat)
	require.NoError(t, err)
	require.Contains(t, check.Description(), "ONLY REPORTS")
}

func TestQuickAnswerSelectionRules(t *testing.T) {
	sel := &types.DocumentSelection{Text: "Điều 3. Hiệu lực thi hành"}
	req := &types.QARequest{CustomAgent: &types.CustomAgent{ID: types.BuiltinQuickAnswerID}, DocumentSelection: sel}
	got := quickAnswerSelectionRules(req)
	require.Contains(t, got, "Trả lời nhanh không có công cụ phù hợp")
	require.NotContains(t, got, "format_check_report")

	req.DocumentSelection = nil
	require.Empty(t, quickAnswerSelectionRules(req), "no selection, plain Q&A")
	req.DocumentSelection = sel
	req.CustomAgent = &types.CustomAgent{ID: types.BuiltinSmartReasoningID}
	require.Empty(t, quickAnswerSelectionRules(req), "only quick answer takes this path")
}

func TestFallbackPromptKeepsSelection(t *testing.T) {
	svc := &sessionService{}
	req := &types.QARequest{CustomAgent: &types.CustomAgent{ID: types.BuiltinQuickAnswerID},
		DocumentSelection: &types.DocumentSelection{Text: "Bước 2: Trong vòng 24 giờ, Trạm y tế xác minh thông tin."}}
	cm := &types.ChatManage{}
	cm.Query = "Sửa đoạn này cho ngắn gọn hơn"
	cm.FallbackPrompt = "Câu hỏi: {{query}}"
	cm.DocumentSelection = req.DocumentSelection
	cm.DocumentContext = quickAnswerSelectionRules(req)
	got, err := svc.renderFallbackPrompt(t.Context(), cm)
	require.NoError(t, err)
	require.Contains(t, got, "Câu hỏi: Sửa đoạn này")
	require.Contains(t, got, "Trạm y tế xác minh", "an empty search must not drop the highlighted passage")
	require.Contains(t, got, "không có công cụ phù hợp")
}
