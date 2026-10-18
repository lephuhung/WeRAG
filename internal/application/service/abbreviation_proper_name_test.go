package service

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// properNameDictionary is the turn dictionary with the "keep as typed"
// store: a kept name joins the active rows as a proper name.
type properNameDictionary struct {
	*turnDictionary
	kept []string
}

var testUnitCodeRe = regexp.MustCompile(`\b[A-Z]{2}\d{2}\b`)

// DetectCandidates stands in for the Clef detector: unit codes are candidates.
func (d *properNameDictionary) DetectCandidates(_ context.Context, text string) types.AbbreviationDetection {
	det := types.AbbreviationDetection{Detector: "clef"}
	for _, m := range testUnitCodeRe.FindAllString(text, -1) {
		det.Keys = append(det.Keys, strings.ToLower(m))
	}
	return det
}

func (d *properNameDictionary) RememberProperName(_ context.Context, word string) (*types.Abbreviation, error) {
	d.kept = append(d.kept, word)
	row := &types.Abbreviation{ID: "pn-" + word, ShortForm: word, FullForm: word, IsActive: true, IsProperName: true}
	d.active = append(d.active, row)
	return row, nil
}

// Session 681b6a7a: "PV01" (a unit code) was asked about on every new
// request, and "3" / "là tên riêng, giữ nguyên" typed under the card were
// not understood. A keep answer now stores it as a proper name.
func TestGateKeepsUnitCodeAsProperName(t *testing.T) {
	db, store, base, owner := newTurnCoordinatorFixture(t)
	require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id, user_id) VALUES ('s2', 7, 'alice')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO messages (id, session_id, role) VALUES ('u9', 's2', 'user'), ('a9', 's2', 'assistant')`).Error)
	dict := &properNameDictionary{turnDictionary: base}
	c := NewAbbreviationTurnCoordinator(store, dict, nil)

	q := "Gửi Phòng PV01 về việc báo cáo kết quả rà soát"
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", q))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, r.Status)
	require.Equal(t, []string{"PV01"}, r.UnknownTerms)

	// "3" = the card's third option, "Giữ nguyên “PV01”"
	_, data := abbreviationChoice(r, false)
	opts := data["options"].([]map[string]interface{})
	require.Equal(t, "PV01 → PV01", opts[2]["resend"])
	v := r.Version
	reply := turnInput(owner, "u2", "a2", "3")
	reply.ContinuationID, reply.ExpectedVersion = r.RequestID, &v
	r, err = c.Prepare(context.Background(), reply)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, q, r.EffectiveQuery)
	require.Equal(t, []string{"PV01"}, dict.kept)

	// a new request, in another session too: not asked again, not expanded
	other := owner
	other.SessionID = "s2"
	r, err = c.Prepare(context.Background(), turnInput(other, "u9", "a9", "Gửi phòng PV01 về việc trao đổi thông tin nghiệp vụ"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Empty(t, r.UnknownTerms)
	require.Equal(t, "Gửi phòng PV01 về việc trao đổi thông tin nghiệp vụ", r.EffectiveQuery)
}

func TestGateUnderstandsTypedKeepReplies(t *testing.T) {
	for _, answer := range []string{"là tên riêng, giữ nguyên", "giữ nguyên", "PA05 là tên đơn vị", "không phải viết tắt"} {
		_, store, base, owner := newTurnCoordinatorFixture(t)
		dict := &properNameDictionary{turnDictionary: base}
		c := NewAbbreviationTurnCoordinator(store, dict, nil)
		r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "Gửi Phòng PA05 về việc báo cáo"))
		require.NoError(t, err, answer)
		require.Equal(t, []string{"PA05"}, r.UnknownTerms, answer)
		// typed in the composer, not clicked: the open question takes it
		r, err = c.Prepare(context.Background(), turnInput(owner, "u2", "a2", answer))
		require.NoError(t, err, answer)
		require.Equal(t, types.AbbreviationStatusReady, r.Status, answer)
		require.Equal(t, []string{"PA05"}, dict.kept, answer)
		require.Empty(t, base.suggestCalls, "%q must not become a dictionary meaning", answer)
	}
}

// An ordinary word kept as typed is not put in the dictionary.
func TestGateDoesNotKeepOrdinaryWordsAsProperNames(t *testing.T) {
	require.True(t, looksLikeProperName("Gửi Phòng PV01", "pv01"))
	require.True(t, looksLikeProperName("Phòng PA05-Đ4 gửi", "pa05-đ4"))
	require.False(t, looksLikeProperName("ý kiến của UBTV", "ubtv"), "an all-caps word may be a real abbreviation")
	require.False(t, looksLikeProperName("số lượng hồ sơ cascc loại", "cascc"))
	require.False(t, looksLikeProperName("hồ sơ Hưng", "hưng"))
	require.Equal(t, "PV01", surfaceForm("Gửi Phòng PV01 về", "pv01"))
}

func TestAbbreviationOptionReply(t *testing.T) {
	r := types.AbbreviationResolution{OriginalQuery: "Gửi PV01", UnknownTerms: []string{"PV01"}, Status: types.AbbreviationStatusNeedsDefinition}
	require.Equal(t, "PV01 → PV01", abbreviationOptionReply(r, "3"))
	require.Equal(t, "PV01 → PV01", abbreviationOptionReply(r, " 3. "))
	require.Equal(t, "1", abbreviationOptionReply(r, "1"), "an option with an input needs its text")
	require.Equal(t, "9", abbreviationOptionReply(r, "9"))
	require.Equal(t, "Gửi PV01 nhé", abbreviationOptionReply(r, "Gửi PV01 nhé"))
}

// Another user's keep made PV01 a proper name: a later keep (or a typo
// read as it) does not render "PV01 (PV01)".
func TestExpandShortFormsSkipsProperNames(t *testing.T) {
	active := []*types.Abbreviation{
		{ShortForm: "PV01", FullForm: "PV01", IsActive: true, IsProperName: true},
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}
	got := expandShortForms([]types.AbbreviationTypo{{Wrong: "PV01", Correct: "PV01"}, {Wrong: "ubnf", Correct: "UBND"}}, active)
	require.Equal(t, "PV01", got[0].Correct)
	require.Equal(t, "Ủy ban nhân dân (UBND)", got[1].Correct)
}
