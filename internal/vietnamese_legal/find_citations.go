package vietnamese_legal

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// FindCitations is the one recogniser of documents cited in Vietnamese
// text: full số hiệu ("53/2022/NĐ-CP", also written "68-2026-NĐ-CP",
// "01/2020/TT- BNNPTNT" or with a look-alike "Ð"), year-less numbers
// ("1461/QĐ-TTg"), Party numbers ("36-NQ/TW"), technical standards
// ("QCVN 06:2022/BXD"), partial references ("Nghị định 68", "NĐ 68/2026",
// "Luật 59/2024") and laws named without a number ("Luật Đấu thầu").
// Dates, money, quantities ("quyết định 3 năm tù"), "khoản 2" or "Điều 5"
// never match. Linear in the text: it runs on whole documents.

// CitationKind classifies a Citation.
type CitationKind string

const (
	// CitationFull is number / year / code: "53/2022/NĐ-CP", "104/2016/QH13".
	CitationFull CitationKind = "full"
	// CitationAdmin is a year-less number with a hyphenated code:
	// "1461/QĐ-TTg", "45/BTC-TCT".
	CitationAdmin CitationKind = "admin"
	// CitationParty is a type-first number: "36-NQ/TW", "21-LCT/HĐNN8".
	CitationParty CitationKind = "party"
	// CitationStandard is a technical standard or regulation:
	// "QCVN 06:2022/BXD", "QTKĐ 12-2017/BCT".
	CitationStandard CitationKind = "standard"
	// CitationPartial is a type with a bare number, maybe a year: "Nghị
	// định 68", "NĐ 68/2026", "Luật 59/2024". It has no catalogue key.
	CitationPartial CitationKind = "partial"
	// CitationName is a law named without a number: "Luật Đấu thầu".
	CitationName CitationKind = "name"
)

// Citation is one document reference found by FindCitations.
type Citation struct {
	// Start and End are rune offsets of Raw in the text as given (also
	// when it is not NFC), the unit chunk offsets use.
	Start, End int
	Raw        string // as written: "NĐ 68/2026", "01/2020/TT- BNNPTNT"
	Kind       CitationKind
	// Number is the normalised display số hiệu: "68/2026/NĐ-CP",
	// "QCVN 06:2022/BXD"; for a partial reference "68/2026" or "68";
	// empty for a name.
	Number string
	// Key is CatalogNumberKey(Number) for the kinds a catalogue row can be
	// looked up by (full, admin, party, standard); empty otherwise.
	Key string
	// DocType is the display type ("Nghị định", "Luật") from the words
	// before the number or, failing that, from the code; may be empty.
	DocType string
	No      string // "68" (full and partial)
	Year    string // "2026" when written
	// Name is the title a law is named by: the whole reference for
	// CitationName ("Luật Đấu thầu"), or the words before "số" of a
	// numbered one ("Luật Bình đẳng giới" in "Luật Bình đẳng giới số
	// 73/2006/QH11"). It may run past the name ("Luật Đấu thầu có hiệu
	// lực"); the catalogue lookup tries its prefixes.
	Name string
	// Role is empty for a plain citation. CitationRoleAmendTarget marks
	// the document a "sửa đổi, bổ sung … của" phrase amends; within the
	// title of an amending law ("Luật sửa đổi, bổ sung một số điều của
	// Luật Đất đai") it is CitationRoleInAmendingTitle: the words name the
	// amending law, never Luật Đất đai itself.
	Role string
}

// Citation roles.
const (
	CitationRoleAmendTarget     = "amend_target"
	CitationRoleInAmendingTitle = "in_amending_title"
)

// citationFold maps look-alike runes onto the ones số hiệu are written
// with: Eth "Ð" (U+00D0) for "Đ", Cyrillic capitals from OCR or bad
// fonts, dash variants and the no-break space.
var citationFold = map[rune]rune{
	'Ð': 'Đ', 'ð': 'đ',
	'А': 'A', 'В': 'B', 'С': 'C', 'Е': 'E', 'Н': 'H', 'К': 'K', 'М': 'M',
	'О': 'O', 'Р': 'P', 'Т': 'T', 'Х': 'X',
	'–': '-', '—': '-', '‐': '-', '‑': '-', '−': '-',
	'\u00a0': ' ',
}

