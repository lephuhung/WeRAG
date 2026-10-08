package tools

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The lexical scorer behind find_in_documents, the retrieved passages of
// the <session_documents> prompt and the no-model document router. It is
// deliberately small: no embeddings, one pass over the units.
//
// A query is cut into terms (words of at least two runes, a short list of
// question words dropped) and codes: số ký hiệu (45/KH-UBND), references
// to an Điều/khoản/điểm/chương, unit codes (PA05, NĐ30), upper-case
// abbreviations, and numbers (with a unit when one follows). Text is
// compared lower-cased, and a term matches with and without diacritics: a
// term typed without them ("phong chong") matches either spelling fully, a
// term typed with them matches its unaccented form at half weight (bàn is
// not bán). Codes weigh most, then an adjacent pair of query terms found
// adjacent in the unit, then single terms (weighted by how rare they are
// among the units), then a match in the unit's header (chunk heading or
// section title). Long units are mildly penalised.

// searchUnit is one searchable piece of a document: a paragraph of a target
// or a chunk (or line) of a source. Doc tells the documents of one search
// apart (neighbours never cross documents).
type searchUnit struct {
	Doc    int
	Index  int
	Text   string
	Header string // chunk context header, or the title of the section holding it
	Label  string // NĐ30 component of a target paragraph
}

// searchHit is a unit that matched: Pos is its position in the unit list,
// Prev/Next the positions of the units before and after it in the same
// document (-1 when none).
type searchHit struct {
	Pos, Index int
	Doc        int
	Score      float64
	Prev, Next int
	// Codes are the query codes the unit holds (as folded in the query).
	Codes []string
}

// Score weights.
const (
	searchWeightDocNumber = 6.0 // 45/KH-UBND, 30/2020/NĐ-CP
	searchWeightReference = 4.0 // Điều 5, khoản 2, điểm a
	searchWeightUnitCode  = 4.0 // PA05, NĐ30
	searchWeightAbbrev    = 2.0 // UBND, CNTT
	searchWeightFigure    = 3.0 // 1.250 tỷ, 15%
	searchWeightNumber    = 2.0 // 1250, 2026
	searchWeightPhrase    = 1.0 // × the pair's mean idf
	searchWeightHeader    = 0.5 // × the term's idf
	// searchFoldedOnly is the weight of a term typed with diacritics that
	// matches only without them.
	searchFoldedOnly = 0.5
	// searchStrongCode is the least weight of a code that locates a
	// passage on its own (see qualifies).
	searchStrongCode = searchWeightFigure
	// searchMinScore drops units that only brushed a common word.
	searchMinScore = 0.4
)

type searchTerm struct {
	lower, folded string
	accented      bool
}

type searchCode struct {
	text   string // folded, digit separators removed
	weight float64
}

type searchQuery struct {
	terms []searchTerm
	pairs [][2]string // adjacent folded terms
	codes []searchCode
}

func (q *searchQuery) empty() bool { return len(q.terms) == 0 && len(q.codes) == 0 }

// searchStopWords are question and filler words that locate nothing;
// searchFoldedStopWords are their unaccented forms, for a query typed
// without diacritics, minus the forms that are also content words
// ("ban" in "ban hanh").
var (
	searchStopWords       = map[string]bool{}
	searchFoldedStopWords = map[string]bool{}
)

func init() {
	for _, w := range strings.Fields(`các của và là này những được cho trong theo về đã có không nào gì một như thì với để từ đến tại ở
		hai ba bốn giúp tôi mình bạn xem hãy cái đó đây kia do nêu cho biết hỏi trả lời văn bản tài liệu nội dung
		thế sao bao nhiêu ai đâu khi nào hay hoặc thì mà nếu vậy rồi còn cũng đang sẽ phải có thể tìm kiểm tra
		the of and or in on to for what which who is are`) {
		searchStopWords[w] = true
	}
	for w := range searchStopWords {
		searchFoldedStopWords[foldSearch(w)] = true
	}
	for _, w := range []string{"ban", "tai", "van", "noi", "dung", "bo", "co", "ho"} {
		delete(searchFoldedStopWords, w)
	}
}

var (
	// on folded text (lower case, no diacritics, digit separators removed)
	searchDocNumberRe = regexp.MustCompile(`\d+[a-z]?(?:/\d{2,4})*/[a-z][a-z0-9]*(?:[-–][a-z0-9]+)*`)
	searchNumYearRe   = regexp.MustCompile(`\b\d{1,5}/\d{4}\b`)
	searchReferenceRe = regexp.MustCompile(`\b(dieu|khoan|diem|chuong|muc|phu luc|phan)\s+(\d+[a-z]?|[ivxlc]+|[a-z])\b`)
	searchFigureRe    = regexp.MustCompile(`\b(\d+)\s*(%|(?:ty|trieu|nghin|ngan|dong|ha|km|m2|m3|tan|kg|nguoi|ho|xa|lan|thang|nam|ngay|gio|phut|bo|chiec|cai|du an|truong|benh vien)\b)`)
	searchNumberRe    = regexp.MustCompile(`\b\d{3,}\b`)
	searchDigitSepRe  = regexp.MustCompile(`(\d)[.,](\d)`)
)

