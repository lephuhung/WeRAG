package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingAbbrevSvc serves a fixed dictionary and counts ListActive calls so
// tests can prove the bound path never re-reads the dictionary.
type countingAbbrevSvc struct {
	interfaces.AbbreviationService
	actives []*types.Abbreviation
	err     error
	calls   int
}

func (*countingAbbrevSvc) DetectCandidates(context.Context, string) types.AbbreviationDetection {
	return types.AbbreviationDetection{}
}

func (s *countingAbbrevSvc) ListActive(context.Context) ([]*types.Abbreviation, error) {
	s.calls++
	return s.actives, s.err
}

func sealedToolCtx(t *testing.T, terms map[string]string) context.Context {
	t.Helper()
	var actives []*types.Abbreviation
	query := ""
	i := 0
	for short, full := range terms {
		actives = append(actives, &types.Abbreviation{
			ID: "m-" + short, ShortForm: short, FullForm: full, IsActive: true,
		})
		query += short + " "
		i++
	}
	res := abbreviation.Inspect(query, actives)
	require.Equal(t, types.AbbreviationStatusReady, res.Status)
	binding := types.AbbreviationBinding{
		Owner: types.AbbreviationOwner{
			TenantID: 1, SessionID: "sess-1", OwnerID: "u1", PrincipalID: "user:u1",
		},
		UserMessageID: "user-1", AssistantMessageID: "assist-1", RawQuery: query,
	}
	ctx, err := abbreviation.BindTurn(context.Background(), binding, res)
	require.NoError(t, err)
	return ctx
}

// A bound QA turn applies the validated mapping without touching the
// dictionary and ignores the model's own selection/literal flags.
func TestResolveToolQuery_BoundTurnUsesSealedMapping(t *testing.T) {
	ctx := sealedToolCtx(t, map[string]string{"ATTT": "An toàn thông tin"})
	svc := &countingAbbrevSvc{err: errors.New("dictionary must not be read")}

	out, blocked, err := ResolveToolQuery(ctx, "tra cứu quy định ATTT", svc,
		map[string]string{"attt": "forged-id"}, true)

	require.Nil(t, blocked)
	require.NoError(t, err)
	assert.Equal(t, "tra cứu quy định An toàn thông tin (ATTT)", out)
	assert.Equal(t, 0, svc.calls, "bound turns must not re-read the dictionary")
}

// A model argument that already carries `Full (SHORT)` is not wrapped again.
func TestResolveToolQuery_BoundTurnSkipsPreExpandedText(t *testing.T) {
	ctx := sealedToolCtx(t, map[string]string{"ATTT": "An toàn thông tin"})

	out, blocked, err := ResolveToolQuery(ctx,
		"tra cứu An toàn thông tin (ATTT) về dữ liệu", nil, nil, false)

	require.Nil(t, blocked)
	require.NoError(t, err)
	assert.Equal(t, "tra cứu An toàn thông tin (ATTT) về dữ liệu", out)
}

// Candidates that were never in the user's request stay literal — they are
// model-produced text, not user intent, and are neither blocked nor learned.
func TestResolveToolQuery_BoundTurnLeavesNewTermsLiteral(t *testing.T) {
	ctx := sealedToolCtx(t, map[string]string{"ATTT": "An toàn thông tin"})
	svc := &countingAbbrevSvc{}

	out, blocked, err := ResolveToolQuery(ctx, "tra cứu XYZ tại ATTT", svc, nil, false)

	require.Nil(t, blocked)
	require.NoError(t, err)
	assert.Equal(t, "tra cứu XYZ tại An toàn thông tin (ATTT)", out)
	assert.Equal(t, 0, svc.calls)
}

// A standalone tool call with an unknown abbreviation blocks without
// retrieval and reports the resolution to the caller.
func TestResolveToolQuery_UnknownBlocksRetrieval(t *testing.T) {
	svc := &countingAbbrevSvc{}

	out, blocked, err := ResolveToolQuery(context.Background(), "ATTT là gì", svc, nil, false)

	assert.Empty(t, out)
	require.NotNil(t, blocked)
	require.Error(t, err)
	assert.False(t, blocked.Success)
	assert.Contains(t, blocked.Error, AbbreviationCodeDefinitionRequired)
	require.NotNil(t, blocked.Data["abbreviation_resolution"])
}