const (
	citeSp = `[ \t]*`
	// citeEnd: a number never runs into a following letter or digit.
	citeEnd = `(?:[^\p{L}\p{N}&]|$)`
	// citeCode is the code after the year: "NĐ-CP", "nđ-cp", "QĐ-TTg",
	// "TTLT-BTP-BCA-BTC", "QH13". Stray spaces around a dash are read only
	// right after a short type code ("TT- BNNPTNT", "NĐ - CP"), so in
	// "15/2023/QĐ-UBND - UBND tỉnh" the code ends at "UBND". A tail after a
	// second slash is kept: "44/2007/TTLT/BTC-BKH&CN".
	citeCode = `(?:[A-ZĐ]{1,5}` + citeSp + `-` + citeSp + `[A-ZĐ][A-ZĐ0-9&]*(?:-[A-Za-zĐđ0-9&]+)*|[A-Za-zĐđ][A-Za-zĐđ0-9&]*(?:-[A-Za-zĐđ0-9&]+)*)` +
		`(?:/[A-ZĐ][A-ZĐ0-9&]*(?:-[A-ZĐ0-9&]+)*)?`
	// citeYear also takes OCR's "2O22"; FindCitations checks it reads 19xx/20xx.
	citeYear = `([12][0-9O]{3})`
	// citeTypeWords are the type words a partial reference starts with.
	citeTypeWords = `(?:(?i:(thông tư liên tịch|nghị quyết liên tịch|nghị định|nghị quyết|quyết định|thông tư|chỉ thị|pháp lệnh|bộ luật|luật|hiến pháp))|(NĐ|TTLT|TT|QĐ|NQ))`
)

var (
	// "53/2022/NĐ-CP", "66.17/2026/NQ-CP", "01 / 2020 / TT-BNNPTNT"
	citeFullRe = regexp.MustCompile(`(\d{1,4}(?:\.\d{1,3})?[a-z]?)` + citeSp + `/` + citeSp + citeYear + citeSp + `/` + citeSp + `(` + citeCode + `)` + citeEnd)
	// "68-2026-NĐ-CP"
	citeDashRe = regexp.MustCompile(`(\d{1,4})-((?:19|20)\d\d)-([A-ZĐ][A-Za-zĐđ0-9&]*(?:-[A-Za-zĐđ0-9&]+)*)` + citeEnd)
	// "53/2022 NĐ-CP": the code after a space must be TYPE-ISSUER
	citeSpacedRe = regexp.MustCompile(`(\d{1,4})` + citeSp + `/` + citeSp + `((?:19|20)\d\d)[ \t]+([A-ZĐ]{1,5}-[A-ZĐ][A-Za-zĐđ0-9&]*(?:-[A-Za-zĐđ0-9&]+)*)` + citeEnd)
	// "1461/QĐ-TTg", "45/BTC-TCT": at least one hyphen, so "01/GLP" is not
	citeAdminRe = regexp.MustCompile(`(\d{1,5})` + citeSp + `/` + citeSp +
		`((?:[A-ZĐ]{1,5}` + citeSp + `-` + citeSp + `[A-ZĐ][A-ZĐ0-9&]*(?:-[A-Za-zĐđ0-9&]+)*|[A-ZĐ][A-Za-zĐđ0-9]*(?:-[A-Za-zĐđ0-9&]+)+))` + citeEnd)
	// "36-NQ/TW", "21-LCT/HĐNN8"
	citePartyRe = regexp.MustCompile(`(\d{1,4})` + citeSp + `-` + citeSp + `([A-ZĐ]{1,6})` + citeSp + `/` + citeSp + `([A-ZĐ][A-ZĐ0-9]*(?:-[A-ZĐ0-9]+)*)` + citeEnd)
	// "QCVN 06:2022/BXD", "QCVN 01-1:2018/BYT", "TCVN 5574:2018", "QTKĐ 12-2017/BCT"
	citeStandardRe = regexp.MustCompile(`(QCVN|TCVN|QCĐP|QTKĐ)` + citeSp + `(\d{1,4}[A-Za-z]?(?:[-.]\d{1,4})*?)` + citeSp + `([:-])` + citeSp + `((?:19|20)\d\d)(?:` + citeSp + `/` + citeSp + `([A-ZĐ][A-ZĐ0-9]*(?:-[A-ZĐ0-9]+)*))?` + citeEnd)
	// "Nghị định 68", "Thông tư số 01", "NĐ 68/2026", "Luật 59/2024",
	// "Nghị quyết 12 năm 2026"
	citePartialRe = regexp.MustCompile(citeTypeWords + citeSp + `((?i:số)` + citeSp + `:?` + citeSp + `)?(\d{1,4}(?:\.\d{1,3})?)(?:` +
		citeSp + `/` + citeSp + `((?:19|20)\d\d)|[ \t]+(?i:năm)[ \t]+((?:19|20)\d\d))?`)
	// a law named by title: "Luật Đấu thầu", "Bộ luật Tố tụng hình sự"
	citeNameRe = regexp.MustCompile(`(Bộ luật|BỘ LUẬT|Luật|LUẬT|Pháp lệnh|PHÁP LỆNH)[ \t]+\p{Lu}`)
	// the type written before a number: "Nghị định số ", "NĐ "
	citeTypeBeforeRe = regexp.MustCompile(citeTypeWords + citeSp + `(?:(?i:số)` + citeSp + `:?` + citeSp + `)?$`)
	citeSoAfterRe    = regexp.MustCompile(`^[ \t]+(?i:số)` + citeSp + `:?` + citeSp)
	// "sửa đổi, bổ sung một số điều của " right before a name; with a type
	// word before "sửa đổi" the name sits in an amending law's title
	citeAmendBeforeRe = regexp.MustCompile(`(?i)(?:(luật|bộ luật|pháp lệnh|nghị định|nghị quyết|thông tư)[ \t]+)?(?:sửa đổi|bổ sung|sửa đổi, bổ sung)(?:[ \t]+một số điều)?(?:[ \t]+(?:của|tại))?[ \t]+$`)
)

