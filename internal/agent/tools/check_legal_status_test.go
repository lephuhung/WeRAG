package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type fakeLegalCatalog struct {
	reports    []*types.LegalStatusReport
	related    []*types.LegalRelatedReport
	expansions []types.LegalExpansion
	gotHits    []string
	gotNum     string
	gotMode    types.LegalRelatedMode
}

func (f *fakeLegalCatalog) ExpandRelated(_ context.Context, ids []string) ([]types.LegalExpansion, error) {
	f.gotHits = ids
	return f.expansions, nil
}

func (f *fakeLegalCatalog) KBGraph(context.Context, string, int) (*types.GraphView, error) {
	return &types.GraphView{}, nil
}

func (f *fakeLegalCatalog) Related(
	_ context.Context, num, _ string, mode types.LegalRelatedMode, _ int, _ bool,
) ([]*types.LegalRelatedReport, error) {
	f.gotNum, f.gotMode = num, mode
	return f.related, nil
}

func (f *fakeLegalCatalog) Lookup(_ context.Context, num, _ string, _ int) ([]*types.LegalStatusReport, error) {
	f.gotNum = num
	return f.reports, nil
}

func TestCheckLegalStatusTool(t *testing.T) {
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	snap := day("2026-07-23")
	oldLaw := &types.LegalDocument{ID: "old", Source: "vbpl", DocType: "Luật", DocumentNumber: "45/2013/QH13",
		Title: "Luật Đất đai số 45/2013/QH13", IssuedOn: day("2013-11-29"), ExpiredOn: day("2024-08-01"),
		Status: "Hết hiệu lực toàn bộ", SourceUpdatedAt: snap}
	newLaw := &types.LegalDocument{ID: "new", Source: "vbpl", DocType: "Luật", DocumentNumber: "31/2024/QH15",
		Title: "Đất đai", IssuedOn: day("2024-01-18"), Status: "Còn hiệu lực"}
	svc := &fakeLegalCatalog{reports: []*types.LegalStatusReport{{
		Document: oldLaw,
		Links:    []types.LegalRelationLink{{Relation: types.LegalRelationExpires, Incoming: true, Other: newLaw}},
	}}}

	res, err := NewCheckLegalStatusTool(svc).Execute(context.Background(), json.RawMessage(`{"document_number":"45/2013/QH13"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %v %+v", err, res)
	}
	for _, want := range []string{
		"## Luật Đất đai số 45/2013/QH13",
		"hết hiệu lực: 01/08/2024",
		"Hết hiệu lực toàn bộ (theo vbpl, cập nhật 23/07/2026)",
		"Bị làm hết hiệu lực bởi:",
		"Luật 31/2024/QH15 — Đất đai (ban hành 18/01/2024; Còn hiệu lực)",
	} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q:\n%s", want, res.Output)
		}
	}

	svc.reports = nil
	res, _ = NewCheckLegalStatusTool(svc).Execute(context.Background(), json.RawMessage(`{"title":"Luật Không Có"}`))
	if !res.Success || !strings.Contains(res.Output, "do not conclude it was repealed") {
		t.Errorf("miss must say unknown, not repealed: %+v", res)
	}
	if _, err := NewCheckLegalStatusTool(svc).Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Error("empty input must fail")
	}
}

func (f *fakeLegalCatalog) SearchTitles(context.Context, []string, int) ([]*types.LegalDocument, error) {
	return nil, nil
}

func (f *fakeLegalCatalog) Candidates(context.Context, string, string, int) ([]*types.LegalDocument, int, error) {
	return nil, 0, nil
}

func TestCheckLegalStatusDerivesExpiryFromAReplacementInForce(t *testing.T) {
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	defer func(prev func() time.Time) { legalNow = prev }(legalNow)
	legalNow = func() time.Time { return *day("2026-10-16") }

	oldLaw := &types.LegalDocument{ID: "old", Source: "vbpl", DocType: "Luật", DocumentNumber: "29/2018/QH14",
		Title: "Luật Bảo vệ bí mật nhà nước", IssuedOn: day("2018-11-15"), EffectiveOn: day("2020-07-01"),
		Status: "Còn hiệu lực", SourceUpdatedAt: day("2026-07-23")}
	newLaw := &types.LegalDocument{ID: "new", Source: "vbpl", DocType: "Luật", DocumentNumber: "117/2025/QH15",
		Title: "LUẬT BẢO VỆ BÍ MẬT NHÀ NƯỚC SỐ 117/2025/QH15", IssuedOn: day("2025-12-10"), EffectiveOn: day("2026-03-01"),
		Status: "Còn hiệu lực"}
	rep := &types.LegalStatusReport{Document: oldLaw,
		Links: []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: newLaw}}}
	svc := &fakeLegalCatalog{reports: []*types.LegalStatusReport{rep}}

	res, err := NewCheckLegalStatusTool(svc).Execute(context.Background(), json.RawMessage(`{"document_number":"29/2018/QH14"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %v %+v", err, res)
	}
	want := "- Tình trạng hiệu lực: Còn hiệu lực (theo vbpl, cập nhật 23/07/2026) — LƯU Ý: đã bị thay thế bởi " +
		"Luật 117/2025/QH15 có hiệu lực từ 01/03/2026, nên văn bản này hết hiệu lực từ ngày đó " +
		"(suy ra từ quan hệ thay thế; vbpl chưa cập nhật trạng thái)"
	if !strings.Contains(res.Output, want) {
		t.Fatalf("output lacks the derived expiry:\n%s", res.Output)
	}
	derived, _ := res.Data["derived_status"].([]map[string]interface{})
	if len(derived) != 1 || derived[0]["derived_expired_on"] != "2026-03-01" || derived[0]["derived_from"] != "117/2025/QH15" {
		t.Fatalf("data lacks the derived status: %+v", res.Data["derived_status"])
	}

	// vbpl holds it partly expired: the note is worded as a possibility
	oldLaw.Status = "Hết hiệu lực một phần"
	res, _ = NewCheckLegalStatusTool(svc).Execute(context.Background(), json.RawMessage(`{"document_number":"29/2018/QH14"}`))
	if !strings.Contains(res.Output, "Hết hiệu lực một phần (theo vbpl, cập nhật 23/07/2026) — LƯU Ý: có thể đã hết hiệu lực toàn bộ — "+
		"đã bị thay thế bởi Luật 117/2025/QH15 có hiệu lực từ 01/03/2026; kiểm tra điều khoản chuyển tiếp của Luật 117/2025/QH15") ||
		strings.Contains(res.Output, "nên văn bản này hết hiệu lực từ ngày đó") {
		t.Fatalf("a partly expired document gets the softened note:\n%s", res.Output)
	}
	if d, _ := res.Data["derived_status"].([]map[string]interface{}); len(d) != 1 || d[0]["derived_status"] != "Có thể đã hết hiệu lực toàn bộ" {
		t.Fatalf("derived status of a partly expired document: %+v", res.Data["derived_status"])
	}
	// other statuses are left as they are
	for _, st := range []string{"Chưa có hiệu lực", "Ngưng hiệu lực"} {
		oldLaw.Status = st
		if by, _ := derivedExpiry(rep, legalNow()); by != nil {
			t.Fatalf("status %q must not be reconciled", st)
		}
	}
	oldLaw.Status = "Còn hiệu lực"

	// not yet in force: nothing derived
	legalNow = func() time.Time { return *day("2026-02-01") }
	res, _ = NewCheckLegalStatusTool(svc).Execute(context.Background(), json.RawMessage(`{"document_number":"29/2018/QH14"}`))
	if strings.Contains(res.Output, "LƯU Ý") || res.Data["derived_status"] != nil {
		t.Fatalf("a replacement not yet in force must not end the document:\n%s", res.Output)
	}

	// a partial expiry or a repeal (vbpl files partial ones there) is not a full end
	legalNow = func() time.Time { return *day("2026-10-16") }
	for _, rel := range []types.LegalRelation{types.LegalRelationPartiallyExpires, types.LegalRelationRepeals} {
		rep.Links = []types.LegalRelationLink{{Relation: rel, Incoming: true, Other: newLaw}}
		if by, _ := derivedExpiry(rep, legalNow()); by != nil {
			t.Fatalf("%s must not derive a full expiry", rel)
		}
	}
	// an outgoing replacement says nothing about this document
	rep.Links = []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: false, Other: newLaw}}
	if by, _ := derivedExpiry(rep, legalNow()); by != nil {
		t.Fatal("an outgoing link must not derive an expiry")
	}
	// the source already records the end: nothing to add
	rep.Links = []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: newLaw}}
	oldLaw.ExpiredOn = day("2026-03-01")
	if by, _ := derivedExpiry(rep, legalNow()); by != nil {
		t.Fatal("a recorded expiry needs no derivation")
	}
}
