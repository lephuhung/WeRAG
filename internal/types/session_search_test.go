package types

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestMakeSnippetVietnamese(t *testing.T) {
	// case folds on letters with diacritics; the match keeps the message's case
	s := MakeSnippet("Theo NGHỊ ĐỊNH 100/2019/NĐ-CP thì mức phạt", "nghị định 100")
	require.Equal(t, Snippet{Before: "Theo ", Match: "NGHỊ ĐỊNH 100", After: "/2019/NĐ-CP thì mức phạt"}, s)

	// accents matter, like ILIKE: "nghi dinh" is not "nghị định"
	s = MakeSnippet("Nghị định 100", "nghi dinh")
	require.Equal(t, Snippet{Before: "Nghị định 100"}, s)

	// a long message is cut on rune boundaries around the match, with "…"
	long := strings.Repeat("Điều khoản chuyển tiếp áp dụng. ", 10) + "Mức xử phạt hành chính" + strings.Repeat(" đối với cá nhân vi phạm", 10)
	s = MakeSnippet(long, "xử phạt")
	require.Equal(t, "xử phạt", s.Match)
	require.True(t, strings.HasPrefix(s.Before, "…"), s.Before)
	require.True(t, strings.HasSuffix(s.After, "…"), s.After)
	require.True(t, strings.HasSuffix(s.Before, "Mức "), s.Before)
	require.True(t, strings.HasPrefix(s.After, " hành chính"), s.After)
	for _, part := range []string{s.Before, s.Match, s.After} {
		require.True(t, utf8.ValidString(part))
	}
	require.LessOrEqual(t, utf8.RuneCountInString(s.Before), snippetContext+1)
	require.LessOrEqual(t, utf8.RuneCountInString(s.After), snippetContext+1)

	// newlines collapse
	s = MakeSnippet("Câu hỏi:\n\n  thời hiệu\tkhởi kiện", "THỜI HIỆU")
	require.Equal(t, Snippet{Before: "Câu hỏi: ", Match: "thời hiệu", After: " khởi kiện"}, s)

	// not found again (e.g. a collation difference): the start of the message
	s = MakeSnippet(strings.Repeat("ạ", 300), "xyz")
	require.Empty(t, s.Match)
	require.Equal(t, 2*snippetContext+1, utf8.RuneCountInString(s.Before))
}
