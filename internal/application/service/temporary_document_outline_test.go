package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// paragraphChunks cuts content into one chunk per paragraph, with the rune
// offsets the chunker gives.
func paragraphChunks(content string) []types.TemporaryDocumentChunk {
	var chunks []types.TemporaryDocumentChunk
	offset := 0
	for _, para := range strings.SplitAfter(content, "\n\n") {
		n := utf8.RuneCountInString(para)
		if strings.TrimSpace(para) != "" {
			chunks = append(chunks, types.TemporaryDocumentChunk{
				Seq: len(chunks), Content: para, Start: offset, End: offset + n, TokenCount: n / 4,
			})
		}
		offset += n
	}
	return chunks
}

// a local báo cáo: header, title block, I./II./III. with numbered
// subsections, signature
const reportText = `| UBND THÀNH PHỐ HUẾ **SỞ NỘI VỤ** | **CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM** |

Số: 268/BC-SNV

**BÁO CÁO**

**Kết quả công tác văn thư, lưu trữ 9 tháng đầu năm 2026**

Thực hiện Công văn số 1021/UBND-NC, Sở Nội vụ báo cáo như sau:

**I. CÔNG TÁC CHỈ ĐẠO, ĐIỀU HÀNH**

Sở đã ban hành kế hoạch công tác văn thư, lưu trữ năm 2026.

**II. KẾT QUẢ THỰC HIỆN**

**1. Công tác văn thư**

Sở đã tiếp nhận 3.624 văn bản đến và phát hành 2.118 văn bản đi.

**2. Công tác lưu trữ**

Toàn Sở đã nộp lưu 415 hồ sơ công việc.

**III. KIẾN NGHỊ, ĐỀ XUẤT**

Đề nghị UBND thành phố bố trí kinh phí số hóa tài liệu.

**GIÁM ĐỐC**

**Trần Minh Quang**
`

func sectionTitles(sections []attachmentSection) []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = fmt.Sprintf("%d %s", s.Depth, s.Title)
	}
	return out
}

func TestHeadingKind(t *testing.T) {
	cases := map[string]string{
		"## Kết quả thực hiện":      "md2",
		"**I. CÔNG TÁC CHỈ ĐẠO**":   "roman",
		"A. PHÂN CẤP GIẢI QUYẾT":    "alpha",
		"1. Công tác văn thư":       "num",
		"1.2. Về đội ngũ nhà giáo:": "dot2",
		"Chương II":                 "chuong",
		"Mục 2":                     "muc",
		"Phụ lục IV":                kindTop,
		"Mẫu số 02":                 kindTop,
		"BM-01":                     kindTop,
		"DỮ LIỆU ĐIỆN TỬ VỀ LỆ PHÍ TRƯỚC BẠ":                                         kindUpper,
		"**Tổ chức thực hiện**":                                                      kindBold,
		"Điều 1. Ban hành kèm theo Quyết định này " + strings.Repeat("quy chế ", 30): "dieu",
		// body text, measurements and the frame of an administrative document
		"1. Giao Sở Tài chính chủ trì, phối hợp với các sở, ban, ngành liên quan tham mưu UBND thành phố bố trí kinh phí": "",
		"53.98 mm53.98 mm": "",
		"85.6 mm":          "",
		"**CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM**": "",
		"Kính gửi: Ủy ban nhân dân thành phố":    "",
		"**GIÁM ĐỐC**":        "",
		"KT. CHỦ TỊCH":        "",
		"**Trần Minh Quang**": "",
		"Sở đã tiếp nhận 3.624 văn bản đến.":   "",
		"| Quý I | 1.180 | 702 |":              "",
		"**Nội dung chính gồm các phần sau:**": "",
	}
	for line, want := range cases {
		kind, _ := headingKind(line)
		require.Equal(t, want, kind, line)
	}
}

