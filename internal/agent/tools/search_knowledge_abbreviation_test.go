package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type fakeKBServiceForAbbreviation struct {
	interfaces.KnowledgeBaseService
	searchParams []types.SearchParams
}

func (f *fakeKBServiceForAbbreviation) GetKnowledgeBasesByIDsOnly(
	context.Context, []string,
) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{kbWithIndexes("kb-1", true, true, "")}, nil
}

func (f *fakeKBServiceForAbbreviation) ResolveEmbeddingModelKeys(
	context.Context, []*types.KnowledgeBase,
) map[string]string {
	return map[string]string{"kb-1": ""}
}

func (f *fakeKBServiceForAbbreviation) HybridSearch(
	_ context.Context, _ string, params types.SearchParams,
) ([]*types.SearchResult, error) {
	f.searchParams = append(f.searchParams, params)
	return nil, nil
}

type searchStubAbbreviationService struct {
	interfaces.AbbreviationService
	actives []*types.Abbreviation
}

func (s *searchStubAbbreviationService) ListActive(context.Context) ([]*types.Abbreviation, error) {
	return s.actives, nil
}

func newAbbreviationSearchTool(
	kb *fakeKBServiceForAbbreviation, svc interfaces.AbbreviationService,
) *SearchKnowledgeTool {
	return NewSearchKnowledgeTool(kb, nil, nil, types.SearchTargets{{
		Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 1,
	}}, nil, nil).WithAbbreviationService(svc)
}

func TestSearchKnowledgeAppliesAbbreviationResolution(t *testing.T) {
	kb := &fakeKBServiceForAbbreviation{}
	svc := &searchStubAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	tool := newAbbreviationSearchTool(kb, svc)

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"UBND tỉnh họp"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: res=%+v err=%v", res, err)
	}
	if len(kb.searchParams) == 0 {
		t.Fatal("HybridSearch was never called")
	}
	for _, p := range kb.searchParams {
		if p.QueryText != "Ủy ban nhân dân (UBND) tỉnh họp" {
			t.Fatalf("QueryText=%q", p.QueryText)
		}
	}
	if res.Data["query"] != "Ủy ban nhân dân (UBND) tỉnh họp" {
		t.Fatalf("data query=%v", res.Data["query"])
	}
	if res.Data["original_query"] != "UBND tỉnh họp" {
		t.Fatalf("original_query=%v", res.Data["original_query"])
	}
	if _, ok := res.Data["abbreviation_resolution"]; !ok {
		t.Fatal("abbreviation_resolution missing from Data")
	}
	if !strings.Contains(res.Output, "<abbreviation_resolution>") ||
		!strings.Contains(res.Output, "Ủy ban nhân dân") {
		t.Fatalf("output=%q", res.Output)
	}
}

func TestSearchKnowledgeAmbiguousAbbreviationStaysUntouched(t *testing.T) {
	kb := &fakeKBServiceForAbbreviation{}
	svc := &searchStubAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "BCH", FullForm: "Ban chấp hành", IsActive: true},
		{ShortForm: "BCH", FullForm: "Bệnh viện C Hòa", IsActive: true},
	}}
	tool := newAbbreviationSearchTool(kb, svc)

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"BCH đã họp"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: res=%+v err=%v", res, err)
	}
	for _, p := range kb.searchParams {
		if p.QueryText != "BCH đã họp" {
			t.Fatalf("ambiguous query must stay unchanged, got %q", p.QueryText)
		}
	}
	if !strings.Contains(res.Output, `<ambiguous short_form="BCH">`) ||
		!strings.Contains(res.Output, "<meaning>Ban chấp hành</meaning>") ||
		!strings.Contains(res.Output, "<meaning>Bệnh viện C Hòa</meaning>") {
		t.Fatalf("output=%q", res.Output)
	}
}
