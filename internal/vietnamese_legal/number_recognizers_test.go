package vietnamese_legal

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// The số hiệu regexes that still live outside FindCitations, run next to it
// over one corpus so every difference is pinned (F1 step 0, 2026-11-11).
// FindCitations is the shared recogniser; the callers below were left on
// their own regex on purpose and a change to one of them must show here.
// The literals are copies — keep them in step with their sources:
var (
	// internal/application/service/legal_amendment_history.go legalDocNumberRe
	// (amendment history amendedNumber, LegalValidityNote): any four-digit
	// middle, no letter needed, no dash variants, no look-alike folding.
	// Kept: the validity note searches the answer for the raw match again
	// (remarksExpiry), and the history keys amendments by it; moving them
	// to FindCitations changes which numbers they see (68-2026-NĐ-CP,
	// "TT- BNNPTNT", Eth "NÐ") — an improvement to make with its own tests,
	// not a refactor.
	corpusAmendHistoryRe = regexp.MustCompile(`\d+/\d{4}/[A-Za-zĐđ0-9\-]+`)
	// internal/agent/tools/document_template_facts.go factDocNumberRe: masks
	// references so their digits are not read as dates or facts; it is
	// broader on purpose (year-less "123/UBND" without a hyphen, any
	// letters after the year).
	corpusTemplateFactRe = regexp.MustCompile(`\d+/\d{4}/[\p{L}Đđ-]+|\d+/[\p{Lu}Đ][\p{L}Đđ-]*`)
	// internal/agent/tools/document_search.go searchDocNumberRe: runs on the
	// search fold (lower case, no marks, đ→d) and makes search tokens, also
	// of partial numbers; a different job.
	corpusSearchRe = regexp.MustCompile(`\d+[a-z]?(?:/\d{2,4})*/[a-z][a-z0-9]*(?:[-–][a-z0-9]+)*`)
)

// DocNumberPattern (entity.go) stays: it builds knowledge-graph merge keys
// (NormalizeEntityID) and clause scoping, where a different match would
// split or merge existing graph nodes. documentNumberRe is shared already
// (FindDocumentNumbers, CitedNumberSpans, ReplaceDocumentNumbers — the
// domain-pack formula check moved to the last one).

func corpusSearchFold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ':
			b.WriteRune('d')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var numberRecognizerCorpus = []string{
	"Nghị định số 30/2020/NĐ-CP ngày 05/3/2020",
	"Thông tư 01/2011/TT-BNV",
	"Luật Đất đai số 31/2024/QH15",
	"53/2022/NÐ-CP",
	"68-2026-NĐ-CP",
	"01/2020/TT- BNNPTNT",
	"Quyết định 1461/QĐ-TTg",
	"Nghị quyết 36-NQ/TW",
	"QCVN 06:2022/BXD",
	"ngày 15/11/2018",
	"1153/2022/NĐ-CP x 12345/2022/NĐ-CP",
	"12/2018/123",
	"Kế hoạch 45/KH-UBND",
	"53/2022/NĐ–CP",
	"30/2020/nđ-cp",
	"Công văn 123/UBND",
	"Nghị định 68",
	"Luật Đất đai 2024",
	"66.17/2026/NQ-CP",
	"44/2007/TTLT/BTC-BKH&CN",
	"Số: 12/2026/QĐ-UBND",
	"NĐ 68/2026",
	"Luật Đất đai năm 2013",
}

type numberRecognizerRun struct {
	pattern, docnum, amend, fact, search, cites []string
}

func runNumberRecognizers(s string) numberRecognizerRun {
	var cites []string
	for _, c := range FindCitations(s) {
		v := string(c.Kind) + ":" + c.Number
		if c.Kind == CitationName {
			v = string(c.Kind) + ":" + c.Name + " " + c.Year
		}
		cites = append(cites, strings.TrimSpace(v))
	}
	return numberRecognizerRun{
		pattern: DocNumberPattern.FindAllString(s, -1),
		docnum:  documentNumberRe.FindAllString(s, -1),
		amend:   corpusAmendHistoryRe.FindAllString(s, -1),
		fact:    corpusTemplateFactRe.FindAllString(s, -1),
		search:  corpusSearchRe.FindAllString(corpusSearchFold(s), -1),
		cites:   cites,
	}
}

