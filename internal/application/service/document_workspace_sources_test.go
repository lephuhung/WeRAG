package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// dwNotifyingAttachments is dwFakeAttachments with the parse notification
// the real temporary document service offers.
type dwNotifyingAttachments struct {
	*dwFakeAttachments
	parsed func(ctx context.Context, tenantID uint64, sessionID, documentID string)
}

func (a *dwNotifyingAttachments) OnDocumentParsed(fn func(context.Context, uint64, string, string)) {
	a.parsed = fn
}

// newDWSourceFixture is newDWFixture whose service listens to parse ends.
func newDWSourceFixture(t *testing.T) (*dwFixture, *dwNotifyingAttachments) {
	fx := newDWFixture(t)
	notify := &dwNotifyingAttachments{dwFakeAttachments: fx.attach}
	fx.svc = newDocumentWorkspaceService(fx.svc.cfg, fx.repo, fx.files, fx.catalog, notify, fx.messages, fx.revs)
	require.NotNil(t, notify.parsed, "the workspace service registers for parse ends")
	// a force-save finds nothing to save: no wait for a callback
	fx.ds.onCommand = func(map[string]interface{}) int { return onlyOfficeCommandNoChanges }
	return fx, notify
}

func (fx *dwFixture) addAttachment(id, name string, data []byte, status string) *types.TemporaryDocument {
	doc := &types.TemporaryDocument{ID: id, SessionID: "sess-1", FileName: name, Status: status}
	if i := strings.LastIndex(name, "."); i >= 0 {
		doc.FileType = name[i:]
	}
	fx.attach.docs[id] = doc
	fx.attach.bytes[id] = data
	return doc
}

func TestDocumentSourceCopiesTheUploadAndOutlivesIt(t *testing.T) {
	fx, notify := newDWSourceFixture(t)
	ctx := context.Background()
	doc := fx.attach.docs["att-pdf"]
	doc.Status = types.TemporaryDocumentStatusProcessing

	ws, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "att-pdf")
	require.NoError(t, err)
	require.True(t, ws.IsSource())
	require.Equal(t, "pdf", ws.FileType)
	require.Equal(t, types.DocumentSourceTextProcessing, ws.TextStatus)
	require.Equal(t, 1, ws.Position)
	require.Equal(t, []byte("%PDF"), fx.files.get(ws.CurrentRef))
	require.False(t, fx.files.temp[ws.CurrentRef], "the source copy must outlive the 24h attachment")

	_, _, err = fx.svc.SourceText(ctx, 7, "sess-1", ws.ID)
	requireAppCode(t, err, apperrors.ErrConflict)

	// parsing ends: the worker tells the workspace service
	doc.Status = types.TemporaryDocumentStatusReady
	doc.Content = "Điều 1. Số liệu năm 2025"
	doc.Chunks = types.JSON(`[{"seq":0,"content":"Điều 1. Số liệu năm 2025","context_header":"Chương I"}]`)
	doc.ChunkCount = 1
	notify.parsed(ctx, 7, "sess-1", "att-pdf")
	row, err := fx.repo.GetByID(ctx, ws.ID)
	require.NoError(t, err)
	require.Equal(t, types.DocumentSourceTextReady, row.TextStatus)

	// the upload expires (CleanupExpired): the source keeps its text and file
	delete(fx.attach.docs, "att-pdf")
	text, got, err := fx.svc.SourceText(ctx, 7, "sess-1", ws.ID)
	require.NoError(t, err)
	require.Equal(t, ws.ID, got.ID)
	require.Equal(t, "Điều 1. Số liệu năm 2025", text.Content)
	require.Contains(t, string(text.Chunks), "Chương I")
	require.Equal(t, []byte("%PDF"), fx.files.get(ws.CurrentRef))
}

