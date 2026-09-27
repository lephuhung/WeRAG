package abbreviation

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestParseUserDefinitionEvidence(t *testing.T) {
	cases := []struct {
		text     string
		sole     bool
		accepted bool
	}{
		{"ATTT = An toàn thông tin", false, true},
		{"ATTT là An toàn thông tin; ATTT có yêu cầu gì?", false, true},
		{"An toàn thông tin", true, true},
		{"An toàn thông tin.", true, false},
		{"ATTT có phải là An toàn thông tin không?", false, false},
		{"ATTT không phải An toàn thông tin", false, false},
		{"tôi không biết", true, false},
		{"assistant nói ATTT là An toàn thông tin", false, false},
		{"An toàn thông tin là gì?", true, false},
		{"thời tiết ngày mai", true, false},
		{"\"ATTT = An toàn thông tin\"", false, false},
		{"```ATTT = An toàn thông tin```", false, false},
		{"ATTT là không phải An toàn thông tin", false, false},
		{"ATTT = An toàn thông tin?", false, false},
		{"ATTT là An toàn thông tin đúng không", false, false},
		{"ATTT là An toàn thông tin phải không", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got, err := ParseUserDefinitions(tc.text, "u2", []string{"ATTT"}, tc.sole)
			require.NoError(t, err)
			require.Equal(t, tc.accepted, len(got.Definitions) == 1)
			for _, d := range got.Definitions {
				require.Equal(t, "u2", d.SourceMessageID)
				require.Equal(t, d.FullForm, tc.text[d.Start:d.End])
			}
			if !tc.accepted {
				require.True(t, got.NeedsExplicitMapping)
			}
		})
	}
}

func TestParseUserDefinitionsPartialAndOffsets(t *testing.T) {
	text := "🙂; ATTT = An toàn thông tin; XYZ là Xây dựng y tế"
	got, err := ParseUserDefinitions(text, "u3", []string{"ATTT", "XYZ", "ABC"}, false)
	require.NoError(t, err)
	require.True(t, got.IsDefinitionReply)
	require.Len(t, got.Definitions, 2)
	for _, d := range got.Definitions {
		require.Equal(t, d.FullForm, text[d.Start:d.End])
	}
	require.Equal(t, strings.Index(text, "An toàn thông tin"), got.Definitions[0].Start)
	require.Equal(t, strings.Index(text, "Xây dựng y tế"), got.Definitions[1].Start)

	partial, err := ParseUserDefinitions("ATTT = An toàn thông tin", "u3", []string{"ATTT", "XYZ"}, false)
	require.NoError(t, err)
	require.Len(t, partial.Definitions, 1)
	require.False(t, partial.NeedsExplicitMapping)
}

func TestParseUserDefinitionsRejectsUnsafeMappings(t *testing.T) {
	cases := []struct {
		text       string
		candidates []string
	}{
		{"OTHER = An toàn thông tin", []string{"ATTT"}},
		{"ATTT = ATTT", []string{"ATTT"}},
		{"ATTT = " + strings.Repeat("a", types.MaxAbbreviationFullFormRunes+1), []string{"ATTT"}},
		{strings.Repeat("A", types.MaxAbbreviationShortFormRunes+1) + " = A B C", []string{strings.Repeat("A", types.MaxAbbreviationShortFormRunes+1)}},
		{"ATTT = An toàn thông tin; attt = An toàn thực phẩm", []string{"ATTT"}},
	}
	for _, tc := range cases {
		t.Run(tc.text[:min(len(tc.text), 45)], func(t *testing.T) {
			got, err := ParseUserDefinitions(tc.text, "u3", tc.candidates, false)
			require.Error(t, err)
			require.Empty(t, got.Definitions)
		})
	}
}

func TestParseUserDefinitionsAcceptsNonNegatingFullForm(t *testing.T) {
	for _, phrase := range []string{"Không gian mạng", "Chính sách cho người dân"} {
		text := "KG = " + phrase
		got, err := ParseUserDefinitions(text, "u4", []string{"KG"}, false)
		require.NoError(t, err)
		require.Len(t, got.Definitions, 1)
		require.Equal(t, phrase, got.Definitions[0].FullForm)
	}
}

func TestParseUserDefinitionsRejectsOversizedBareReply(t *testing.T) {
	got, err := ParseUserDefinitions(strings.Repeat("A", types.MaxAbbreviationFullFormRunes+1), "u3", []string{"ATTT"}, true)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection)
	require.Empty(t, got.Definitions)
}

func TestParseUserDefinitionsBareReplyRequiresSoleCandidate(t *testing.T) {
	got, err := ParseUserDefinitions("An toàn thông tin", "u3", []string{"ATTT", "XYZ"}, false)
	require.NoError(t, err)
	require.Empty(t, got.Definitions)
	require.True(t, got.NeedsExplicitMapping)
}
