package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// ListChunksByKnowledgeID is the whole-document listing search_document_section
// reads; the read_document fake only paged.
func (r *readDocChunkRepo) ListChunksByKnowledgeID(
	_ context.Context, _ uint64, knowledgeID string,
) ([]*types.Chunk, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*types.Chunk
	for _, c := range r.ordered {
		if c.KnowledgeID == knowledgeID && c.ChunkType == types.ChunkTypeText {
			out = append(out, c)
		}
	}
	return out, nil
}

func legalChunk(t *testing.T, id, knowledgeID string, index int, content string, meta *vietnamese_legal.LegalMetadata) *types.Chunk {
	t.Helper()
	c := &types.Chunk{
		ID: id, TenantID: 7, KnowledgeID: knowledgeID, KnowledgeBaseID: "kb-1",
		ChunkIndex: index, ChunkType: types.ChunkTypeText, IsEnabled: true, Content: content,
	}
	if meta != nil {
		if err := c.SetDocumentMetadata(&types.DocumentChunkMetadata{Legal: meta}); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func newSectionFixture(t *testing.T) (*SearchDocumentSectionTool, *readDocChunkRepo) {
	t.Helper()
	decree := &types.Knowledge{
		ID: "doc-53", TenantID: 7, KnowledgeBaseID: "kb-1", Title: "Nghị định 53/2022/NĐ-CP về an ninh mạng",
		Type: "file", FileName: "nd53.pdf", ParseStatus: "completed",
		Profile: &types.KnowledgeProfile{DocumentNumber: "53/2022/NĐ-CP", DocTypeCode: "nghi_dinh"},
	}
	decree2 := &types.Knowledge{
		ID: "doc-53b", TenantID: 7, KnowledgeBaseID: "kb-1", Title: "Nghị định 53/2024/NĐ-CP sửa đổi",
		Type: "file", FileName: "nd53-2024.pdf", ParseStatus: "completed",
		Profile: &types.KnowledgeProfile{DocumentNumber: "53/2024/NĐ-CP", DocTypeCode: "nghi_dinh"},
	}
	legacy := &types.Knowledge{
		ID: "doc-old", TenantID: 7, KnowledgeBaseID: "kb-1", Title: "Quyết định 12/QĐ-UBND (cũ)",
		Type: "file", FileName: "qd12.pdf", ParseStatus: "completed",
	}
	chunks := []*types.Chunk{
		legalChunk(t, "c53-0", "doc-53", 0, "CHÍNH PHỦ ... Số: 53/2022/NĐ-CP", nil),
		legalChunk(t, "c53-1", "doc-53", 1, "Điều 3. Giải thích từ ngữ\n1. ...", &vietnamese_legal.LegalMetadata{
			HeadingPath: []string{"Chương I. QUY ĐỊNH CHUNG", "Điều 3. Giải thích từ ngữ"},
			ArticleNos:  []string{"3"}, KhoanNos: []string{"1", "2"},
			SubdivisionRefs: []string{"khoan:1", "khoan:2"},
		}),
		legalChunk(t, "c53-2", "doc-53", 2, "Điều 17. Trách nhiệm\n1. ...\n2. ...\na) ...", &vietnamese_legal.LegalMetadata{
			HeadingPath: []string{"Chương II. TỔ CHỨC THỰC HIỆN", "Điều 17. Trách nhiệm"},
			ArticleNos:  []string{"17"}, KhoanNos: []string{"1", "2"}, DiemLabels: []string{"a"},
			SubdivisionRefs: []string{"khoan:1", "khoan:2", "khoan:2/diem:a"},
		}),
		legalChunk(t, "c53-3", "doc-53", 3, "3. ...\nb) ...", &vietnamese_legal.LegalMetadata{
			HeadingPath: []string{"Chương II. TỔ CHỨC THỰC HIỆN", "Điều 17. Trách nhiệm"},
			ArticleNos:  []string{"17"}, KhoanNos: []string{"3"}, DiemLabels: []string{"b"},
			SubdivisionRefs: []string{"khoan:3", "khoan:3/diem:b"},
		}),
		legalChunk(t, "c53-4", "doc-53", 4, "Điều 30. Hiệu lực thi hành", &vietnamese_legal.LegalMetadata{
			HeadingPath: []string{"Chương III. ĐIỀU KHOẢN THI HÀNH", "Điều 30. Hiệu lực thi hành"},
			ArticleNos:  []string{"30"},
		}),
		legalChunk(t, "c53b-1", "doc-53b", 1, "Điều 17. Sửa đổi", &vietnamese_legal.LegalMetadata{
			HeadingPath: []string{"Điều 17. Sửa đổi"}, ArticleNos: []string{"17"},
		}),
		legalChunk(t, "cold-1", "doc-old", 1, "## Điều 3. Phạm vi\nnội dung điều 3", nil),
		legalChunk(t, "cold-2", "doc-old", 2, "## Điều 30. Hiệu lực\ntheo Điều 3 của Luật", nil),
	}
	repo := &readDocChunkRepo{ordered: chunks}
	service := &readDocKnowledgeService{
		docs:  map[string]*types.Knowledge{decree.ID: decree, decree2.ID: decree2, legacy.ID: legacy},
		pages: map[string]*types.PageResult{"kb-1": {Data: []*types.Knowledge{decree, decree2, legacy}}},
	}
	tool := NewSearchDocumentSectionTool(service, &readDocChunkService{repo: repo}, types.SearchTargets{{
		Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 7,
	}})
	return tool, repo
}

func sectionChunkIDs(t *testing.T, res *types.ToolResult) []string {
	t.Helper()
	return chunkIDsFromData(t, res.Data)
}

func TestSearchDocumentSectionValidatesTheCitation(t *testing.T) {
	tool, _ := newSectionFixture(t)
	for _, args := range []string{`{}`, `{"section":"phạm vi điều chỉnh"}`, `{"section":"khoản 2 và khoản 3 Điều 17"}`} {
		res, err := tool.Execute(context.Background(), json.RawMessage(args))
		if err == nil || res.Success {
			t.Errorf("%s must be rejected: res=%+v err=%v", args, res, err)
		}
	}
}

func TestSearchDocumentSectionByKnowledgeID(t *testing.T) {
	tool, _ := newSectionFixture(t)
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"section":"khoản 2 Điều 17","knowledge_id":"doc-53"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "c53-2" {
		t.Fatalf("khoản 2 Điều 17 must hit only its chunk: %v", ids)
	}
	if res.Data["query"] != "Khoản 2 Điều 17" || res.Data["match_mode"] != "metadata" || res.Data["match_count"] != 1 {
		t.Fatalf("data = %+v", res.Data)
	}
	if !strings.Contains(res.Output, `document_number="53/2022/NĐ-CP"`) ||
		!strings.Contains(res.Output, `heading_path="Chương II. TỔ CHỨC THỰC HIỆN &gt; Điều 17. Trách nhiệm"`) {
		t.Fatalf("output = %s", res.Output)
	}
	rows, _ := res.Data["chunks"].([]map[string]interface{})
	if rows[0]["knowledge_title"] == "" || rows[0]["role"] != "match" {
		t.Fatalf("row = %+v", rows[0])
	}

	// A whole Điều spans its split chunks; Điều 3 never matches Điều 30.
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17","knowledge_id":"doc-53"}`))
	if ids := sectionChunkIDs(t, res); len(ids) != 2 || ids[0] != "c53-2" || ids[1] != "c53-3" {
		t.Fatalf("Điều 17 chunks = %v", ids)
	}
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 3","knowledge_id":"doc-53"}`))
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "c53-1" {
		t.Fatalf("Điều 3 chunks = %v", ids)
	}
	// Điểm belongs to one Khoản: điểm a is under khoản 2, not khoản 3.
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"điểm a khoản 3 Điều 17","knowledge_id":"doc-53"}`))
	if ids := sectionChunkIDs(t, res); len(ids) != 0 || !strings.Contains(res.Output, "<empty>") {
		t.Fatalf("cross-product match leaked: %v / %s", ids, res.Output)
	}
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"Chương II","knowledge_id":"doc-53"}`))
	if ids := sectionChunkIDs(t, res); len(ids) != 2 {
		t.Fatalf("Chương II chunks = %v", ids)
	}
}