// foldSearch lower-cases s, drops its diacritics (đ → d), removes
// thousand separators between digits ("1.250" → "1250") and collapses
// whitespace.
func foldSearch(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ':
			b.WriteRune('d')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	for i := 0; i < 2; i++ { // "1.250.000": overlapping matches need a second pass
		out = searchDigitSepRe.ReplaceAllString(out, "$1$2")
	}
	return out
}

// searchWords cuts s into lower-case words of letters and digits.
func searchWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(norm.NFC.String(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.Is(unicode.Mn, r)
	})
}

// parseSearchQuery reads the terms and codes of query and of extra terms
// (words picked by the caller, e.g. from a selected paragraph).
func parseSearchQuery(query string, extra ...string) *searchQuery {
	q := &searchQuery{}
	seenTerm := map[string]bool{}
	seenCode := map[string]bool{}
	addCode := func(text string, w float64) {
		text = strings.TrimSpace(text)
		if text == "" || seenCode[text] {
			return
		}
		// a code inside a longer one already found (30/2020 in 30/2020/nd-cp)
		for _, c := range q.codes {
			if strings.Contains(c.text, text) {
				return
			}
		}
		seenCode[text] = true
		q.codes = append(q.codes, searchCode{text: text, weight: w})
	}
	for _, src := range append([]string{query}, extra...) {
		folded := foldSearch(src)
		for _, m := range searchDocNumberRe.FindAllString(folded, -1) {
			addCode(m, searchWeightDocNumber)
		}
		for _, m := range searchNumYearRe.FindAllString(folded, -1) {
			addCode(m, searchWeightReference)
		}
		for _, m := range searchReferenceRe.FindAllString(folded, -1) {
			addCode(strings.Join(strings.Fields(m), " "), searchWeightReference)
		}
		for _, c := range queryCodes(src) {
			w := searchWeightAbbrev
			if strings.IndexFunc(c, unicode.IsDigit) >= 0 {
				w = searchWeightUnitCode
			}
			f := foldSearch(strings.Trim(c, "-–/"))
			if isDigits(f) {
				continue // a bare number is weighed below
			}
			addCode(f, w)
		}
		for _, m := range searchFigureRe.FindAllStringSubmatch(folded, -1) {
			if m[2] == "%" {
				addCode(m[1]+"%", searchWeightFigure)
			} else {
				addCode(m[1]+" "+m[2], searchWeightFigure)
			}
		}
		for _, m := range searchNumberRe.FindAllString(folded, -1) {
			addCode(m, searchWeightNumber)
		}

		var prev string
		for _, w := range searchWords(src) {
			f := foldSearch(w)
			stop := searchStopWords[w] || (f == w && searchFoldedStopWords[f]) // typed without diacritics
			if utf8.RuneCountInString(w) < 2 || stop {
				prev = ""
				continue
			}
			if prev != "" {
				q.pairs = append(q.pairs, [2]string{prev, f})
			}
			prev = f
			if seenTerm[w] || isDigits(w) {
				continue
			}
			seenTerm[w] = true
			q.terms = append(q.terms, searchTerm{lower: w, folded: f, accented: f != w})
		}
	}
	return q
}

func isDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

// unitIndex is the per-unit data the scorer compares.
type unitIndex struct {
	lower, folded map[string]int
	pairs         map[[2]string]bool
	text          string // folded full text, for codes
	header        map[string]bool
	headerText    string
	runes         int
}

func indexSearchUnit(u searchUnit) unitIndex {
	ix := unitIndex{lower: map[string]int{}, folded: map[string]int{}, pairs: map[[2]string]bool{}, header: map[string]bool{}}
	var prev string
	for _, w := range searchWords(u.Text) {
		f := foldSearch(w)
		ix.lower[w]++
		ix.folded[f]++
		if prev != "" {
			ix.pairs[[2]string{prev, f}] = true
		}
		prev = f
	}
	for _, w := range searchWords(u.Header) {
		ix.header[w] = true
		ix.header[foldSearch(w)] = true
	}
	ix.text = " " + foldSearch(u.Text) + " "
	ix.headerText = " " + foldSearch(u.Header) + " "
	ix.runes = utf8.RuneCountInString(u.Text)
	return ix
}

