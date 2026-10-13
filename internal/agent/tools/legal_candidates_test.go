package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestParseBareLegalReference(t *testing.T) {
	cases := map[string]string{
		"Nghị định 53 quy định gì?":                    "Nghị định|53",
		"nghị định số 53 về an ninh mạng":              "Nghị định|53",
		"Thông tư 01 của Bộ Nội vụ còn hiệu lực không": "Thông tư|1",
		"Nghị định 53/2022/NĐ-CP quy định gì":          "",
		"Nghị định 30 năm 2021 về văn thư":             "",
		"Nghị định 30-2020 về văn thư":                 "",
		"Luật bí mật nhà nước 2025 có gì mới":          "",
		"Văn bản này có mấy độ mật":                    "",
		"Theo Luật 117 thì sao":                        "Luật|117",
	}
	for q, want := range cases {
		ref, ok := ParseBareLegalReference(q)
		got := ""
		if ok {
			got = ref.DocType + "|" + ref.Number
		}
		if got != want {
			t.Errorf("%q: got %q want %q", q, got, want)
		}
	}
}

type fakeCatalog struct {
	types.LegalDocument
	docs []*types.LegalDocument
}

func (f *fakeCatalog) Lookup(context.Context, string, string, int) ([]*types.LegalStatusReport, error) {
	return nil, nil
}
func (f *fakeCatalog) Related(context.Context, string, string, types.LegalRelatedMode, int, bool) ([]*types.LegalRelatedReport, error) {
	return nil, nil
}
func (f *fakeCatalog) ExpandRelated(context.Context, []string) ([]types.LegalExpansion, error) {
	return nil, nil
}
func (f *fakeCatalog) KBGraph(context.Context, string, int) (*types.GraphView, error) {
	return nil, nil
}
func (f *fakeCatalog) Candidates(_ context.Context, number, docType string, limit int) ([]*types.LegalDocument, int, error) {
	var out []*types.LegalDocument
	for _, d := range f.docs {
		if strings.HasPrefix(d.DocumentNumber, number+"/") && (docType == "" || d.DocType == docType) {
			out = append(out, d)
		}
	}
	total := len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func legalDoc(num, title, status string, year int) *types.LegalDocument {
	d := time.Date(year, 5, 1, 0, 0, 0, 0, time.UTC)
	return &types.LegalDocument{DocumentNumber: num, Title: title, DocType: "Nghị định", Status: status, IssuedOn: &d}
}

func TestBuildLegalCandidates(t *testing.T) {
	svc := &fakeCatalog{docs: []*types.LegalDocument{
		legalDoc("53/2024/NĐ-CP", "Nghị định quy định chi tiết Luật Tài nguyên nước", "Hết hiệu lực một phần", 2024),
		legalDoc("53/2022/NĐ-CP", "Nghị định quy định chi tiết một số điều của Luật An ninh mạng", "Còn hiệu lực", 2022),
		legalDoc("53/2014/NĐ-CP", "Nghị định về lấy ý kiến tổ chức", "Còn hiệu lực", 2014),
	}}
	ctx := context.Background()
	card, err := BuildLegalCandidates(ctx, svc, "Nghị định 53 quy định gì?")
	if err != nil || card == nil || card.Total != 3 {
		t.Fatalf("card=%+v err=%v", card, err)
	}
	if got := card.Resend(svc.docs[1]); got != "Nghị định 53/2022/NĐ-CP quy định gì?" {
		t.Fatalf("resend %q", got)
	}
	if !strings.Contains(card.Message(), "Danh mục có 3 văn bản") || !strings.Contains(card.Message(), "1. 53/2024/NĐ-CP") {
		t.Fatalf("message %q", card.Message())
	}
	if d := card.Data(); d["display_type"] != LegalCandidatesType || len(d["candidates"].([]map[string]interface{})) != 3 {
		t.Fatalf("data %+v", d)
	}
	// the topic singles one out: no card
	if card, _ := BuildLegalCandidates(ctx, svc, "Nghị định 53 về an ninh mạng quy định gì?"); card != nil {
		t.Fatalf("topic should resolve the reference, got %+v", card)
	}
	// a full number: no card
	if card, _ := BuildLegalCandidates(ctx, svc, "Nghị định 53/2022/NĐ-CP quy định gì?"); card != nil {
		t.Fatal("a full number needs no card")
	}
	// one match only: no card
	one := &fakeCatalog{docs: svc.docs[:1]}
	if card, _ := BuildLegalCandidates(ctx, one, "Nghị định 53 quy định gì?"); card != nil {
		t.Fatal("one document needs no card")
	}
}
