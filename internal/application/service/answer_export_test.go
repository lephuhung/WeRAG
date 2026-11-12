package service

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type exportSessionRepo struct {
	interfaces.SessionRepository
}

func (exportSessionRepo) Get(_ context.Context, tenantID uint64, ownerID, id string) (*types.Session, error) {
	if tenantID != 1 || ownerID != "user-1" || id != "s1" {
		return nil, apperrors.ErrSessionNotFound
	}
	return &types.Session{ID: id, TenantID: 1, UserID: ownerID}, nil
}

func (exportSessionRepo) GetIMPlatform(context.Context, uint64, string) (string, error) {
	return "", nil
}

type exportMessageRepo struct {
	interfaces.MessageRepository
	answer *types.Message
}

func (r *exportMessageRepo) GetMessage(_ context.Context, sessionID, id string) (*types.Message, error) {
	if sessionID != "s1" || id != r.answer.ID {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *r.answer
	return &cp, nil
}

func (r *exportMessageRepo) GetMessagesByRequestIDs(context.Context, string, []string) ([]*types.MessageWithSession, error) {
	return nil, nil
}

type exportKBNames struct {
	interfaces.KnowledgeBaseService
}

func (exportKBNames) GetKnowledgeBasesByIDsOnly(_ context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	names := map[string]string{"kb-t": "CSTT chung", "kb-r": "CSTT hạn chế"}
	var out []*types.KnowledgeBase
	for _, id := range ids {
		out = append(out, &types.KnowledgeBase{ID: id, Name: names[id]})
	}
	return out, nil
}

// Sources of a knowledge base the caller can no longer read, and of a
// document that no longer resolves, are left out of the file — title and
// knowledge base name alike — the way history hides them.
func TestAnswerExportHidesUnreadableSources(t *testing.T) {
	grants, _ := restrictedReadFixture()
	know := &citationKnowledgeService{docs: map[string]*types.Knowledge{
		"doc-t": {ID: "doc-t", TenantID: 1, KnowledgeBaseID: "kb-t"},
		"doc-r": {ID: "doc-r", TenantID: 1, KnowledgeBaseID: "kb-r"},
	}}
	answer := &types.Message{ID: "a1", SessionID: "s1", Role: "assistant", IsCompleted: true,
		CreatedAt: time.Date(2026, 10, 16, 7, 5, 0, 0, time.UTC), Content: "Câu trả lời.",
		KnowledgeReferences: types.References{
			{ID: "c1", KnowledgeID: "doc-t", KnowledgeBaseID: "kb-t", KnowledgeTitle: "Tài liệu đọc được"},
			{ID: "c2", KnowledgeID: "doc-r", KnowledgeBaseID: "kb-r", KnowledgeTitle: "Tài liệu bị hạn chế"},
			{ID: "c3", KnowledgeID: "doc-r", KnowledgeTitle: "Hạn chế qua tài liệu"},
			{ID: "c4", KnowledgeID: "doc-gone", KnowledgeTitle: "Tài liệu đã xoá"},
		}}
	svc := NewAnswerExportService(&exportMessageRepo{answer: answer}, exportSessionRepo{}, exportKBNames{}, know, grants, nil, nil)

	file, err := svc.Export(restrictedMemberCtx(), "s1", "a1")
	require.NoError(t, err)
	doc := docxDocumentXML(t, file.Data)
	require.Contains(t, doc, ">Tài liệu đọc được</w:t>")
	require.Contains(t, doc, "> — CSTT chung</w:t>")
	for _, hidden := range []string{"Tài liệu bị hạn chế", "Hạn chế qua tài liệu", "CSTT hạn chế", "Tài liệu đã xoá"} {
		require.NotContains(t, doc, hidden)
	}
	require.Equal(t, "Trả lời WeRAG.docx", file.FileName) // no question found

	// with an invite the restricted source is back (the dangling one is not)
	grants.invites["kb-r/user-1"] = true
	file, err = svc.Export(restrictedMemberCtx(), "s1", "a1")
	require.NoError(t, err)
	doc = docxDocumentXML(t, file.Data)
	require.Contains(t, doc, ">Tài liệu bị hạn chế</w:t>")
	require.Contains(t, doc, "> — CSTT hạn chế</w:t>")
	require.NotContains(t, doc, "Tài liệu đã xoá")

	// another caller of the same tenant does not see the session at all
	other := context.WithValue(restrictedMemberCtx(), types.UserIDContextKey, "user-2")
	_, err = svc.Export(other, "s1", "a1")
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)
}

func docxDocumentXML(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			require.NoError(t, err)
			defer rc.Close()
			b, err := io.ReadAll(rc)
			require.NoError(t, err)
			return string(b)
		}
	}
	t.Fatal("document.xml missing")
	return ""
}