func TestBuildAttachmentSectionsOfALocalReport(t *testing.T) {
	sections := buildAttachmentSections(reportText, paragraphChunks(reportText))
	require.Equal(t, []string{
		"0 Mở đầu: BÁO CÁO Kết quả công tác văn thư, lưu trữ 9 tháng đầu năm 2026",
		"0 I. CÔNG TÁC CHỈ ĐẠO, ĐIỀU HÀNH",
		"0 II. KẾT QUẢ THỰC HIỆN",
		"1 1. Công tác văn thư",
		"1 2. Công tác lưu trữ",
		"0 III. KIẾN NGHỊ, ĐỀ XUẤT",
	}, sectionTitles(sections))

	// II. covers its subsections; its own text ends at "1."
	ii := sections[2]
	require.Equal(t, sections[5].Start, ii.End)
	require.Equal(t, sections[3].Start, ii.OwnEnd)
	chunks := paragraphChunks(reportText)
	require.Contains(t, chunks[ii.First].Content, "II. KẾT QUẢ")
	require.Contains(t, chunks[ii.Last].Content, "415 hồ sơ")
	require.Greater(t, ii.Tokens, sections[3].Tokens)
}

// A whole-line bold numbered heading longer than a list item is still a
// heading (Báo cáo UBND tỉnh, 2026-10-16: "1." merged into "I." and "4."
// fell out of the outline).
func TestBuildAttachmentSectionsKeepsLongBoldNumberedHeadings(t *testing.T) {
	body := strings.Repeat("- Ưu điểm: các đơn vị đã triển khai đầy đủ theo kế hoạch được giao trong năm.\n\n", 4)
	text := "**BÁO CÁO**\n\nKết quả kiểm tra công tác đảm bảo an toàn hệ thống thông tin\n\n" +
		"**I. CÔNG TÁC QUẢN LÝ NHÀ NƯỚC HỆ THỐNG THÔNG TIN**\n\n" +
		"**1. Việc ban hành quy chế hoạt động và chấp hành quy định vận hành Cổng/Trang thông tin điện tử đối với tên miền “.vn”**\n\n" + body +
		"**2. Việc công khai, minh bạch thông tin trên Cổng/Trang thông tin điện tử của đơn vị**\n\n" + body +
		"**3. Việc tuân thủ các quy định về đảm bảo an toàn thông tin và ứng cứu sự cố an toàn**\n\n" + body +
		"**4. Việc triển khai thực hiện các văn bản chỉ đạo về công tác bảo đảm an toàn thông tin cho cán bộ, công chức, viên chức tại đơn vị**\n\n" + body +
		"**II. NHẬN XÉT, ĐÁNH GIÁ**\n\n" + body
	sections := buildAttachmentSections(text, paragraphChunks(text))
	var got []string
	for _, s := range sections {
		got = append(got, fmt.Sprintf("%d %s", s.Depth, strings.SplitN(s.Title, " ", 3)[0]))
	}
	require.Equal(t, []string{"0 Mở", "0 I.", "1 1.", "1 2.", "1 3.", "1 4.", "0 II."}, got)
	require.Equal(t, "I. CÔNG TÁC QUẢN LÝ NHÀ NƯỚC HỆ THỐNG THÔNG TIN", sections[1].Title)

	// a bold sentence of a list stays body text, and is not merged into
	// the heading above it
	for _, line := range []string{
		"**1. Giao Sở Tài chính chủ trì, phối hợp với các sở, ban, ngành và Ủy ban nhân dân các huyện, thị xã, thành phố tham mưu bố trí kinh phí theo quy định.**",
		"**2. Các sở, ban, ngành, Ủy ban nhân dân các huyện, thị xã, thành phố căn cứ chức năng, nhiệm vụ được giao tổ chức triển khai thực hiện;**",
	} {
		kind, _ := headingKind(line)
		require.Equal(t, "", kind, line)
	}
	// bold italic keeps its numbering level
	kind, title := headingKind("***1.1. Dịch vụ công Hà Tĩnh***")
	require.Equal(t, "dot2", kind)
	require.Equal(t, "1.1. Dịch vụ công Hà Tĩnh", title)

	// a plain (not bold) numbered line that long stays body text
	kind, _ = headingKind("1. Việc ban hành quy chế hoạt động và chấp hành quy định vận hành Cổng/Trang thông tin điện tử đối với tên miền “.vn”")
	require.Equal(t, "", kind)
}