// Unknown abbreviations still block when literal mode is requested — literal
// only applies to known multi-meaning terms.
func TestResolveToolQuery_LiteralDoesNotBypassUnknown(t *testing.T) {
	svc := &countingAbbrevSvc{}

	_, blocked, err := ResolveToolQuery(context.Background(), "ATTT là gì", svc, nil, true)

	require.NotNil(t, blocked)
	require.Error(t, err)
	assert.Contains(t, blocked.Error, AbbreviationCodeDefinitionRequired)
}

// A single active meaning expands normally on the standalone path.
func TestResolveToolQuery_SingleActiveExpands(t *testing.T) {
	svc := &countingAbbrevSvc{actives: []*types.Abbreviation{
		{ID: "m-1", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}

	out, blocked, err := ResolveToolQuery(context.Background(), "UBND tỉnh họp", svc, nil, false)

	require.Nil(t, blocked)
	require.NoError(t, err)
	assert.Equal(t, "Ủy ban nhân dân (UBND) tỉnh họp", out)
	assert.Equal(t, 1, svc.calls)
}

// Multiple active meanings require a valid meaning ID or literal mode.
func TestResolveToolQuery_MultiMeaningPolicy(t *testing.T) {
	actives := []*types.Abbreviation{
		{ID: "m-a", ShortForm: "BCH", FullForm: "Ban chấp hành", IsActive: true},
		{ID: "m-b", ShortForm: "BCH", FullForm: "Bệnh viện C Hòa", IsActive: true},
	}
	newSvc := func() *countingAbbrevSvc { return &countingAbbrevSvc{actives: actives} }

	// No selection, no literal: blocked, not silently guessed.
	_, blocked, err := ResolveToolQuery(context.Background(), "BCH đã họp", newSvc(), nil, false)
	require.NotNil(t, blocked)
	require.Error(t, err)
	assert.Contains(t, blocked.Error, AbbreviationCodeSelectionRequired)

	// A valid resolver-provided ID selects that meaning.
	out, blocked, err := ResolveToolQuery(context.Background(), "BCH đã họp", newSvc(),
		map[string]string{"bch": "m-b"}, false)
	require.Nil(t, blocked)
	require.NoError(t, err)
	assert.Equal(t, "Bệnh viện C Hòa (BCH) đã họp", out)

	// A foreign or non-active ID is rejected.
	_, blocked, err = ResolveToolQuery(context.Background(), "BCH đã họp", newSvc(),
		map[string]string{"bch": "forged"}, false)
	require.NotNil(t, blocked)
	require.Error(t, err)
	assert.Contains(t, blocked.Error, AbbreviationCodeSelectionRequired)

	// Literal mode keeps the multi-meaning term unexpanded.
	out, blocked, err = ResolveToolQuery(context.Background(), "BCH đã họp", newSvc(), nil, true)
	require.Nil(t, blocked)
	require.NoError(t, err)
	assert.Equal(t, "BCH đã họp", out)
}

// A query without abbreviation candidates never touches the dictionary.
func TestResolveToolQuery_NoCandidatesSkipsDictionary(t *testing.T) {
	svc := &countingAbbrevSvc{err: errors.New("must not be called")}

	out, blocked, err := ResolveToolQuery(context.Background(), "hôm nay trời đẹp", svc, nil, false)

	require.Nil(t, blocked)
	require.NoError(t, err)
	assert.Equal(t, "hôm nay trời đẹp", out)
	assert.Equal(t, 0, svc.calls)
}

// Dictionary failures fail closed — the tool query is not passed through.
func TestResolveToolQuery_DictionaryErrorBlocks(t *testing.T) {
	svc := &countingAbbrevSvc{err: errors.New("db down")}

	_, blocked, err := ResolveToolQuery(context.Background(), "ATTT là gì", svc, nil, false)

	require.NotNil(t, blocked)
	require.Error(t, err)
	assert.Contains(t, blocked.Error, AbbreviationCodeDictionaryError)
}

// A missing dictionary service with candidates present also fails closed.
func TestResolveToolQuery_NilServiceBlocks(t *testing.T) {
	_, blocked, err := ResolveToolQuery(context.Background(), "ATTT là gì", nil, nil, false)

	require.NotNil(t, blocked)
	require.Error(t, err)
	assert.Contains(t, blocked.Error, AbbreviationCodeDictionaryError)
}
