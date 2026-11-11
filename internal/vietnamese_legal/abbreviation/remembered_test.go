package abbreviation

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func rememberedFixture() types.AbbreviationResolution {
	active := []*types.Abbreviation{
		{ID: "tt", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "tp", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
		{ID: "pc1", ShortForm: "PCCC", FullForm: "Phòng cháy chữa cháy", IsActive: true},
		{ID: "pc2", ShortForm: "PCCC", FullForm: "Phòng chống cháy công cộng", IsActive: true},
	}
	return InspectDetected("ATTT và PCCC", active, types.AbbreviationDetection{})
}

func TestApplyRememberedSelections(t *testing.T) {
	r := rememberedFixture()
	require.Equal(t, types.AbbreviationErrorSelectionRequired, r.ErrorCode)

	// one of two remembered: still asked about the other
	part := ApplyRememberedSelections(r, map[string]string{"attt": "tp"}, nil)
	require.Equal(t, types.AbbreviationErrorSelectionRequired, part.ErrorCode)
	require.Equal(t, []types.AbbreviationTypo{{Wrong: "ATTT", Correct: "An toàn thực phẩm"}}, part.RememberedSelections)
	asked := AskForSelection(part)
	require.Equal(t, []string{"PCCC"}, asked.UnknownTerms)
	require.Empty(t, r.RememberedSelections, "input untouched")

	// both remembered: ready, rendered
	all := ApplyRememberedSelections(r, map[string]string{"attt": "tp", "pccc": "pc1"}, nil)
	require.Equal(t, types.AbbreviationStatusReady, all.Status)
	require.Equal(t, "An toàn thực phẩm (ATTT) và Phòng cháy chữa cháy (PCCC)", all.EffectiveQuery)
	sel := SelectionsOf(all)
	require.Equal(t, map[string]string{"attt": "tp", "pccc": "pc1"}, sel)

	// asked again, unknown id, nothing remembered: unchanged
	require.Equal(t, r, ApplyRememberedSelections(r, map[string]string{"attt": "tp"}, map[string]bool{"attt": true}))
	require.Equal(t, r, ApplyRememberedSelections(r, map[string]string{"attt": "gone"}, nil))
	require.Equal(t, r, ApplyRememberedSelections(r, nil, nil))
	// a single-meaning term is not a choice and is never "remembered"
	single := InspectDetected("ATTT", []*types.Abbreviation{{ID: "tt", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true}}, types.AbbreviationDetection{})
	require.Empty(t, SelectionsOf(single))
}
