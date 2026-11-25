package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSpellingVerdictRequestNormalize(t *testing.T) {
	const para = "Ủy ban nhân dân xã ban hành quyết đinh số 5."
	cases := []struct {
		name    string
		req     SpellingVerdictRequest
		ok      bool
		verdict string
		corr    string
	}{
		{"default is not_error", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Suggestion: "định", Correction: "x"},
			true, types.SpellingVerdictNotError, ""},
		{"explicit not_error", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Verdict: "not_error"},
			true, types.SpellingVerdictNotError, ""},
		{"accepted", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Suggestion: "định", Verdict: "accepted", Correction: "định"},
			true, types.SpellingVerdictAccepted, ""},
		{"accepted without suggestion", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Verdict: "accepted"}, false, "", ""},
		{"fixed", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Suggestion: "đỉnh", Verdict: "fixed", Correction: " định "},
			true, types.SpellingVerdictFixed, "định"},
		{"fixed without correction", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Verdict: "fixed", Correction: "  "}, false, "", ""},
		{"fixed with correction == wrong", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Verdict: "fixed", Correction: "đinh"}, false, "", ""},
		{"fixed with long correction", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Verdict: "fixed", Correction: string(make([]rune, 256))}, false, "", ""},
		{"unknown verdict", SpellingVerdictRequest{Paragraph: para, Wrong: "đinh", Verdict: "error"}, false, "", ""},
		{"wrong not in paragraph", SpellingVerdictRequest{Paragraph: para, Wrong: "định", Verdict: "accepted", Suggestion: "định"}, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			require.Equal(t, tc.ok, req.normalize())
			if tc.ok {
				require.Equal(t, tc.verdict, req.Verdict)
				require.Equal(t, tc.corr, req.Correction)
			}
		})
	}
}
