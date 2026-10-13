package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWithTypoCandidatesAddsTypoKeys(t *testing.T) {
	typos := []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}}
	det := withTypoCandidates(types.AbbreviationDetection{Detector: "clef", Keys: []string{"ubnd"}}, "cascc thủ tục UBND", typos)
	require.Equal(t, "clef", det.Detector)
	require.Equal(t, []string{"ubnd", "cascc"}, det.Keys)
	require.Equal(t, typos, det.Typos)

	// heuristic detection becomes an explicit key set with the typo added
	h := withTypoCandidates(types.AbbreviationDetection{}, "cascc thủ tục UBND", typos)
	require.Equal(t, types.AbbreviationDetectorHeuristicSpellcheck, h.Detector)
	require.ElementsMatch(t, []string{"ubnd", "cascc"}, h.Keys)

	require.Equal(t, types.AbbreviationDetection{}, withTypoCandidates(types.AbbreviationDetection{}, "x", nil))
}

func TestUnclearWordChoice(t *testing.T) {
	r := types.AbbreviationResolution{
		UnknownTerms:    []string{"cascc", "xyz"},
		TypoSuggestions: []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}},
	}
	answer, data := abbreviationChoice(r, false)
	require.Contains(t, answer, "Tôi nhận thấy từ “cascc” chưa rõ ý nghĩa")
	require.NotContains(t, answer, "xyz") // asked in the next turn
	require.Equal(t, AbbreviationChoiceType, data["display_type"])
	opts := data["options"].([]map[string]interface{})
	require.Len(t, opts, 4)
	require.Equal(t, "cascc = {text}", opts[0]["template"])
	require.Nil(t, opts[0]["resend"])
	require.Equal(t, "cascc → các", opts[1]["resend"])
	require.Equal(t, "cascc → cascc", opts[2]["resend"])
	require.Equal(t, "{text}", opts[3]["template"])

	// no guess: the typo option asks for the intended word
	_, data = abbreviationChoice(types.AbbreviationResolution{UnknownTerms: []string{"xyz"}}, false)
	require.Equal(t, "xyz → {text}", data["options"].([]map[string]interface{})[1]["template"])
}
