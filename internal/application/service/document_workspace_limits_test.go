package service

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// dwTestDocx builds a minimal .docx whose body says marker, with mediaBytes
// of (stored, uncompressed) picture data under word/media/.
func dwTestDocx(marker string, mediaBytes int) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` +
		marker + `</w:t></w:r></w:p></w:body></w:document>`))
	if mediaBytes > 0 {
		m, _ := zw.CreateHeader(&zip.FileHeader{Name: "word/media/image1.png", Method: zip.Store})
		_, _ = m.Write(bytes.Repeat([]byte{0x89}, mediaBytes))
	}
	_ = zw.Close()
	return buf.Bytes()
}

var (
	dwOriginalDocx  = dwTestDocx("original", 0)
	dwConvertedDocx = dwTestDocx("converted", 0)
)

func TestCheckDocumentWorkspaceSize(t *testing.T) {
	require.NoError(t, checkDocumentWorkspaceSize("a.docx", dwTestDocx("ok", 200<<10)), "a seal image is fine")

	err := checkDocumentWorkspaceSize("anh.docx", dwTestDocx("x", types.MaxDocumentWorkspaceMediaBytes+1))
	requireAppCode(t, err, apperrors.ErrBadRequest)
	require.Contains(t, err.Error(), "hình ảnh")

	err = checkDocumentWorkspaceSize("big.docx", make([]byte, types.MaxDocumentWorkspaceFileBytes+1))
	requireAppCode(t, err, apperrors.ErrBadRequest)
	require.Contains(t, err.Error(), "vượt giới hạn")

	err = checkDocumentWorkspaceSize("broken.docx", []byte("not a zip"))
	requireAppCode(t, err, apperrors.ErrBadRequest)
}

func TestDocumentWorkspaceSeveralDocumentsPerSession(t *testing.T) {
	fx := newDWFixture(t)
	ctx := context.Background()
	for _, id := range []string{"att-2", "att-3", "att-4", "att-5"} {
		fx.attach.docs[id] = &types.TemporaryDocument{ID: id, SessionID: "sess-1", FileName: "van-ban-" + id + ".docx", FileType: ".docx"}
		fx.attach.bytes[id] = dwTestDocx(id, 0)
	}

	first := fx.create(t)
	require.Equal(t, 1, first.Position)
	require.Equal(t, "vb1", first.Handle())
	second, err := fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", "att-2")
	require.NoError(t, err)
	require.Equal(t, 2, second.Position)

	active, err := fx.svc.GetBySession(ctx, 7, "sess-1")
	require.NoError(t, err)
	require.Equal(t, second.ID, active.ID, "the document just opened is active")

	// opening the same upload again shows its tab instead of a copy
	again, err := fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", "att-docx")
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	active, _ = fx.svc.GetBySession(ctx, 7, "sess-1")
	require.Equal(t, first.ID, active.ID)

	_, err = fx.svc.Activate(ctx, 7, "sess-1", second.ID)
	require.NoError(t, err)
	active, _ = fx.svc.GetBySession(ctx, 7, "sess-1")
	require.Equal(t, second.ID, active.ID)

	_, err = fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", "att-3")
	require.NoError(t, err)
	fourth, err := fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", "att-4")
	require.NoError(t, err)
	_, err = fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", "att-5")
	requireAppCode(t, err, apperrors.ErrConflict)
	require.True(t, strings.Contains(err.Error(), "tối đa 4"), err.Error())

	// each document is addressed on its own
	rc, ws, err := fx.svc.OpenCurrent(ctx, 7, "sess-1", fourth.ID)
	require.NoError(t, err)
	_ = rc.Close()
	require.Equal(t, fourth.ID, ws.ID)
	_, err = fx.svc.Get(ctx, 7, "sess-2", fourth.ID)
	requireAppCode(t, err, apperrors.ErrNotFound)

	// closing a tab frees a slot; the handle is not reused
	require.NoError(t, fx.svc.Remove(ctx, 7, "sess-1", second.ID))
	fifth, err := fx.svc.CreateFromAttachment(ctx, 7, "sess-1", "user-1", "att-5")
	require.NoError(t, err)
	require.Equal(t, "vb5", fifth.Handle())
	list, err := fx.svc.List(ctx, 7, "sess-1")
	require.NoError(t, err)
	require.Len(t, list, 4)
}