// citeTypeNames maps a lower-cased type word or abbreviation to its
// display name.
var citeTypeNames = map[string]string{
	"thông tư liên tịch": "Thông tư liên tịch", "nghị quyết liên tịch": "Nghị quyết liên tịch",
	"nghị định": "Nghị định", "nghị quyết": "Nghị quyết", "quyết định": "Quyết định",
	"thông tư": "Thông tư", "chỉ thị": "Chỉ thị", "pháp lệnh": "Pháp lệnh",
	"bộ luật": "Bộ luật", "luật": "Luật", "hiến pháp": "Hiến pháp",
	"nđ": "Nghị định", "ttlt": "Thông tư liên tịch", "tt": "Thông tư", "qđ": "Quyết định", "nq": "Nghị quyết",
}

// citeSpan is a citation while FindCitations works on the folded text.
type citeSpan struct {
	Citation
	fs, fe int // byte offsets in the folded text
}

// FindCitations returns the documents cited in text, in text order and
// without overlap. A full number wins over a partial reading of the same
// words, and a law named right before its number ("Luật Đất đai số
// 31/2024/QH15") is one citation carrying both.
func FindCitations(text string) []Citation {
	folded, origRune := foldCitationText(text)
	orig := []rune(text)
	var spans []citeSpan
	covered := make([]bool, len(folded)+1)
	taken := func(s, e int) bool {
		for i := s; i < e; i++ {
			if covered[i] {
				return true
			}
		}
		return false
	}
	add := func(c citeSpan) {
		for i := c.fs; i < c.fe; i++ {
			covered[i] = true
		}
		spans = append(spans, c)
	}
	// each match without the trailing boundary rune citeEnd consumed
	matches := func(re *regexp.Regexp) [][]int {
		var out [][]int
		for _, m := range re.FindAllStringSubmatchIndex(folded, -1) {
			if r, size := utf8.DecodeLastRuneInString(folded[:m[1]]); m[1] > m[0] && size > 0 && !isCiteBodyRune(r) {
				m[1] -= size
			}
			out = append(out, m)
		}
		return out
	}
	sub := func(m []int, i int) string {
		if m[2*i] < 0 {
			return ""
		}
		return folded[m[2*i]:m[2*i+1]]
	}
	span := func(m []int, c Citation) citeSpan { return citeSpan{Citation: c, fs: m[0], fe: m[1]} }

	for _, m := range matches(citeStandardRe) {
		if gluedLetterBefore(folded, m[0]) || taken(m[0], m[1]) {
			continue
		}
		sep := ":"
		if sub(m, 1) == "QTKĐ" {
			sep = sub(m, 3)
		}
		num := sub(m, 1) + " " + sub(m, 2) + sep + sub(m, 4)
		if iss := sub(m, 5); iss != "" {
			num += "/" + iss
		}
		add(span(m, Citation{Kind: CitationStandard, Number: num, Key: CatalogNumberKey(num), Year: sub(m, 4)}))
	}
	for _, re := range []*regexp.Regexp{citeFullRe, citeDashRe, citeSpacedRe} {
		for _, m := range matches(re) {
			if gluedNumberBefore(folded, m[0]) || taken(m[0], m[1]) {
				continue
			}
			year := strings.ReplaceAll(sub(m, 2), "O", "0")
			if !strings.HasPrefix(year, "19") && !strings.HasPrefix(year, "20") {
				continue
			}
			num := sub(m, 1) + "/" + year + "/" + normalizeCitationCode(sub(m, 3))
			add(span(m, Citation{Kind: CitationFull, Number: num, Key: CatalogNumberKey(num), No: sub(m, 1), Year: year}))
		}
	}
	for _, m := range matches(citePartyRe) {
		if gluedNumberBefore(folded, m[0]) || taken(m[0], m[1]) {
			continue
		}
		num := sub(m, 1) + "-" + sub(m, 2) + "/" + sub(m, 3)
		add(span(m, Citation{Kind: CitationParty, Number: num, Key: CatalogNumberKey(num), No: sub(m, 1)}))
	}
	for _, m := range matches(citeAdminRe) {
		if gluedNumberBefore(folded, m[0]) || taken(m[0], m[1]) {
			continue
		}
		num := sub(m, 1) + "/" + normalizeCitationCode(sub(m, 2))
		add(span(m, Citation{Kind: CitationAdmin, Number: num, Key: CatalogNumberKey(num), No: sub(m, 1)}))
	}
	for _, m := range citePartialRe.FindAllStringSubmatchIndex(folded, -1) {
		if gluedLetterBefore(folded, m[0]) || taken(m[0], m[1]) {
			continue
		}
		word := sub(m, 1) + sub(m, 2)
		lw := strings.ToLower(word)
		hasSo := sub(m, 3) != ""
		no, year := sub(m, 4), sub(m, 5)+sub(m, 6)
		if !partialEndsClean(folded[m[1]:], year != "") {
			continue
		}
		switch {
		case lw == "hiến pháp":
			continue // a constitution is named by year, never numbered
		case lw == "luật" && (word == "luật" || precededByWord(folded, m[0], "pháp")):
			continue // "pháp luật 1.", "luật" as a common noun
		case sub(m, 1) != "" && !unicode.IsUpper(firstRune(word)) && !hasSo && year == "":
			continue // "quyết định 3 phương án": lower case needs "số" or a year
		case !hasSo && year == "" && len(no) == 4 && (strings.HasPrefix(no, "19") || strings.HasPrefix(no, "20")):
			continue // "Luật 2013", "Nghị định 2020": a year, not a number
		}
		num := no
		if year != "" {
			num += "/" + year
		}
		add(span(m, Citation{Kind: CitationPartial, Number: num, DocType: citeTypeNames[lw], No: no, Year: year}))
	}
	byStart := map[int]int{}
	for i := range spans {
		byStart[spans[i].fs] = i
	}
	for _, m := range citeNameRe.FindAllStringSubmatchIndex(folded, -1) {
		if gluedLetterBefore(folded, m[0]) || taken(m[0], m[0]+1) {
			continue
		}
		word := folded[m[2]:m[3]]
		end, year := scanLawName(folded, m[3])
		if end <= m[3] || taken(m[0], end) {
			continue
		}
		docType := citeTypeNames[strings.ToLower(word)]
		name := docType + folded[m[3]:end]
		// "Luật X số 73/2006/QH11": the name belongs to the number after it
		if loc := citeSoAfterRe.FindStringIndex(folded[end:]); loc != nil {
			if i, ok := byStart[end+loc[1]]; ok && spans[i].Kind != CitationPartial {
				spans[i].Name, spans[i].DocType = name, docType
				continue
			}
		}
		c := citeSpan{Citation: Citation{Kind: CitationName, Name: name, DocType: docType, Year: year}, fs: m[0], fe: end}
		if am := citeAmendBeforeRe.FindStringSubmatch(folded[max(0, m[0]-80):m[0]]); am != nil {
			c.Role = CitationRoleAmendTarget
			if am[1] != "" {
				c.Role = CitationRoleInAmendingTitle
			}
		}
		add(c)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].fs < spans[j].fs })
	out := make([]Citation, len(spans))
	for i, c := range spans {
		if c.DocType == "" && c.Kind != CitationStandard {
			if m := citeTypeBeforeRe.FindStringSubmatch(folded[max(0, c.fs-60):c.fs]); m != nil {
				c.DocType = citeTypeNames[strings.ToLower(m[1]+m[2])]
			} else if d := DocTypeFromNumber(c.Number); d != nil {
				c.DocType = d.Name
			}
		}
		c.Start, c.End = origRune[c.fs], origRune[c.fe]
		c.Raw = string(orig[c.Start:c.End])
		out[i] = c.Citation
	}
	return out
}

