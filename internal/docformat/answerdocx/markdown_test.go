package answerdocx

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
)

// para is a compact view of a paragraph for table tests.
type para struct {
	style  string
	text   string // runs joined; bold as **x**, italic as *x*, code as `x`, strike as ~~x~~
	list   string // "" | "b0" (bullet level 0) | "n1:3" (ordered level 1, start 3)
	indent int
}

func view(blocks []docxwrite.Block) []para {
	var out []para
	for _, b := range blocks {
		switch v := b.(type) {
		case docxwrite.Paragraph:
			out = append(out, viewPara(v))
		case docxwrite.Table:
			for _, row := range v.Rows {
				var cells []string
				for _, c := range row {
					var t []string
					for _, p := range c.Paragraphs {
						t = append(t, viewPara(p).text)
					}
					cells = append(cells, strings.Join(t, "/"))
				}
				out = append(out, para{style: "row", text: strings.Join(cells, " | ")})
			}
		}
	}
	return out
}

func viewPara(p docxwrite.Paragraph) para {
	var b strings.Builder
	for _, r := range p.Runs {
		t := r.Text
		if r.Code {
			t = "`" + t + "`"
		}
		if r.Strike {
			t = "~~" + t + "~~"
		}
		if r.Italic {
			t = "*" + t + "*"
		}
		if r.Bold {
			t = "**" + t + "**"
		}
		b.WriteString(t)
	}
	v := para{style: p.Style, text: b.String(), indent: p.IndentTwips}
	if p.List != nil {
		if p.List.Ordered {
			v.list = "n" + string(rune('0'+p.List.Level)) + ":" + itoa(p.List.Start)
		} else {
			v.list = "b" + string(rune('0'+p.List.Level))
		}
	}
	return v
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func TestMarkdownBlocks(t *testing.T) {
	const (
		h1    = docxwrite.StyleHeading1
		h2    = docxwrite.StyleHeading2
		h3    = docxwrite.StyleHeading3
		h4    = docxwrite.StyleHeading4
		quote = docxwrite.StyleQuote
		code  = docxwrite.StyleCodeBlock
		rule  = docxwrite.StyleRule
	)
	cases := []struct {
		name string
		md   string
		want []para
	}{
		{"headings", "# Thủ tục\n## Hồ sơ\n### Bước 1 ###\n#### Ghi chú\n###### Sâu",
			[]para{{style: h1, text: "Thủ tục"}, {style: h2, text: "Hồ sơ"}, {style: h3, text: "Bước 1"},
				{style: h4, text: "Ghi chú"}, {style: h4, text: "Sâu"}}},
		{"setext headings", "Tiêu đề lớn\n===\nTiêu đề nhỏ\n---\nđoạn",
			[]para{{style: h1, text: "Tiêu đề lớn"}, {style: h2, text: "Tiêu đề nhỏ"}, {text: "đoạn"}}},
		{"paragraphs keep single newlines as breaks", "Dòng một\nDòng hai  \nDòng ba\\\nDòng bốn\n\nĐoạn mới",
			[]para{{text: "Dòng một\nDòng hai\nDòng ba\nDòng bốn"}, {text: "Đoạn mới"}}},
		{"emphasis", "Theo **Điều 5** và *khoản 2*, __đậm__ _nghiêng_ ***cả hai*** ~~bỏ~~ `mã_số`",
			[]para{{text: "Theo **Điều 5** và *khoản 2*, **đậm** *nghiêng* ***cả hai*** ~~bỏ~~ `mã_số`"}}},
		{"nested emphasis", "*nghiêng **đậm** tiếp*", []para{{text: "*nghiêng ****đậm**** tiếp*"}}},
		{"literal stars and underscores", "2 * 3 * 4 và snake_case_name và 5*", []para{{text: "2 * 3 * 4 và snake_case_name và 5*"}}},
		{"unclosed emphasis stays literal", "**chưa đóng", []para{{text: "**chưa đóng"}}},
		{"escapes", `\*không nghiêng\* và \_x\_`, []para{{text: "*không nghiêng* và _x_"}}},
		{"links", "Xem [Cổng VBPL](https://vbpl.vn/a?b=1) và <https://chinhphu.vn> hoặc [https://x.vn](https://x.vn)",
			[]para{{text: "Xem Cổng VBPL (https://vbpl.vn/a?b=1) và https://chinhphu.vn hoặc https://x.vn"}}},
		{"image and wiki link", "![Sơ đồ quy trình](resource://a.png) [[legal/nd-30|Nghị định 30]]",
			[]para{{text: "[Hình: Sơ đồ quy trình] Nghị định 30"}}},
		{"html", "a<br>b <b>c</b> <sup>2</sup>", []para{{text: "a\nb c 2"}}},
		{"think and people envelopes", "<think>suy nghĩ</think>Trả lời<people_lookup>x</people_lookup>",
			[]para{{text: "Trả lời"}}},
		{"bullets", "- một\n- **hai**\n* ba",
			[]para{{text: "một", list: "b0"}, {text: "**hai**", list: "b0"}, {text: "ba", list: "b0"}}},
		{"numbered with start", "3. ba\n4. bốn", []para{{text: "ba", list: "n0:3"}, {text: "bốn", list: "n0:4"}}},
		{"nested lists", "1. Hồ sơ gồm:\n   - Đơn\n   - Bản sao\n     1. công chứng\n2. Nộp",
			[]para{{text: "Hồ sơ gồm:", list: "n0:1"}, {text: "Đơn", list: "b1"}, {text: "Bản sao", list: "b1"},
				{text: "công chứng", list: "n2:1"}, {text: "Nộp", list: "n0:2"}}},
		{"two-space nesting", "- cha\n  - con\n- cha 2", []para{{text: "cha", list: "b0"}, {text: "con", list: "b1"}, {text: "cha 2", list: "b0"}}},
		{"loose list with follow-up paragraph", "1. Bước một\n\n   Giải thích thêm.\n\n2. Bước hai",
			[]para{{text: "Bước một", list: "n0:1"}, {text: "Giải thích thêm.", indent: docxwrite.ListTextIndent(0)},
				{text: "Bước hai", list: "n0:2"}}},
		{"lazy continuation", "- dòng đầu\ntiếp nối", []para{{text: "dòng đầu\ntiếp nối", list: "b0"}}},
		{"list ends at paragraph", "- mục\n\nĐoạn sau", []para{{text: "mục", list: "b0"}, {text: "Đoạn sau"}}},
		{"year is not a list inside a paragraph", "Ban hành năm\n2024. Có hiệu lực", []para{{text: "Ban hành năm\n2024. Có hiệu lực"}}},
		{"table", "| Loại | Phí |\n|:---|---:|\n| Cấp mới | 50.000 đ |\n| Gia hạn \\| đổi | `0` |",
			[]para{{style: "row", text: "Loại | Phí"}, {style: "row", text: "Cấp mới | 50.000 đ"}, {style: "row", text: "Gia hạn | đổi | `0`"}}},
		{"table without outer pipes, short row", "A | B | C\n--- | --- | ---\n1 | 2\n\nsau",
			[]para{{style: "row", text: "A | B | C"}, {style: "row", text: "1 | 2 | "}, {text: "sau"}}},
		{"block quote", "> Trích **Điều 3**\n> dòng hai\n\nsau", []para{{style: quote, text: "Trích **Điều 3**\ndòng hai"}, {text: "sau"}}},
		{"nested quote", "> ngoài\n>\n> > trong", []para{{style: quote, text: "ngoài"}, {style: quote, text: "trong", indent: 568}}},
		{"fenced code", "```go\nfunc a() {\n    return\n}\n```\nsau", []para{{style: code, text: "func a() {\n    return\n}"}, {text: "sau"}}},
		{"unclosed fence runs to the end", "~~~\nx\ny", []para{{style: code, text: "x\ny"}}},
		{"thematic break", "trên\n\n---\n**Lịch sử sửa đổi**\n- NĐ 1", []para{{text: "trên"}, {style: rule}, {text: "**Lịch sử sửa đổi**"}, {text: "NĐ 1", list: "b0"}}},
		{"empty", "  \n\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := view(MarkdownBlocks(tc.md))
			if len(got) != len(tc.want) {
				t.Fatalf("got %d blocks %+v, want %d %+v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				w := tc.want[i]
				if w.style == "" {
					w.style = got[i].style // headings set it; plain paragraphs leave it ""
					if got[i].style != "" && got[i].style != "row" {
						t.Errorf("block %d style = %q, want plain", i, got[i].style)
					}
				}
				if got[i] != w {
					t.Errorf("block %d = %+v, want %+v", i, got[i], w)
				}
			}
		})
	}
}

func TestOrderedListsRestartPerList(t *testing.T) {
	blocks := MarkdownBlocks("1. a\n2. b\n\nĐoạn\n\n1. c")
	var keys []int
	for _, b := range blocks {
		if p, ok := b.(docxwrite.Paragraph); ok && p.List != nil {
			keys = append(keys, p.List.List)
		}
	}
	if len(keys) != 3 || keys[0] != keys[1] || keys[1] == keys[2] {
		t.Fatalf("list keys = %v, want the second list on its own key", keys)
	}
}
