package service

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
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

type exportReviewRepo struct {
	interfaces.LegalReviewRepository
}

func (exportReviewRepo) ApprovedLinksForKnowledge(_ context.Context, ids []string) (map[string]*types.LegalDocumentLink, error) {
	out := map[string]*types.LegalDocumentLink{}
	for _, id := range ids {
		switch id {
		case "k-nd80":
			out[id] = &types.LegalDocumentLink{KnowledgeID: id, LegalDocumentID: "vb-80"}
		case "k-luat59":
			out[id] = &types.LegalDocumentLink{KnowledgeID: id, LegalDocumentID: "vb-59"}
		}
	}
	return out, nil
}

type exportCatalogRepo struct {
	interfaces.LegalCatalogRepository
}

func (exportCatalogRepo) GetByIDs(context.Context, []string) ([]*types.LegalDocument, error) {
	return []*types.LegalDocument{
		{ID: "vb-80", DocType: "Nghị định", DocumentNumber: "80/2021/NĐ-CP",
			Title: "Quy định chi tiết và hướng dẫn thi hành một số điều của Luật Hỗ trợ doanh nghiệp nhỏ và vừa"},
		{ID: "vb-59", DocType: "Luật", DocumentNumber: "59/2020/QH14", Title: "Luật Doanh nghiệp"},
	}, nil
}

func ref(id, knowledgeID, kb, title string) *types.SearchResult {
	return &types.SearchResult{ID: id, KnowledgeID: knowledgeID, KnowledgeBaseID: kb, KnowledgeTitle: title}
}

// Only cited sources are listed, in order of first citation, matched by
// chunk, by document title or by URL; legal ones carry the catalogue title.
func TestAnswerExportListsCitedSourcesOnly(t *testing.T) {
	refs := types.References{
		ref("c-lam", "k-lam", "kb-t", "Thông tư số 22-2024-TT-BNNPTNT lâm nghiệp.html"),
		ref("c-80a", "k-nd80", "kb-t", "Nghị định số 80-2021-NĐ-CP Quy định chi tiết.html"),
		ref("c-80b", "k-nd80", "kb-t", "Nghị định số 80-2021-NĐ-CP Quy định chi tiết.html"),
		ref("c-59", "k-luat59", "kb-t", "Luật doanh nghiệp số 59-2020-QH14.html"),
		ref("c-cand", "k-cand", "kb-t", "Thông tư số 03-2023-TT-BCA kế toán trưởng CAND.html"),
		ref("c-hd", "k-hd", "kb-t", "Hướng dẫn nội bộ.docx"),
		{ID: "https://a.vn/x", KnowledgeID: "https://a.vn/x", KnowledgeTitle: "Trang A", ChunkType: string(types.ChunkTypeWebSearch),
			Metadata: map[string]string{"url": "https://a.vn/x"}},
	}
	content := `Theo Luật <kb doc="Luật doanh nghiệp số 59-2020-QH14.html" chunk_id="c-59" kb_id="kb-t" /> và Nghị định ` +
		`<kb doc="x" chunk_id="c-80b" kb_id="kb-t" />, lại <kb doc="x" chunk_id="c-80a" kb_id="kb-t" />; ` +
		`hướng dẫn <kb doc="Hướng dẫn nội bộ.docx" chunk_id="c-khac" kb_id="kb-t" />; web <web url="https://a.vn/x" title="A" /> ` +
		`và <web url="https://b.vn/y" title="Trang B" />; nguồn không đọc được <kb doc="Bí mật.html" chunk_id="c-an" kb_id="kb-r" />.`
	got := citedReferences(content, refs)
	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	require.Equal(t, []string{"c-59", "c-80b", "c-80a", "c-hd", "https://a.vn/x", "https://b.vn/y"}, ids)

	answer := &types.Message{ID: "a1", SessionID: "s1", Role: "assistant", IsCompleted: true,
		CreatedAt: time.Date(2026, 10, 16, 7, 5, 0, 0, time.UTC), Content: content, KnowledgeReferences: refs}
	svc := NewAnswerExportService(&exportMessageRepo{answer: answer}, exportSessionRepo{}, exportKBNames{}, nil, nil,
		exportReviewRepo{}, exportCatalogRepo{})
	file, err := svc.Export(newSharedAccessContext(), "s1", "a1")
	require.NoError(t, err)
	doc := docxDocumentXML(t, file.Data)
	for _, want := range []string{
		">Luật Doanh nghiệp số 59/2020/QH14</w:t>",
		">Nghị định số 80/2021/NĐ-CP Quy định chi tiết và hướng dẫn thi hành một số điều của Luật Hỗ trợ doanh nghiệp nhỏ và vừa</w:t>",
		">Hướng dẫn nội bộ</w:t>", ">Trang A</w:t>", ">Trang B</w:t>", "> — https://b.vn/y</w:t>",
	} {
		require.Contains(t, doc, want)
	}
	for _, absent := range []string{"lâm nghiệp", "CAND", "Bí mật", ".html", ".docx", "Số hiệu"} {
		require.NotContains(t, doc, absent)
	}
	require.Less(t, strings.Index(doc, "Luật Doanh nghiệp số"), strings.Index(doc, "Nghị định số 80/2021"))
	require.Equal(t, 1, strings.Count(doc, "Nghị định số 80/2021/NĐ-CP Quy định"))
}

// Without citation tags the list falls back to the first documents
// retrieved, at most maxUncitedReferences of them.
func TestAnswerExportUncitedAnswerCapsTheList(t *testing.T) {
	var refs types.References
	for i := 0; i < 15; i++ {
		id := string(rune('a' + i))
		refs = append(refs, ref("c-"+id+"1", "k-"+id, "kb-t", "Văn bản "+id+".html"), ref("c-"+id+"2", "k-"+id, "kb-t", "Văn bản "+id+".html"))
	}
	got := citedReferences("Không trích dẫn.", refs)
	docs := map[string]bool{}
	for _, r := range got {
		docs[r.KnowledgeID] = true
	}
	require.Len(t, docs, maxUncitedReferences)
	require.Len(t, got, 2*maxUncitedReferences) // both chunks of each kept document
	// tags that match nothing fall back the same way
	require.Len(t, citedReferences(`x <kb doc="khác" chunk_id="c-zz" />`, refs), 2*maxUncitedReferences)
}