func TestDocumentSourceIsNotAnEditorDocument(t *testing.T) {
	fx, _ := newDWSourceFixture(t)
	ctx := context.Background()
	ws, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "att-pdf")
	require.NoError(t, err)
	again, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "att-pdf")
	require.NoError(t, err)
	require.Equal(t, ws.ID, again.ID, "one document per upload")

	_, err = fx.svc.GetBySession(ctx, 7, "sess-1")
	requireAppCode(t, err, apperrors.ErrNotFound)
	view, err := fx.svc.View(ctx, ws, "user-1", "User", "vi")
	require.NoError(t, err)
	require.Nil(t, view.Editor, "a source gets no ONLYOFFICE config")

	refused := func(err error) {
		t.Helper()
		requireAppCode(t, err, apperrors.ErrConflict)
		require.Contains(t, err.Error(), "vb1 là tài liệu nguồn, chỉ tra cứu được")
	}
	_, err = fx.svc.Activate(ctx, 7, "sess-1", ws.ID)
	refused(err)
	refused(fx.svc.ForceSave(ctx, 7, "sess-1", ws.ID))
	_, err = fx.svc.Snapshot(ctx, 7, "sess-1", ws.ID, "x", types.DocumentRevisionSourceManual, 0)
	refused(err)
	_, err = fx.svc.ListRevisions(ctx, 7, "sess-1", ws.ID)
	refused(err)
	_, err = fx.svc.Restore(ctx, 7, "sess-1", ws.ID, 1)
	refused(err)
	_, _, err = fx.svc.PrepareExternalWrite(ctx, 7, "sess-1", ws.ID, time.Millisecond)
	refused(err)
	_, err = fx.svc.CommitExternalWrite(ctx, 7, "sess-1", ws.ID, 0, []byte("x"))
	refused(err)

	// it can be read (download) and removed
	rc, _, err := fx.svc.OpenCurrent(ctx, 7, "sess-1", ws.ID)
	require.NoError(t, err)
	_ = rc.Close()
	require.NoError(t, fx.svc.Remove(ctx, 7, "sess-1", ws.ID))
	require.Empty(t, fx.revs.rows, "removing a source takes no snapshot")

	fx.addAttachment("att-png", "scan.png", []byte("png"), types.TemporaryDocumentStatusReady)
	_, err = fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "att-png")
	requireAppCode(t, err, apperrors.ErrBadRequest)
}

func TestDocumentSourceAndTargetLimitsAreSeparate(t *testing.T) {
	fx, _ := newDWSourceFixture(t)
	ctx := context.Background()
	for i := 0; i < types.MaxDocumentSourcesPerSession; i++ {
		id := fmt.Sprintf("src-%d", i)
		fx.addAttachment(id, id+".pdf", []byte("%PDF"), types.TemporaryDocumentStatusReady)
		_, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", id)
		require.NoError(t, err)
	}
	fx.addAttachment("src-x", "x.pdf", []byte("%PDF"), types.TemporaryDocumentStatusReady)
	_, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "src-x")
	requireAppCode(t, err, apperrors.ErrConflict)
	docs, _ := fx.svc.List(ctx, 7, "sess-1")
	require.Error(t, SourceCapacityError(docs), "the upload handler refuses before storing the file")

	// targets count apart: four tabs still open next to ten sources
	for i := 0; i < types.MaxDocumentWorkspacesPerSession; i++ {
		id := fmt.Sprintf("tab-%d", i)
		fx.addAttachment(id, id+".docx", dwOriginalDocx, types.TemporaryDocumentStatusReady)
		_, err := fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", id)
		require.NoError(t, err)
	}
	fx.addAttachment("tab-x", "x.docx", dwOriginalDocx, types.TemporaryDocumentStatusReady)
	_, err = fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", "tab-x")
	requireAppCode(t, err, apperrors.ErrConflict)
	docs, _ = fx.svc.List(ctx, 7, "sess-1")
	require.Len(t, docs, types.MaxDocumentSourcesPerSession+types.MaxDocumentWorkspacesPerSession)
}

