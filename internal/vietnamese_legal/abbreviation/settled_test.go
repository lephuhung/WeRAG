package abbreviation

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// Words the 2026-10-14 VLSP run asked about although nobody needs to be asked.
func TestSettledWithoutAsking(t *testing.T) {
	cases := []struct {
		text, word string
		want       bool
	}{
		{"Công ty TNHH Geodis có được công nhận không?", "TNHH", true},
		{"thông tin về tình trạng nhiễm HIV", "HIV", true},
		{"công suất lắp đặt 25 MW", "MW", true},
		{"đơn vị dự toán cấp III", "III", true},
		{"quỹ tiền lương kế hoạch năm 2025", "2025", true},
		{"Để luận văn được đánh giá cao, cần tuân thủ luật", "luật", true},
		{"Tập đoàn Dầu khí Việt Nam (PVN) triển khai dự án", "PVN", true},
		{"Một tổ chức tài chính vi mô (TCTCV) muốn đặt tên", "TCTCV", true},
		{"VSDC (Tổng công ty Lưu ký và Bù trừ chứng khoán) xử lý", "VSDC", true},
		// still asked: unknown, not spelled out
		{"Trong trường hợp TVBT vi phạm nghĩa vụ", "TVBT", false},
		{"đơn vị bị phát hiện tài liệu BMNN", "BMNN", false},
		{"thủ tục (TTHC)", "BMNN", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, SettledWithoutAsking(c.text, c.word, nil, true), "%s in %q", c.word, c.text)
	}
	// a marked lower-case word is settled as a detector key, open as a typo suspect
	require.True(t, SettledWithoutAsking("Tổng công ty đóng tầu Việt Nam", "tầu", nil, true))
	require.False(t, SettledWithoutAsking("số lượng mưcs phạt", "mưcs", nil, false))
	// a tenant dictionary entry keeps a common short form expandable
	require.False(t, SettledWithoutAsking("Giấy chứng nhận ATTP", "ATTP", map[string]bool{"attp": true}, true))
}

func TestDropSettled(t *testing.T) {
	text := "Công ty TNHH A và TVBT vi phạm, cấp III"
	det := types.AbbreviationDetection{Detector: "clef", Keys: []string{"tnhh", "tvbt", "iii"},
		Typos: []types.AbbreviationTypo{{Wrong: "TNHH", Correct: "x"}}}
	out := DropSettled(text, det, nil)
	require.Equal(t, []string{"tvbt"}, out.Keys)
	require.Empty(t, out.Typos)

	heur := DropSettled("Công ty TNHH và BMNN", types.AbbreviationDetection{}, nil)
	require.Equal(t, types.AbbreviationDetectorHeuristicSpellcheck, heur.Detector)
	require.Equal(t, []string{"bmnn"}, heur.Keys)
}

func TestSuspectWordsSkipNumbers(t *testing.T) {
	require.Empty(t, SuspectWords("năm 2025 cấp III"))
}
