package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func (fx *dwFixture) createWordAddin(t *testing.T) *types.DocumentWorkspace {
	ws, err := fx.svc.CreateFromAttachmentFor(context.Background(), 7, "sess-1", "user-1", "att-docx",
		types.DocumentEditorKindWordAddin)
	require.NoError(t, err)
	return ws
}

func TestWordAddinWorkspaceNeedsNoDocumentServer(t *testing.T) {
	fx := newDWFixture(t)
	// no ONLYOFFICE configuration at all
	fx.svc = newDocumentWorkspaceService(&config.OnlyOfficeConfig{}, fx.repo, fx.files, fx.catalog, fx.attach, fx.messages, fx.revs)
	require.False(t, fx.svc.Enabled())

	_, err := fx.svc.CreateFromAttachment(context.Background(), 7, "sess-1", "user-1", "att-docx")
	requireAppCode(t, err, apperrors.ErrServiceUnavailable)

	ws := fx.createWordAddin(t)
	require.True(t, ws.IsWordAddin())
	require.Equal(t, dwOriginalDocx, fx.files.get(ws.CurrentRef))

	view, err := fx.svc.View(context.Background(), ws, "user-1", "An", "vi")
	require.NoError(t, err)
	require.Nil(t, view.Editor, "Word holds the document: no editor config")

	require.NoError(t, fx.svc.ForceSave(context.Background(), 7, "sess-1", ws.ID))
	require.Empty(t, fx.ds.commands)
}

func TestWordAddinRefusesLegacyDoc(t *testing.T) {
	fx := newDWFixture(t)
	_, err := fx.svc.CreateFromAttachmentFor(context.Background(), 7, "sess-1", "user-1", "att-doc",
		types.DocumentEditorKindWordAddin)
	requireAppCode(t, err, apperrors.ErrBadRequest)

	_, err = fx.svc.CreateFromAttachmentFor(context.Background(), 7, "sess-1", "user-1", "att-docx", "notepad")
	requireAppCode(t, err, apperrors.ErrBadRequest)
}

func TestWordAddinStoreClientSave(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.createWordAddin(t)
	ctx := context.Background()

	// the file as created: nothing is written
	got, err := fx.svc.StoreClientSave(ctx, 7, "sess-1", ws.ID, 0, dwOriginalDocx)
	require.NoError(t, err)
	require.Equal(t, ws.CurrentRef, got.CurrentRef)
	require.Equal(t, 0, got.SaveCount)

	edited := dwTestDocx("edited-in-word", 0)
	got, err = fx.svc.StoreClientSave(ctx, 7, "sess-1", ws.ID, 0, edited)
	require.NoError(t, err)
	require.NotEqual(t, ws.CurrentRef, got.CurrentRef)
	require.Equal(t, edited, fx.files.get(got.CurrentRef))
	require.Equal(t, 1, got.SaveCount)
	require.NotNil(t, got.LastSavedAt)
	require.Equal(t, 0, got.Revision, "an upload is a save, not an external write")

	// the taskpane has not seen a restore yet
	_, err = fx.svc.StoreClientSave(ctx, 7, "sess-1", ws.ID, 3, edited)
	requireAppCode(t, err, apperrors.ErrConflict)

	_, err = fx.svc.StoreClientSave(ctx, 7, "sess-1", ws.ID, 0, nil)
	requireAppCode(t, err, apperrors.ErrBadRequest)

	// an ONLYOFFICE document is not uploaded by a client
	other := newDWFixture(t)
	oo := other.create(t)
	_, err = other.svc.StoreClientSave(ctx, 7, "sess-1", oo.ID, 0, edited)
	requireAppCode(t, err, apperrors.ErrBadRequest)
}