// NormalizeCitationNumber returns the display form of one written số
// hiệu ("68-2026-NĐ-CP", "01/2020/TT- BNNPTNT", "53/2022/NÐ–CP",
// "nghị định số 53/2022/nđ-cp") — "68/2026/NĐ-CP", "01/2020/TT-BNNPTNT",
// "53/2022/NĐ-CP" — or "" when s holds no full, year-less, Party or
// standard number. The first such number in s is taken.
func NormalizeCitationNumber(s string) string {
	for _, c := range FindCitations(s) {
		if c.Key != "" {
			return c.Number
		}
	}
	return ""
}

// foldCitationText returns the NFC form of text with citationFold applied
// and, for each of its bytes (and its end), the rune offset in text of the
// normalisation segment the byte comes from.
func foldCitationText(text string) (string, []int) {
	var b strings.Builder
	origRune := make([]int, 0, len(text)+1)
	var it norm.Iter
	it.InitString(norm.NFC, text)
	runeAt, prev := 0, 0
	for !it.Done() {
		seg := it.Next()
		start := runeAt
		pos := it.Pos()
		runeAt += utf8.RuneCountInString(text[prev:pos])
		prev = pos
		for _, r := range string(seg) {
			if f, ok := citationFold[r]; ok {
				r = f
			}
			n, _ := b.WriteRune(r)
			for k := 0; k < n; k++ {
				origRune = append(origRune, start)
			}
		}
	}
	return b.String(), append(origRune, runeAt)
}

