package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"

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

func TestUnclearWordChoiceOffersRewrite(t *testing.T) {
	r := types.AbbreviationResolution{
		UnknownTerms:    []string{"cascc"},
		TypoSuggestions: []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}},
		Rewrite:         "số lượng hồ sơ các loại ưu tiên",
	}
	answer, data := abbreviationChoice(r, false)
	require.Contains(t, answer, "Ý bạn là: “số lượng hồ sơ các loại ưu tiên”?")
	opts := data["options"].([]map[string]interface{})
	require.Len(t, opts, 5)
	require.Equal(t, "số lượng hồ sơ các loại ưu tiên", opts[2]["resend"])
}

type memFeedback struct {
	rows []types.AbbreviationTypoFeedback
}

func (m *memFeedback) Record(_ context.Context, rows []types.AbbreviationTypoFeedback) error {
	m.rows = append(m.rows, rows...)
	return nil
}

func TestRecordAnswersAndRewrite(t *testing.T) {
	fb := &memFeedback{}
	c := &AbbreviationTurnCoordinator{feedback: fb, now: time.Now}
	b := types.AbbreviationBinding{Owner: types.AbbreviationOwner{TenantID: 7, SessionID: "s"}, UserMessageID: "u2"}
	asked := types.AbbreviationResolution{
		OriginalQuery: "hồ sơ cascc loại hornor", UnknownTerms: []string{"cascc", "hornor"},
		TypoSuggestions: []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}}, Rewrite: "hồ sơ các loại hornor",
	}
	c.recordAnswers(context.Background(), b, "req", asked, abbreviation.DefinitionParseResult{
		Typos: []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}, {Wrong: "hornor", Correct: "hornor"}},
	})
	require.Len(t, fb.rows, 2)
	require.Equal(t, types.AbbreviationFeedbackTypo, fb.rows[0].Choice)
	require.Equal(t, "các", fb.rows[0].SpellcheckGuess)
	require.Equal(t, types.AbbreviationFeedbackKeep, fb.rows[1].Choice)

	b.RawQuery = "hồ sơ các  loại hornor"
	c.recordUnanswered(context.Background(), b, &types.AbbreviationTurnState{ID: "req", Payload: types.AbbreviationTurnPayload{Resolution: asked}})
	require.Equal(t, types.AbbreviationFeedbackRewrite, fb.rows[2].Choice)
	require.Equal(t, "cascc", fb.rows[2].Word)
}
