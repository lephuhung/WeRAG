package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// fakeKBServiceForLegal answers the whole-KB search with a hit on the law
// and a search restricted to knowledge IDs with one chunk per document.
type fakeKBServiceForLegal struct {
	fakeKBServiceForAbbreviation
}

func (f *fakeKBServiceForLegal) HybridSearch(
	_ context.Context, _ string, params types.SearchParams,
) ([]*types.SearchResult, error) {
	f.searchParams = append(f.searchParams, params)
	if len(params.KnowledgeIDs) == 0 {
		return []*types.SearchResult{{ID: "c-law", KnowledgeID: "k-law", KnowledgeBaseID: "kb-1",
			KnowledgeTitle: "law.html", Content: "Điều 1. Thuế thu nhập cá nhân", Score: 0.9}}, nil
	}
	var out []*types.SearchResult
	for _, id := range params.KnowledgeIDs {
		out = append(out, &types.SearchResult{ID: "c-" + id, KnowledgeID: id, KnowledgeBaseID: "kb-1",
			KnowledgeTitle: id + ".html", Content: "Điều 3. Giảm trừ gia cảnh", Score: 0.6})
	}
	return out, nil
}

func TestSearchKnowledgeExpandsLegalRelations(t *testing.T) {
	law := &types.LegalDocument{ID: "law", DocType: "Luật", DocumentNumber: "109/2025/QH15",
		Title: "Luật Thuế thu nhập cá nhân số 109/2025/QH15", Status: "Còn hiệu lực"}
	doc := func(id, num, status string) *types.LegalDocument {
		return &types.LegalDocument{ID: id, DocType: "Nghị định", DocumentNumber: num, Title: "Nghị định " + num, Status: status}
	}
	held := func(kid, kb string) []types.LegalKnowledgeRef {
		return []types.LegalKnowledgeRef{{KnowledgeID: kid, KnowledgeBaseID: kb, KnowledgeBaseName: "Nghị định"}}
	}
	guide := func(d *types.LegalDocument, refs []types.LegalKnowledgeRef) types.LegalExpansion {
		return types.LegalExpansion{Anchor: law, AnchorKnowledgeID: "k-law", Related: types.LegalRelatedDocument{
			Document: d, Depth: 1, ParentID: "law", Relation: types.LegalRelationGuides, Incoming: true, Knowledge: refs}}
	}
	catalog := &fakeLegalCatalog{expansions: []types.LegalExpansion{
		guide(doc("nd", "12/2026/NĐ-CP", "Còn hiệu lực"), held("k-nd", "kb-1")),
		guide(doc("nd-old", "65/2013/NĐ-CP", "Hết hiệu lực toàn bộ"), held("k-nd-old", "kb-1")),
		guide(doc("nd-missing", "20/2026/NĐ-CP", "Còn hiệu lực"), nil),
		guide(doc("nd-elsewhere", "30/2026/NĐ-CP", "Còn hiệu lực"), held("k-other", "kb-2")),
	}}
	kb := &fakeKBServiceForLegal{}
	tool := NewSearchKnowledgeTool(kb, nil, nil, types.SearchTargets{{
		Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 1,
	}}, nil, nil).WithLegalCatalog(catalog)

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"giảm trừ gia cảnh"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: res=%+v err=%v", res, err)
	}
	if strings.Join(catalog.gotHits, ",") != "k-law" {
		t.Fatalf("expanded hits = %v", catalog.gotHits)
	}
	// Only the in-force, held, in-scope decree is searched.
	var restricted [][]string
	for _, p := range kb.searchParams {
		if len(p.KnowledgeIDs) > 0 {
			restricted = append(restricted, p.KnowledgeIDs)
		}
	}
	if len(restricted) != 1 || strings.Join(restricted[0], ",") != "k-nd" {
		t.Fatalf("restricted searches = %v", restricted)
	}
	rows, _ := res.Data["results"].([]map[string]interface{})
	if len(rows) != 2 || rows[1]["knowledge_id"] != "k-nd" {
		t.Fatalf("results = %v", rows)
	}
	if meta, _ := rows[1]["knowledge_metadata"].(string); !strings.Contains(meta,
		"Văn bản liên quan: quy định chi tiết, hướng dẫn Luật Thuế thu nhập cá nhân số 109/2025/QH15") {
		t.Fatalf("related chunk metadata = %q", meta)
	}
	note, _ := res.Data["legal_related_note"].(string)
	for _, want := range []string{
		"65/2013/NĐ-CP (Hết hiệu lực toàn bộ)", "đã hết hiệu lực toàn bộ, không dùng nội dung",
		"20/2026/NĐ-CP (Còn hiệu lực)", "chưa có trong kho tài liệu",
		"30/2026/NĐ-CP (Còn hiệu lực)", "ngoài phạm vi tìm kiếm",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "12/2026/NĐ-CP") {
		t.Errorf("the searched decree must not be listed as catalogue-only:\n%s", note)
	}
}

