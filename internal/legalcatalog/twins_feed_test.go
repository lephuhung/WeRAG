package legalcatalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// A legal feed's row of a document the portal now lists is replaced like a
// placeholder: the portal copy is imported, the feed row aliased to it;
// even a reviewed feed row never keeps the portal copy out.
func TestResolveTwinsFeedRow(t *testing.T) {
	reviewed := time.Now()
	feed := catalogued("tvpl-777", "777/2026/NĐ-CP", "777/2026/ND-CP", "Nghị định",
		"Nghị định 777/2026/NĐ-CP quy định chi tiết về thuế hộ kinh doanh", "2026-11-05")
	feed.Source, feed.Issuer = "tvpl", "Chính phủ"
	other := catalogued("tvpl-778", "778/2026/NĐ-CP", "778/2026/ND-CP", "Nghị định", "Nghị định 778/2026/NĐ-CP về cảng biển", "2026-11-05")
	other.Source, other.ReviewedAt = "tvpl", &reviewed
	manual := catalogued("m1", "779/2026/NĐ-CP", "779/2026/ND-CP", "Nghị định", "Nghị định 779/2026/NĐ-CP về đường bộ", "2026-11-05")
	manual.Source = types.LegalDocumentSourceManual

	portal := imported("v777", "777/2026/NĐ-CP", "Nghị định", "Nghị định số 777/2026/NĐ-CP Quy định chi tiết về thuế hộ kinh doanh", "2026-11-05")
	portal.Issuer = "Chính phủ"
	portal778 := imported("v778", "778/2026/NĐ-CP", "Nghị định", "Nghị định số 778/2026/NĐ-CP về cảng biển", "2026-11-05")
	portal779 := imported("v779", "779/2026/NĐ-CP", "Nghị định", "Nghị định số 779/2026/NĐ-CP về đường bộ", "2026-11-05")
	// same number, another document (another day): no alias
	reused := imported("v900", "777/2026/NĐ-CP", "Quyết định", "Quyết định về giá đất", "2026-01-02")

	res := ResolveTwins([]*types.LegalDocument{feed, other, manual}, []*types.LegalDocument{portal, portal778, portal779, reused})
	require.Equal(t, "v777", res.Alias["tvpl-777"], "the feed row goes to the portal copy")
	require.Equal(t, "v778", res.Alias["tvpl-778"], "a reviewed feed row too")
	require.Equal(t, "m1", res.Alias["v779"], "a manual row keeps its old behaviour: the portal copy is attached to it")
	keep := map[string]bool{}
	for _, d := range res.Keep {
		keep[d.ID] = true
	}
	require.True(t, keep["v777"])
	require.True(t, keep["v778"])
	require.True(t, keep["v900"])
	require.False(t, keep["v779"])
	_, aliased := res.Alias["v900"]
	require.False(t, aliased)
}
