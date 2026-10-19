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
	phraseLeadRe   = regexp.MustCompile(`(?i)(?:sửa đổi|thay thế|thay đổi|thay|bãi bỏ)\s+(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)\s*:?\s*[“"]`)
	phraseRemoveRe = regexp.MustCompile(`(?i)bãi bỏ\s+(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)\s*:?\s*[“"]`)
	// phraseMoreRe opens a clause that goes on with the verb before it:
	// "…; cụm từ “C” bằng cụm từ “D” tại …".
	phraseMoreRe = regexp.MustCompile(`(?i)^(?:và\s+)?(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)\s*:?\s*[“"]`)
	// phraseVerbRe starts a new clause after "," or "và": another verb.
	phraseVerbRe = regexp.MustCompile(`(?i)^(?:và\s+)?(?:bãi bỏ|sửa đổi|thay thế|thay đổi|thay|bổ sung)\s`)
	// phrasePairRe reads one “X” [tại …] (bằng|thành) [cụm từ|từ] “Y”.
	phrasePairRe = regexp.MustCompile(`[“"]([^”"]{1,200})[”"]\s*(tại\s+[^“”"]*?\s+)?(?:bằng|thành)\s+(?:cụm\s+từ\s+|từ\s+ngữ\s+|từ\s+)?[“"]([^”"]{0,200})[”"]`)
	// phraseOnlyRe reads a removed “X”.
	phraseOnlyRe = regexp.MustCompile(`[“"]([^”"]{1,200})[”"]`)
	// phraseProvRe reads the provisions after tại / trong / quy định tại. A
	// bare number (or, after điểm, a letter) after "," or "và" continues the
	// label just before it; provMatches checks both and that the match does
	// not run on into a word.
	// Only the keywords ignore case: a capital after "," ("…, Điều 4") is
	// not a bare điểm letter.
	phraseProvRe = regexp.MustCompile(`((?i:điểm|khoản|điều))\s+(\d+[a-zđ]?|[a-zđ]\d*)|(?:,|\svà)\s*(\d+[a-zđ]?)|(?:,|\svà)\s*([a-zđ]\d*)`)
	// phraseBareNextRe is what may follow a bare label: the list goes on or
	// ends ("Điều 3, 4 Luật …"), not a quantity ("Điều 3, 15 ngày").
	phraseBareNextRe = regexp.MustCompile(`(?i)^(?:\s*[,;.)]|\s*$|\s+(?:và|khoản|điểm|điều|của|tại|bộ luật|luật|pháp lệnh|nghị định|nghị quyết|thông tư|quyết định)(?:\s|$))`)
	// phraseListMoreRe is a segment after ";" that only goes on with the
	// provision list: labels and connectors, then at most a document name
	// ("; khoản 6 Điều 5; Điều 74 Nghị định số 53/2024/NĐ-CP"), not "; Điều 5
	// được sửa đổi như sau".
	phraseListMoreRe = regexp.MustCompile(`(?i)^\s*(?:(?:các|tên)\s+)?(?:điểm|khoản|điều)\s+[0-9a-zđ]+(?:(?:\s*,\s*|\s+và\s+|\s+)(?:(?:các|tên)\s+)?(?:(?:điểm|khoản|điều)\s+)?[0-9a-zđ]+)*(?:\s+(?:của\s+)?(?:` + "Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định" + `)\s[^;()]*)?\s*[.]?\s*$`)
	// phraseBracketRe is a bracketed remark in a provision list.
	phraseBracketRe = regexp.MustCompile(`\([^()]*\)`)
	// phraseOtherDocRe, inside a title before its number, means the number
	// is another document's ("Nghị định này, Nghị định số 12/2020/NĐ-CP",
	// "Luật Đất đai, được hướng dẫn tại Nghị định số …").
	phraseOtherDocRe = regexp.MustCompile(`(?i)(?:^|\s)(?:` + "Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định" + `|này|được|tại|theo)(?:\s|,|$)`)
	// phraseDocTypeRe is the document type a title starts with.
	phraseDocTypeRe = regexp.MustCompile(`(?i)^(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s`)
	// phraseSpacedNumberRe joins "36/2024/TT- BGTVT".
	phraseSpacedNumberRe = regexp.MustCompile(`(\d+/\d{4}/[A-ZĐ]+)-\s+([A-ZĐ])`)
	// phraseExceptRe marks an exception ("trừ …"): such a list is not read.
	phraseExceptRe = regexp.MustCompile(`(?i)(?:^|[\s,(])(?:ngoại trừ|trừ|không áp dụng)(?:[\s,:]|$)`)
	fileNumberRe   = regexp.MustCompile(`\b(\d+)-(\d{4})-([A-ZĐ])`)
	phraseNumberRe = regexp.MustCompile(`\d+/\d{4}/[A-ZĐa-zđ0-9\-]+`)
	// phraseDocRe reads the document named right after the provisions,
	// with or without a title before its number: "Luật Xây dựng số
	// 50/2014/QH13", "Nghị định số 68/2026/NĐ-CP", "Luật Quản lý thuế".
	phraseDocRe = regexp.MustCompile(`(?i)^[\s,]*(?:của\s+|tại\s+)?((?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s[^,;:(“"]*)`)
	// phraseDocNumberRe is tried first: the title may hold commas ("Luật
	// Phòng, chống ma túy số 73/2021/QH14").
	phraseDocNumberRe = regexp.MustCompile(`(?i)^[\s,]*(?:của\s+|tại\s+)?((?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s[^;:(“"]{0,160}?\d+/\d{4}/[A-ZĐa-zđ0-9\-]+)`)
	phraseDocEnd      = regexp.MustCompile(`(?i)\s+(?:ngày\s+\d|được sửa đổi|đã được|như sau|và\s)`)
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
	text = phraseSpacedNumberRe.ReplaceAllString(text, "$1-$2")
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

