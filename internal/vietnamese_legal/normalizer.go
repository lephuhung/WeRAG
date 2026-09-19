// normalizer.go is the Go port of AIRAG's Vietnamese text normalization
// (deep_document_parser.py::_fix_scattered_vietnamese and the CÔNG BÁO
// furniture strip) plus the broken-Vietnamese-text-layer detector used to
// route corrupt-text PDFs to OCR.
//
// Background: PDF text extractors reconstruct text from per-glyph
// coordinates. Many Vietnamese PDFs — especially government legislation —
// encode each glyph with its own positioning operator, producing a space
// between every character ("L u ậ t" instead of "Luật"). Others ship a text
// layer that lost its tone marks ("BỘ"→"B", "Độc lập"→"Đc lâp").
package vietnamese_legal

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Lines that must never be rewritten (markdown structure).
var structureLineRe = regexp.MustCompile(
	`^\s*(?:` +
		`[#]{1,6}\s` + // markdown headings
		`|[|]` + // table rows
		`|[-*_]{3,}` + // horizontal rules
		`|` + "```" + // code fences
		`|!\[.*\]\(.*\)` + // images
		`|\[.*\]\(.*\)` + // links on their own line
		`|<!--.*-->` + // HTML comments
		`|>\s` + // blockquotes
		`)`,
)

// vnMarker matches Vietnamese-specific pre-composed characters. Covers
// ă â đ ê ô ơ ư plus all pre-composed Vietnamese in U+1E00–1EFF and the
// Latin Extended Additional block (ò ã ũ … in U+00C0–024F).
var vnMarker = regexp.MustCompile("[\\x{00C0}-\\x{024F}\\x{1E00}-\\x{1EFF}]")

// congBaoFurnitureRe detects the "CÔNG BÁO/Số ..." government gazette
// header/footer lines (including scattered-char forms "C Ô N G  B Á O").
// Anchored to END of line (after the number only a "/Ngày ..." tail may
// follow): a real BODY sentence like "Công báo/Số 1133 đã đăng toàn văn..."
// must NOT match — matching lines get DELETED, so a false positive is
// silent content loss.
var congBaoFurnitureRe = regexp.MustCompile(
	`(?i)^\d*\s*(?:C\s*Ô\s*N\s*G\s+B\s*Á\s*O\s*|CÔNG\s+BÁO\s*)[/\\]\s*` +
		`S\s*ố\s*[\d\s+.\-]*\d(?:\s*[/\\][^\n]*)?$`,
)

var headingPrefixRe = regexp.MustCompile(`^(\s*#{1,6}\s+)`)

var multiSpaceRe = regexp.MustCompile(`  +`)

