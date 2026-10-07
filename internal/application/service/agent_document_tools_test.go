package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type fakeDocumentWorkspaces struct {
	interfaces.DocumentWorkspaceService
	ws *types.DocumentWorkspace
}

func (f *fakeDocumentWorkspaces) Enabled() bool { return true }

func (f *fakeDocumentWorkspaces) GetBySession(context.Context, uint64, string) (*types.DocumentWorkspace, error) {
	if f.ws == nil {
		return nil, apperrors.NewNotFoundError("no workspace")
	}
	return f.ws, nil
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
	for _, name := range []string{tools.ToolCheckDocumentFormat, tools.ToolReadDocumentOutline,
		tools.ToolApplyFormatFixes, tools.ToolRewriteParagraphs, tools.ToolInsertParagraphs, tools.ToolMarkPassages} {
		require.NotContains(t, names, name)
	}
	require.Contains(t, names, tools.ToolSearchKnowledge)
}
