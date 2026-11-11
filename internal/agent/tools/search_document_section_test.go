package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
		legalChunk(t, "c53b-2", "doc-53b", 2, "6. Sửa đổi, bổ sung Điều 9 như sau:\n> “Điều 9. Ngân sách", &vietnamese_legal.LegalMetadata{
			HeadingPath: []string{"Điều 1. Sửa đổi Nghị định 53/2022"}, ArticleNos: []string{"1"}, KhoanNos: []string{"6"},
			SubdivisionRefs: []string{"khoan:6"}, Quoted: true, Source: "vbpl", SourceDocID: "999",
			ProvisionIDs: map[string]string{"dieu": "p-d1", "khoan:6": "p-k6"},
			Amends:       []vietnamese_legal.AmendRef{{Document: "Nghị định số 53/2022/NĐ-CP", Article: "9"}},
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

func TestSearchDocumentSectionFindsAmendingText(t *testing.T) {
	tool, _ := newSectionFixture(t)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"section":"khoản 2 Điều 9","knowledge_id":"doc-53b"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "c53b-2" {
		t.Fatalf("amending chunk not returned: %v", ids)
	}
	if res.Data["match_mode"] != "amendment" || !strings.Contains(fmt.Sprint(res.Data["model_note"]), "not a provision") {
		t.Fatalf("data = %+v", res.Data)
	}
	rows, _ := res.Data["chunks"].([]map[string]interface{})
	if rows[0]["role"] != "amendment" ||
		rows[0]["legal_cite"] != "khoản 6 Điều 1 (sửa đổi Điều 9 Nghị định số 53/2022/NĐ-CP)" ||
		rows[0]["source_url"] != "https://vbpl.vn/van-ban/chi-tiet/999?go=p-k6" {
		t.Fatalf("row = %+v", rows[0])
	}
	// The document's own Điều wins over amending text.
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17","knowledge_id":"doc-53b"}`))
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "c53b-1" || res.Data["match_mode"] != "metadata" {
		t.Fatalf("own Điều 17 = %v %v", ids, res.Data["match_mode"])
	}
}

func TestSearchDocumentSectionLinksTheAskedKhoan(t *testing.T) {
	tool, repo := newSectionFixture(t)
	repo.ordered = append(repo.ordered, legalChunk(t, "c53-9", "doc-53", 9, "Điều 14. Nổ súng\n1. ...\n2. ...", &vietnamese_legal.LegalMetadata{
		ArticleNos: []string{"14"}, KhoanNos: []string{"1", "2"}, SubdivisionRefs: []string{"khoan:1", "khoan:2"},
		Source: "vbpl", SourceDocID: "136051",
		ProvisionIDs: map[string]string{"dieu": "p-d14", "khoan:1": "p-k1", "khoan:2": "p-k2"},
	}))
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{"section":"khoản 2 Điều 14","knowledge_id":"doc-53"}`))
	rows, _ := res.Data["chunks"].([]map[string]interface{})
	if len(rows) != 1 || rows[0]["legal_cite"] != "khoản 2 Điều 14" ||
		rows[0]["source_url"] != "https://vbpl.vn/van-ban/chi-tiet/136051?go=p-k2" {
		t.Fatalf("rows = %+v", rows)
	}
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 14","knowledge_id":"doc-53"}`))
	rows, _ = res.Data["chunks"].([]map[string]interface{})
	if len(rows) != 1 || rows[0]["legal_cite"] != "Điều 14 (khoản 1, 2)" ||
		rows[0]["source_url"] != "https://vbpl.vn/van-ban/chi-tiet/136051?go=p-d14" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestSearchDocumentSectionPointsAppendicesToReadDocument(t *testing.T) {
	tool, _ := newSectionFixture(t)
	for _, section := range []string{"Phụ lục I", "phụ lục", "Mẫu số 02"} {
		res, _ := tool.Execute(context.Background(), json.RawMessage(`{"section":"`+section+`"}`))
		if res.Success || !strings.Contains(res.Error, "are not sections") ||
			!strings.Contains(res.Error, `read_document(id=dN, query="Phụ lục I")`) ||
			!strings.Contains(res.Error, "appendix_file") {
			t.Errorf("%s: error = %q", section, res.Error)
		}
	}
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{"section":"phạm vi điều chỉnh"}`))
	if strings.Contains(res.Error, "are not sections") {
		t.Errorf("a non-appendix section gets no appendix guidance: %q", res.Error)
	}
	if !strings.Contains(tool.Description(), "are not sections") {
		t.Errorf("the description must carry the appendix guidance")
	}
}

// Fable review: read_document and search_document_section show the model
// the current wording too, looked up once for the legal rows only.
func TestSectionAndReadDocumentApplyCurrentWording(t *testing.T) {
	section, _ := newSectionFixture(t)
	var asked [][]string
	wording := func(_ context.Context, ids []string) map[string]string {
		asked = append(asked, ids)
		return map[string]string{"c53-2": "Điều 17. Trách nhiệm\n1. cấp xã [thay cho “cấp huyện” theo …]"}
	}
	section.WithCurrentWording(wording)
	res, err := section.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17","knowledge_id":"doc-53"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	rows, _ := res.Data["chunks"].([]map[string]interface{})
	if len(rows) != 2 || rows[0]["chunk_id"] != "c53-2" {
		t.Fatalf("rows = %+v", rows)
	}
	if !strings.HasPrefix(rows[0]["content"].(string), "Điều 17. Trách nhiệm\n1. ...") ||
		!strings.Contains(fmt.Sprint(rows[0]["model_content"]), "cấp xã [thay cho") {
		t.Fatalf("section row: %+v", rows[0])
	}
	if _, ok := rows[1]["model_content"]; ok {
		t.Fatalf("unchanged row got model_content: %+v", rows[1])
	}
	if _, ok := rows[0]["model_match"]; ok {
		t.Fatal("the heading path is not a text match to recompute")
	}

	reader := NewReadDocumentTool(section.knowledgeService, section.chunkService, section.searchTargets).WithCurrentWording(wording)
	res, err = reader.Execute(context.Background(), json.RawMessage(`{"id":"doc-53"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	ids := asked[len(asked)-1]
	for _, id := range ids {
		if id == "c53-0" {
			t.Fatalf("a chunk without legal structure was looked up: %v", ids)
		}
	}
	rows, _ = res.Data["chunks"].([]map[string]interface{})
	found := false
	for _, r := range rows {
		if r["chunk_id"] == "c53-2" {
			found = strings.Contains(fmt.Sprint(r["model_content"]), "cấp xã") && !strings.Contains(fmt.Sprint(r["content"]), "cấp xã")
		}
	}
	if !found {
		t.Fatalf("read_document rows: %+v", rows)
	}
	if strings.Contains(res.Output, "cấp xã") {
		t.Fatal("the UI output keeps the stored text")
	}
}

// Several sections of one document come back from one call, each in its
// own block, sharing one result; a repeated citation is read once.
func TestSearchDocumentSectionReadsSeveralSections(t *testing.T) {
	tool, _ := newSectionFixture(t)
	res, err := tool.Execute(context.Background(), json.RawMessage(
		`{"sections":["Điều 3","khoản 2 Điều 17","điều 3"],"knowledge_id":"doc-53"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ids := sectionChunkIDs(t, res); len(ids) != 2 || ids[0] != "c53-1" || ids[1] != "c53-2" {
		t.Fatalf("chunks = %v", ids)
	}
	if res.Data["query"] != "Điều 3; Khoản 2 Điều 17" || res.Data["match_count"] != 2 {
		t.Fatalf("data = %+v", res.Data)
	}
	if secs, _ := res.Data["sections"].([]map[string]interface{}); len(secs) != 2 {
		t.Fatalf("sections = %#v", res.Data["sections"])
	}
	if strings.Count(res.Output, "<sections ref=") != 2 {
		t.Fatalf("output = %s", res.Output)
	}

	// section and sections combine; a chunk shown for one is not repeated
	res, _ = tool.Execute(context.Background(), json.RawMessage(
		`{"section":"Điều 17","sections":["khoản 3 Điều 17"],"knowledge_id":"doc-53"}`))
	if ids := sectionChunkIDs(t, res); len(ids) != 2 || ids[0] != "c53-2" || ids[1] != "c53-3" {
		t.Fatalf("chunks = %v", ids)
	}
	if note, _ := res.Data["model_note"].(string); !strings.Contains(note, "Khoản 3 Điều 17 is in the chunk(s) already shown above for Điều 17") {
		t.Fatalf("model_note = %q", note)
	}
}

func TestSearchDocumentSectionCapsTheSectionList(t *testing.T) {
	tool, _ := newSectionFixture(t)
	res, err := tool.Execute(context.Background(), json.RawMessage(
		`{"sections":["Điều 1","Điều 2","Điều 3","Điều 4","Điều 5","Điều 6","Điều 17"],"knowledge_id":"doc-53"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "c53-1" {
		t.Fatalf("only the first six sections are read: %v", ids)
	}
	if note, _ := res.Data["model_note"].(string); !strings.Contains(note, "not read: Điều 17") {
		t.Fatalf("model_note = %q", note)
	}

	// one string naming two parts still points at the list
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"section":"khoản 2 và khoản 3 Điều 17","knowledge_id":"doc-53"}`))
	if err == nil || res.Success || !strings.Contains(res.Error, "entry of sections") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// The result names the neighbouring Điều of the document by title, so the
// next one is picked by title rather than probed by number; quoted
// amending text of another document is not listed.
func TestSearchDocumentSectionListsNearbyArticles(t *testing.T) {
	tool, _ := newSectionFixture(t)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17","knowledge_id":"doc-53"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	note, _ := res.Data["model_note"].(string)
	if !strings.Contains(note, "Nearby Điều of this document (it has 3)") ||
		!strings.Contains(note, "Điều 3. Giải thích từ ngữ") || !strings.Contains(note, "Điều 30. Hiệu lực thi hành") ||
		strings.Contains(note, "Điều 17.") {
		t.Fatalf("model_note = %q", note)
	}
	// the model reads the note with the chunks
	if !strings.Contains(res.Output, "<note>Nearby Điều") {
		t.Fatalf("output = %s", res.Output)
	}

	// an absent Điều says so where the model reads it, with its neighbours
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 18","knowledge_id":"doc-53"}`))
	note, _ = res.Data["model_note"].(string)
	if !strings.Contains(note, "Điều 18 was not found in") || !strings.Contains(note, "Điều 17. Trách nhiệm") {
		t.Fatalf("model_note = %q", note)
	}

	// the amending document lists its own Điều only, never the quoted Điều 9
	arts := documentArticles(func() []*types.Chunk {
		_, repo := newSectionFixture(t)
		var out []*types.Chunk
		for _, c := range repo.ordered {
			if c.KnowledgeID == "doc-53b" {
				out = append(out, c)
			}
		}
		return out
	}())
	if len(arts) != 1 || arts[0].no != "17" {
		t.Fatalf("articles of doc-53b = %+v", arts)
	}

	// a scan over several documents has no single neighbour list
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"section":"Điều 17"}`))
	if note, _ := res.Data["model_note"].(string); strings.Contains(note, "Nearby") {
		t.Fatalf("model_note = %q", note)
	}
}

func TestNearbySectionsNoteWindow(t *testing.T) {
	var chunks []*types.Chunk
	for i := 1; i <= 30; i++ {
		chunks = append(chunks, legalChunk(t, fmt.Sprintf("c%d", i), "doc", i, "x", &vietnamese_legal.LegalMetadata{
			HeadingPath: []string{"Chương I", fmt.Sprintf("Điều %d. Tiêu đề %d", i, i)},
			ArticleNos:  []string{fmt.Sprint(i)},
		}))
	}
	refs := []vietnamese_legal.SectionRef{{Article: "12"}, {Article: "14"}}
	note := nearbySectionsNote(chunks, refs)
	for _, want := range []string{"Điều 7.", "Điều 13.", "Điều 19."} {
		if !strings.Contains(note, want) {
			t.Errorf("missing %s in %q", want, note)
		}
	}
	for _, not := range []string{"Điều 6.", "Điều 12.", "Điều 14.", "Điều 20."} {
		if strings.Contains(note, not) {
			t.Errorf("unexpected %s in %q", not, note)
		}
	}
	// a number past the end sits at the end
	if note := nearbySectionsNote(chunks, []vietnamese_legal.SectionRef{{Article: "40"}}); !strings.Contains(note, "Điều 25.") ||
		!strings.Contains(note, "Điều 30.") || strings.Contains(note, "Điều 24.") {
		t.Errorf("past the end: %q", note)
	}
	if nearbySectionsNote(chunks, []vietnamese_legal.SectionRef{{Heading: "chương i"}}) != "" {
		t.Error("a heading lookup lists no neighbours")
	}
}

// A section cut by the output budget says how much of it was shown; a later
// section is named as not shown unless an earlier one already showed it.
func TestSearchDocumentSectionTruncationAcrossSections(t *testing.T) {
	tool, _ := newSectionFixture(t)
	ctx := WithOutputBudget(context.Background(), 10)
	res, err := tool.Execute(ctx, json.RawMessage(
		`{"sections":["Điều 17","khoản 2 Điều 17","Điều 3"],"knowledge_id":"doc-53"}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ids := sectionChunkIDs(t, res); len(ids) != 1 || ids[0] != "c53-2" {
		t.Fatalf("chunks = %v", ids)
	}
	note, _ := res.Data["model_note"].(string)
	if !strings.Contains(note, "Điều 17: 1 chunk(s) not shown") ||
		!strings.Contains(note, "Điều 3 was not shown") || strings.Contains(note, "Khoản 2 Điều 17 was not shown") {
		t.Fatalf("model_note = %q", note)
	}
	if res.Data["truncated"] != true {
		t.Fatalf("data = %+v", res.Data)
	}
}

func TestSearchDocumentSectionAmbiguousKeepsEverySection(t *testing.T) {
	tool, _ := newSectionFixture(t)
	res, err := tool.Execute(context.Background(), json.RawMessage(
		`{"sections":["Điều 17","Điều 3"],"document_number":"Nghị định 53"}`))
	if err != nil || !res.Success || res.Data["ambiguous"] != true {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if note, _ := res.Data["model_note"].(string); !strings.Contains(note, `sections=["Điều 17", "Điều 3"]`) ||
		!strings.Contains(res.Output, "sections=[&quot;Điều 17&quot;, &quot;Điều 3&quot;]") {
		t.Fatalf("model_note = %q output = %s", note, res.Output)
	}
	if secs, _ := res.Data["sections"].([]map[string]interface{}); len(secs) != 2 {
		t.Fatalf("sections = %#v", res.Data["sections"])
	}
}

// Far-apart sections get a window each, never everything between them.
func TestNearbySectionsNoteFarApart(t *testing.T) {
	var chunks []*types.Chunk
	for i := 1; i <= 200; i++ {
		chunks = append(chunks, legalChunk(t, fmt.Sprintf("c%d", i), "doc", i, "x", &vietnamese_legal.LegalMetadata{
			HeadingPath: []string{fmt.Sprintf("Điều %d. Tiêu đề %d", i, i)},
			ArticleNos:  []string{fmt.Sprint(i)},
		}))
	}
	note := nearbySectionsNote(chunks, []vietnamese_legal.SectionRef{{Article: "3"}, {Article: "190"}})
	lines := strings.Split(note, "\n")
	if len(lines) > sectionNearbyMaxLines+2 {
		t.Fatalf("%d lines: %q", len(lines), note)
	}
	for _, want := range []string{"Điều 8.", "Điều 185.", "Điều 195.", "\n…\n"} {
		if !strings.Contains(note, want) {
			t.Errorf("missing %q in %q", want, note)
		}
	}
	if strings.Contains(note, "Điều 100.") || strings.Contains(note, "Điều 9.") {
		t.Errorf("window leaked: %q", note)
	}
}
