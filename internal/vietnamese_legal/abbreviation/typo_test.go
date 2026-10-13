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

func TestTypoSuggestions(t *testing.T) {
	q := "quys định 172/GM-UBND về cascc thủ tục, mẫu 02a"
	got := TypoSuggestions(q, [][2]string{{"quys", "quy định"}, {"GM", "giám"}, {"cascc", "các"}, {"02a", "02"}})
	// the repeated "định" is trimmed; a word inside a code and a word
	// with a digit are never suggested
	require.Equal(t, []types.AbbreviationTypo{{Wrong: "quys", Correct: "quy"}, {Wrong: "cascc", Correct: "các"}}, got)
}

func typoDetection(q string) types.AbbreviationDetection {
	return types.AbbreviationDetection{
		Detector: "clef",
		Keys:     []string{"cascc", "ubnd"},
		Typos:    []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}},
	}
}

func TestTypoWordIsAskedThenCorrected(t *testing.T) {
	q := "cascc thủ tục của UBND là gì"
	active := []*types.Abbreviation{{ID: "a1", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true}}
	r := InspectDetected(q, active, typoDetection(q))
	// a suspected typo is asked about like an unknown abbreviation
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, r.Status)
	require.Equal(t, []string{"cascc"}, r.UnknownTerms)
	require.Equal(t, "các", TypoSuggestionFor(r, "CASCC"))

	parsed, err := ParseUserDefinitions("cascc → các", "m2", r.UnknownTerms, true)
	require.NoError(t, err)
	require.True(t, parsed.IsDefinitionReply)
	require.Empty(t, parsed.Definitions)
	r, err = ApplyTypoCorrections(r, parsed.Typos)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, "các thủ tục của Ủy ban nhân dân (UBND) là gì", r.EffectiveQuery)
	require.NoError(t, validateReadyResolution(r))
	// the re-derived detection keeps the suggestion but not the typo key
	require.NotContains(t, r.Detection().Keys, "cascc")
}

func TestTypoKeptAsTyped(t *testing.T) {
	q := "giá hornor magic 9"
	r := InspectDetected(q, nil, types.AbbreviationDetection{Detector: "clef", Keys: []string{"hornor"}})
	parsed, err := ParseUserDefinitions("hornor -> hornor", "m2", r.UnknownTerms, true)
	require.NoError(t, err)
	r, err = ApplyTypoCorrections(r, parsed.Typos)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, q, r.EffectiveQuery)
	require.NoError(t, validateReadyResolution(r))
}

func TestTypoCorrectionUnderHeuristic(t *testing.T) {
	q := "thủ tục XYZQ ở đâu"
	r := InspectDetected(q, nil, types.AbbreviationDetection{})
	require.Equal(t, []string{"XYZQ"}, r.UnknownTerms)
	r, err := ApplyTypoCorrections(r, []types.AbbreviationTypo{{Wrong: "XYZQ", Correct: "này"}})
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationDetectorHeuristicSpellcheck, r.Detector)
	require.Equal(t, "thủ tục này ở đâu", r.EffectiveQuery)
	require.NoError(t, validateReadyResolution(r))
}

func TestTypoAnswerForUnknownWordFails(t *testing.T) {
	_, err := ParseUserDefinitions("abc → các", "m2", []string{"cascc"}, true)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection)
}
