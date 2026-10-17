package service

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type subChatRootsRepo struct {
	interfaces.SessionRepository
	roots map[string]string
	reads int
}

func (r *subChatRootsRepo) ChatRootID(_ context.Context, _ uint64, id string) (string, error) {
	r.reads++
	if root := r.roots[id]; root != "" {
		return root, nil
	}
	return id, nil
}

type subChatInnerWorkspaces struct {
	interfaces.DocumentWorkspaceService
	sessions []string
	hooked   bool
}

func (w *subChatInnerWorkspaces) List(_ context.Context, _ uint64, sessionID string) ([]*types.DocumentWorkspace, error) {
	w.sessions = append(w.sessions, sessionID)
	return nil, nil
}

func (w *subChatInnerWorkspaces) Get(_ context.Context, _ uint64, sessionID, _ string) (*types.DocumentWorkspace, error) {
	w.sessions = append(w.sessions, sessionID)
	return &types.DocumentWorkspace{SessionID: sessionID}, nil
}

func (w *subChatInnerWorkspaces) OnSourceTextReady(func(context.Context, *types.DocumentWorkspace)) {
	w.hooked = true
}

type subChatInnerUploads struct {
	interfaces.TemporaryDocumentService
	sessions []string
	hooked   bool
}

func (u *subChatInnerUploads) Create(_ context.Context, _ uint64, sessionID, _, _ string, _ int64, _ io.Reader, _ types.TemporaryDocumentCreateOptions) (*types.TemporaryDocument, error) {
	u.sessions = append(u.sessions, sessionID)
	return &types.TemporaryDocument{SessionID: sessionID}, nil
}

func (u *subChatInnerUploads) List(_ context.Context, _ uint64, sessionID string) ([]*types.TemporaryDocument, error) {
	u.sessions = append(u.sessions, sessionID)
	return nil, nil
}

func (u *subChatInnerUploads) OnDocumentParsed(func(ctx context.Context, tenantID uint64, sessionID, documentID string)) {
	u.hooked = true
}

// A sub-chat works on its root chat's documents and uploads; the root and
// ordinary sessions keep their own; the hooks other services look for by
// type assertion still reach the wrapped services.
func TestSubChatDocumentsResolveToTheRootChat(t *testing.T) {
	ctx := context.Background()
	roots := &subChatRootsRepo{roots: map[string]string{"sub": "root"}}

	inner := &subChatInnerWorkspaces{}
	ws := NewSubChatWorkspaces(inner, roots)
	_, _ = ws.List(ctx, 1, "sub")
	_, _ = ws.List(ctx, 1, "sub")
	got, err := ws.Get(ctx, 1, "sub", "d1")
	require.NoError(t, err)
	require.Equal(t, "root", got.SessionID)
	_, _ = ws.List(ctx, 1, "root")
	_, _ = ws.List(ctx, 1, "plain")
	require.Equal(t, []string{"root", "root", "root", "root", "plain"}, inner.sessions)
	require.Equal(t, 3, roots.reads, "a resolved session is cached")

	hook, ok := ws.(interface {
		OnSourceTextReady(func(context.Context, *types.DocumentWorkspace))
	})
	require.True(t, ok)
	hook.OnSourceTextReady(func(context.Context, *types.DocumentWorkspace) {})
	require.True(t, inner.hooked)

	innerUp := &subChatInnerUploads{}
	up := NewSubChatUploads(innerUp, roots)
	_, err = up.Create(ctx, 1, "sub", "a.pdf", "application/pdf", 3, strings.NewReader("abc"), types.TemporaryDocumentCreateOptions{})
	require.NoError(t, err)
	_, _ = up.List(ctx, 1, "sub")
	require.Equal(t, []string{"root", "root"}, innerUp.sessions)
	n, ok := up.(temporaryDocumentReadyNotifier)
	require.True(t, ok)
	n.OnDocumentParsed(func(context.Context, uint64, string, string) {})
	require.True(t, innerUp.hooked)
}