// phraseItem is one phrase of a clause: rest[start:end] is the phrase
// (or pair), mid the text between a pair's two phrases.
type phraseItem struct {
	from, to, mid string
	start, end    int
}

// phraseSentence reads one sentence clause by clause: each clause has its
// own verb ("Bãi bỏ cụm từ “A”, sửa đổi cụm từ “B” thành “C” tại …") and
// its own provisions — nothing is carried over a ";" or to the next verb,
// a missed amendment being better than a wrong one.
func phraseSentence(text string, enclosing func() string) []Amend {
	var out []Amend
	remove, known := false, false
	for _, cl := range splitClauses(text) {
		var rest string
		switch lead := phraseLeadRe.FindStringIndex(cl); {
		case lead != nil:
			remove, known = phraseRemoveRe.MatchString(cl[lead[0]:]), true
			rest = cl[lead[1]-len(quoteRuneAt(cl, lead[1])):]
		case known && phraseMoreRe.MatchString(strings.TrimSpace(cl)):
			t := strings.TrimSpace(cl)
			i := strings.IndexAny(t, "“\"")
			rest = t[i:]
		default:
			known = false
			continue
		}
		out = append(out, phraseClause(rest, remove, enclosing)...)
	}
	return out
}

func phraseClause(rest string, remove bool, enclosing func() string) []Amend {
	var items []phraseItem
	if remove {
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
	// phrases followed by none share the next list ("“A”, “B” và “C” tại
	// khoản 6 Điều 5"). A pair with the provisions inside ("“A” tại khoản 2
	// Điều 5 bằng cụm từ “B”") keeps those.
	var out []Amend
	var pending []phraseItem
	for i, it := range items {
		tail := rest[it.end:]
		if i+1 < len(items) {
			tail = rest[it.end:items[i+1].start]
		}
		pending = append(pending, it)
		provs, doc, ok := readPhraseTail(tail)
		if len(provs) == 0 && ok && it.mid != "" {
			provs, doc, ok = readPhraseTail(it.mid)
		}
		if !ok {
			// the provisions are in the amending document itself
			return out
		}
		if len(provs) == 0 {
			continue
		}
		if doc == "" {
			doc = phraseSpacedNumberRe.ReplaceAllString(enclosing(), "$1-$2")
		}
		for _, p := range pending {
			from := strings.TrimSpace(p.from)
			if from == "" {
				continue
			}
			for _, pr := range provs {
				out = append(out, Amend{Document: doc, Article: pr.article, Khoan: pr.khoan, Diem: pr.diem,
					From: from, To: strings.TrimSpace(p.to), Phrase: true})
			}
		}
		pending = nil
	}
	return out
}

// hasException reports a "trừ" / "ngoại trừ" / "không áp dụng" in tail,
// not the "trừ" of "khấu trừ".
func hasException(tail string) bool {
	for _, m := range phraseExceptRe.FindAllStringIndex(tail, -1) {
		if !strings.HasSuffix(strings.ToLower(strings.TrimRight(tail[:m[0]+1], " ,(")), "khấu") {
			return true
		}
	}
	return false
}

// readPhraseTail reads the provisions of a tail and the document named
// right after them; a tail with an exception gives none. ok is false when
// that document is the amending one itself ("Nghị định này").
func readPhraseTail(tail string) (provs []phraseProv, doc string, ok bool) {
	// "khoản 2 Điều 9 (được sửa đổi bởi khoản 1 Điều 1 Nghị định số …)":
	// a bracket says where the provision comes from, it is not a target
	tail = phraseBracketRe.ReplaceAllString(tail, " ")
	if hasException(tail) {
		// "tại khoản 2 Điều 9, trừ điểm a": what is excepted cannot be
		// told apart reliably, so the amendment is left out
		return nil, "", true
	}
	provs, end := readPhraseTargets(tail)
	if len(provs) == 0 {
		return nil, "", true
	}
	var name string
	if m := phraseDocNumberRe.FindStringSubmatch(tail[end:]); m != nil && !phraseOtherDocRe.MatchString(phraseDocTypeRe.ReplaceAllString(m[1], "")) {
		name = m[1]
	} else if m := phraseDocRe.FindStringSubmatch(tail[end:]); m != nil {
		name = m[1]
	} else {
		return provs, "", true
	}
	if n := phraseNumberRe.FindStringIndex(name); n != nil {
		name = name[:n[1]]
	} else if e := phraseDocEnd.FindStringIndex(name); e != nil {
		name = name[:e[0]]
	}
	name = strings.TrimRight(strings.TrimSpace(name), " ,.")
	low := strings.ToLower(name)
	if strings.HasSuffix(low, " này") {
		return nil, "", false
	}
	return provs, sentenceCase(name), true
}

// nextSegment is text up to its next ";" outside quoted phrases.
func nextSegment(text string) string {
	inQuote := false
	for i, r := range text {
		switch r {
		case '“':
			inQuote = true
		case '”':
			inQuote = false
		case '"':
			inQuote = !inQuote
		case ';':
			if !inQuote {
				return text[:i]
			}
		}
	}
	return text
}

// splitClauses cuts a sentence, outside quoted phrases, at ";" — unless a
// provision label follows, going on with the list — and before another
// verb after "," or "và".
func splitClauses(text string) []string {
	var out []string
	inQuote := false
	start := 0
	for i, r := range text {
		switch r {
		case '“':
			inQuote = true
		case '”':
			inQuote = false
		case '"':
			inQuote = !inQuote
		case ';':
			// "tại khoản 2 Điều 4; khoản 6 Điều 5; Điều 74" is one list
			if !inQuote && !phraseListMoreRe.MatchString(phraseBracketRe.ReplaceAllString(nextSegment(text[i+1:]), " ")) {
				out = append(out, text[start:i])
				start = i + 1
			}
		case ',', ' ':
			if !inQuote && i > start && phraseVerbRe.MatchString(strings.TrimLeft(text[i+1:], " ")) &&
				(r == ',' || strings.HasPrefix(text[i+1:], "và ")) {
				out = append(out, text[start:i])
				start = i + 1
			}
		}
	}
	return append(out, text[start:])
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

type phraseProv struct{ article, khoan, diem string }

// readPhraseTargets lists the provisions of "tại điểm a và c khoản 2, các
// khoản 3, 4 Điều 25, khoản 2 Điều 16, Điều 19": điểm collected before a
// khoản belong to it, khoản collected before a Điều belong to it, and a
// bare label right after one continues that list. Reading stops at a
// document number ("Điều 14 Thông tư số 09/2015/TT-BGTVT"). end is the byte
// offset just after the last provision read.
func readPhraseTargets(s string) (out []phraseProv, end int) {
	if i := phraseNumberRe.FindStringIndex(s); i != nil {
		s = s[:i[0]]
	}
	type khoan struct {
		n     string
		diems []string
	}
	var khoans []khoan
	var diems []string
	seen := map[phraseProv]bool{}
	add := func(p phraseProv) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	article := func(n string) {
		if len(khoans) == 0 {
			khoans = []khoan{{diems: diems}}
		}
		for _, k := range khoans {
			if len(k.diems) == 0 {
				add(phraseProv{article: n, khoan: k.n})
			}
			for _, d := range k.diems {
				add(phraseProv{article: n, khoan: k.n, diem: d})
			}
		}
		khoans, diems = nil, nil
	}
	for _, m := range provMatches(s) {
		switch m.kw {
		case "điểm":
			diems = append(diems, strings.ToLower(m.label))
		case "khoản":
			khoans = append(khoans, khoan{n: m.label, diems: diems})
			diems = nil
		case "điều":
			article(m.label)
		}
		end = m.end
	}
	return out, end
}

type provMatch struct {
	kw, label string
	end       int
}

// provMatches returns the provision labels of s: "Điều 3," but not "điều
// của"; a bare label only right after a label of its kind (a number after
// a khoản or Điều, a letter after a điểm) and followed by the list going on
// or ending, so "…, 15 ngày" is not one. A rejected match does not hide
// the text after its first rune (", khoản 6" is not điểm k).
func provMatches(s string) []provMatch {
	var out []provMatch
	last, lastEnd := "", -1
	for pos := 0; pos < len(s); {
		m := phraseProvRe.FindStringSubmatchIndex(s[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		next := m[1]
		accepted := false
		if r, _ := utf8.DecodeRuneInString(s[m[1]:]); m[1] >= len(s) || !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			bare := lastEnd == m[0] && phraseBareNextRe.MatchString(s[m[1]:])
			switch {
			case m[2] >= 0 && strings.HasSuffix(strings.ToLower(s[:m[0]]), "tên "):
				// "tên Điều 31": the title, not the provisions — not read
				last = ""
			case m[2] >= 0:
				last = strings.ToLower(s[m[2]:m[3]])
				out = append(out, provMatch{kw: last, label: s[m[4]:m[5]], end: m[1]})
				accepted = true
			case bare && m[6] >= 0 && (last == "khoản" || last == "điều"):
				out = append(out, provMatch{kw: last, label: s[m[6]:m[7]], end: m[1]})
				accepted = true
			case bare && m[8] >= 0 && last == "điểm":
				out = append(out, provMatch{kw: last, label: s[m[8]:m[9]], end: m[1]})
				accepted = true
			}
		}
		if accepted {
			lastEnd = m[1]
		} else {
			_, n := utf8.DecodeRuneInString(s[m[0]:])
			next = m[0] + n
		}
		pos = next
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
