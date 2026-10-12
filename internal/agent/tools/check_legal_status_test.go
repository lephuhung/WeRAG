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
	reports []*types.LegalStatusReport
	related []*types.LegalRelatedReport
	gotNum  string
	gotMode types.LegalRelatedMode
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
