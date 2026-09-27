package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
	"github.com/stretchr/testify/require"
)

func TestAbbreviationSelectorRequiresEveryAmbiguousTerm(t *testing.T) {
	r := abbreviation.Inspect("ATTT và UBND là gì", []*types.Abbreviation{
		{ID: "a", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "b", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
		{ID: "c", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
		{ID: "d", ShortForm: "UBND", FullForm: "Ủy ban nhà đất", IsActive: true},
	})
	require.Len(t, r.Terms, 2)
	model := &abbreviationChoiceChat{replies: []string{`{"attt":"a"}`, `{"attt":"b","ubnd":"c"}`}}
	chosen, err := NewAbbreviationMeaningSelector(&abbreviationChoiceModels{model: model}).Select(context.Background(), "query-model", "", r)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"attt": "b", "ubnd": "c"}, chosen)
	require.Equal(t, 2, model.calls)
}