// FixScatteredVietnamese repairs per-glyph spacing artefacts produced by
// PDF text extraction on Vietnamese documents.
//
// Processes each line independently so markdown structure is preserved.
// Only activates when the text looks Vietnamese (contains Vietnamese
// diacritics) to avoid mangling non-Vietnamese content.
//
// Algorithm (line-by-line):
//  1. NFC-normalize Unicode so decomposed diacritics become single codepoints.
//  2. Delete CÔNG BÁO furniture lines.
//  3. Detect "single-char + single-space" runs — consecutive letters
//     separated by exactly ONE space (≥50% single-char tokens).
//  4. Rejoin those runs into solid words.
func FixScatteredVietnamese(text string) string {
	if !vnMarker.MatchString(text) {
		return text
	}
	text = norm.NFC.String(text)

	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped := strings.TrimSpace(line)

		// Drop predictable government furniture (headers/footers).
		if congBaoFurnitureRe.MatchString(stripped) {
			continue
		}

		isHeading := headingPrefixRe.MatchString(line)
		if structureLineRe.MatchString(line) && !isHeading {
			result = append(result, line)
			continue
		}
		if stripped == "" {
			result = append(result, line)
			continue
		}

		// For headings: keep the "## " prefix, fix only the remainder.
		headingPrefix := ""
		textToFix := stripped
		if isHeading {
			m := headingPrefixRe.FindStringSubmatchIndex(line)
			headingPrefix = line[m[2]:m[3]]
			textToFix = line[m[1]:]
		}

		tokens := strings.Fields(textToFix)
		if len(tokens) < 3 {
			result = append(result, line)
			continue
		}
		singles := 0
		for _, t := range tokens {
			if utf8.RuneCountInString(t) == 1 {
				singles++
			}
		}
		if float64(singles)/float64(len(tokens)) < 0.5 {
			result = append(result, line)
			continue
		}

		// Split on runs of 2+ spaces (real word/segment boundaries), then
		// rejoin consecutive single-char tokens inside each segment.
		var fixedSegments []string
		for _, seg := range multiSpaceRe.Split(strings.TrimSpace(textToFix), -1) {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			words := strings.Fields(seg)
			var merged []string
			var buf []string
			bufKind := byte(0) // 'l' letters | 'd' digits | 0 unknown/punct
			flush := func() {
				if len(buf) > 0 {
					merged = append(merged, strings.Join(buf, ""))
					buf = buf[:0]
					bufKind = 0
				}
			}
			for _, w := range words {
				if utf8.RuneCountInString(w) == 1 {
					r, _ := utf8.DecodeRuneInString(w)
					var kind byte
					switch {
					case unicode.IsLetter(r):
						kind = 'l'
					case unicode.IsDigit(r):
						kind = 'd'
					}
					// Break the glue on a letter<->digit transition so a
					// uniformly spaced heading "Đ I Ề U 5 . P h ạ m v i"
					// becomes "ĐIỀU 5. Phạmvi" (still detectable) instead
					// of "ĐIỀU5.Phạmvi". Punctuation sticks to the buffer
					// ("5 ." → "5.").
					if len(buf) > 0 && kind != 0 && bufKind != 0 && kind != bufKind {
						flush()
					}
					buf = append(buf, w)
					if bufKind == 0 {
						bufKind = kind
					}
				} else {
					flush()
					merged = append(merged, w)
				}
			}
			flush()
			fixedSegments = append(fixedSegments, strings.Join(merged, " "))
		}
		fixedBody := strings.Join(fixedSegments, " ")

		if isHeading {
			result = append(result, headingPrefix+fixedBody)
		} else {
			// Preserve leading indentation.
			leading := len(line) - len(strings.TrimLeft(line, " \t"))
			result = append(result, line[:leading]+fixedBody)
		}
	}
	return strings.Join(result, "\n")
}

// vnBaseLetters are the Vietnamese-specific base letters that survive a
// corrupt text layer (the layer drops tone marks but keeps these).
var vnBaseLetters = map[rune]bool{
	'ă': true, 'â': true, 'đ': true, 'ê': true, 'ô': true, 'ơ': true, 'ư': true,
	'Ă': true, 'Â': true, 'Đ': true, 'Ê': true, 'Ô': true, 'Ơ': true, 'Ư': true,
}

// HasBrokenVNTextLayer reports whether extracted text looks like a corrupt
// Vietnamese text layer: Vietnamese base letters are present but the complex
// tone characters (U+1EA0–1EF9: ộ ệ ấ ợ …) are missing. Clean Vietnamese is
// dense with tone chars; English has neither marker and stays on the fast
// path. True → the document should be routed to OCR.
//
// sample is the extracted text of the first few pages. toneMinRatio is the
// minimum tone/letter ratio for a healthy layer (AIRAG
// HRAG_OCR_VN_TONE_MIN_RATIO, typically ~0.005–0.01).
func HasBrokenVNTextLayer(sample string, toneMinRatio float64) bool {
	letters, tone, base := 0, 0, 0
	for _, c := range sample {
		if unicode.IsLetter(c) {
			letters++
		}
		if c >= 'Ạ' && c <= 'ỹ' {
			tone++
		}
		if vnBaseLetters[c] {
			base++
		}
	}
	if letters < 200 {
		return false // too little selectable text (scanned handled elsewhere)
	}
	return base >= 5 && float64(tone)/float64(letters) < toneMinRatio
}

// PageMarker matches the "<!-- page N -->" markers emitted by the OCR
// pipeline and kept by downstream chunking/page-assignment code.
var PageMarker = regexp.MustCompile(`<!--\s*page\s+(\d+)\s*-->`)
