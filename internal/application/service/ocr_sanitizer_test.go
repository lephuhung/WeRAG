package service

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
)

func TestSanitizeOCRText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   \n\t  ",
			want:  "",
		},
		{
			name:  "pure HTML skeleton with no text",
			input: `<html><body><div class="image"><img/></div></body></html>`,
			want:  "",
		},
		{
			name:  "HTML with only whitespace text",
			input: "<html><body>  \n  </body></html>",
			want:  "",
		},
		{
			name:  "valid markdown passes through",
			input: "# Tiêu đề\n\nĐây là một đoạn văn bản, có một số nội dung.\n\n| Cột 1 | Cột 2 |\n| --- | --- |\n| Dữ liệu 1 | Dữ liệu 2 |",
			want:  "# Tiêu đề\n\nĐây là một đoạn văn bản, có một số nội dung.\n\n| Cột 1 | Cột 2 |\n| --- | --- |\n| Dữ liệu 1 | Dữ liệu 2 |",
		},
		{
			name:  "code block wrapper stripped",
			input: "```markdown\n# Tiêu đề tài liệu\n\nNội dung chính ở đây.\n```",
			want:  "# Tiêu đề tài liệu\n\nNội dung chính ở đây.",
		},
		{
			name:  "html code block wrapper stripped",
			input: "```html\n<p>Đây là một đoạn nội dung</p>\n```",
			want:  "Đây là một đoạn nội dung",
		},
		{
			name:  "HTML document converted to markdown",
			input: "<html><body><h1>Tiêu đề</h1><p>Đây là một đoạn văn bản khá dài, dùng để kiểm tra việc chuyển đổi HTML sang Markdown.</p></body></html>",
			want:  "# Tiêu đề\n\nĐây là một đoạn văn bản khá dài, dùng để kiểm tra việc chuyển đổi HTML sang Markdown.",
		},
		{
			name:  "known empty reply - Chinese",
			input: "无文字内容",
			want:  "",
		},
		{
			name:  "known empty reply - no text",
			input: "No text",
			want:  "",
		},
		{
			name:  "known empty reply - Chinese no-text phrase",
			input: "图片中没有文字",
			want:  "",
		},
		{
			name:  "plain text with minimal HTML not converted",
			input: "Đây là văn bản bình thường, giá <100 nghìn đồng.",
			want:  "Đây là văn bản bình thường, giá <100 nghìn đồng.",
		},
		{
			name:  "multiple blank lines collapsed",
			input: "Đoạn một\n\n\n\n\nĐoạn hai",
			want:  "Đoạn một\n\nĐoạn hai",
		},
		{
			name:  "HTML with substantial text content is converted",
			input: "<div><h2>Tóm tắt báo cáo</h2><p>Doanh thu quý này tăng 15% so với cùng kỳ, lợi nhuận ròng đạt 2,3 tỷ đồng.</p><table><tr><th>Chỉ tiêu</th><th>Giá trị</th></tr><tr><td>Doanh thu</td><td>10 tỷ</td></tr></table></div>",
			want:  "", // placeholder; will be checked for non-empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeOCRText(tt.input)

			if tt.name == "HTML with substantial text content is converted" {
				if got == "" {
					t.Errorf("sanitizeOCRText() returned empty for substantial HTML content")
				}
				if got == tt.input {
					t.Errorf("sanitizeOCRText() did not convert HTML, got original")
				}
				return
			}

			if got != tt.want {
				t.Errorf("sanitizeOCRText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripMarkdownCodeBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no code block",
			input: "just normal text",
			want:  "just normal text",
		},
		{
			name:  "markdown code block",
			input: "```markdown\n# Title\nContent here\n```",
			want:  "# Title\nContent here",
		},
		{
			name:  "html code block",
			input: "```html\n<p>hello</p>\n```",
			want:  "<p>hello</p>",
		},
		{
			name:  "plain code block",
			input: "```\nsome text\n```",
			want:  "some text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkdownCodeBlock(tt.input)
			if got != tt.want {
				t.Errorf("stripMarkdownCodeBlock() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLooksLikeHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "HTML document",
			input: "<html><body><p>text</p></body></html>",
			want:  true,
		},
		{
			name:  "DOCTYPE",
			input: "<!DOCTYPE html><html><body></body></html>",
			want:  true,
		},
		{
			name:  "body tag",
			input: "<body><p>content</p></body>",
			want:  true,
		},
		{
			name:  "plain markdown",
			input: "# Title\n\nSome paragraph text",
			want:  false,
		},
		{
			name:  "text with minor HTML",
			input: "This is mostly text with a <b>bold</b> word.",
			want:  false,
		},
		{
			name:  "heavy HTML tags",
			input: "<div><p><span>x</span></p></div><div><p><span>y</span></p></div>",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeHTML(tt.input)
			if got != tt.want {
				t.Errorf("looksLikeHTML() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSanitizeOCRText_ConvertsInlineHTMLTable guards against a regression where
// an HTML <table> embedded in a markdown body survived sanitization untouched:
// the mixed content does not satisfy looksLikeHTML, so the old HTML-to-markdown
// path never ran and the raw <table> markup reached the chunker.
func TestSanitizeOCRText_ConvertsInlineHTMLTable(t *testing.T) {
	para := "Trong quý này, tình hình hoạt động chung của đơn vị giữ ổn định, hai chỉ tiêu cốt lõi là doanh thu và lợi nhuận đều tăng so với cùng kỳ. " +
		"Để lãnh đạo nhanh chóng nắm được kết quả hoạt động, bảng dưới đây tổng hợp các số liệu tài chính chính trong kỳ báo cáo, " +
		"phục vụ công tác phân tích hoạt động, lập dự toán và đánh giá cuối năm, " +
		"đề nghị đối chiếu với tình hình thực tế để nhận định tổng hợp, không tách riêng bất kỳ con số nào khỏi bối cảnh nghiệp vụ."
	tail := "Các số liệu trên đều lấy từ báo cáo chính thức đã được phòng Tài chính thẩm định, tiêu chí thống kê giữ nguyên như kỳ báo cáo trước, " +
		"không có thay đổi chính sách kế toán hay điều chỉnh hồi tố. Nếu có thắc mắc đề nghị liên hệ phòng Tài chính để xác nhận, " +
		"quyền giải thích cuối cùng thuộc về phòng Tài chính của đơn vị."
	input := "# Báo cáo\n\n" + para + "\n\n" +
		`<table><tr><th>Chỉ tiêu</th><th>Giá trị</th></tr>` +
		`<tr><td>Doanh thu</td><td>10 tỷ</td></tr>` +
		`<tr><td>Lợi nhuận</td><td>2,3 tỷ</td></tr></table>` +
		"\n\n" + tail + "\n\n"

	if looksLikeHTML(input) {
		t.Fatalf("test input unexpectedly looks like HTML; the regression test must exercise the mixed-content path")
	}

	got := sanitizeOCRText(input)

	if strings.Contains(got, "<table") {
		t.Fatalf("expected inline HTML table to be converted, got:\n%s", got)
	}
	if !strings.Contains(got, "# Báo cáo") || !strings.Contains(got, "quyền giải thích cuối cùng thuộc về phòng Tài chính của đơn vị.") {
		t.Fatalf("expected surrounding markdown to be preserved, got:\n%s", got)
	}
	for _, want := range []string{"Chỉ tiêu", "Giá trị", "Doanh thu", "10 tỷ", "Lợi nhuận", "2,3 tỷ"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected converted table to retain %q, got:\n%s", want, got)
		}
	}
	if !regexp.MustCompile(`\|[-:]{3,}\|`).MatchString(got) {
		t.Fatalf("expected a GFM table separator row, got:\n%s", got)
	}
}

func TestIsKnownEmptyReply(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"无文字内容", true},
		{"无法识别", true},
		{"no text", true},
		{"No Text", true},
		{"NO CONTENT", true},
		{"empty", true},
		{"Đây là nội dung bình thường", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isKnownEmptyReply(tt.input)
			if got != tt.want {
				t.Errorf("isKnownEmptyReply(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// A portrait photo sent to senocr-vi (2026-10-16): the model recited the OCR
// prompt's numbered list in a loop until max_tokens, 362 lines of 14
// distinct, 20 KB. Nothing of it is text from the image.
func TestSanitizeOCRTextDropsLoopingPromptEcho(t *testing.T) {
	block := "<system_prompt>\n1. Write a test sentence that shows the following:\n"
	for i := 2; i <= 10; i++ {
		block += fmt.Sprintf("%d. Write a test sentence that shows the right order of writing.\n", i)
	}
	block += "</system_prompt>\n"
	if got := sanitizeOCRText(strings.Repeat(block, 30)); got != "" {
		t.Fatalf("looping prompt echo must be dropped, got %d bytes: %.120q", len(got), got)
	}
	// A short echo with nothing else is dropped too; an echo around real text keeps the text.
	if got := sanitizeOCRText("<system_prompt>\n</system_prompt>"); got != "" {
		t.Fatalf("bare prompt echo kept: %q", got)
	}
	if got := sanitizeOCRText("<system_prompt>\nCông văn số 12/UBND-VP\n</system_prompt>"); got != "Công văn số 12/UBND-VP" {
		t.Fatalf("text inside an echo lost: %q", got)
	}
}

// A scanned PDF page sent with the long OCR prompt (2026-10-16, NQ
// 74/2018): "1. The text content." up to 566, cut off at "5" by
// max_tokens. The numbers made every line distinct and the stub hid the
// run from the tail collapse.
func TestSanitizeOCRTextDropsANumberedLoop(t *testing.T) {
	var loop strings.Builder
	for i := 1; i <= 566; i++ {
		fmt.Fprintf(&loop, "%d. The text content.\n", i)
	}
	loop.WriteString("5")
	if got := sanitizeOCRText(loop.String()); got != "" {
		t.Fatalf("numbered loop kept, %d bytes: %.120q", len(got), got)
	}
	page := "Điều 2\nGiao Chính phủ tổ chức thực hiện Nghị quyết này.\n" + loop.String()
	if got := sanitizeOCRText(page); got != "Điều 2\nGiao Chính phủ tổ chức thực hiện Nghị quyết này." {
		t.Fatalf("real text before the loop: %.200q", got)
	}
}

func TestIsDegenerateOCRKeepsRealDocuments(t *testing.T) {
	// A blank form, one block of the same field labels per family member,
	// plus dotted leaders: repeated, but not a loop.
	form := ""
	for i := 0; i < 5; i++ {
		form += "Họ và tên: ..........\nNgày sinh: ..........\nNơi sinh: ..........\nQuê quán: ..........\nDân tộc: ..........\nQuan hệ với chủ hộ: ..........\n............................................\n"
	}
	if docparser.IsDegenerateOCR(form) {
		t.Fatal("a blank form repeating its labels per person is not degenerate")
	}
	// A table whose rows differ.
	table := ""
	for i := 0; i < 30; i++ {
		table += fmt.Sprintf("| %d | Mặt hàng %d | %d.000 |\n", i, i, i*7)
	}
	if docparser.IsDegenerateOCR(table) {
		t.Fatal("a table of distinct rows is not degenerate")
	}
	// Fewer than 12 lines are never judged, however repetitive.
	if docparser.IsDegenerateOCR(strings.Repeat("KÍNH GỬI\n", 11)) {
		t.Fatal("short outputs are left to the thin-text gate")
	}
	if !docparser.IsDegenerateOCR(strings.Repeat("KÍNH GỬI\n", 12)) {
		t.Fatal("twelve copies of one line are a loop")
	}
}
