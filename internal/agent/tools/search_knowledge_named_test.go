package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

type namedChunkRepo struct {
	interfaces.ChunkRepository
	chunks map[string]*types.Chunk
}

func (r *namedChunkRepo) ListChunksByIDOnly(_ context.Context, ids []string) ([]*types.Chunk, error) {
	var out []*types.Chunk
	for _, id := range ids {
		if c := r.chunks[id]; c != nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (r *namedChunkRepo) ListChunksByParentIDs(context.Context, uint64, []string) ([]*types.Chunk, error) {
	return nil, nil
}

type namedChunkService struct {
	interfaces.ChunkService
	repo *namedChunkRepo
}

func (s *namedChunkService) GetRepository() interfaces.ChunkRepository { return s.repo }

func namedDoc(id, number, docType, status string) *types.LegalDocument {
	return &types.LegalDocument{ID: id, DocumentNumber: number, NumberKey: vietnamese_legal.CatalogNumberKey(number),
		DocType: docType, Status: status, Title: docType + " " + number}
}

func namedSearchTool(catalog *fakeLegalCatalog, targets types.SearchTargets) (*SearchKnowledgeTool, *fakeKBServiceForLegal) {
	kb := &fakeKBServiceForLegal{}
	know := &readDocKnowledgeService{docs: map[string]*types.Knowledge{
		"k15": {ID: "k15", KnowledgeBaseID: "kb-1", Title: "15-2026-NĐ-CP.html", FileName: "15-2026-NĐ-CP.html"},
	}}
	chunks := &namedChunkService{repo: &namedChunkRepo{chunks: map[string]*types.Chunk{
		"c15-d1": {ID: "c15-d1", KnowledgeID: "k15", KnowledgeBaseID: "kb-1", ChunkIndex: 1, ChunkType: types.ChunkTypeText, Content: "Điều 1. Sửa đổi Điều 9"},
		"c15-d9": {ID: "c15-d9", KnowledgeID: "k15", KnowledgeBaseID: "kb-1", ChunkIndex: 9, ChunkType: types.ChunkTypeText, Content: "Điều 9. Sửa đổi"},
	}}}
	return NewSearchKnowledgeTool(kb, know, chunks, targets, nil, nil).WithLegalCatalog(catalog).
		WithAbbreviationService(&searchStubAbbreviationService{}), kb
}

var namedWholeKB = types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 1}}

func resultIDs(t *testing.T, res *types.ToolResult) []string {
	t.Helper()
	rows, _ := res.Data["results"].([]map[string]interface{})
	var ids []string
	for _, r := range rows {
		id, _ := r["chunk_id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func TestFindArticleNumbersForNamedSearch(t *testing.T) {
	got := strings.Join(vietnamese_legal.FindArticleNumbers("Điều 1 và Điều 10 của Nghị định 15/2026; các điều 3, 4 hoặc 5a"), ",")
	if got != "1,10,3,4,5a" {
		t.Fatalf("articles = %s", got)
	}
}

func TestSearchKnowledgeKeepsTheNamedArticlesOfTheNamedDocument(t *testing.T) {
	d15 := namedDoc("tvpl-15", "15/2026/NĐ-CP", "Nghị định", "Còn hiệu lực")
	catalog := &fakeLegalCatalog{
		named:         []types.LegalNamedCopy{{Document: d15, KnowledgeID: "k15", KnowledgeBaseID: "kb-1"}},
		articleChunks: map[string]string{"k15\x001": "c15-d1", "k15\x009": "c15-d9"},
	}
	tool, kb := namedSearchTool(catalog, namedWholeKB)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Điều 1 và Điều 9 của Nghị định 15/2026/NĐ-CP còn hiệu lực không?"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %+v %v", res, err)
	}
	ids := resultIDs(t, res)
	if len(ids) < 2 || ids[0] != "c15-d1" || ids[1] != "c15-d9" {
		t.Fatalf("results = %v, want the two named articles first", ids)
	}
	if !strings.Contains(res.Output, "đích danh") {
		t.Errorf("the rows should say why they are there")
	}
	scoped := false
	for _, p := range kb.searchParams {
		scoped = scoped || strings.Join(p.KnowledgeIDs, ",") == "k15"
	}
	if !scoped {
		t.Error("the named copy should be searched within itself too")
	}
}

