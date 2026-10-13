package abbreviation

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestTypoWords(t *testing.T) {
	require.Equal(t, [][2]string{{"cascc", "các"}}, TypoWords("cascc", "các"))
	require.Equal(t, [][2]string{{"quyddinh", "quy định"}}, TypoWords("quyddinh", "quy định"))
	require.Equal(t, [][2]string{{"quys", "quý"}}, TypoWords("quys trình", "quý trình"))
	require.Nil(t, TypoWords("các", "Các"))
	require.Nil(t, TypoWords("cá c", "các"))
	require.Nil(t, TypoWords("abc,", "abc"))
}

func TestTypoTokensAreNotCandidates(t *testing.T) {
	q := "cascc thủ tục của UBND là gì"
	det := types.AbbreviationDetection{
		Detector: "clef",
		Keys:     []string{"ubnd"},
		Typos:    []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}},
	}
	active := []*types.Abbreviation{{ID: "a1", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true}}
	r := InspectDetected(q, active, det)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Empty(t, r.UnknownTerms)
	require.Equal(t, "các thủ tục của Ủy ban nhân dân (UBND) là gì", r.EffectiveQuery)
	require.NoError(t, validateReadyResolution(r))
	require.Equal(t, det.Typos, r.PublicState().TypoCorrections)
}

func TestTypoOnlyQueryIsReady(t *testing.T) {
	det := types.AbbreviationDetection{
		Detector: types.AbbreviationDetectorHeuristicSpellcheck,
		Typos:    []types.AbbreviationTypo{{Wrong: "phaps", Correct: "pháp"}, {Wrong: "quys", Correct: "quy"}},
	}
	r := InspectDetected("phaps luật quys định gì", nil, det)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, "pháp luật quy định gì", r.EffectiveQuery)
	require.NoError(t, validateReadyResolution(r))
}