func TestSearchDocumentSectionResolvesDocumentNumber(t *testing.T) {
	tool, _ := newSectionFixture(t)
	for _, number := range []string{"53/2022/NĐ-CP", "Nghị định 53/2022", "nghị định số 53 năm 2022"} {
		res, err := tool.Execute(context.Background(),
			json.RawMessage(`{"section":"Điều 17","document_number":"`+number+`"}`))
		if err != nil || !res.Success {
			t.Fatalf("%s: res=%+v err=%v", number, res, err)
		}
		if res.Data["knowledge_id"] != "doc-53" {
			t.Fatalf("%s resolved to %v", number, res.Data["knowledge_id"])
		}
	}
	// A bare number that fits two decrees is reported, never guessed.
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17","document_number":"Nghị định 53"}`))
	if err != nil || !res.Success || res.Data["ambiguous"] != true || res.Data["display_type"] != "document_info" {
		t.Fatalf("ambiguous: res=%+v err=%v", res, err)
	}
	rows, _ := res.Data["documents"].([]map[string]interface{})
	if len(rows) != 2 || !strings.Contains(res.Output, "knowledge_id=dN") {
		t.Fatalf("ambiguous rows = %+v output=%s", rows, res.Output)
	}
	// A type that contradicts the profile excludes the document.
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17","document_number":"Thông tư 53"}`))
	if err == nil || res.Success {
		t.Fatalf("unknown number must fail: res=%+v err=%v", res, err)
	}
}