func TestNumberRecognizersCorpus(t *testing.T) {
	// columns: DocNumberPattern | documentNumberRe | amendment history |
	// template facts | search (on the fold) | FindCitations kind:number
	cases := []struct{ in, pattern, docnum, amend, fact, search, cites string }{
		{"Nghị định số 30/2020/NĐ-CP ngày 05/3/2020", "30/2020/NĐ-CP", "30/2020/NĐ-CP", "30/2020/NĐ-CP", "30/2020/NĐ-CP", "30/2020/nd-cp", "full:30/2020/NĐ-CP"},
		{"Thông tư 01/2011/TT-BNV", "01/2011/TT-BNV", "01/2011/TT-BNV", "01/2011/TT-BNV", "01/2011/TT-BNV", "01/2011/tt-bnv", "full:01/2011/TT-BNV"},
		{"Luật Đất đai số 31/2024/QH15", "31/2024/QH15", "31/2024/QH15", "31/2024/QH15", "31/2024/QH", "31/2024/qh15", "full:31/2024/QH15"},
		{"53/2022/NÐ-CP", "53/2022/N", "53/2022/N", "53/2022/N", "53/2022/NÐ-CP", "53/2022/n", "full:53/2022/NĐ-CP"},
		{"68-2026-NĐ-CP", "", "", "", "", "", "full:68/2026/NĐ-CP"},
		{"01/2020/TT- BNNPTNT", "01/2020/TT-", "01/2020/TT", "01/2020/TT-", "01/2020/TT-", "01/2020/tt", "full:01/2020/TT-BNNPTNT"},
		{"Quyết định 1461/QĐ-TTg", "", "", "", "1461/QĐ-TTg", "1461/qd-ttg", "admin:1461/QĐ-TTg"},
		{"Nghị quyết 36-NQ/TW", "", "", "", "", "", "party:36-NQ/TW"},
		{"QCVN 06:2022/BXD", "", "", "", "2022/BXD", "2022/bxd", "standard:QCVN 06:2022/BXD"},
		{"ngày 15/11/2018", "", "", "", "", "", ""},
		{"1153/2022/NĐ-CP x 12345/2022/NĐ-CP", "1153/2022/NĐ-CP | 12345/2022/NĐ-CP", "1153/2022/NĐ-CP | 2345/2022/NĐ-CP", "1153/2022/NĐ-CP | 12345/2022/NĐ-CP", "1153/2022/NĐ-CP | 12345/2022/NĐ-CP", "1153/2022/nd-cp | 12345/2022/nd-cp", "full:1153/2022/NĐ-CP"},
		{"12/2018/123", "", "", "12/2018/123", "", "", ""},
		{"Kế hoạch 45/KH-UBND", "", "", "", "45/KH-UBND", "45/kh-ubnd", "admin:45/KH-UBND"},
		{"53/2022/NĐ–CP", "53/2022/NĐ", "53/2022/NĐ–CP", "53/2022/NĐ", "53/2022/NĐ", "53/2022/nd–cp", "full:53/2022/NĐ-CP"},
		{"30/2020/nđ-cp", "30/2020/nđ-cp", "30/2020/nđ-cp", "30/2020/nđ-cp", "30/2020/nđ-cp", "30/2020/nd-cp", "full:30/2020/NĐ-CP"},
		{"Công văn 123/UBND", "", "", "", "123/UBND", "123/ubnd", ""},
		{"Nghị định 68", "", "", "", "", "", "partial:68"},
		{"Luật Đất đai 2024", "", "", "", "", "", "name:Luật Đất đai 2024"},
		{"66.17/2026/NQ-CP", "17/2026/NQ-CP", "17/2026/NQ-CP", "17/2026/NQ-CP", "17/2026/NQ-CP", "17/2026/nq-cp", "full:66.17/2026/NQ-CP"},
		{"44/2007/TTLT/BTC-BKH&CN", "44/2007/TTLT", "44/2007/TTLT", "44/2007/TTLT", "44/2007/TTLT", "44/2007/ttlt", "full:44/2007/TTLT/BTC-BKH&CN"},
		{"Số: 12/2026/QĐ-UBND", "12/2026/QĐ-UBND", "12/2026/QĐ-UBND", "12/2026/QĐ-UBND", "12/2026/QĐ-UBND", "12/2026/qd-ubnd", "full:12/2026/QĐ-UBND"},
		{"NĐ 68/2026", "", "", "", "", "", "partial:68/2026"},
		{"Luật Đất đai năm 2013", "", "", "", "", "", "name:Luật Đất đai 2013"},
	}
	if len(cases) != len(numberRecognizerCorpus) {
		t.Fatalf("%d cases for %d corpus lines", len(cases), len(numberRecognizerCorpus))
	}
	for i, c := range cases {
		if c.in != numberRecognizerCorpus[i] {
			t.Fatalf("case %d is %q, corpus has %q", i, c.in, numberRecognizerCorpus[i])
		}
		r := runNumberRecognizers(c.in)
		for _, col := range []struct {
			name      string
			got, want string
		}{
			{"DocNumberPattern", strings.Join(r.pattern, " | "), c.pattern},
			{"documentNumberRe", strings.Join(r.docnum, " | "), c.docnum},
			{"amendment history", strings.Join(r.amend, " | "), c.amend},
			{"template facts", strings.Join(r.fact, " | "), c.fact},
			{"search", strings.Join(r.search, " | "), c.search},
			{"FindCitations", strings.Join(r.cites, " | "), c.cites},
		} {
			if col.got != col.want {
				t.Errorf("%s(%q) = %q, want %q", col.name, c.in, col.got, col.want)
			}
		}
	}
}

// ReplaceDocumentNumbers is the regex the domain-pack formula check used
// (docNumberRefPattern, a byte-for-byte copy of documentNumberRe): same
// replacement on the corpus.
func TestReplaceDocumentNumbersMatchesFormerFormulaPattern(t *testing.T) {
	former := regexp.MustCompile(`\d{1,4}/(?:19|20)\d\d/[0-9]*[A-Za-zĐđ][A-Za-zĐđ0-9]*(?:[-–—‐][A-Za-zĐđ0-9]+)*`)
	for _, s := range append(numberRecognizerCorpus, "lãi 500 triệu theo 68/2026/NĐ-CP, $1 phí 100 %") {
		if got, want := ReplaceDocumentNumbers(s, " "), former.ReplaceAllString(s, " "); got != want {
			t.Errorf("ReplaceDocumentNumbers(%q) = %q, want %q", s, got, want)
		}
	}
}
