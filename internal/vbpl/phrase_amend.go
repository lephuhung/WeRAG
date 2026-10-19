package vbpl

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Phrase amendments change words in place instead of quoting a new
// provision: "Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ đồng” tại Điều
// 3, Điều 4, khoản 1 Điều 8 Nghị định số 68/2026/NĐ-CP", "Thay thế cụm từ
// “cấp huyện” bằng cụm từ “cấp xã” tại khoản 2 Điều 34", "Bãi bỏ cụm từ
// “cấp huyện,” tại khoản 1 Điều 11". Each named provision becomes one Amend
// with From/To set (To empty for a removal).

var (
	// phraseLeadRe opens a replacement or removal: the verb, then "cụm từ" /
	// "từ" / "từ ngữ" right before the first quoted phrase.
	phraseLeadRe   = regexp.MustCompile(`(?i)(?:sửa đổi|thay thế|thay đổi|thay|bãi bỏ)\s+(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)\s*[“"]`)
	phraseRemoveRe = regexp.MustCompile(`(?i)^bãi bỏ`)
	// phrasePairRe reads one “X” [tại …] (bằng|thành) [cụm từ|từ] “Y”.
	phrasePairRe = regexp.MustCompile(`[“"]([^”"]{1,200})[”"]\s*(tại\s+[^“”"]*?\s+)?(?:bằng|thành)\s+(?:cụm\s+từ\s+|từ\s+ngữ\s+|từ\s+)?[“"]([^”"]{0,200})[”"]`)
	// phraseOnlyRe reads a removed “X”.
	phraseOnlyRe = regexp.MustCompile(`[“"]([^”"]{1,200})[”"]`)
	// phraseProvRe reads the provisions after tại / trong / quy định tại.
	// A bare number after "," or "và" continues the previous keyword; the
	// match must not run on into a word (checked by provMatches).
	phraseProvRe   = regexp.MustCompile(`(?i)(điểm|khoản|Điều)\s+(\d+[a-zđ]?|[a-zđ])|(?:,|\svà)\s*(\d+[a-zđ]?)`)
	fileNumberRe   = regexp.MustCompile(`\b(\d+)-(\d{4})-([A-ZĐ])`)
	phraseNumberRe = regexp.MustCompile(`\d+/\d{4}/[A-ZĐa-zđ0-9\-]+`)
	phraseDocRe    = regexp.MustCompile(`(?i)(Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s+(?:số\s+)?\d+/\d{4}/[A-ZĐa-zđ0-9\-]+`)
)

// phraseAmends reads the phrase amendments of one paragraph, sentence by
// sentence. Provisions in an appendix, mục or table (no Điều) are skipped;
// so is a phrase naming no Điều at all ("tại Thông tư này").
func (b *builder) phraseAmends(text string) []Amend {
	return parsePhraseAmends(text, b.enclosingAmendedDocument)
}

// parsePhraseAmends reads text sentence by sentence; enclosing names the
// amended document when a sentence names none.
func parsePhraseAmends(text string, enclosing func() string) []Amend {
	var out []Amend
	for _, s := range splitSentences(text) {
		out = append(out, phraseSentence(s, enclosing)...)
	}
	return out
}

// PhraseAmendsInChunk reads the phrase amendments of a stored chunk, for
// chunks ingested before the converter read them: its unquoted paragraphs
// are read as at ingest, the amended document coming from the heading path
// (innermost first) or else the document title. The converter itself has
// the clause and item lead-ins too, so it can name a document this misses.
func PhraseAmendsInChunk(content string, headingPath []string, title string) []Amend {
	// knowledge titles come from file names: "Nghị định số 140-2021-NĐ-CP"
	title = fileNumberRe.ReplaceAllString(title, "$1/$2/$3")
	enclosing := func() string {
		for i := len(headingPath) - 1; i >= 0; i-- {
			if d := amendedDocument(ownLabelRe.ReplaceAllString(headingPath[i], "")); d != "" {
				return d
			}
		}
		if amendVerbRe.MatchString(title) {
			if d := amendedDocument(title); d != "" && !strings.Contains(strings.ToLower(d), " và ") {
				return d
			}
		}
		return ""
	}
	var out []Amend
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ">") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") {
			continue
		}
		for _, a := range parsePhraseAmends(line, enclosing) {
			dup := false
			for _, have := range out {
				if have == a {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, a)
			}
		}
	}
	return out
}

// phraseItem is one phrase of a sentence: rest[start:end] is the phrase
// (or pair), mid the text between a pair's two phrases.
type phraseItem struct {
	from, to, mid string
	start, end    int
}