// containsCode reports whether code occurs in text not glued to a letter or
// digit on either side ("pa05" is not in "pa051").
func containsCode(text, code string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], code)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(code)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		from = start + 1
	}
}

func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// qualifies tells a unit that holds enough of the query from one that
// only shares a syllable with it (Vietnamese words are syllables: "tư" in
// "đầu tư" is also in "Tự do"). A unit holding a strong code (số ký hiệu,
// reference, unit code, figure) always qualifies; when the query has such
// a code the others need half its terms (two at least); otherwise a third
// of the terms (one at least), a term typed with diacritics that matches
// only without them counting half.
func (q *searchQuery) qualifies(matchedW float64, anyCode, strongHit bool) bool {
	nt := float64(len(q.terms))
	switch {
	case strongHit:
		return true
	case q.strong():
		return matchedW >= math.Max(2, 0.5*nt)
	case anyCode && (nt == 0 || matchedW >= 0.5):
		return true
	}
	return matchedW >= math.Max(1, 0.34*nt)
}

// strong reports whether the query names a strong code.
func (q *searchQuery) strong() bool {
	for _, c := range q.codes {
		if c.weight >= searchStrongCode {
			return true
		}
	}
	return false
}

// rankSearchUnits scores units against q and returns the matching ones,
// best first (ties in document order).
func rankSearchUnits(q *searchQuery, units []searchUnit) []searchHit {
	if q == nil || q.empty() || len(units) == 0 {
		return nil
	}
	idx := make([]unitIndex, len(units))
	df := map[string]int{}
	for i, u := range units {
		idx[i] = indexSearchUnit(u)
		for _, t := range q.terms {
			if idx[i].folded[t.folded] > 0 {
				df[t.folded]++
			}
		}
	}
	n := float64(len(units))
	idf := func(folded string) float64 { return math.Log(1 + n/(float64(df[folded])+0.5)) }

	var hits []searchHit
	for i, u := range units {
		ix := idx[i]
		score, matched, matchedW := 0.0, 0, 0.0
		for _, t := range q.terms {
			tf, w := ix.lower[t.lower], 1.0
			if tf == 0 || !t.accented {
				if f := ix.folded[t.folded]; f > tf {
					if t.accented {
						w = searchFoldedOnly
					}
					tf = f
				}
			}
			if tf > 0 {
				matched++
				matchedW += w
				score += idf(t.folded) * w * (1 + 0.5*float64(min(tf-1, 2)))
			}
			if ix.header[t.lower] || (!t.accented && ix.header[t.folded]) {
				score += searchWeightHeader * idf(t.folded)
			}
		}
		for _, p := range q.pairs {
			if ix.pairs[p] {
				score += searchWeightPhrase * (idf(p[0]) + idf(p[1])) / 2
			}
		}
		var codes []string
		strongHit := false
		for _, c := range q.codes {
			inText, inHeader := containsCode(ix.text, c.text), containsCode(ix.headerText, c.text)
			if inText || inHeader {
				codes = append(codes, c.text)
				strongHit = strongHit || c.weight >= searchStrongCode
				w := c.weight
				if !inText {
					w *= 0.5
				} else if inHeader {
					w *= 1.25
				}
				score += w
			}
		}
		if !q.qualifies(matchedW, len(codes) > 0, strongHit) {
			continue
		}
		if len(q.terms) > 0 {
			score *= 0.5 + float64(matched)/float64(len(q.terms))
		}
		score /= 1 + 0.15*math.Log2(1+float64(ix.runes)/400)
		if score < searchMinScore {
			continue
		}
		h := searchHit{Pos: i, Index: u.Index, Doc: u.Doc, Score: score, Prev: -1, Next: -1, Codes: codes}
		if i > 0 && units[i-1].Doc == u.Doc {
			h.Prev = i - 1
		}
		if i+1 < len(units) && units[i+1].Doc == u.Doc {
			h.Next = i + 1
		}
		hits = append(hits, h)
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	return hits
}

// searchDocuments is the scorer's entry point: rank units against the query
// (and extra terms).
func searchDocuments(query string, extra []string, units []searchUnit) []searchHit {
	return rankSearchUnits(parseSearchQuery(query, extra...), units)
}

// passageTerms picks the words of a passage worth searching other
// documents with: its codes and figures, and its longer words. Cross-checking
// a paragraph against the sources searches with these, not only with the
// user's words.
func passageTerms(text string, limit int) []string {
	q := parseSearchQuery(text)
	var out []string
	for _, c := range q.codes {
		out = append(out, c.text)
	}
	for _, t := range q.terms {
		if len(out) >= limit {
			break
		}
		if utf8.RuneCountInString(t.lower) >= 3 {
			out = append(out, t.lower)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
