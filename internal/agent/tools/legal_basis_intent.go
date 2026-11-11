package tools

import (
	"regexp"
	"strings"
	"unicode"
)

// Requests to check the legal basis of the open document ("rà soát căn cứ
// pháp lý", "các văn bản viện dẫn còn hiệu lực không"). The knowledge-lookup
// classifier reads them as legal questions and the turn starts with
// search_knowledge, where the model answers from search hits instead of
// checking every citation against the catalogue. A request this matcher
// recognises starts with check_document_legal_basis instead (the agent's
// firstRoundToolChoice). It is deliberately narrow: a question about one
// law ("Nghị định 30/2020/NĐ-CP còn hiệu lực không"), about what the law
// says ("căn cứ pháp lý để xử phạt … là gì") or about a law discussed
// earlier ("luật này còn hiệu lực không") is not one.

var (
	// the document itself: "văn bản này", "dự thảo", "của tài liệu", "vb2"
	legalBasisDocRe = regexp.MustCompile(`\b(van ban|tai lieu|du thao|ban thao|to trinh|file|tep) (nay|dang mo|dang soan|hien tai)\b|` +
		`\b(cua|trong) (van ban|tai lieu|du thao|ban thao|to trinh|file|tep|vb ?\d+)\b|\bvb ?\d+\b|\bdu thao\b`)
	// the documents it cites: "văn bản viện dẫn", "căn cứ pháp lý được dẫn",
	// "các văn bản được trích dẫn"
	legalBasisCitedRe = regexp.MustCompile(`\b(van ban|can cu|can cu phap ly)( duoc| da)? (vien dan|dan chieu|trich dan|dan)\b|\bvien dan\b|\bdan chieu\b`)
	// "căn cứ", "căn cứ pháp lý", "căn cứ ban hành"
	legalBasisTermRe = regexp.MustCompile(`\bcan cu( phap ly| ban hanh| phap luat)?\b`)
	// asking for a check
	legalBasisVerbRe = regexp.MustCompile(`\b(ra soat|soat|kiem tra|doi chieu|xem lai|xem giup|xem ho|danh gia|check)\b`)
	// asking about validity
	legalBasisValidityRe = regexp.MustCompile(`\b(con|het|chua co|con co) hieu luc\b|\bhieu luc\b`)
	legalBasisStillRe    = regexp.MustCompile(`\b(con|het) hieu luc\b`)
	// "các căn cứ": the list of căn cứ a draft opens with
	legalBasisPluralRe = regexp.MustCompile(`\b(cac|nhung) can cu\b`)
	// a named law: a số hiệu ("30/2020/NĐ-CP", "1461/QĐ-TTg") or a type with
	// a number or a year ("Nghị định 30", "Luật Đất đai 2013")
	legalBasisNamedLawRe = regexp.MustCompile(`\d+/(\d{4}/)?[a-z]|\b(luat|nghi dinh|thong tu|quyet dinh|nghi quyet|chi thi|phap lenh|bo luat|nd|tt|qd)\b[^,.;?!]{0,40}?\b\d{1,4}\b`)
	// a question about a date or a term ("văn bản này còn hiệu lực đến khi
	// nào"): the document itself answers it
	legalBasisDateQuestionRe = regexp.MustCompile(`\b(khi nao|ngay nao|bao gio|bao lau|thoi han|tu ngay|den ngay)\b`)
	// "rà soát (lại) (các) căn cứ" with nothing else after it
	legalBasisVerbTermRe = regexp.MustCompile(`\b(ra soat|soat|kiem tra|doi chieu|xem lai|danh gia|check)( lai)?( (cac|nhung))? can cu\b`)
	legalBasisHandleRe   = regexp.MustCompile(`^vb\d+$`)
)

// legalBasisFillerWords may follow "rà soát căn cứ (pháp lý)" in a request
// that names nothing else: "… của văn bản này giúp tôi nhé".
var legalBasisFillerWords = map[string]bool{
	"cua": true, "trong": true, "cho": true, "giup": true, "ho": true, "toi": true, "minh": true, "em": true,
	"anh": true, "chi": true, "di": true, "nhe": true, "nha": true, "voi": true, "van": true, "ban": true,
	"tai": true, "lieu": true, "du": true, "thao": true, "nay": true, "dang": true, "mo": true, "soan": true,
	"hien": true, "cac": true, "nhung": true, "duoc": true, "vien": true, "dan": true, "trich": true,
	"con": true, "hieu": true, "luc": true, "het": true, "chua": true, "khong": true, "hay": true, "xem": true,
	"lai": true, "phap": true, "ly": true, "hanh": true, "luat": true, "va": true, "to": true, "trinh": true,
	"file": true, "tep": true, "co": true, "da": true, "nao": true, "gi": true, "can": true, "cu": true,
	"ra": true, "soat": true, "kiem": true, "tra": true, "lam": true, "on": true, "a": true, "ok": true,
}

// legalBasisFold lower-cases, strips diacritics (đ → d) and turns
// punctuation other than "/" into spaces.
func legalBasisFold(s string) string {
	f := foldSearch(s)
	f = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '/' || r == '-' {
			return r
		}
		return ' '
	}, f)
	return strings.Join(strings.Fields(f), " ")
}

// IsLegalBasisCheckRequest reports whether query asks to check the legal
// basis of the open document (every document it cites) rather than to
// look up one law or what the law says.
func IsLegalBasisCheckRequest(query string) bool {
	f := legalBasisFold(query)
	if f == "" || strings.Contains(f, "the thuc") {
		// a format check has its own route (check_document_format)
		return false
	}
	doc := legalBasisDocRe.MatchString(f) || strings.Contains(query, "@")
	cited := legalBasisCitedRe.MatchString(f)
	// a named law with nothing pointing at the document is a lookup
	if legalBasisNamedLawRe.MatchString(f) && !doc && !cited {
		return false
	}
	switch {
	case cited && legalBasisValidityRe.MatchString(f):
		// "các văn bản viện dẫn còn hiệu lực không"
		return true
	case cited && legalBasisVerbRe.MatchString(f):
		// "kiểm tra các văn bản được viện dẫn"
		return true
	case doc && legalBasisStillRe.MatchString(f) && !legalBasisNamedLawRe.MatchString(f) &&
		!legalBasisDateQuestionRe.MatchString(f):
		// "kiểm tra văn bản này còn hiệu lực không"
		return true
	case legalBasisTermRe.MatchString(f) && (doc || legalBasisPluralRe.MatchString(f)) &&
		(legalBasisVerbRe.MatchString(f) || legalBasisValidityRe.MatchString(f)):
		// "các căn cứ của dự thảo còn hiệu lực không"
		return true
	}
	// "rà soát căn cứ pháp lý (giúp tôi)": the verb right before the term
	// and nothing but filler after it
	loc := legalBasisVerbTermRe.FindStringIndex(f)
	if loc == nil {
		return false
	}
	for _, w := range strings.Fields(f[loc[1]:]) {
		if !legalBasisFillerWords[w] && !legalBasisHandleRe.MatchString(w) {
			return false
		}
	}
	return true
}