func phraseSentence(text string, enclosing func() string) []Amend {
	lead := phraseLeadRe.FindStringIndex(text)
	if lead == nil {
		return nil
	}
	// rest starts at the first phrase's opening quote
	rest := text[lead[1]-len(quoteRuneAt(text, lead[1])):]
	var items []phraseItem
	if phraseRemoveRe.MatchString(strings.TrimSpace(text[lead[0]:])) {
		for _, m := range phraseOnlyRe.FindAllStringSubmatchIndex(rest, -1) {
			items = append(items, phraseItem{from: rest[m[2]:m[3]], start: m[0], end: m[1]})
		}
	} else {
		for _, m := range phrasePairRe.FindAllStringSubmatchIndex(rest, -1) {
			it := phraseItem{from: rest[m[2]:m[3]], to: rest[m[6]:m[7]], start: m[0], end: m[1]}
			if m[4] >= 0 {
				it.mid = rest[m[4]:m[5]]
			}
			items = append(items, it)
		}
	}
	// Each phrase takes the provisions after it, up to the next phrase;
	// phrases followed by none share the next list ("từ “A” bằng từ “B”;
	// cụm từ “C” bằng cụm từ “D” tại điểm a khoản 1 Điều 28"). A pair with
	// the provisions inside ("“A” tại khoản 2 Điều 5 bằng cụm từ “B”")
	// keeps those.
	var out []Amend
	var pending []phraseItem
	for i, it := range items {
		tail := rest[it.end:]
		if i+1 < len(items) {
			tail = rest[it.end:items[i+1].start]
		}
		pending = append(pending, it)
		where := tail
		provs := readPhraseTargets(tail)
		if len(provs) == 0 && it.mid != "" {
			where, provs = it.mid, readPhraseTargets(it.mid)
		}
		if len(provs) == 0 {
			continue
		}
		doc := ""
		if m := phraseDocRe.FindString(where); m != "" {
			doc = sentenceCase(m)
		} else {
			doc = enclosing()
		}
		for _, p := range pending {
			from := strings.TrimSpace(p.from)
			if from == "" {
				continue
			}
			for _, pr := range provs {
				out = append(out, Amend{Document: doc, Article: pr.article, Khoan: pr.khoan, From: from, To: strings.TrimSpace(p.to), Phrase: true})
			}
		}
		pending = nil
	}
	return out
}

// splitSentences cuts text at ". " followed by a capital letter, outside
// quoted phrases.
func splitSentences(text string) []string {
	var out []string
	inQuote := false
	start := 0
	rs := []rune(text)
	pos := 0 // byte offset of rs[i]
	for i, r := range rs {
		switch r {
		case '“':
			inQuote = true
		case '”':
			inQuote = false
		case '"':
			inQuote = !inQuote
		case '.':
			if !inQuote && i+2 < len(rs) && rs[i+1] == ' ' && unicode.IsUpper(rs[i+2]) {
				out = append(out, text[start:pos+1])
				start = pos + 2
			}
		}
		pos += utf8.RuneLen(r)
	}
	return append(out, text[start:])
}

// quoteRuneAt is the opening quote ending at byte offset end.
func quoteRuneAt(s string, end int) string {
	if strings.HasSuffix(s[:end], "“") {
		return "“"
	}
	return `"`
}

type phraseProv struct{ article, khoan string }

// readPhraseTargets lists the provisions of "tại các khoản 2, 3 và khoản 5
// Điều 25, khoản 2 Điều 16, Điều 19": khoản collected before a Điều belong
// to it; a bare number after a khoản or Điều continues that list. điểm
// narrows to its khoản and is not kept. Reading stops at a document number
// ("Điều 14 Thông tư số 09/2015/TT-BGTVT" ends the list).
func readPhraseTargets(s string) []phraseProv {
	if i := phraseNumberRe.FindStringIndex(s); i != nil {
		s = s[:i[1]]
	}
	var out []phraseProv
	var khoans []string
	seen := map[phraseProv]bool{}
	add := func(p phraseProv) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	article := func(n string) {
		if len(khoans) == 0 {
			add(phraseProv{article: n})
		}
		for _, k := range khoans {
			add(phraseProv{article: n, khoan: k})
		}
		khoans = nil
	}
	last := ""
	for _, m := range provMatches(s) {
		kw, n := strings.ToLower(m[0]), m[1]
		if kw == "" {
			kw = last // bare number: continues the list
		} else {
			last = kw
		}
		switch kw {
		case "khoản":
			khoans = append(khoans, n)
		case "điều":
			article(n)
		}
	}
	return out
}

// provMatches returns (keyword, label) of each phraseProvRe match not
// followed by a letter or digit ("Điều 3," but not "điều của").
func provMatches(s string) [][2]string {
	var out [][2]string
	for _, m := range phraseProvRe.FindAllStringSubmatchIndex(s, -1) {
		if r, _ := utf8.DecodeRuneInString(s[m[1]:]); m[1] < len(s) && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			continue
		}
		if m[2] >= 0 {
			out = append(out, [2]string{s[m[2]:m[3]], s[m[4]:m[5]]})
		} else {
			out = append(out, [2]string{"", s[m[6]:m[7]]})
		}
	}
	return out
}

// enclosingAmendedDocument is the document the open Điều / khoản amends,
// else the one the title names.
func (b *builder) enclosingAmendedDocument() string {
	for _, i := range []int{b.item, b.clause, b.article} {
		if i >= 0 {
			if d := amendedDocument(ownLabelRe.ReplaceAllString(b.doc.Blocks[i].Text, "")); d != "" {
				return d
			}
		}
	}
	return b.titleAmendedDocument()
}
