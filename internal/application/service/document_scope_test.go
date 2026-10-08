package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type scopeWorkspaces struct {
	interfaces.DocumentWorkspaceService
	docs []*types.DocumentWorkspace
}

func (f *scopeWorkspaces) List(context.Context, uint64, string) ([]*types.DocumentWorkspace, error) {
	return f.docs, nil
}

func TestDocumentScopeServiceSetGetClear(t *testing.T) {
	ws := &scopeWorkspaces{docs: []*types.DocumentWorkspace{
		{ID: "ws-a", Position: 1, FileName: "a.docx"},
		{ID: "ws-b", Position: 2, FileName: "b.pdf", Role: types.DocumentWorkspaceRoleSource},
	}}
	svc := NewDocumentScopeService(ws)
	ctx := context.Background()

	got, err := svc.Set(ctx, 7, "s-scope-svc", &types.DocumentScope{
		DocumentIDs: []string{"vb2", "ws-a"}, Task: "compare", SetBy: types.DocumentScopeSetByRouter,
		Sections: []types.DocumentScopeSection{{DocumentID: "vb2", From: 2, To: 4, Title: "Chương II"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"ws-b", "ws-a"}, got.DocumentIDs)
	require.Equal(t, types.DocumentScopeSetByUser, got.SetBy, "a scope set here is the user's")
	require.Equal(t, "ws-b", got.Sections[0].DocumentID)

	read := svc.Get(ctx, 7, "s-scope-svc", nil)
	require.NotNil(t, read)
	require.Equal(t, types.DocumentScopeTaskCompare, read.Task)

	// a closed document drops out of the scope it was in
	ws.docs = ws.docs[:1]
	read = svc.Get(ctx, 7, "s-scope-svc", nil)
	require.Equal(t, []string{"ws-a"}, read.DocumentIDs)
	require.Empty(t, read.Sections)

	_, err = svc.Set(ctx, 7, "s-scope-svc", &types.DocumentScope{DocumentIDs: []string{"vb9"}})
	require.Error(t, err)
	_, err = svc.Set(ctx, 7, "s-scope-svc", &types.DocumentScope{DocumentIDs: []string{"vb1"}, Task: "dance"})
	require.Error(t, err)

	svc.Clear(ctx, "s-scope-svc")
	require.Nil(t, svc.Get(ctx, 7, "s-scope-svc", nil))
}