func TestBuildAttachmentSectionsKeepsTheLegalOrder(t *testing.T) {
	// Chương I has no Mục, so Điều appears before the first Mục
	text := "Chương I\n\nQUY ĐỊNH CHUNG\n\nĐiều 1. Phạm vi\n\nNội dung.\n\nĐiều 2. Đối tượng\n\nNội dung.\n\n" +
		"Chương II\n\nMục 1\n\nĐiều 3. Hồ sơ\n\nNội dung.\n\nMục 2\n\nĐiều 4. Thủ tục\n\nNội dung.\n"
	sections := buildAttachmentSections(text, paragraphChunks(text))
	require.Equal(t, []string{
		"0 Chương I QUY ĐỊNH CHUNG",
		"1 Điều 1. Phạm vi",
		"1 Điều 2. Đối tượng",
		"0 Chương II",
		"1 Mục 1",
		"2 Điều 3. Hồ sơ",
		"1 Mục 2",
		"2 Điều 4. Thủ tục",
	}, sectionTitles(sections))
}

func TestBuildAttachmentSectionsOfFormsWithNumberedParts(t *testing.T) {
	// each form has an upper-case title and its own list from 1.
	text := "GIẤY KHAI ĐĂNG KÝ XE\n\n1. Thông tin chủ xe\n\nHọ tên.\n\n2. Đặc điểm xe\n\nSố máy.\n\n" +
		"ĐƠN ĐỀ NGHỊ XÁC NHẬN HỒ SƠ\n\n1. Thông tin người đề nghị\n\nHọ tên.\n\n2. Nội dung đề nghị\n\nXác nhận.\n\n" +
		"GIẤY XÁC NHẬN HỒ SƠ XE\n\n1. Thông tin xe\n\nBiển số.\n\n2. Kết quả xác nhận\n\nĐã xác nhận.\n"
	sections := buildAttachmentSections(text, paragraphChunks(text))
	require.Equal(t, []string{
		"0 GIẤY KHAI ĐĂNG KÝ XE",
		"1 1. Thông tin chủ xe",
		"1 2. Đặc điểm xe",
		"0 ĐƠN ĐỀ NGHỊ XÁC NHẬN HỒ SƠ",
		"1 1. Thông tin người đề nghị",
		"1 2. Nội dung đề nghị",
		"0 GIẤY XÁC NHẬN HỒ SƠ XE",
		"1 1. Thông tin xe",
		"1 2. Kết quả xác nhận",
	}, sectionTitles(sections))
}

func TestBuildAttachmentSectionsWithoutStructure(t *testing.T) {
	plain := strings.Repeat("Biên bản ghi nhận ý kiến của các đại biểu tại cuộc họp, không chia mục.\n\n", 40)
	require.Nil(t, buildAttachmentSections(plain, paragraphChunks(plain)))

	// two small headings and one section with almost all of the text
	lopsided := "I. MỞ ĐẦU\n\nNgắn.\n\nII. NỘI DUNG\n\n" + plain + "III. KẾT LUẬN\n\nNgắn.\n"
	require.Nil(t, buildAttachmentSections(lopsided, paragraphChunks(lopsided)))
}

func TestBuildAttachmentSectionsSkipsALongFlatList(t *testing.T) {
	// OCR of a worksheet: the same numbered lines over and over
	var b strings.Builder
	for i := 0; i < 40; i++ {
		for j := 1; j <= 8; j++ {
			fmt.Fprintf(&b, "%d. Write a test sentence.\n\nanswer line\n\n", j)
		}
	}
	text := b.String()
	require.Nil(t, buildAttachmentSections(text, paragraphChunks(text)))
}

func TestBuildAttachmentSectionsKeepsABareArticleLine(t *testing.T) {
	// PDF text puts the article title on its own line, or leaves none
	text := "NGHỊ QUYẾT\n\nKỳ họp thứ 6\n\nĐiều 1\n\nQuốc hội đã hoàn thành chương trình kỳ họp.\n\n" +
		"Điều 2\n\nGiao Chính phủ tổ chức thực hiện.\n\nĐiều 3.\n\nNghị quyết có hiệu lực từ ngày ký.\n"
	sections := buildAttachmentSections(text, paragraphChunks(text))
	require.Equal(t, []string{"0 Mở đầu: NGHỊ QUYẾT", "0 Điều 1", "0 Điều 2", "0 Điều 3."}, sectionTitles(sections))

	kind, _ := headingKind("Điều 5 Luật này quy định về trách nhiệm của cơ quan")
	require.Equal(t, "", kind, "a sentence that starts with Điều 5 is body text")
}

