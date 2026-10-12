package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFindRelatedLegalDocumentsTool(t *testing.T) {
	law := &types.LegalDocument{ID: "law", DocType: "Luật", DocumentNumber: "109/2025/QH15",
		Title: "Luật Thuế thu nhập cá nhân số 109/2025/QH15", Status: "Còn hiệu lực"}
	decree := &types.LegalDocument{ID: "nd", DocType: "Nghị định", DocumentNumber: "12/2026/NĐ-CP",
		Title: "Quy định chi tiết Luật Thuế thu nhập cá nhân", Status: "Còn hiệu lực"}
	circular := &types.LegalDocument{ID: "tt", DocType: "Thông tư", DocumentNumber: "40/2026/TT-BTC",
		Title: "Hướng dẫn Nghị định 12/2026/NĐ-CP", Status: "Còn hiệu lực"}
	svc := &fakeLegalCatalog{related: []*types.LegalRelatedReport{{
		Document: law, Mode: types.LegalRelatedGuidance,
		Related: []types.LegalRelatedDocument{
			{Document: decree, Depth: 1, ParentID: "law", Relation: types.LegalRelationGuides, Incoming: true,
				Knowledge: []types.LegalKnowledgeRef{{KnowledgeID: "k-nd", KnowledgeBaseName: "Nghị định"}}},
			{Document: circular, Depth: 2, ParentID: "nd", Relation: types.LegalRelationGuides, Incoming: true},
		},
	}}}
	tool := NewFindRelatedLegalDocumentsTool(svc)

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"document_number":"109/2025/QH15"}`))
	if err != nil || !res.Success {
		t.Fatalf("Execute: %v %+v", err, res)
	}
	if svc.gotNum != "109/2025/QH15" || svc.gotMode != types.LegalRelatedGuidance {
		t.Fatalf("defaults not applied: %q %q", svc.gotNum, svc.gotMode)
	}
	out := res.Output
	for _, want := range []string{
		"Văn bản quy định chi tiết, hướng dẫn",
		"- Nghị định 12/2026/NĐ-CP — Quy định chi tiết Luật Thuế thu nhập cá nhân",
		"quy định chi tiết, hướng dẫn Luật Thuế thu nhập cá nhân số 109/2025/QH15",
		`knowledge_id="k-nd"`,
		"  - Thông tư 40/2026/TT-BTC", // second hop is indented under the decree
		"quy định chi tiết, hướng dẫn Nghị định 12/2026/NĐ-CP",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Chưa văn bản nào") {
		t.Errorf("a held document must not trigger the nothing-held note:\n%s", out)
	}

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("a call without document_number or title must fail")
	}
}
