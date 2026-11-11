package answerdocx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
)

// timed fails when converting md takes longer than limit.
func timed(t *testing.T, name, md string, limit time.Duration) []docxwrite.Block {
	t.Helper()
	start := time.Now()
	blocks, err := MarkdownBlocksContext(context.Background(), md)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if d := time.Since(start); d > limit {
		t.Fatalf("%s (%d bytes) took %v, limit %v", name, len(md), d, limit)
	}
	return blocks
}

// Unmatched emphasis openers of mixed widths once made the closer search
// exponential (" *a **b" × 20 took 12 s).
func TestEmphasisIsLinear(t *testing.T) {
	timed(t, "mixed openers ×24", strings.Repeat(" *a **b", 24), 50*time.Millisecond)
	blocks := timed(t, "mixed openers ×1000", strings.Repeat(" *a **b", 1000), 200*time.Millisecond)
	// nothing closes: every delimiter stays as written
	if got := view(blocks); len(got) != 1 || got[0].text != strings.TrimSpace(strings.Repeat(" *a **b", 1000)) {
		t.Errorf("text changed: %.200v", got)
	}
	timed(t, "nested closers", strings.Repeat("*a ", 20000)+strings.Repeat("a* ", 20000), 500*time.Millisecond)
	timed(t, "underscores", strings.Repeat("_a __b ", 20000), 500*time.Millisecond)
}

func TestHostileInputsStayFast(t *testing.T) {
	const limit = 500 * time.Millisecond
	timed(t, "200 KB of [", strings.Repeat("[", 200<<10), limit)
	timed(t, "200 KB of [a](", strings.Repeat("[a](", 50<<10), limit)
	timed(t, "100 KB of \"*a \"", strings.Repeat("*a ", 100<<10/3), limit)
	timed(t, "50 KB of ![", strings.Repeat("![", 25<<10), limit)
	timed(t, "backtick runs of growing length", func() string {
		var b strings.Builder
		for n := 1; b.Len() < 200<<10; n++ {
			b.WriteString(strings.Repeat("`", n) + "a")
		}
		return b.String()
	}(), limit)
	timed(t, "100 KB of <", strings.Repeat("<a ", 100<<10/3), limit)
	timed(t, "blank lines inside a list item", "- a\n"+strings.Repeat("\n", 200<<10)+"  b", limit)
	timed(t, "table with many columns", strings.Repeat("|a", 20000)+"|\n"+strings.Repeat("|-", 20000)+"|\n"+strings.Repeat("|\n", 20000), limit)

	// short rows are padded to the header: the cells are capped, so the
	// file stays small
	wide := strings.Repeat("|a", 20000) + "|\n" + strings.Repeat("|-", 20000) + "|\n" + strings.Repeat("|\n", 20000)
	data, err := Build(context.Background(), Input{Content: wide})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 2<<20 {
		t.Errorf("wide table docx is %d bytes", len(data))
	}

	var nested strings.Builder
	for i := 0; i < 2000; i++ {
		nested.WriteString(strings.Repeat("  ", i) + "- mục\n")
	}
	timed(t, "2000 nested list levels", nested.String(), limit) // over the size cap: plain text
	timed(t, "2000 nested quotes", strings.Repeat(">", 2000)+" trích", limit)
}

func TestNestingIsCapped(t *testing.T) {
	var nested strings.Builder
	for i := 0; i < 300; i++ {
		nested.WriteString(strings.Repeat("  ", i) + "- mục\n")
	}
	blocks := timed(t, "300 nested list levels", nested.String(), 500*time.Millisecond)
	deepest := 0
	for _, p := range view(blocks) {
		if p.list != "" && int(p.list[1]-'0') > deepest {
			deepest = int(p.list[1] - '0')
		}
	}
	if deepest >= maxNesting {
		t.Errorf("list level %d, want below %d", deepest, maxNesting)
	}
	quotes := view(timed(t, "quotes", strings.Repeat(">", 50)+" trích", 100*time.Millisecond))
	if len(quotes) != 1 || !strings.HasSuffix(quotes[0].text, "trích") {
		t.Errorf("deep quote = %+v", quotes)
	}
}

func TestOversizedAnswerIsPlainText(t *testing.T) {
	md := "**đậm**\n\n" + strings.Repeat("x", MaxMarkdownBytes)
	got := view(timed(t, "oversized", md, 500*time.Millisecond))
	if len(got) != 2 || got[0].text != "**đậm**" {
		t.Errorf("oversized answer: %.200v", got)
	}
}

func TestBuildStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, Input{Content: strings.Repeat("*a* [b](c) ", 20000)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// only the conversion budget ran out: plain paragraphs, no error
	blocks, err := contentBlocks(context.Background(), strings.Repeat("**a** ", 50000), time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if got := view(blocks); len(got) != 1 || !strings.HasPrefix(got[0].text, "**a** **a**") {
		t.Fatalf("fallback = %.200v", got)
	}
}

func TestThinkAndEnvelopeStripping(t *testing.T) {
	cases := []struct{ name, md, want string }{
		{"leading closed", "<think>nháp</think>\nTrả lời", "Trả lời"},
		{"closed in the middle", "Mở đầu <think>nháp</think>kết", "Mở đầu kết"},
		{"unclosed keeps the text", "<think>Trả lời thật", "Trả lời thật"},
		{"inside a code span", "Dùng `<think>x</think>` để", "Dùng `<think>x</think>` để"},
		{"people envelope", "A<people_lookup>{}</people_lookup>B", "AB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := view(MarkdownBlocks(tc.md))
			if len(got) != 1 || got[0].text != tc.want {
				t.Errorf("got %+v, want %q", got, tc.want)
			}
		})
	}
	fenced := view(MarkdownBlocks("```\n<think>giữ</think>\n```\nsau <think>bỏ</think>"))
	if len(fenced) != 2 || fenced[0].text != "<think>giữ</think>" || fenced[1].text != "sau" {
		t.Errorf("fenced = %+v", fenced)
	}
}

func TestInlineTextDetails(t *testing.T) {
	cases := []struct{ name, md, want string }{
		{"entities in text, not in code", "A &amp; B &lt;C&gt; `&amp;`", "A & B <C> `&amp;`"},
		{"escaped angle brackets", `Gửi \<email\> nhé`, "Gửi <email> nhé"},
		{"prose in angle brackets", "a <b and c> d", "a <b and c> d"},
		{"known tags dropped", `<span class="x">y</span> <b>z</b> <sup>2</sup>`, "y **z** 2"},
		{"unknown tag kept", "x <custom> y", "x <custom> y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := view(MarkdownBlocks(tc.md))
			if len(got) != 1 || got[0].text != strings.ReplaceAll(tc.want, " **z** ", " z ") {
				t.Errorf("got %+v, want %q", got, tc.want)
			}
		})
	}
	// pipes split cells even inside backticks, as in the chat; \| does not
	rows := view(MarkdownBlocks("| a | b | c |\n|---|---|---|\n| `x|y` | `p\\|q` |"))
	if len(rows) != 2 || rows[1].text != "`x | y` | `p|q`" {
		t.Errorf("rows = %+v", rows)
	}
}