func TestWordAddinFlushWaitsForUploadAfterAISnapshot(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.createWordAddin(t)
	ctx := context.Background()

	// nothing expected: no wait
	start := time.Now()
	_, data, err := fx.svc.PrepareExternalWrite(ctx, 7, "sess-1", ws.ID, 5*time.Second)
	require.NoError(t, err)
	require.Less(t, time.Since(start), time.Second)
	require.Equal(t, dwOriginalDocx, data)

	// a tool snapshots before returning its plan; Word applies it and uploads
	_, err = fx.svc.Snapshot(ctx, 7, "sess-1", ws.ID, "ai: sửa", types.DocumentRevisionSourceAI, time.Second)
	require.NoError(t, err)
	applied := dwTestDocx("with-ai-edits", 0)
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = fx.svc.StoreClientSave(context.Background(), 7, "sess-1", ws.ID, 0, applied)
	}()
	start = time.Now()
	_, data, err = fx.svc.PrepareExternalWrite(ctx, 7, "sess-1", ws.ID, 5*time.Second)
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond, "the flush waited for the upload")
	require.Equal(t, applied, data)

	// the upload cleared the expectation
	start = time.Now()
	_, _, err = fx.svc.PrepareExternalWrite(ctx, 7, "sess-1", ws.ID, 5*time.Second)
	require.NoError(t, err)
	require.Less(t, time.Since(start), time.Second)
	require.Empty(t, fx.ds.commands, "no Document Server command for a Word document")
}

func TestWordAddinFlushGivesUpAfterWait(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.createWordAddin(t)
	ctx := context.Background()
	_, err := fx.svc.Snapshot(ctx, 7, "sess-1", ws.ID, "ai: sửa", types.DocumentRevisionSourceAI, time.Second)
	require.NoError(t, err)

	start := time.Now()
	_, data, err := fx.svc.PrepareExternalWrite(ctx, 7, "sess-1", ws.ID, 300*time.Millisecond)
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 250*time.Millisecond)
	require.Equal(t, dwOriginalDocx, data, "proceeds with the last stored version")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = fx.svc.PrepareExternalWrite(cancelled, 7, "sess-1", ws.ID, time.Second)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWordAddinRestoreNeedsReload(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.createWordAddin(t)
	ctx := context.Background()
	first, err := fx.svc.Snapshot(ctx, 7, "sess-1", ws.ID, "đầu", types.DocumentRevisionSourceManual, time.Second)
	require.NoError(t, err)
	_, err = fx.svc.StoreClientSave(ctx, 7, "sess-1", ws.ID, 0, dwTestDocx("later", 0))
	require.NoError(t, err)

	restored, err := fx.svc.Restore(ctx, 7, "sess-1", ws.ID, first.Seq)
	require.NoError(t, err)
	require.Equal(t, 1, restored.Revision)
	require.Equal(t, dwOriginalDocx, fx.files.get(restored.CurrentRef))

	// an upload from before the restore is stale
	_, err = fx.svc.StoreClientSave(ctx, 7, "sess-1", ws.ID, 0, dwTestDocx("later", 0))
	requireAppCode(t, err, apperrors.ErrConflict)
	// once Word holds the restored file, its uploads are stored again
	got, err := fx.svc.StoreClientSave(ctx, 7, "sess-1", ws.ID, 1, dwOriginalDocx)
	require.NoError(t, err)
	require.Equal(t, 1, got.Revision)
}

func TestSourceDocumentNeedsNoDocumentServer(t *testing.T) {
	fx := newDWFixture(t)
	fx.svc = newDocumentWorkspaceService(&config.OnlyOfficeConfig{}, fx.repo, fx.files, fx.catalog, fx.attach, fx.messages, fx.revs)
	require.False(t, fx.svc.Enabled())
	require.True(t, fx.svc.DocumentsEnabled())

	ws, err := fx.svc.CreateSourceFromAttachment(context.Background(), 7, "sess-1", "user-1", "att-pdf")
	require.NoError(t, err)
	require.True(t, ws.IsSource())
	require.Equal(t, []byte("%PDF"), fx.files.get(ws.CurrentRef))

	// opening a source in the embedded editor still needs ONLYOFFICE
	_, err = fx.svc.SetRole(context.Background(), 7, "sess-1", ws.ID, types.DocumentWorkspaceRoleTarget)
	requireAppCode(t, err, apperrors.ErrServiceUnavailable)
}
