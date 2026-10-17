package docparser

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/searchutil"
)

// promptEchoLine matches a line that is the OCR prompt's own markup coming
// back (<system_prompt>, </instructions>): an OCR-only model given an image
// without text (a portrait, a photo) tends to recite the prompt instead of
// answering "No text content".
var promptEchoLine = regexp.MustCompile(`(?m)^\s*</?(system_prompt|instructions)>\s*$\n?`)

// truncatedMarker is a bare list number on the last line: the model ran
// out of max_tokens right after the "\n" of a looping list ("565. X\n566.
// X\n5"), and the stub keeps CollapseDegenerateTail from seeing the run. A
// lone page number at the foot of a page goes too, which costs nothing.
var truncatedMarker = regexp.MustCompile(`\n\s*\d{1,4}[.)]?\s*$`)

// listMarker is the number in front of a list item: a model looping over
// "1. X", "2. X", … writes a new line every time, the same item every time.
var listMarker = regexp.MustCompile(`^\d{1,4}[.)]\s*`)

// Degenerate OCR output: at least this many text lines of which at most a
// quarter are distinct. CollapseDegenerateTail only cuts a run of one
// repeated segment; a model looping over a cycle of lines (1. … 10. …, over
// and over until max_tokens) passes it and would reach the user as a
// 20 KB "document" read from a photo.
const (
	degenerateOCRMinLines      = 12
	degenerateOCRDistinctRatio = 4 // distinct*ratio <= total
	degenerateOCRMinRepeat     = 8 // the most repeated line occurs this often
)

// CleanOCRLoops removes what a looping OCR model adds to its answer: a
// repeated tail, a truncated list stub and echoed prompt markup. It returns
// "" when what is left is still a loop (no real text in the image). Both
// the PaddleOCR-VL reader and the VLM OCR path run it.
func CleanOCRLoops(text string) string {
	text = searchutil.CollapseDegenerateTail(truncatedMarker.ReplaceAllString(text, ""))
	text = strings.TrimSpace(promptEchoLine.ReplaceAllString(text, ""))
	if IsDegenerateOCR(text) {
		return ""
	}
	return text
}

// IsDegenerateOCR reports whether text is a looping, content-free OCR
// answer. List numbers do not make lines distinct ("1. X" … "566. X").
// Lines of only punctuation (dotted leaders of a form) are ignored,
// and a blank form that repeats its field labels a few times (one block per
// person) stays below the repeat floor, so neither is mistaken for a loop.
func IsDegenerateOCR(text string) bool {
	total, maxRepeat := 0, 0
	seen := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		line = listMarker.ReplaceAllString(strings.TrimSpace(line), "")
		if line == "" || !strings.ContainsFunc(line, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			continue
		}
		total++
		seen[line]++
		if seen[line] > maxRepeat {
			maxRepeat = seen[line]
		}
	}
	return total >= degenerateOCRMinLines && len(seen)*degenerateOCRDistinctRatio <= total &&
		maxRepeat >= degenerateOCRMinRepeat
}
