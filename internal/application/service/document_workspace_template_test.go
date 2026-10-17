package service

import (
	"context"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestStoreTemplateFillsAnEmptyDocument(t *testing.T) {
	fx := newDWFixture(t)
	fx.attach.bytes["att-docx"] = dwTestDocx("", 0)
	// nothing unsaved in the editor: no save callback to wait for
	fx.ds.onCommand = func(map[string]interface{}) int { return 4 }
	ws := fx.create(t)
	ctx := context.Background()
	template := dwTestDocx("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", 0)

	_, err := fx.svc.StoreTemplate(ctx, 7, "sess-1", ws.ID, ws.Revision+1, template)
	requireAppCode(t, err, apperrors.ErrConflict)
	_, err = fx.svc.StoreTemplate(ctx, 7, "sess-1", ws.ID, ws.Revision, []byte("not a docx"))
	requireAppCode(t, err, apperrors.ErrBadRequest)

	got, err := fx.svc.StoreTemplate(ctx, 7, "sess-1", ws.ID, ws.Revision, template)
	require.NoError(t, err)
	require.Equal(t, ws.Revision+1, got.Revision, "a new editor key reloads the editor")
	require.Equal(t, template, fx.files.get(got.CurrentRef))
	labels := []string{}
	for _, r := range fx.revs.rows {
		labels = append(labels, r.Label)
	}
	require.Equal(t, []string{documentRevisionLabelBeforeTemplate, documentRevisionLabelTemplate}, labels)

	// now it has text: a second template never replaces it
	_, err = fx.svc.StoreTemplate(ctx, 7, "sess-1", ws.ID, got.Revision, template)
	requireAppCode(t, err, apperrors.ErrConflict)
}

func TestStoreTemplateRefusesTextAndWordAddin(t *testing.T) {
	fx := newDWFixture(t)
	fx.ds.onCommand = func(map[string]interface{}) int { return 4 }
	ctx := context.Background()
	ws := fx.create(t) // "original" in its body
	_, err := fx.svc.StoreTemplate(ctx, 7, "sess-1", ws.ID, ws.Revision, dwTestDocx("x", 0))
	requireAppCode(t, err, apperrors.ErrConflict)
	require.Equal(t, dwOriginalDocx, fx.files.get(ws.CurrentRef))

	fx2 := newDWFixture(t)
	addin, err := fx2.svc.CreateFromAttachmentFor(ctx, 7, "sess-1", "user-1", "att-docx", types.DocumentEditorKindWordAddin)
	require.NoError(t, err)
	_, err = fx2.svc.StoreTemplate(ctx, 7, "sess-1", addin.ID, addin.Revision, dwTestDocx("x", 0))
	requireAppCode(t, err, apperrors.ErrBadRequest)
}
