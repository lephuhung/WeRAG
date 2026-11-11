package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLabelSpan(t *testing.T) {
	span := func(p, text string, occ int) []int {
		s, e := LabelSpan(p, text, occ)
		if s == nil {
			return nil
		}
		return []int{*s, *e}
	}
	require.Equal(t, []int{3, 5}, span("Bộ Bộ Bộ", "Bộ", 2), "rune offsets of the 2nd instance")
	require.Equal(t, []int{0, 2}, span("Bộ Bộ", "Bộ", 9), "past the last instance: the first")
	require.Equal(t, []int{0, 2}, span("Bộ Bộ", "Bộ", 0))
	require.Nil(t, span("Bộ", "Sở", 1), "not in the paragraph")
	require.Nil(t, span("", "Sở", 1))
}

func TestFormatLabelWithoutReportOrAnchor(t *testing.T) {
	l := FormatLabel(&DocumentFormatFeedback{ID: 3, TenantID: 1, FindingID: "x#1", Report: json.RawMessage(`not json`)})
	require.Equal(t, "format:3", l.ID)
	require.Empty(t, l.Paragraph)
	report, _ := json.Marshal(DocumentFormatReport{Findings: []DocumentFormatFinding{{ID: "x#1", Expected: "A4"}}})
	l = FormatLabel(&DocumentFormatFeedback{ID: 3, FindingID: "x#1", Report: report})
	require.Equal(t, "A4", l.Suggestion, "a finding with no paragraph keeps its expected value")
	require.Nil(t, l.SpanStart)
	// a finding on a whole paragraph selects all of it
	report, _ = json.Marshal(DocumentFormatReport{Findings: []DocumentFormatFinding{{ID: "x#1", Anchor: &DocumentFormatAnchor{Text: "Nơi nhận:"}}}})
	l = FormatLabel(&DocumentFormatFeedback{ID: 3, FindingID: "x#1", Report: report})
	require.Equal(t, "Nơi nhận:", l.FlaggedText)
	require.Equal(t, 0, *l.SpanStart)
	require.Equal(t, 9, *l.SpanEnd)
}