func TestBuildAttachmentSectionsLimitsTheDepth(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "Chương %d\n\n", i)
		for j := 1; j <= 15; j++ {
			fmt.Fprintf(&b, "Điều %d. Quy định %d\n\nNội dung của điều.\n\n", i*100+j, j)
		}
	}
	text := b.String()
	sections := buildAttachmentSections(text, paragraphChunks(text))
	require.Len(t, sections, 5, "5 chương + 75 điều exceed the cap: only the chương remain")
}

func TestRenderAttachmentOutlineIndentsTheTree(t *testing.T) {
	sections := buildAttachmentSections(reportText, paragraphChunks(reportText))
	outline, cut := renderAttachmentOutline(sections, 0)
	require.False(t, cut)
	lines := strings.Split(outline, "\n")
	require.Len(t, lines, 6)
	require.True(t, strings.HasPrefix(lines[2], "[S3] II. KẾT QUẢ THỰC HIỆN (~"))
	require.True(t, strings.HasPrefix(lines[3], "  [S4] 1. Công tác văn thư (~"))

	short, cut := renderAttachmentOutline(sections, 120)
	require.True(t, cut)
	require.NotContains(t, short, "[S3]")
}

func TestParseAttachmentPick(t *testing.T) {
	pick, err := parseAttachmentPick("Đây là kết quả: {\"sections\": [\"S3\", \"s1\", 2, \"S3\", \"S9\", \"x\"], \"whole\": false}", 4)
	require.NoError(t, err)
	require.Equal(t, []int{3, 1, 2}, pick.Sections, "duplicates, unknown and out-of-range ids are dropped")
	require.False(t, pick.Whole)

	pick, err = parseAttachmentPick(`{"sections": [], "whole": true}`, 4)
	require.NoError(t, err)
	require.True(t, pick.Whole)

	_, err = parseAttachmentPick("Không có phần nào.", 4)
	require.Error(t, err)
}

func plainChunk(tokens int, content string) types.TemporaryDocumentChunk {
	return types.TemporaryDocumentChunk{Content: content, TokenCount: tokens}
}

func TestPackPickedSectionsKeepsKeywordHitsWhenThePickIsWrong(t *testing.T) {
	chunks := []types.TemporaryDocumentChunk{
		plainChunk(400, "phần mở đầu"),
		plainChunk(400, "nội dung A"),
		plainChunk(400, "nội dung B"),
		plainChunk(400, "thời hạn hiệu lực của văn bản"),
	}
	sections := []attachmentSection{
		{ID: 1, Title: "Mở đầu", First: 0, Last: 0, Tokens: 400},
		{ID: 2, Title: "A", First: 1, Last: 2, Tokens: 800},
		{ID: 3, Title: "Hiệu lực", First: 3, Last: 3, Tokens: 400},
	}
	// the pick names the wrong sections; the keyword reserve keeps chunk 3
	got := packPickedSections(chunks, sections, attachmentPick{Sections: []int{1, 2}}, "hiệu lực", 1200)
	require.Equal(t, []int{0, 1, 3}, got)

	// a right pick needs no keyword
	got = packPickedSections(chunks, sections, attachmentPick{Sections: []int{3}}, "khi nào áp dụng", 1200)
	require.Equal(t, []int{3}, got)

	// whole: the opening chunk of every section
	got = packPickedSections(chunks, sections, attachmentPick{Whole: true}, "tóm tắt", 1200)
	require.Equal(t, []int{0, 1, 3}, got)
}

func TestPackPickedSectionsCountsAdjacentChunksAsOnePart(t *testing.T) {
	chunks := make([]types.TemporaryDocumentChunk, 40)
	for i := range chunks {
		chunks[i] = plainChunk(10, fmt.Sprintf("đoạn %d", i))
	}
	sections := []attachmentSection{{ID: 1, Title: "Toàn bộ", First: 0, Last: 39, Tokens: 400}}
	got := packPickedSections(chunks, sections, attachmentPick{Sections: []int{1}}, "câu hỏi", 1000)
	require.Len(t, got, 40, "one section of 40 chunks is one part, not 40")
}

type pickerSettings struct {
	interfaces.SystemSettingService
	on     bool
	inline int64 // chat.attachment_inline_tokens; 0 = default
}

