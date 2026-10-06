package tools

import (
	"context"
	"encoding/json"
	"errors"
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

func (*searchStubAbbreviationService) DetectCandidates(context.Context, string) types.AbbreviationDetection {
	return types.AbbreviationDetection{}
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
		{ID: "m-1", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
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
		{ID: "m-a", ShortForm: "BCH", FullForm: "Ban chấp hành", IsActive: true},
		{ID: "m-b", ShortForm: "BCH", FullForm: "Bệnh viện C Hòa", IsActive: true},
	}}
	tool := newAbbreviationSearchTool(kb, svc)

	// Without selection or literal mode, retrieval must not run at all.
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"BCH đã họp"}`))
	if err == nil || res == nil || res.Success {
		t.Fatalf("ambiguous query without selection must be blocked: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Error, "abbreviation_selection_required") {
		t.Fatalf("error=%q", res.Error)
	}
	if len(kb.searchParams) != 0 {
		t.Fatalf("HybridSearch ran %d times on a blocked query", len(kb.searchParams))
	}

	// Explicit literal mode keeps the multi-meaning term unexpanded.
	kb2 := &fakeKBServiceForAbbreviation{}
	tool2 := newAbbreviationSearchTool(kb2, svc)
	res2, err := tool2.Execute(context.Background(),
		json.RawMessage(`{"query":"BCH đã họp","literal_abbreviations":true}`))
	if err != nil || !res2.Success {
		t.Fatalf("literal execute: res=%+v err=%v", res2, err)
	}
	for _, p := range kb2.searchParams {
		if p.QueryText != "BCH đã họp" {
			t.Fatalf("literal query must stay unchanged, got %q", p.QueryText)
		}
	}
}

// An unknown abbreviation blocks retrieval entirely — no HybridSearch call.
func TestSearchKnowledgeUnknownAbbreviationBlocks(t *testing.T) {
	kb := &fakeKBServiceForAbbreviation{}
	tool := newAbbreviationSearchTool(kb, &searchStubAbbreviationService{})

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"ATTT là gì"}`))
	if err == nil || res == nil || res.Success {
		t.Fatalf("unknown abbreviation must be blocked: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Error, "abbreviation_definition_required") {
		t.Fatalf("error=%q", res.Error)
	}
	if res.Data["abbreviation_resolution"] == nil {
		t.Fatal("blocked result must carry resolution data")
	}
	if len(kb.searchParams) != 0 {
		t.Fatalf("HybridSearch ran %d times on a blocked query", len(kb.searchParams))
	}
}

// Inside a sealed QA turn the validated mapping is used and the dictionary
// is never re-read — client-supplied selection flags cannot override it.
func TestSearchKnowledgeBoundTurnUsesSealedMapping(t *testing.T) {
	kb := &fakeKBServiceForAbbreviation{}
	svc := &countingAbbrevSvc{err: errors.New("dictionary must not be read")}
	tool := newAbbreviationSearchTool(kb, svc)

	ctx := sealedToolCtx(t, map[string]string{"ATTT": "An toàn thông tin"})
	res, err := tool.Execute(ctx, json.RawMessage(
		`{"query":"quy định ATTT","abbreviation_meaning_ids":{"attt":"forged"},"literal_abbreviations":true}`))
	if err != nil || !res.Success {
		t.Fatalf("bound execute: res=%+v err=%v", res, err)
	}
	if svc.calls != 0 {
		t.Fatalf("dictionary re-read %d times inside a sealed turn", svc.calls)
	}
	for _, p := range kb.searchParams {
		if p.QueryText != "quy định An toàn thông tin (ATTT)" {
			t.Fatalf("QueryText=%q", p.QueryText)
		}
	}
}