func TestSearchKnowledgeKeepsTheNamedDocumentsBestChunkWithoutArticles(t *testing.T) {
	d15 := namedDoc("tvpl-15", "15/2026/NĐ-CP", "Nghị định", "Còn hiệu lực")
	catalog := &fakeLegalCatalog{named: []types.LegalNamedCopy{{Document: d15, KnowledgeID: "k15", KnowledgeBaseID: "kb-1"}}}
	tool, _ := namedSearchTool(catalog, namedWholeKB)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Nghị định 15/2026/NĐ-CP quy định gì"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %+v %v", res, err)
	}
	if ids := resultIDs(t, res); len(ids) == 0 || ids[0] != "c-k15" {
		t.Fatalf("results = %v, want the named document's retrieved chunk first", ids)
	}
}

func TestSearchKnowledgeDoesNotPinAnAmbiguousNumber(t *testing.T) {
	var copies []types.LegalNamedCopy
	for _, id := range []string{"a", "b", "c"} {
		copies = append(copies, types.LegalNamedCopy{Document: namedDoc(id, "12/2024/QĐ-UBND", "Quyết định", "Còn hiệu lực"),
			KnowledgeID: "k-" + id, KnowledgeBaseID: "kb-1"})
	}
	tool, _ := namedSearchTool(&fakeLegalCatalog{named: copies}, namedWholeKB)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Quyết định 12/2024/QĐ-UBND"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %+v %v", res, err)
	}
	for _, id := range resultIDs(t, res) {
		if strings.HasPrefix(id, "c-k-") {
			t.Fatalf("an ambiguous number must not be pinned: %v", resultIDs(t, res))
		}
	}
	if !strings.Contains(res.Output, "3 văn bản cùng số hiệu") {
		t.Errorf("output should say the number is ambiguous: %s", res.Output)
	}
}

func TestSearchKnowledgeSaysWhenTheNamedDocumentIsNotInScope(t *testing.T) {
	expired := namedDoc("old", "15/2023/NĐ-CP", "Nghị định", "Hết hiệu lực toàn bộ")
	tool, _ := namedSearchTool(&fakeLegalCatalog{named: []types.LegalNamedCopy{{Document: expired}}}, namedWholeKB)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Nghị định 15/2023/NĐ-CP"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %+v %v", res, err)
	}
	if !strings.Contains(res.Output, "toàn văn không còn được giữ") {
		t.Errorf("output should say the text is gone: %s", res.Output)
	}
}

func TestSearchKnowledgeDoesNotWidenANarrowedSearch(t *testing.T) {
	d15 := namedDoc("tvpl-15", "15/2026/NĐ-CP", "Nghị định", "Còn hiệu lực")
	catalog := &fakeLegalCatalog{named: []types.LegalNamedCopy{{Document: d15, KnowledgeID: "k15", KnowledgeBaseID: "kb-1"}}}
	narrowed := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-1", TenantID: 1, KnowledgeIDs: []string{"k-other"}}}
	tool, kb := namedSearchTool(catalog, narrowed)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Nghị định 15/2026/NĐ-CP"}`)); err != nil {
		t.Fatal(err)
	}
	for _, p := range kb.searchParams {
		for _, id := range p.KnowledgeIDs {
			if id == "k15" {
				t.Fatal("a search narrowed to other files must not reach the named copy")
			}
		}
	}
}

func TestSearchKnowledgeNamedStepOffForPatternFilter(t *testing.T) {
	d15 := namedDoc("tvpl-15", "15/2026/NĐ-CP", "Nghị định", "Còn hiệu lực")
	catalog := &fakeLegalCatalog{named: []types.LegalNamedCopy{{Document: d15, KnowledgeID: "k15", KnowledgeBaseID: "kb-1"}}}
	tool, _ := namedSearchTool(catalog, namedWholeKB)
	tool.patternFilter = nil
	tool.legalCatalog = nil
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Nghị định 15/2026/NĐ-CP"}`)); err != nil {
		t.Fatal(err)
	}
	if len(catalog.namedCalls) != 0 {
		t.Fatal("without the catalogue the step does not run")
	}
}