func (s pickerSettings) GetInt(_ context.Context, key, _ string, def int64) int64 {
	if key == types.SettingChatAttachmentInlineTokens && s.inline > 0 {
		return s.inline
	}
	return def
}

func (s pickerSettings) GetBool(_ context.Context, key, _ string, def bool) bool {
	if key == types.SettingChatAttachmentSectionPicker {
		return s.on
	}
	return def
}

func largeReportDoc() *types.TemporaryDocument {
	raw, _ := json.Marshal(paragraphChunks(reportText))
	return &types.TemporaryDocument{ID: "doc", FileName: "bao_cao.docx", Content: reportText, Chunks: types.JSON(raw), TokenCount: 20000}
}

func TestSelectForPromptUsesTheModelsPick(t *testing.T) {
	var calls []string
	svc := &temporaryDocumentService{
		settings: pickerSettings{on: true},
		modelService: auxModelService{chats: map[string]chat.Chat{
			"qwen": replyChat{id: "qwen", reply: `{"sections": ["S5"], "whole": false}`, calls: &calls},
		}},
	}
	sel := svc.selectForPrompt(context.Background(), largeReportDoc(), "Đã nộp lưu bao nhiêu hồ sơ?", 60,
		types.TemporaryDocumentPromptOptions{ModelID: "qwen"})
	require.Equal(t, []string{"qwen"}, calls)
	require.Equal(t, "selected_sections", sel.mode)
	require.Contains(t, sel.content, "415 hồ sơ")
	require.NotContains(t, sel.content, "kinh phí số hóa")
	require.Contains(t, sel.outline, "[S5] 2. Công tác lưu trữ")
}

func TestSelectForPromptFallsBackToKeywords(t *testing.T) {
	failing := auxModelService{chats: map[string]chat.Chat{
		"qwen": replyChat{id: "qwen", reply: "không biết", calls: new([]string)},
	}}
	cases := map[string]struct {
		svc  *temporaryDocumentService
		opts types.TemporaryDocumentPromptOptions
	}{
		"no model":         {&temporaryDocumentService{settings: pickerSettings{on: true}, modelService: failing}, types.TemporaryDocumentPromptOptions{}},
		"setting off":      {&temporaryDocumentService{settings: pickerSettings{on: false}, modelService: failing}, types.TemporaryDocumentPromptOptions{ModelID: "qwen"}},
		"unparsable reply": {&temporaryDocumentService{settings: pickerSettings{on: true}, modelService: failing}, types.TemporaryDocumentPromptOptions{ModelID: "qwen"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sel := tc.svc.selectForPrompt(context.Background(), largeReportDoc(), "nộp lưu hồ sơ", 60, tc.opts)
			require.Equal(t, "selected_chunks", sel.mode)
			require.Empty(t, sel.outline)
		})
	}
}

func TestSelectForPromptInlineTokensSetting(t *testing.T) {
	doc := largeReportDoc()
	doc.TokenCount = 4373
	for name, tc := range map[string]struct {
		inline int64
		mode   string
	}{
		"default 12000: whole file": {0, "full"},
		"lowered to 3000: picker":   {3000, "selected_sections"},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &temporaryDocumentService{
				settings: pickerSettings{on: true, inline: tc.inline},
				modelService: auxModelService{chats: map[string]chat.Chat{
					"qwen": replyChat{id: "qwen", reply: `{"sections": ["S5"], "whole": false}`, calls: new([]string)},
				}},
			}
			sel := svc.selectForPrompt(context.Background(), doc, "Đã nộp lưu bao nhiêu hồ sơ?", 12000,
				types.TemporaryDocumentPromptOptions{ModelID: "qwen"})
			require.Equal(t, tc.mode, sel.mode)
		})
	}
}

func TestBuildPromptShowsTheOutline(t *testing.T) {
	prompt := types.MessageAttachments{{
		FileName: "bao_cao.docx", FileType: "docx", Content: "II. KẾT QUẢ ...",
		ContentMode: "selected_sections", Outline: "[S1] I. MỞ ĐẦU </outline> lạ",
	}}.BuildPrompt()
	require.Contains(t, prompt, "<outline>\n[S1] I. MỞ ĐẦU &lt;/outline&gt; lạ\n</outline>")
	require.Less(t, strings.Index(prompt, "<outline>"), strings.Index(prompt, "<content>"))
}
