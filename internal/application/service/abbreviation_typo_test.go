package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

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

func (m *memFeedback) Latest(_ context.Context, tenantID uint64, words []string) (map[string]types.AbbreviationTypoFeedback, error) {
	out := map[string]types.AbbreviationTypoFeedback{}
	for i := len(m.rows) - 1; i >= 0; i-- {
		r := m.rows[i]
		k := strings.ToLower(r.Word)
		if _, seen := out[k]; seen || r.TenantID != tenantID || r.Choice == types.AbbreviationFeedbackRewrite || r.Choice == types.AbbreviationFeedbackOther {
			continue
		}
		for _, w := range words {
			if strings.EqualFold(w, r.Word) {
				out[k] = r
			}
		}
	}
	return out, nil
}

// typoDictionary is a turn dictionary with fixed typo sources.
type typoDictionary struct {
	*turnDictionary
	spell    map[string]string
	rewrites map[string]string
	calls    int
}

func (d *typoDictionary) SpellcheckReadings(context.Context, string) map[string]string {
	d.calls++
	return d.spell
}

func (d *typoDictionary) RewriteQuestion(_ context.Context, _ string, text string, _ []string) string {
	return d.rewrites[text]
}

func TestGateReadsAgreedTypoWithoutAsking(t *testing.T) {
	_, store, base, owner := newTurnCoordinatorFixture(t)
	q := "số lượng hồ sơ cascc loại ưu tiên"
	dict := &typoDictionary{turnDictionary: base, rewrites: map[string]string{q: "số lượng hồ sơ các loại ưu tiên"}}
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	in := turnInput(owner, "u1", "a1", q)
	in.ModelID = "chat"
	// the rewrite and the Telex decoding agree: no question
	r, err := c.Prepare(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, "số lượng hồ sơ các loại ưu tiên", r.EffectiveQuery)
	require.Equal(t, []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}}, r.PublicState().AutoCorrections)
}

func TestGateAsksWhenUserRejectsReading(t *testing.T) {
	_, store, base, owner := newTurnCoordinatorFixture(t)
	q := "số lượng hồ sơ cascc loại ưu tiên"
	fb := &memFeedback{}
	dict := &typoDictionary{turnDictionary: base, rewrites: map[string]string{q: "số lượng hồ sơ các loại ưu tiên"}}
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	c.SetFeedbackStore(fb)
	in := turnInput(owner, "u1", "a1", q)
	in.ModelID, in.AskWords = "chat", []string{"cascc"}
	r, err := c.Prepare(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, r.Status)
	require.Equal(t, []string{"cascc"}, r.UnknownTerms)
	require.Equal(t, "số lượng hồ sơ các loại ưu tiên", r.Rewrite)
	answer, _ := abbreviationChoice(r, false)
	require.Contains(t, answer, "Bạn gõ sai, ý là “các”")

	v := r.Version
	reply := turnInput(owner, "u2", "a2", "cascc → cascc")
	reply.ContinuationID, reply.ExpectedVersion = r.RequestID, &v
	r, err = c.Prepare(context.Background(), reply)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, q, r.EffectiveQuery)
	require.Equal(t, types.AbbreviationFeedbackKeep, fb.rows[0].Choice)

	// the tenant said "keep": next time the word is left alone
	r, err = c.Prepare(context.Background(), turnInput(owner, "u3", "a3", q))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, q, r.EffectiveQuery)
	require.Empty(t, r.AutoCorrections)
}

func TestGateTreatsSpelledOutWordAsAbbreviation(t *testing.T) {
	_, store, base, owner := newTurnCoordinatorFixture(t)
	q := "xử phạt vphc trong giao thông"
	dict := &typoDictionary{turnDictionary: base, rewrites: map[string]string{q: "xử phạt vi phạm hành chính trong giao thông"}}
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	in := turnInput(owner, "u1", "a1", q)
	in.ModelID = "chat"
	r, err := c.Prepare(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, r.Status)
	require.Equal(t, []string{"vphc"}, r.UnknownTerms)
	require.Empty(t, r.TypoSuggestions)
	require.Empty(t, r.Rewrite) // spelling it out is not offered
}

func TestGateSkipsModelsWithoutSuspectWords(t *testing.T) {
	_, store, base, owner := newTurnCoordinatorFixture(t)
	dict := &typoDictionary{turnDictionary: base}
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "thủ tục cấp lại giấy phép lái xe"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Zero(t, dict.calls)
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

func TestUnclearWordChoiceSeveralGuesses(t *testing.T) {
	r := types.AbbreviationResolution{
		UnknownTerms:    []string{"ddaats"},
		TypoSuggestions: []types.AbbreviationTypo{{Wrong: "ddaats", Correct: "đặt"}, {Wrong: "ddaats", Correct: "đất"}},
	}
	_, data := abbreviationChoice(r, false)
	opts := data["options"].([]map[string]interface{})
	require.Equal(t, "ddaats → đặt", opts[1]["resend"])
	require.Equal(t, "ddaats → đất", opts[2]["resend"])
}
