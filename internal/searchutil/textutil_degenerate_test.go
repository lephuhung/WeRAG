package searchutil

import (
	"fmt"
	"strings"
	"testing"
)

func repeat(n int, format string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(fmt.Sprintf(format, i))
	}
	return b.String()
}

func TestCollapseDegenerateTail_NumberedListOnOneLine(t *testing.T) {
	junk := repeat(15, "%d. Use LaTeX to output the output text. ")
	input := "Điều 1. Nội dung thật của trang scan.\n" + junk
	got := CollapseDegenerateTail(input)
	if got != "Điều 1. Nội dung thật của trang scan." {
		t.Fatalf("expected degenerate tail removed, got:\n%s", got)
	}
}

func TestCollapseDegenerateTail_NumberedListLines(t *testing.T) {
	junk := repeat(10, "%d. Same repeated sentence here.\n")
	input := "Real heading\nReal paragraph text.\n" + junk
	got := CollapseDegenerateTail(input)
	if got != "Real heading\nReal paragraph text." {
		t.Fatalf("expected degenerate tail removed, got:\n%s", got)
	}
}

func TestCollapseDegenerateTail_EntireOutputDegenerate(t *testing.T) {
	input := repeat(20, "%d. Use LaTeX to output the output text. ")
	if got := CollapseDegenerateTail(input); got != "" {
		t.Fatalf("expected empty result, got: %q", got)
	}
}

func TestCollapseDegenerateTail_RepeatedLinesNoNumbering(t *testing.T) {
	input := "Real text\n" + strings.Repeat("Repeated junk line here.\n", 8)
	got := CollapseDegenerateTail(input)
	if got != "Real text" {
		t.Fatalf("expected degenerate tail removed, got:\n%s", got)
	}
}

func TestCollapseDegenerateTail_ShortRepeatsKept(t *testing.T) {
	// Legit-looking short repeats (checkboxes, signatures) under the
	// minimum segment length must not be truncated.
	input := "Form fields\n" + strings.Repeat("N/A\n", 8)
	if got := CollapseDegenerateTail(input); got != strings.TrimRight(input, "\n") && got != input {
		t.Fatalf("short repeats should be kept, got:\n%s", got)
	}
}

func TestCollapseDegenerateTail_LegitNumberedListKept(t *testing.T) {
	input := "Steps:\n1. Open the file.\n2. Read the contents.\n3. Close the file.\n4. Save the draft.\n5. Submit it."
	if got := CollapseDegenerateTail(input); got != input {
		t.Fatalf("distinct list items must not be truncated, got:\n%s", got)
	}
}

func TestCollapseDegenerateTail_BelowMinRunKept(t *testing.T) {
	input := "Tail " + repeat(4, "%d. Same repeated sentence here. ")
	if got := CollapseDegenerateTail(input); got != input {
		t.Fatalf("runs shorter than %d must be kept, got:\n%s", degenerateMinRun, got)
	}
}

func TestCollapseDegenerateTail_RealContentAfterRepeatsKept(t *testing.T) {
	// Repetition that does NOT reach the end of the text is not a
	// degeneration tail — nothing is cut.
	input := "Intro\n" + strings.Repeat("A repeated mid-text line.\n", 7) + "Final real paragraph."
	if got := CollapseDegenerateTail(input); got != input {
		t.Fatalf("mid-text repetition must be kept, got:\n%s", got)
	}
}

func TestCollapseDegenerateTail_BlankLineSeparatedItems(t *testing.T) {
	var b strings.Builder
	b.WriteString("Real text\n\n")
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&b, "%d. Same repeated sentence here.\n\n", i)
	}
	got := CollapseDegenerateTail(b.String())
	if got != "Real text" {
		t.Fatalf("expected degenerate tail removed, got:\n%s", got)
	}
}

func TestInlineImageText_CollapsesDegenerateOCR(t *testing.T) {
	content := "![page1](resource://page1)"
	raw := `[{"url":"resource://page1","ocr_text":"Điều 1. Thật ` +
		repeat(15, "%d. Use LaTeX to output the output text. ") + `"}]`
	got := InlineImageText(content, raw)
	if strings.Contains(got, "Use LaTeX") {
		t.Fatalf("degenerate OCR tail should be collapsed:\n%s", got)
	}
	if !strings.Contains(got, "Điều 1. Thật") {
		t.Fatalf("real OCR prefix should be kept:\n%s", got)
	}
}