func TestSearchDocumentSectionScansScopeAndGroupsByDocument(t *testing.T) {
	tool, _ := newSectionFixture(t)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	ids := sectionChunkIDs(t, res)
	if len(ids) != 3 || ids[2] != "c53b-1" {
		t.Fatalf("chunks across documents = %v", ids)
	}
	docs, _ := res.Data["documents"].([]map[string]interface{})
	if len(docs) != 2 || docs[0]["knowledge_id"] != "doc-53" || docs[1]["knowledge_id"] != "doc-53b" {
		t.Fatalf("documents = %+v", docs)
	}
	if _, ok := res.Data["document"]; ok {
		t.Fatal("a multi-document result carries no single document header")
	}
	if !strings.Contains(res.Output, `<sections ref="Điều 17" documents="2" matches="3">`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestSearchDocumentSectionFallsBackToHeadingsWithoutMetadata(t *testing.T) {
	tool, _ := newSectionFixture(t)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 3","knowledge_id":"doc-old"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "cold-1" || res.Data["match_mode"] != "content" {
		t.Fatalf("content fallback = %v data=%+v", ids, res.Data)
	}
	// Khoản cannot be resolved from body text: the whole Điều comes back
	// with a note instead of nothing.
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"khoản 2 Điều 3","knowledge_id":"doc-old"}`))
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "cold-1" || !strings.Contains(res.Output, "<note>") {
		t.Fatalf("khoản without metadata must return the Điều with a note: %v / %s", ids, res.Output)
	}
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 9","knowledge_id":"doc-old"}`))
	if ids := sectionChunkIDs(t, res); len(ids) != 0 || !strings.Contains(res.Output, "no legal structure metadata") {
		t.Fatalf("missing Điều without metadata must explain itself: %v / %s", ids, res.Output)
	}
}

func TestSearchDocumentSectionRejectsOutOfScopeDocument(t *testing.T) {
	tool, _ := newSectionFixture(t)
	tool.searchTargets = types.SearchTargets{{
		Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-1", TenantID: 7, KnowledgeIDs: []string{"doc-53b"},
	}}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17","knowledge_id":"doc-53"}`))
	if err == nil || res.Success {
		t.Fatalf("pinned scope must reject other documents: res=%+v err=%v", res, err)
	}
	// Scanning the scope only reaches the pinned document.
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "c53b-1" {
		t.Fatalf("scope scan = %v", ids)
	}
}