// normalizeCitationCode drops the spaces of a code and upper-cases one
// written all in lower case ("nđ-cp"); a mixed one keeps its case
// ("QĐ-TTg").
func normalizeCitationCode(code string) string {
	code = strings.Join(strings.Fields(code), "")
	if strings.ToLower(code) == code {
		code = strings.ToUpper(code)
	}
	return code
}

func isCiteBodyRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '&' }

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// gluedNumberBefore: a match right after a digit, "/" or "." is the tail
// of a longer number ("1153/2022/…" is not "53/2022/…").
func gluedNumberBefore(s string, i int) bool {
	if i == 0 {
		return false
	}
	c := s[i-1]
	return c == '/' || c == '.' || (c >= '0' && c <= '9')
}

// gluedLetterBefore: a type word or standard prefix must start a word.
func gluedLetterBefore(s string, i int) bool {
	if i == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// nextWord returns the word starting after the spaces at s[i:] and the
// offset just past it; it reads only that far.
func nextWord(s string, i int) (string, int) {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	j := i
	for j < len(s) {
		r, size := utf8.DecodeRuneInString(s[j:])
		if !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			break
		}
		j += size
	}
	return s[i:j], j
}

// partialEndsClean: a bare number is complete — not followed by a letter,
// a digit, or "/", "-", "." / ":" leading to more of a number — and,
// without a year, is not a quantity ("quyết định 15 ngày", "3 năm tù").
func partialEndsClean(rest string, hasYear bool) bool {
	r, size := utf8.DecodeRuneInString(rest)
	if size == 0 {
		return true
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '%' {
		return false
	}
	if r == '/' || r == '-' || r == '.' || r == ':' {
		i := size
		for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
			i++
		}
		n, _ := utf8.DecodeRuneInString(rest[i:])
		return !(unicode.IsLetter(n) || unicode.IsDigit(n))
	}
	if hasYear {
		return true
	}
	w, _ := nextWord(rest, 0)
	return !citeQuantityUnits[strings.ToLower(w)]
}

