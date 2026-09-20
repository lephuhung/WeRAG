package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// stubAbbreviationService feeds the tool a fixed dictionary and records
// suggestions.
type stubAbbreviationService struct {
	interfaces.AbbreviationService

	actives   []*types.Abbreviation
	byShort   map[string][]*types.Abbreviation
	suggested *types.AbbreviationCreateRequest
}

func (s *stubAbbreviationService) ListActive(
	_ context.Context,
) ([]*types.Abbreviation, error) {
	return s.actives, nil
}

func (s *stubAbbreviationService) ListByShortForm(
	_ context.Context, short string,
) ([]*types.Abbreviation, error) {
	return s.byShort[short], nil
}

func (s *stubAbbreviationService) Suggest(
	_ context.Context, req *types.AbbreviationCreateRequest,
) (*types.Abbreviation, error) {
	s.suggested = req
	return &types.Abbreviation{
		ID: "ab-1", ShortForm: req.ShortForm, FullForm: req.FullForm,
		Description: req.Description, IsActive: false,
	}, nil
}

func TestResolveAbbreviation_Expand(t *testing.T) {
	stub := &stubAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	res, err := NewResolveAbbreviationTool(stub).Execute(
		t.Context(), json.RawMessage(`{"action":"expand","text":"UBND tỉnh họp"}`))
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Contains(t, res.Output, "Ủy ban nhân dân tỉnh họp")
	require.Contains(t, res.Output, "applied: UBND")
}

func TestResolveAbbreviation_ExpandAmbiguous(t *testing.T) {
	stub := &stubAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "BCH", FullForm: "Ban chấp hành", IsActive: true},
		{ShortForm: "BCH", FullForm: "Bệnh viện C Hòa", IsActive: true},
	}}
	res, err := NewResolveAbbreviationTool(stub).Execute(
		t.Context(), json.RawMessage(`{"action":"expand","text":"BCH đã họp"}`))
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Contains(t, res.Output, "ambiguous: BCH")
	require.Contains(t, res.Output, "Ban chấp hành")
	// not silently expanded
	require.NotContains(t, res.Output, "expanded_text: Ban chấp hành")
}

func TestResolveAbbreviation_Lookup(t *testing.T) {
	stub := &stubAbbreviationService{byShort: map[string][]*types.Abbreviation{
		"TTHT": {{ShortForm: "TTHT", FullForm: "Trợ giúp tương hỗ", IsActive: true}},
	}}
	res, err := NewResolveAbbreviationTool(stub).Execute(
		t.Context(), json.RawMessage(`{"action":"lookup","short_form":"TTHT"}`))
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Contains(t, res.Output, "Trợ giúp tương hỗ")
}

func TestResolveAbbreviation_SuggestStaysInactive(t *testing.T) {
	stub := &stubAbbreviationService{}
	res, err := NewResolveAbbreviationTool(stub).Execute(t.Context(),
		json.RawMessage(`{"action":"suggest","short_form":"BCĐ","full_form":"Ban chỉ đạo","description":"test"}`))
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, "BCĐ", stub.suggested.ShortForm)
	require.Contains(t, res.Output, "inactive")
	// suggestion must not pretend it already expands
	require.Equal(t, false, res.Data["is_active"])
}

func TestResolveAbbreviation_BadAction(t *testing.T) {
	res, err := NewResolveAbbreviationTool(&stubAbbreviationService{}).Execute(
		t.Context(), json.RawMessage(`{"action":"delete_all"}`))
	require.Error(t, err)
	require.False(t, res.Success)
}

func TestPeopleLookupTool_DisabledFailsFast(t *testing.T) {
	tool := NewPeopleLookupTool(nil)
	res, err := tool.Execute(t.Context(),
		json.RawMessage(`{"lookup_type":"cccd","query":"012345678901"}`))
	require.Error(t, err)
	require.False(t, res.Success)
}
