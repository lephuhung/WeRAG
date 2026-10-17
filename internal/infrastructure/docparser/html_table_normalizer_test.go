package docparser

import (
	"strings"
	"testing"
)

func TestNormalizeHTMLTables_ConvertsStyledTableToMarkdown(t *testing.T) {
	// Mirrors PaddleOCR-VL output: a data table where every cell carries a
	// text-align style that wastes tokens.
	input := `# Báo cáo

<table><tr><td style="text-align:center;">Chỉ tiêu</td><td style="text-align:center;">Giá trị</td></tr>` +
		`<tr><td style="text-align:center;">Doanh thu</td><td style="text-align:right;">10 tỷ</td></tr>` +
		`<tr><td style="text-align:center;">Lợi nhuận</td><td style="text-align:right;">2,3 tỷ</td></tr></table>

Kết thúc.`

	got := NormalizeHTMLTables(input)

	if strings.Contains(got, "<table") {
		t.Fatalf("expected HTML table to be converted away, got:\n%s", got)
	}
	if strings.Contains(got, "text-align") {
		t.Fatalf("expected style attributes removed, got:\n%s", got)
	}
	if !markdownTableSeparatorPattern.MatchString(got) {
		t.Fatalf("expected a Markdown table separator row, got:\n%s", got)
	}
	for _, want := range []string{"Chỉ tiêu", "Giá trị", "Doanh thu", "10 tỷ", "Lợi nhuận", "2,3 tỷ"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected converted table to retain %q, got:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "# Báo cáo") || !strings.Contains(got, "Kết thúc.") {
		t.Fatalf("expected surrounding markdown to be preserved, got:\n%s", got)
	}
}

func TestNormalizeHTMLTables_NoTableUnchanged(t *testing.T) {
	input := "# Tiêu đề\n\nĐoạn thường, không có bảng.\n\n| a | b |\n| --- | --- |\n| 1 | 2 |"
	if got := NormalizeHTMLTables(input); got != input {
		t.Fatalf("expected content without HTML tables to be unchanged, got:\n%s", got)
	}
}

func TestNormalizeHTMLTables_SpanValueOneIsConvertible(t *testing.T) {
	// rowspan=1 / colspan=1 merge nothing, so the table must still become GFM.
	// Covers quoted, unquoted and space-padded attribute forms.
	input := `<table><tr>` +
		`<td rowspan="1" colspan="1" style="text-align:center;">Chỉ tiêu</td>` +
		`<td rowspan=1 colspan=1>Giá trị</td></tr>` +
		`<tr><td rowspan="1" colspan='1'>Doanh thu</td>` +
		`<td rowspan = 1 colspan = 1>10 tỷ</td></tr></table>`

	got := NormalizeHTMLTables(input)

	if strings.Contains(got, "<table") {
		t.Fatalf("expected span=1 table to be converted away, got:\n%s", got)
	}
	if !markdownTableSeparatorPattern.MatchString(got) {
		t.Fatalf("expected a Markdown table separator row, got:\n%s", got)
	}
	for _, want := range []string{"Chỉ tiêu", "Giá trị", "Doanh thu", "10 tỷ"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected converted table to retain %q, got:\n%s", want, got)
		}
	}
}

func TestNormalizeHTMLTables_RealSpanKeepsHTMLWithRowNewlines(t *testing.T) {
	// Genuine merges (colspan>1 / rowspan>1) cannot be GFM, so the table stays
	// HTML — but each row must start on its own line so the chunker can split it.
	input := `<table><tr><td colspan="6" style="text-align:center;">Tổng cộng</td></tr>` +
		`<tr><td rowspan="3">A</td><td>1</td></tr>` +
		`<tr><td rowspan = 2>B</td><td>2</td></tr></table>`

	got := NormalizeHTMLTables(input)

	if !strings.Contains(got, "<table") {
		t.Fatalf("expected real span table to remain HTML, got:\n%s", got)
	}
	if !strings.Contains(got, `colspan="6"`) || !strings.Contains(got, `rowspan="3"`) {
		t.Fatalf("expected structural span attrs preserved, got:\n%s", got)
	}
	if !strings.Contains(got, "\n<tr") {
		t.Fatalf("expected each <tr> to start on a new line, got:\n%q", got)
	}
	if !strings.Contains(got, "\n\n<table") {
		t.Fatalf("expected block to be padded at the head with blank lines, got:\n%q", got)
	}
	if !strings.HasSuffix(got, "</table>\n\n") {
		t.Fatalf("expected block to be padded at the tail with blank lines, got:\n%q", got)
	}
	if n := strings.Count(got, "\n<tr"); n != 3 {
		t.Fatalf("expected 3 rows each preceded by a newline, got %d in:\n%q", n, got)
	}
}

func assertHTMLRowsSplittable(t *testing.T, got string) {
	t.Helper()
	if !strings.Contains(got, "<tr") {
		t.Fatalf("expected remaining HTML table rows, got:\n%s", got)
	}
	for i := 0; ; {
		idx := strings.Index(got[i:], "<tr")
		if idx < 0 {
			return
		}
		abs := i + idx
		if abs == 0 || got[abs-1] != '\n' {
			t.Fatalf("remaining <tr> at offset %d is not preceded by a newline:\n%s", abs, got)
		}
		i = abs + 1
	}
}