// citeQuantityUnits follow a count, never a document number.
var citeQuantityUnits = map[string]bool{
	"ngày": true, "tháng": true, "năm": true, "giờ": true, "phút": true, "lần": true, "người": true,
	"đồng": true, "triệu": true, "tỷ": true, "nghìn": true, "văn": true, "bản": true, "phương": true,
	"nội": true, "trường": true, "hợp": true, "loại": true, "nhóm": true, "mức": true, "bước": true,
	"tuổi": true, "kỳ": true, "vụ": true, "cái": true, "chiếc": true, "lượt": true, "tù": true,
}

func precededByWord(s string, i int, word string) bool {
	before := strings.TrimRight(s[max(0, i-len(word)-8):i], " \t")
	return len(before) >= len(word) && strings.EqualFold(before[len(before)-len(word):], word) &&
		!gluedLetterBefore(before, len(before)-len(word))
}

// lawNameStops end a law's name: words of a clause, never of a title.
// "không", "có", "theo", "hiện" are not among them: "Luật Hàng không dân
// dụng Việt Nam", "Pháp lệnh Ưu đãi người có công với cách mạng", "Luật
// Đầu tư theo phương thức đối tác công tư", "Luật Thực hiện dân chủ ở cơ
// sở". A run that reads on past the name is trimmed by the catalogue
// lookup.
var lawNameStops = map[string]bool{
	"số": true, "năm": true, "ngày": true, "được": true, "đã": true, "đang": true, "sẽ": true,
	"thì": true, "là": true, "này": true, "nêu": true,
}

// lawNameStopPairs end a name at a verb that is two words: "Luật Đấu thầu
// quy định", while "Luật Tiêu chuẩn và quy chuẩn kỹ thuật" keeps "quy".
var lawNameStopPairs = map[string]bool{
	"quy định": true, "sửa đổi": true, "bổ sung": true, "hướng dẫn": true, "bãi bỏ": true, "thay thế": true,
	"có hiệu": true, "hết hiệu": true,
}

const lawNameMaxWords = 16

// scanLawName reads a law's name from i (just after the type word, at the
// space before its first capitalised word) and returns where it ends and
// the year written right after it ("Luật Đấu thầu 2023", "… năm 2013").
// It stops at punctuation, a number, a clause word, a following type word
// ("…, Luật Đầu tư", "… và Luật Nhà ở") or a comma before a capital; a
// comma before a lower-case word stays inside ("Luật Khám bệnh, chữa
// bệnh"). It reads word by word, never the rest of the text.
func scanLawName(s string, i int) (end int, year string) {
	end = i
	words := 0
	pos := i
	for words < lawNameMaxWords {
		j := pos
		comma := false
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == ',') {
			if s[j] == ',' {
				if comma {
					break
				}
				comma = true
			}
			j++
		}
		k := j
		for k < len(s) {
			r, size := utf8.DecodeRuneInString(s[k:])
			if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' && k > j) {
				break
			}
			k += size
		}
		if k == j {
			break
		}
		w := s[j:k]
		first := firstRune(w)
		lw := strings.ToLower(w)
		if unicode.IsDigit(first) {
			if len(w) == 4 && (strings.HasPrefix(w, "19") || strings.HasPrefix(w, "20")) && !comma {
				year = w
			}
			break
		}
		if next, _ := nextWord(s, k); next != "" && lawNameStopPairs[lw+" "+strings.ToLower(next)] {
			break
		}
		if lawNameStops[lw] {
			if lw == "năm" {
				if y, _ := nextWord(s, k); len(y) == 4 && (strings.HasPrefix(y, "19") || strings.HasPrefix(y, "20")) &&
					strings.Trim(y, "0123456789") == "" {
					year = y
				}
			}
			break
		}
		if comma && unicode.IsUpper(first) {
			break
		}
		if (lw == "và" || lw == "hoặc") && startsTypeWord(s[k:min(len(s), k+24)]) {
			break
		}
		if words > 0 && unicode.IsUpper(first) && citeTypeNames[lw] != "" && lw != "tt" {
			break
		}
		if words == 0 && !unicode.IsUpper(first) {
			break
		}
		words++
		end, pos = k, k
	}
	return end, year
}

func startsTypeWord(s string) bool {
	s = strings.TrimLeft(s, " \t")
	for _, w := range []string{"Luật", "Bộ luật", "Pháp lệnh", "Nghị định", "Nghị quyết", "Thông tư", "Quyết định"} {
		if strings.HasPrefix(s, w) {
			return true
		}
	}
	return false
}