// fakeKBServiceTwoBases holds the old law in kb-1 and its replacement in kb-2.
type fakeKBServiceTwoBases struct {
	fakeKBServiceForAbbreviation
}

func (f *fakeKBServiceTwoBases) GetKnowledgeBasesByIDsOnly(
	_ context.Context, ids []string,
) ([]*types.KnowledgeBase, error) {
	out := make([]*types.KnowledgeBase, 0, len(ids))
	for _, id := range ids {
		out = append(out, kbWithIndexes(id, true, true, ""))
	}
	return out, nil
}

func (f *fakeKBServiceTwoBases) ResolveEmbeddingModelKeys(
	context.Context, []*types.KnowledgeBase,
) map[string]string {
	return map[string]string{"kb-1": "", "kb-2": ""}
}

func (f *fakeKBServiceTwoBases) HybridSearch(
	_ context.Context, _ string, params types.SearchParams,
) ([]*types.SearchResult, error) {
	f.searchParams = append(f.searchParams, params)
	if len(params.KnowledgeIDs) == 0 {
		return []*types.SearchResult{{ID: "c-old", KnowledgeID: "k-old", KnowledgeBaseID: "kb-1",
			KnowledgeTitle: "29.2018QH14.pdf", Content: "Điều 5. Các hành vi bị nghiêm cấm", Score: 0.9}}, nil
	}
	return []*types.SearchResult{{ID: "c-new", KnowledgeID: "k-new", KnowledgeBaseID: "kb-2",
		KnowledgeTitle: "117-2025-QH15.md", Content: "Điều 5. Các hành vi bị nghiêm cấm", Score: 0.7}}, nil
}

// A knowledge_base_ids filter from the model is ignored: the search covers
// every bound base, and the old law's replacement held by another base is
// added after the hit.
func TestSearchKnowledgeIgnoresModelBaseFilter(t *testing.T) {
	oldLaw := &types.LegalDocument{ID: "old", DocType: "Luật", DocumentNumber: "29/2018/QH14",
		Title: "Luật Bảo vệ bí mật nhà nước số 29/2018/QH14", Status: "Còn hiệu lực"}
	newLaw := &types.LegalDocument{ID: "new", DocType: "Luật", DocumentNumber: "117/2025/QH15",
		Title: "Luật Bảo vệ bí mật nhà nước số 117/2025/QH15", Status: "Còn hiệu lực"}
	catalog := &fakeLegalCatalog{expansions: []types.LegalExpansion{{
		Anchor: oldLaw, AnchorKnowledgeID: "k-old", Related: types.LegalRelatedDocument{
			Document: newLaw, Depth: 1, ParentID: "old", Relation: types.LegalRelationReplaces, Incoming: true,
			Knowledge: []types.LegalKnowledgeRef{{KnowledgeID: "k-new", KnowledgeBaseID: "kb-2"}},
		},
	}}}
	kb := &fakeKBServiceTwoBases{}
	tool := NewSearchKnowledgeTool(kb, nil, nil, types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 1},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-2", TenantID: 1},
	}, nil, nil).WithLegalCatalog(catalog)

	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"query":"lưu giữ bí mật nhà nước","knowledge_base_ids":["kb-1"]}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: res=%+v err=%v", res, err)
	}
	var restricted []types.SearchParams
	for _, p := range kb.searchParams {
		if len(p.KnowledgeIDs) > 0 {
			restricted = append(restricted, p)
		} else if strings.Join(p.KnowledgeBaseIDs, ",") != "kb-1,kb-2" {
			t.Fatalf("hit search covered %v, want every bound base", p.KnowledgeBaseIDs)
		}
	}
	if len(restricted) != 1 || strings.Join(restricted[0].KnowledgeIDs, ",") != "k-new" {
		t.Fatalf("restricted searches = %+v", restricted)
	}
	rows, _ := res.Data["results"].([]map[string]interface{})
	if len(rows) != 2 || rows[1]["knowledge_id"] != "k-new" {
		t.Fatalf("results = %v", rows)
	}
	if meta, _ := rows[1]["knowledge_metadata"].(string); !strings.Contains(meta,
		"Văn bản liên quan: thay thế Luật Bảo vệ bí mật nhà nước số 29/2018/QH14") {
		t.Fatalf("replacement chunk metadata = %q", meta)
	}
	if note, _ := res.Data["legal_related_note"].(string); strings.Contains(note, "ngoài phạm vi") {
		t.Fatalf("replacement reported out of scope:\n%s", note)
	}
}