func named15Catalog() *fakeLegalCatalog {
	d15 := namedDoc("tvpl-15", "15/2026/NĐ-CP", "Nghị định", "Còn hiệu lực")
	return &fakeLegalCatalog{
		named:         []types.LegalNamedCopy{{Document: d15, KnowledgeID: "k15", KnowledgeBaseID: "kb-1"}},
		articleChunks: map[string]string{"k15\x001": "c15-d1", "k15\x009": "c15-d9"},
	}
}

// TestSearchKnowledgeDoesNotRepinInTheSameTurn: a later search of the turn
// is not crowded by chunks an earlier round already showed.
func TestSearchKnowledgeDoesNotRepinInTheSameTurn(t *testing.T) {
	tool, _ := namedSearchTool(named15Catalog(), namedWholeKB)
	q := `{"query":"Điều 1 và Điều 9 của Nghị định 15/2026/NĐ-CP còn hiệu lực không?"}`
	first, err := tool.Execute(turnCtx("m1", 1), json.RawMessage(q))
	if err != nil || !first.Success {
		t.Fatalf("round 1: %+v %v", first, err)
	}
	if ids := resultIDs(t, first); len(ids) == 0 || ids[0] != "c15-d1" {
		t.Fatalf("round 1 = %v", ids)
	}
	second, err := tool.Execute(turnCtx("m1", 2), json.RawMessage(`{"query":"Nghị định 15/2026/NĐ-CP bị bãi bỏ bởi văn bản nào"}`))
	if err != nil || !second.Success {
		t.Fatalf("round 2: %+v %v", second, err)
	}
	for _, id := range resultIDs(t, second) {
		if id == "c15-d1" || id == "c15-d9" {
			t.Fatalf("round 2 pinned again: %v", resultIDs(t, second))
		}
	}
	// a new turn starts over
	third, _ := tool.Execute(turnCtx("m2", 1), json.RawMessage(q))
	if ids := resultIDs(t, third); len(ids) == 0 || ids[0] != "c15-d1" {
		t.Fatalf("new turn = %v", ids)
	}
}

func TestSearchKnowledgeNamedPinsRespectTheLimit(t *testing.T) {
	tool, _ := namedSearchTool(named15Catalog(), namedWholeKB)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Điều 1 và Điều 9 của Nghị định 15/2026/NĐ-CP","limit":1}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %+v %v", res, err)
	}
	if ids := resultIDs(t, res); len(ids) != 1 || ids[0] != "c15-d1" {
		t.Fatalf("results = %v, want one row", ids)
	}
}

func TestSearchKnowledgeNamedNoteOnlyWhenTheSearchCoversWholeBases(t *testing.T) {
	missing := namedDoc("d20", "20/2026/NĐ-CP", "Nghị định", "Còn hiệu lực")
	narrowed := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-1", TenantID: 1, KnowledgeIDs: []string{"k-file"}}}
	tool, _ := namedSearchTool(&fakeLegalCatalog{named: []types.LegalNamedCopy{{Document: missing}}}, narrowed)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Nghị định 20/2026/NĐ-CP"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %+v %v", res, err)
	}
	if _, ok := res.Data["legal_named_note"]; ok {
		t.Fatalf("a narrowed search must not claim the document is missing: %v", res.Data["legal_named_note"])
	}
	whole, _ := namedSearchTool(&fakeLegalCatalog{named: []types.LegalNamedCopy{{Document: missing}}}, namedWholeKB)
	res, _ = whole.Execute(context.Background(), json.RawMessage(`{"query":"Nghị định 20/2026/NĐ-CP"}`))
	if note, _ := res.Data["legal_named_note"].(string); !strings.Contains(note, "không có toàn văn") {
		t.Fatalf("note = %q", note)
	}
}