func TestNormalizeHTMLTables_StripsAttrsOnSpanTables(t *testing.T) {
	// rowspan/colspan cannot be expressed in Markdown, so the table stays HTML
	// but its presentational attributes are stripped and rows become splittable.
	input := `<table><tr>` +
		`<td colspan="2" style="text-align:center;" class="hdr">Tổng cộng</td></tr>` +
		`<tr><td style="text-align:left;">A</td><td width="80">B</td></tr></table>`

	got := NormalizeHTMLTables(input)

	if !strings.Contains(got, "<table") {
		t.Fatalf("expected span table to remain HTML, got:\n%s", got)
	}
	if !strings.Contains(got, `colspan="2"`) {
		t.Fatalf("expected colspan to be preserved, got:\n%s", got)
	}
	for _, banned := range []string{"text-align", "class=", "width="} {
		if strings.Contains(got, banned) {
			t.Fatalf("expected %q to be stripped, got:\n%s", banned, got)
		}
	}
	assertHTMLRowsSplittable(t, got)
}

func TestNormalizeHTMLTables_BRCellsStaySplittableHTML(t *testing.T) {
	// <br> inside a cell makes the table plugin give up (it emits a newline).
	// The HTML fallback must still put each <tr> on its own line.
	input := `<table><tr><td>Chỉ tiêu<br/>Tên</td><td>Giá trị<br/>Đơn vị</td></tr>` +
		`<tr><td>Độ bền kéo<br/>MPa</td><td>Đạt<br/>Loại A</td></tr></table>`

	got := NormalizeHTMLTables(input)

	if !strings.Contains(got, "<table") {
		t.Fatalf("expected <br> table to remain HTML rather than flattened text, got:\n%s", got)
	}
	assertHTMLRowsSplittable(t, got)
	for _, want := range []string{"Chỉ tiêu", "Tên", "Độ bền kéo", "Đạt"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected fallback HTML to retain %q, got:\n%s", want, got)
		}
	}
}

func TestNormalizeHTMLTables_ListInCellFallsBackToSplittableHTML(t *testing.T) {
	input := `<table><tr><td><ul><li>a</li><li>b</li></ul></td><td>x</td></tr>` +
		`<tr><td>c</td><td>d</td></tr></table>`

	got := NormalizeHTMLTables(input)

	if !strings.Contains(got, "<table") {
		t.Fatalf("expected list-in-cell table to remain HTML, got:\n%s", got)
	}
	assertHTMLRowsSplittable(t, got)
}

func TestNormalizeHTMLTables_SingleColumnConvertsToGFM(t *testing.T) {
	input := `<table><tr><td>Mục lục</td></tr><tr><td>Chương I</td></tr><tr><td>Chương II</td></tr></table>`

	got := NormalizeHTMLTables(input)

	if strings.Contains(got, "<table") {
		t.Fatalf("expected single-column table to convert to GFM, got:\n%s", got)
	}
	if !markdownTableSeparatorPattern.MatchString(got) {
		t.Fatalf("expected a Markdown table separator row, got:\n%s", got)
	}
	for _, want := range []string{"Mục lục", "Chương I", "Chương II"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected converted table to retain %q, got:\n%s", want, got)
		}
	}
}

func TestNormalizeHTMLTables_ImgInSingleColumnBecomesGFM(t *testing.T) {
	input := `<table><tr><td><img src="local://images/profile.png" alt="profile"/></td></tr></table>`

	got := NormalizeHTMLTables(input)

	if strings.Contains(got, "<table") {
		t.Fatalf("expected single-column image table to convert to GFM, got:\n%s", got)
	}
	if !strings.Contains(got, "![profile](local://images/profile.png)") {
		t.Fatalf("expected markdown image to be preserved, got:\n%s", got)
	}
}

func TestNormalizeHTMLTables_MarkdownImageInCellNotEscaped(t *testing.T) {
	input := `<table><tr><td>![cover](local://images/cover.png)</td><td>Ghi chú</td></tr></table>`

	got := NormalizeHTMLTables(input)

	if strings.Contains(got, `!\[`) {
		t.Fatalf("markdown image syntax escaped:\n%s", got)
	}
	if !strings.Contains(got, "![cover](local://images/cover.png)") {
		t.Fatalf("expected markdown image to survive conversion, got:\n%s", got)
	}
}

func TestNormalizeHTMLTables_CodeFenceTableLeftAlone(t *testing.T) {
	input := "Ví dụ:\n\n```html\n<table><tr><td>a</td><td>b</td></tr></table>\n```\n"

	got := NormalizeHTMLTables(input)

	if got != input {
		t.Fatalf("expected fenced HTML table to be unchanged, got:\n%s", got)
	}
}

func TestNormalizeHTMLTables_SpanTableIdempotent(t *testing.T) {
	input := `<table><tr><td colspan="6">Tổng hợp</td></tr><tr><td colspan="6">Ghi chú</td></tr></table>`
	once := NormalizeHTMLTables(input)
	twice := NormalizeHTMLTables(once)
	if once != twice {
		t.Fatalf("NormalizeHTMLTables is not idempotent for span tables:\n1=%q\n2=%q", once, twice)
	}
}
