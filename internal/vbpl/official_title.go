package vbpl

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// officialTitleMinLetters is how many letters a trích yếu needs to count:
// "Chính phủ ban hành Nghị định." names nothing. A law's name is short
// ("Luật Đầu tư"), so Luật and Bộ luật need officialLawNameMinLetters.
const (
	officialTitleMinLetters   = 10
	officialLawNameMinLetters = 4
)

var (
	officialParaRe = regexp.MustCompile(`(?is)<p\b([^>]*)>(.*?)</p>`)
	// officialBodyClassRe: the body starts here; the enacting clause comes
	// before it.
	officialBodyClassRe = regexp.MustCompile(`(?i)class\s*=\s*["'][^"']*\bprov-(?:part|chapter|section|article)\b`)
	officialSupRe       = regexp.MustCompile(`(?is)<sup\b.*?</sup>`)
	// officialTableRe: the header table (cơ quan, số, ngày) and the Nơi nhận
	// / signature tables hold no enacting clause
	officialTableRe   = regexp.MustCompile(`(?is)<table\b.*?</table>`)
	officialTagRe     = regexp.MustCompile(`(?s)<[^>]*>`)
	officialArticleRe = regexp.MustCompile(`^(?i)Điều\s+\d`)
	// officialToneFold: old and new tone placement ("hoá" / "hóa") compare equal
	officialToneFold = strings.NewReplacer(
		"oá", "óa", "oà", "òa", "oả", "ỏa", "oã", "õa", "oạ", "ọa",
		"oé", "óe", "oè", "òe", "oẻ", "ỏe", "oẽ", "õe", "oẹ", "ọe",
		"uý", "úy", "uỳ", "ùy", "uỷ", "ủy", "uỹ", "ũy", "uỵ", "ụy")
)

// OfficialTitle is the catalogue title of a pushed document written the
// way vbpl.vn writes its titles, built from the document's enacting clause
// ("Chính phủ ban hành Nghị định sửa đổi, bổ sung …."), whose trích yếu is
// already in sentence case with its proper names capitalised:
//
//	Nghị định số 15/2026/NĐ-CP Sửa đổi, bổ sung một số điều …
//	Luật Đất đai số 31/2024/QH15  (Luật and Bộ luật: name, then số hiệu)
//
// The clause counts only when its trích yếu is the document's ALL-CAPS
// heading word for word ("SỬA ĐỔI, BỔ SUNG MỘT SỐ ĐIỀU …"), so a sentence of
// the body or a căn cứ line that happens to say "ban hành Nghị định" is
// never taken. "" when no clause before the first Điều agrees with the
// heading (a Quyết định that ends its preamble with "QUYẾT ĐỊNH:"); the
// caller keeps the title it had.
func OfficialTitle(content []byte, docType, number string) string {
	docType = strings.Join(strings.Fields(docType), " ")
	if docType == "" || len(content) == 0 {
		return ""
	}
	clauseRe, err := regexp.Compile(`\sban hành\s+(?i:(` + regexp.QuoteMeta(docType) + `))(?:\s+(.*))?$`)
	if err != nil {
		return ""
	}
	var headings []string // ALL-CAPS paragraphs so far, folded
	content = officialTableRe.ReplaceAll(content, nil)
	for _, m := range officialParaRe.FindAllSubmatch(content, -1) {
		if officialBodyClassRe.Match(m[1]) {
			return ""
		}
		raw := officialTagRe.ReplaceAllString(officialSupRe.ReplaceAllString(string(m[2]), " "), " ")
		text := strings.Join(strings.Fields(html.UnescapeString(raw)), " ")
		if officialArticleRe.MatchString(text) {
			return ""
		}
		if officialAllCaps(text) {
			headings = append(headings, officialFold(text))
			continue
		}
		cm := clauseRe.FindStringSubmatch(" " + text)
		if cm == nil {
			continue
		}
		rest := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(cm[2]), ".;:"))
		if officialLetters(rest) < officialMinLetters(docType) || !officialIsHeading(headings, officialFold(rest)) {
			continue
		}
		typ := cm[1]
		if officialAllCaps(typ) { // "ban hành NGHỊ ĐỊNH …": the metadata's spelling
			typ = docType
		}
		return officialCompose(typ, number, rest)
	}
	return ""
}

// officialIsHeading: want is one heading paragraph or a run of consecutive
// ones (a heading broken over lines: "LUẬT" is its own line, so a law's
// name is matched by the line after it).
func officialIsHeading(headings []string, want string) bool {
	if want == "" {
		return false
	}
	for i := range headings {
		joined := ""
		for _, h := range headings[i:] {
			joined += h
			if joined == want {
				return true
			}
			if len(joined) >= len(want) {
				break
			}
		}
	}
	return false
}

// officialFold keeps the lower-cased letters and digits, tone placement
// folded: the heading and the clause differ in case, spacing and commas.
func officialFold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return officialToneFold.Replace(b.String())
}

// officialAllCaps: the paragraph has letters and none of them is lower case.
func officialAllCaps(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters > 0
}

func officialCompose(docType, number, rest string) string {
	number = strings.TrimSpace(number)
	docType = officialUpperFirst(docType)
	if officialIsLaw(docType) {
		// "Luật Đất đai số 31/2024/QH15": the clause's own casing of the name
		if number == "" {
			return docType + " " + rest
		}
		return docType + " " + rest + " số " + number
	}
	rest = officialUpperFirst(rest)
	if number == "" {
		return docType + " " + rest
	}
	return docType + " số " + number + " " + rest
}

func officialUpperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

func officialIsLaw(docType string) bool {
	switch strings.ToLower(docType) {
	case "luật", "bộ luật":
		return true
	}
	return false
}

func officialMinLetters(docType string) int {
	if officialIsLaw(docType) {
		return officialLawNameMinLetters
	}
	return officialTitleMinLetters
}

func officialLetters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}
