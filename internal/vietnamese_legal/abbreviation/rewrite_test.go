package abbreviation

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func readyResolution(t *testing.T, query string, actives ...*types.Abbreviation) types.AbbreviationResolution {
	t.Helper()
	r := Inspect(query, actives)
	require.Equal(t, types.AbbreviationStatusReady, r.Status, "resolution must be ready: %+v", r)
	require.NotEmpty(t, r.EffectiveQuery)
	return r
}

func protectedMarkers(t *testing.T, p ProtectedQuery) []string {
	t.Helper()
	text := p.Text()
	var out []string
	for {
		i := strings.Index(text, "⟦ABBR-")
		if i < 0 {
			return out
		}
		j := strings.Index(text[i:], "⟧")
		require.Greater(t, j, 0, "marker must be closed")
		out = append(out, text[i:i+j+len("⟧")])
		text = text[i+j:]
	}
}

func TestProtectRewrite_RestoreOnMarkerDeletion(t *testing.T) {
	r := readyResolution(t, "ATTT có yêu cầu gì",
		&types.Abbreviation{ID: "m1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true})
	p, err := ProtectRewrite(r)
	require.NoError(t, err)
	require.NotContains(t, p.Text(), "ATTT")

	// The model replaced the abbreviation with a different expansion instead
	// of keeping the protected marker: the restore path must fall back to the
	// validated effective query, never the raw or the tampered text.
	query, valid := p.Restore("An toàn thực phẩm có yêu cầu gì")
	require.False(t, valid)
	require.Equal(t, "An toàn thông tin (ATTT) có yêu cầu gì", query)
}

func TestProtectRewrite_RestorePreservesMarkers(t *testing.T) {
	r := readyResolution(t, "ATTT có yêu cầu gì",
		&types.Abbreviation{ID: "m1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true})
	p, err := ProtectRewrite(r)
	require.NoError(t, err)

	markers := protectedMarkers(t, p)
	require.Len(t, markers, 1)

	rewrite := "theo " + markers[0] + " thì cần những yêu cầu nào"
	out, valid := p.Restore(rewrite)
	require.True(t, valid)
	require.Equal(t, "theo An toàn thông tin (ATTT) thì cần những yêu cầu nào", out)
	require.NotContains(t, out, markers[0])
}

func TestProtectRewrite_TwoOccurrences(t *testing.T) {
	r := readyResolution(t, "ATTT và ATTT khác nhau không",
		&types.Abbreviation{ID: "m1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true})
	require.Len(t, r.Terms, 1)
	require.Len(t, r.Terms[0].Occurrences, 2)

	p, err := ProtectRewrite(r)
	require.NoError(t, err)
	require.NotContains(t, p.Text(), "ATTT")

	markers := protectedMarkers(t, p)
	require.Len(t, markers, 2, "one marker per occurrence")
	require.NotEqual(t, markers[0], markers[1])

	// Deleting only one of the two markers is still a violation.
	out, valid := p.Restore("theo " + markers[0] + " và ATTT khác nhau không")
	require.False(t, valid)
	require.Equal(t, r.EffectiveQuery, out)

	// A faithful rewrite restores both occurrences.
	out, valid = p.Restore(p.Text())
	require.True(t, valid)
	require.Equal(t, "An toàn thông tin (ATTT) và An toàn thông tin (ATTT) khác nhau không", out)
	require.Equal(t, r.EffectiveQuery, out)
}

func TestProtectRewrite_DuplicatedMarkerInvalid(t *testing.T) {
	r := readyResolution(t, "ATTT có yêu cầu gì",
		&types.Abbreviation{ID: "m1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true})
	p, err := ProtectRewrite(r)
	require.NoError(t, err)

	out, valid := p.Restore(p.Text() + " " + p.Text())
	require.False(t, valid)
	require.Equal(t, r.EffectiveQuery, out)
}

func TestProtectRewrite_ForeignMarkerRejected(t *testing.T) {
	r := readyResolution(t, "ATTT có yêu cầu gì",
		&types.Abbreviation{ID: "m1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true})
	p, err := ProtectRewrite(r)
	require.NoError(t, err)
	markers := protectedMarkers(t, p)
	require.Len(t, markers, 1)

	out, valid := p.Restore(markers[0] + " và ⟦ABBR-9⟧ cần gì")
	require.False(t, valid)
	require.Equal(t, r.EffectiveQuery, out)
}

func TestProtectRewrite_LiteralMarkerLikeTextNotConfused(t *testing.T) {
	// The user query literally contains a marker-shaped token whose payload
	// differs from the issued namespace (plus a lowercase bracket literal).
	// The issued markers stay distinguishable and a faithful echo restores.
	r := readyResolution(t, "ATTT xem ⟦ghi chú⟧ có yêu cầu gì",
		&types.Abbreviation{ID: "m1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true})
	p, err := ProtectRewrite(r)
	require.NoError(t, err)
	require.Contains(t, p.Text(), "⟦ghi chú⟧")
	markers := protectedMarkers(t, p)
	require.Len(t, markers, 1)

	out, valid := p.Restore(p.Text())
	require.True(t, valid)
	require.Equal(t, r.EffectiveQuery, out)
	require.Contains(t, out, "⟦ghi chú⟧")
}

func TestProtectRewrite_NotReadyRejected(t *testing.T) {
	r := Inspect("XYZABC là gì", nil)
	require.NotEqual(t, types.AbbreviationStatusReady, r.Status)
	_, err := ProtectRewrite(r)
	require.ErrorIs(t, err, types.ErrAbbreviationNotReady)
}
