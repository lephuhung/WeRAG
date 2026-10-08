package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// A source's profile starts once its text is stored: at parse end and when
// a target becomes a source. Without a document assistant model it fails
// with "no model" instead of staying "being read" forever.
func TestDocumentSourceTextReadyStartsItsProfile(t *testing.T) {
	fx, notify := newDWSourceFixture(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	var ready []string
	fx.svc.OnSourceTextReady(func(_ context.Context, ws *types.DocumentWorkspace) { ready = append(ready, ws.ID) })
	precheck := NewDocumentFormatPrecheck(fx.svc, nil, nil)

	doc := fx.attach.docs["att-pdf"]
	doc.Status = types.TemporaryDocumentStatusProcessing
	ws, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "att-pdf")
	require.NoError(t, err)
	require.Empty(t, ready, "no profile while the upload is parsed")

	doc.Status = types.TemporaryDocumentStatusReady
	doc.Content = "Điều 1. Số liệu năm 2025"
	notify.parsed(ctx, 7, "sess-1", "att-pdf")
	require.Equal(t, []string{ws.ID}, ready)

	var st *types.DocumentProfile
	require.Eventually(t, func() bool {
		st = tools.SessionDocumentProfile(ctx, ws.ID)
		return st != nil && !st.InProgress()
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, types.DocumentProfileFailed, st.Status)
	require.Equal(t, "no model", st.Error)
	row, err := fx.repo.GetByID(ctx, ws.ID)
	require.NoError(t, err)
	require.Equal(t, "no model", precheck.Profile(ctx, row).Error)

	tab := fx.create(t)
	_, err = fx.svc.SetRole(ctx, 7, "sess-1", tab.ID, types.DocumentWorkspaceRoleSource)
	require.NoError(t, err)
	require.Equal(t, []string{ws.ID, tab.ID}, ready, "a demoted target is profiled as a source")
}