func TestDocumentSourcePromotedToTargetKeepsItsHandle(t *testing.T) {
	fx, _ := newDWSourceFixture(t)
	ctx := context.Background()
	fx.attach.docs["att-docx"].Status = types.TemporaryDocumentStatusReady
	first, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "att-pdf")
	require.NoError(t, err)
	src, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "att-docx")
	require.NoError(t, err)
	require.Equal(t, "vb2", src.Handle())

	// "Mở để soạn thảo" from the editor flow: the same upload opened as a tab
	tab, err := fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", "att-docx")
	require.NoError(t, err)
	require.Equal(t, src.ID, tab.ID)
	require.Equal(t, "vb2", tab.Handle(), "the handle is kept")
	require.True(t, tab.IsTarget())
	require.Equal(t, "docx", tab.FileType)
	require.Empty(t, tab.TextStatus)
	active, err := fx.svc.GetBySession(ctx, 7, "sess-1")
	require.NoError(t, err)
	require.Equal(t, tab.ID, active.ID, "the promoted document is the visible tab")
	view, err := fx.svc.View(ctx, tab, "user-1", "User", "vi")
	require.NoError(t, err)
	require.NotNil(t, view.Editor)

	// a pdf has no editor file
	_, err = fx.svc.SetRole(ctx, 7, "sess-1", first.ID, types.DocumentWorkspaceRoleTarget)
	requireAppCode(t, err, apperrors.ErrBadRequest)
	_, err = fx.svc.SetRole(ctx, 7, "sess-1", first.ID, "owner")
	requireAppCode(t, err, apperrors.ErrBadRequest)
}

func TestDocumentSourcePromotedFromDocIsConverted(t *testing.T) {
	fx, _ := newDWSourceFixture(t)
	ctx := context.Background()
	src, err := fx.svc.CreateSourceFromAttachment(ctx, 7, "sess-1", "user-1", "att-doc")
	require.NoError(t, err)
	require.Equal(t, "doc", src.FileType)
	tab, err := fx.svc.SetRole(ctx, 7, "sess-1", src.ID, types.DocumentWorkspaceRoleTarget)
	require.NoError(t, err)
	require.Equal(t, "old.docx", tab.FileName)
	require.Equal(t, dwConvertedDocx, fx.files.get(tab.CurrentRef))
}

func TestDocumentTargetDemotedToSourceKeepsItsText(t *testing.T) {
	fx, _ := newDWSourceFixture(t)
	ctx := context.Background()
	tab := fx.create(t)

	src, err := fx.svc.SetRole(ctx, 7, "sess-1", tab.ID, types.DocumentWorkspaceRoleSource)
	require.NoError(t, err)
	require.True(t, src.IsSource())
	require.Equal(t, tab.Handle(), src.Handle())
	require.Equal(t, types.DocumentSourceTextReady, src.TextStatus)
	require.Len(t, fx.revs.rows, 1, "the last state is snapshotted, like closing the tab")
	text, _, err := fx.svc.SourceText(ctx, 7, "sess-1", src.ID)
	require.NoError(t, err)
	require.Equal(t, "original\n", text.Content)
	_, err = fx.svc.GetBySession(ctx, 7, "sess-1")
	requireAppCode(t, err, apperrors.ErrNotFound)

	// and back: a Word source opens in the editor again
	back, err := fx.svc.SetRole(ctx, 7, "sess-1", src.ID, types.DocumentWorkspaceRoleTarget)
	require.NoError(t, err)
	require.True(t, back.IsTarget())
}

// A save callback that reaches a document demoted while its editor was
// still open is not stored.
func TestDocumentSourceIgnoresEditorSaves(t *testing.T) {
	fx, _ := newDWSourceFixture(t)
	ctx := context.Background()
	tab := fx.create(t)
	ticket := fx.ticket(t, tab)
	src, err := fx.svc.SetRole(ctx, 7, "sess-1", tab.ID, types.DocumentWorkspaceRoleSource)
	require.NoError(t, err)

	cb := &types.OnlyOfficeCallback{Key: tab.EditorKey(), Status: onlyOfficeStatusMustSave, URL: fx.ds.put("late.docx", dwConvertedDocx)}
	require.NoError(t, fx.svc.HandleCallback(ctx, ticket, dwBearer(t, dwTestSecret, cb), cb))
	row, err := fx.repo.GetByID(ctx, src.ID)
	require.NoError(t, err)
	require.Equal(t, src.CurrentRef, row.CurrentRef)
	require.True(t, row.IsSource())
}
