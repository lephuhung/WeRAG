package vietnamese_legal

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// documentNumberRe matches a full số hiệu: number / year / suffix, the
// suffix holding at least one letter ("53/2022/NĐ-CP", "01/2024/TT-BCA",
// "12/2024/QĐ-UBND", "104/2016/QH13"). A date ("15/11/2018") has no
// four-digit middle part and no letter, so it never matches.
var documentNumberRe = regexp.MustCompile(`\d{1,4}/(?:19|20)\d\d/[0-9]*[A-Za-zĐđ][A-Za-zĐđ0-9]*(?:[-–—‐][A-Za-zĐđ0-9]+)*`)

// FindDocumentNumbers returns the full số hiệu written in text, in order,
// each once (by CatalogNumberKey). A number glued to a longer one
// ("1153/2022/…" is not "53/2022/…") is taken whole.
func FindDocumentNumbers(text string) []string {
	text = norm.NFC.String(text)
	seen := map[string]bool{}
	var out []string
	for _, loc := range documentNumberRe.FindAllStringIndex(text, -1) {
		if loc[0] > 0 {
			if c := text[loc[0]-1]; c == '/' || (c >= '0' && c <= '9') {
				continue
			}
		}
		n := text[loc[0]:loc[1]]
		if k := CatalogNumberKey(n); !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	return out
}

// FindDocumentNumberIndex is FindDocumentNumbers for the first number only,
// with its byte offsets in the NFC form of text (which callers that look
// around the number must use too); -1, -1 when there is none.
func FindDocumentNumberIndex(text string) (string, int, int) {
	text = norm.NFC.String(text)
	for _, loc := range documentNumberRe.FindAllStringIndex(text, -1) {
		if loc[0] > 0 {
			if c := text[loc[0]-1]; c == '/' || (c >= '0' && c <= '9') {
				continue
			}
		}
		return text[loc[0]:loc[1]], loc[0], loc[1]
	}
	return "", -1, -1
}

// normativeTypeCodes are the codes a số hiệu of a normative legal document
// (văn bản quy phạm pháp luật, what vbpl.vn publishes) starts its suffix
// with, after CatalogNumberKey folding (Đ→D): NĐ-CP, TT-BCA, TTLT-…, QĐ-TTg,
// QĐ-UBND, NQ-HĐND, NQLT-…, CT-TTg, PL-UBTVQH…, L-CTN (lệnh), QH13 (luật),
// UBTVQH… and HP. An organization's own paperwork (TTr-, CV-, KH-, BC-,
// TB-, GM-, HD-…) never is.
var normativeTypeCodes = map[string]bool{
	"ND": true, "TT": true, "TTLT": true, "QD": true, "NQ": true, "NQLT": true,
	"CT": true, "PL": true, "L": true, "QH": true, "UBTVQH": true, "HP": true,
}

// IsNormativeDocumentNumber reports whether a full số hiệu names a
// normative legal document by its type code: "53/2022/NĐ-CP",
// "12/2024/QĐ-UBND", "104/2016/QH13" yes; "45/2025/TTr-CAT",
// "123/2025/CV-PV01" no.
func IsNormativeDocumentNumber(number string) bool {
	parts := strings.Split(CatalogNumberKey(number), "/")
	if len(parts) < 3 {
		return false
	}
	code := strings.Join(parts[2:], "/")
	if i := strings.IndexAny(code, "-/"); i >= 0 {
		code = code[:i]
	}
	// a session number follows the code without a dash: QH13, UBTVQH14
	code = strings.TrimRight(code, "0123456789")
	return normativeTypeCodes[code]
}

// administrativeNumberRe matches a số hiệu written without a year
// ("123/QĐ-UBND", "45/BTC-TCT"): an upper-case code with at least one
// hyphen, so "01/GT" or "41/VPQH" do not count.
var administrativeNumberRe = regexp.MustCompile(`\d{1,5}/[A-ZĐ][A-ZĐ0-9]*(?:[-–—‐][A-ZĐ0-9]+)+`)

// CitedNumberSpans returns the byte spans [start, end) in text, as written
// (not normalised), of the full số hiệu (FindDocumentNumbers' shape) and of
// year-less administrative numbers, in order and without overlap. A match
// right after "/" or a digit is part of a longer number and is skipped.
func CitedNumberSpans(text string) [][2]int {
	var spans [][2]int
	add := func(locs [][]int) {
		for _, loc := range locs {
			if loc[0] > 0 {
				if c := text[loc[0]-1]; c == '/' || (c >= '0' && c <= '9') {
					continue
				}
			}
			spans = append(spans, [2]int{loc[0], loc[1]})
		}
	}
	add(documentNumberRe.FindAllStringIndex(text, -1))
	full := len(spans)
	add(administrativeNumberRe.FindAllStringIndex(text, -1))
	// keep a year-less match only where no full number is
	out := append([][2]int(nil), spans[:full]...)
	for _, s := range spans[full:] {
		overlaps := false
		for _, f := range spans[:full] {
			if s[0] < f[1] && f[0] < s[1] {
				overlaps = true
				break
			}
		}
		if !overlaps {
			out = append(out, s)
		}
	}
	// in text order
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j][0] < out[j-1][0]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
