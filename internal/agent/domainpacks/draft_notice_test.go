package domainpacks

import (
	"strings"
	"testing"

	domainpackassets "github.com/Tencent/WeKnora/domain-packs"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
)

func opcSource(t *testing.T) *Source {
	t.Helper()
	reg, err := Load(domainpackassets.FS)
	require.NoError(t, err)
	pack, ok := reg.Get("opc-vn-business-law")
	require.True(t, ok)
	return NewSource([]*Pack{pack})
}

const smeLine = "- Dự thảo Luật Phát triển doanh nghiệp nhỏ và vừa là dự thảo (Lần 2, ngày 28/08/2026), chưa có hiệu lực; " +
	"không áp dụng như quy định hiện hành."

func TestSplitParagraphs(t *testing.T) {
	got := splitParagraphs("Mở đầu\ndòng hai\n\n### 2. Tiêu đề\n\nĐoạn sau tiêu đề.\n- mục 1\n- mục 2\n| a | b |\n| c | d |\n\nKết.")
	assert.Equal(t, []string{
		"Mở đầu\ndòng hai",
		"### 2. Tiêu đề\nĐoạn sau tiêu đề.",
		"- mục 1", "- mục 2", "| a | b |", "| c | d |", "Kết.",
	}, got)
	// headings that open a list lead every item
	assert.Equal(t, []string{"## A\n- x", "## A\n- y"}, splitParagraphs("## A\n\n- x\n- y"))
	// a lead-in ending with ":" leads the items after it, not the paragraph after them
	assert.Equal(t, []string{"Theo dự thảo:", "Theo dự thảo:\n- x", "Theo dự thảo:\n- y", "Hết."},
		splitParagraphs("Theo dự thảo:\n\n- x\n- y\n\nHết."))
}

func TestDraftNotice(t *testing.T) {
	src := opcSource(t)
	cases := []struct {
		name, text string
		want       []string // notice lines; nil = no notice
	}{
		{"labelled", "Theo dự thảo Lần 2, Luật Phát triển DNNVV cho phép không có Chủ tịch.", nil},
		{"unlabelled alias", "Luật Phát triển DNNVV cho phép không cần Chủ tịch công ty.", []string{smeLine}},
		{"reversed article order", "Khoản 8 Điều 20 cho phép bỏ Chủ tịch.", []string{smeLine}},
		{"generic one-person company is not the draft", "Doanh nghiệp một người theo luật hiện hành là công ty TNHH MTV.", nil},
		{"label in another paragraph does not count",
			"Đây là dự thảo.\n\nĐiều 20 khoản 8 cho phép bỏ Chủ tịch.", []string{smeLine}},
		{"heading then a labelled paragraph (orig-4)",
			"### 2. Điều 20 khoản 8 là gì?\n\nTheo dự thảo Lần 2 ngày 28/8/2026, đó là …", nil},
		{"heading then an unlabelled paragraph",
			"### Luật Phát triển DNNVV\n\nCho phép bỏ Chủ tịch công ty.", []string{smeLine}},
		{"list item under a labelled lead-in", "Tóm tắt (dự thảo):\n- Luật Phát triển DNNVV bỏ Chủ tịch công ty.", nil},
		{"list item under an unlabelled lead-in", "Tóm tắt:\n\n- Ý khác.\n- Luật Phát triển DNNVV bỏ Chủ tịch công ty.", []string{smeLine}},
		{"word boundary", "Xem Điều 20 khoản 80 của luật khác.", nil},
		{"case and Unicode form", norm.NFD.String("LUẬT PHÁT TRIỂN DOANH NGHIỆP NHỎ VÀ VỪA có ưu đãi."), []string{smeLine}},
		{"two drafts, one undated",
			"Điều 20 khoản 8 có ưu đãi.\n\nViệc sửa đổi Luật Kế toán và Luật Kiểm toán độc lập có thể bỏ kế toán trưởng.",
			[]string{smeLine, "- Dự án Luật sửa đổi, bổ sung Luật Kế toán và Luật Kiểm toán độc lập là dự án luật " +
				"(chưa rõ lần dự thảo), chưa được thông qua; không áp dụng như quy định hiện hành."}},
		{"inside code blocks only",
			"Câu trả lời." + types.ToolAnswerBlockMarker + "Luật Phát triển DNNVV\n" + FooterMarker + "Điều 20 khoản 8\n", nil},
		{"no mention", "Luật Doanh nghiệp 59/2020/QH14 quy định Chủ tịch công ty.", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := src.DraftNotice(tc.text)
			if tc.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, DraftNoticeMarker+strings.Join(tc.want, "\n")+"\n", got)
		})
	}
}

func TestStripFooterCutsTheDraftNotice(t *testing.T) {
	answer := "Trả lời." + DraftNoticeMarker + "- x\n" + types.ToolAnswerBlockMarker + "y\n" + FooterMarker + "z\n"
	assert.Equal(t, "Trả lời.", StripFooter(answer))
}

func TestDraftAliasRules(t *testing.T) {
	drafts := []DraftDocument{{Title: "Dự thảo A", Aliases: []string{"doanh nghiệp một người", "Luật mới", "abc", " "}},
		{Title: "Dự thảo B", Aliases: []string{"Doanh nghiệp một người"}}}
	msgs := ""
	for _, err := range checkDraftAliases(drafts) {
		msgs += err.Error() + "\n"
	}
	for _, want := range []string{`"Luật mới" is too generic`, `"abc" is shorter than 4`, "aliases[3] is empty",
		`names both draft 0 and draft 1`} {
		assert.Contains(t, msgs, want)
	}
	assert.Empty(t, checkDraftAliases([]DraftDocument{{Title: "Dự thảo A", Aliases: []string{"doanh nghiệp một người"}}}))
}
