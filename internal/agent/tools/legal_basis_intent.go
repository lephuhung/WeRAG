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
// says ("căn cứ pháp lý để xử phạt … là gì", "cách viện dẫn … như thế
// nào"), about a draft law ("dự thảo Luật Đất đai") or a provision ("điều 5
// còn hiệu lực không"), or a "căn cứ" that is not a legal basis ("căn cứ
// tính thuế", "căn cứ khoa học") is not one.
//
// A request that is nothing but the check is "pure": the report alone
// answers it. One that also asks something else ("… và cho biết mức phạt
// nồng độ cồn", a named law) still runs the check but keeps the grounding
// gate: the rest needs a search.

// LegalBasisIntent is what ClassifyLegalBasisRequest made of a request.
type LegalBasisIntent struct {
	// Check: the request asks to check the open document's legal basis.
	Check bool
	// Pure: the check is all it asks.
	Pure bool
}

var (
	// the document itself: "văn bản này", "của tài liệu", "trong tờ trình",
	// "vb2" (a bare "dự thảo" is not: it names draft laws too)
	legalBasisDocRe = regexp.MustCompile(`\b(van ban|tai lieu|du thao|ban thao|to trinh|file|tep) (nay|dang mo|dang soan|hien tai)\b|` +
		`\b(cua|trong) (van ban|tai lieu|du thao|ban thao|to trinh|file|tep|vb ?\d+)\b|\bvb ?\d+\b`)
	// the căn cứ part of the document: "phần căn cứ"
	legalBasisSectionRe = regexp.MustCompile(`\bphan can cu\b`)
	// the documents it cites: "văn bản viện dẫn", "căn cứ pháp lý được dẫn",
	// "các văn bản được nêu"
	legalBasisCitedRe = regexp.MustCompile(`\b(van ban|can cu|can cu phap ly)( duoc| da)? (vien dan|dan chieu|trich dan|dan|neu|neu ra|nhac den|de cap)\b|\bvien dan\b|\bdan chieu\b`)
	// "căn cứ", "căn cứ pháp lý", "căn cứ ban hành"
	legalBasisTermRe = regexp.MustCompile(`\bcan cu( phap ly| ban hanh| phap luat)?\b`)
	// asking for a check
	legalBasisVerbRe = regexp.MustCompile(`\b(ra soat|soat|kiem tra|doi chieu|xem lai|xem giup|xem ho|danh gia|check)\b`)
	// asking whether they are still in force (a bare "hiệu lực" is not:
	// "hiệu lực hồi tố")
	legalBasisStillRe = regexp.MustCompile(`\b(con|het) hieu luc\b`)
	// "các căn cứ": the list of căn cứ a draft opens with
	legalBasisPluralRe = regexp.MustCompile(`\b(cac|nhung) can cu\b`)
	// a named law: a số hiệu ("30/2020/NĐ-CP", "1461/QĐ-TTg") or a type with
	// a number or a year ("Nghị định 30", "Luật Đất đai 2013", "Thông tư 01")
	legalBasisNamedLawRe = regexp.MustCompile(`\d+/(\d{4}/)?[a-z]|\b(luat|nghi dinh|thong tu|quyet dinh|nghi quyet|chi thi|phap lenh|bo luat|nd|tt|qd)\b[^,.;?!]{0,40}?\b\d{1,4}\b`)
	// what makes it a question about the law or something else:
	//   - a question form ("là gì", "như thế nào", "quy định", "cách …");
	//   - a date or term ("đến khi nào"): the document itself answers it;
	//   - a provision ("điều 5", "khoản 2");
	//   - a draft law ("dự thảo Luật Đất đai");
	//   - another check ("chính tả");
	//   - a "căn cứ" that is not a legal basis ("căn cứ tính thuế", "căn cứ
	//     khoa học", "căn cứ để xử phạt", "căn cứ vào").
	legalBasisBailRe = regexp.MustCompile(`\b(la gi|nghia la|nhu the nao|the nao|ra sao|quy dinh|cach)\b|` +
		`\b(khi nao|ngay nao|bao gio|bao lau|thoi han|tu ngay|den ngay)\b|` +
		`\b(dieu|khoan|diem) \d+|` +
		`\bdu thao (luat|bo luat|nghi dinh|thong tu|quyet dinh|nghi quyet|phap lenh|chi thi)\b|` +
		`\bchinh ta\b|` +
		`\bcan cu( phap ly)? (de|vao|theo|khoa hoc|thuc tien|tinh|xac dinh|thuc te|kinh te|ky thuat)\b`)
	legalBasisHandleRe = regexp.MustCompile(`^vb\d+$`)
)

// legalBasisFillerWords are the words a pure check is made of: the check,
// the document, politeness ("rà soát giúp tôi căn cứ pháp lý của văn bản
// này nhé"). Anything else is another question.
var legalBasisFillerWords = map[string]bool{
	"cua": true, "trong": true, "cho": true, "giup": true, "ho": true, "toi": true, "minh": true, "em": true,
	"anh": true, "chi": true, "di": true, "nhe": true, "nha": true, "voi": true, "van": true, "ban": true,
	"tai": true, "lieu": true, "du": true, "thao": true, "nay": true, "dang": true, "mo": true, "soan": true,
	"hien": true, "cac": true, "nhung": true, "duoc": true, "vien": true, "dan": true, "trich": true,
	"chieu": true, "neu": true, "nhac": true, "den": true, "de": true, "cap": true,
	"con": true, "hieu": true, "luc": true, "het": true, "chua": true, "khong": true, "hay": true, "xem": true,
	"lai": true, "phap": true, "ly": true, "hanh": true, "luat": true, "va": true, "to": true, "trinh": true,
	"file": true, "tep": true, "co": true, "da": true, "nao": true, "can": true, "cu": true,
	"ra": true, "soat": true, "kiem": true, "tra": true, "doi": true, "danh": true, "gia": true, "check": true,
	"lam": true, "on": true, "a": true, "ok": true, "phan": true, "doan": true, "tat": true, "ca": true,
	"toan": true, "bo": true, "tung": true, "mot": true, "vui": true, "long": true,
}

// legalBasisFold lower-cases, strips diacritics (đ → d) and turns
// punctuation other than "/" and "-" into spaces.
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

// legalBasisAllFiller reports whether every word of f is a filler word or a
// document handle.
func legalBasisAllFiller(f string) bool {
	for _, w := range strings.Fields(f) {
		if !legalBasisFillerWords[w] && !legalBasisHandleRe.MatchString(w) {
			return false
		}
	}
	return true
}

// ClassifyLegalBasisRequest reads a request: whether it asks to check the
// legal basis of the open document (every document it cites) and whether
// that is all it asks. mentioned is how many documents the turn named
// with @ (they point at a document as "văn bản này" does).
func ClassifyLegalBasisRequest(query string, mentioned int) LegalBasisIntent {
	f := legalBasisFold(query)
	if f == "" || strings.Contains(f, "the thuc") || legalBasisBailRe.MatchString(f) {
		// a format check has its own route (check_document_format)
		return LegalBasisIntent{}
	}
	section := legalBasisSectionRe.MatchString(f)
	doc := legalBasisDocRe.MatchString(f) || mentioned > 0 || section
	// a named law with nothing pointing at the document is a lookup
	// ("các văn bản viện dẫn trong Nghị định 15/2020/NĐ-CP …")
	named := legalBasisNamedLawRe.MatchString(f)
	if named && !doc {
		return LegalBasisIntent{}
	}
	cited := legalBasisCitedRe.MatchString(f)
	verb := legalBasisVerbRe.MatchString(f)
	still := legalBasisStillRe.MatchString(f)
	term := legalBasisTermRe.MatchString(f)
	pure := !named && legalBasisAllFiller(f)
	check := false
	switch {
	case cited && (verb || still):
		// "các văn bản viện dẫn còn hiệu lực không", "rà soát hiệu lực các
		// văn bản được nêu"
		check = true
	case doc && still:
		// "kiểm tra văn bản này còn hiệu lực không" (answered with its
		// citations' statuses), "phần căn cứ có văn bản nào đã hết hiệu lực"
		check = true
	case term && (doc || legalBasisPluralRe.MatchString(f)) && (verb || still):
		// "các căn cứ của dự thảo còn hiệu lực không"
		check = true
	case term && (verb || still) && pure:
		// "rà soát (giúp tôi) căn cứ pháp lý", "có căn cứ nào hết hiệu lực
		// không"
		check = true
	}
	if !check {
		return LegalBasisIntent{}
	}
	return LegalBasisIntent{Check: true, Pure: pure}
}

// IsLegalBasisCheckRequest reports whether query asks to check the legal
// basis of the open document (ClassifyLegalBasisRequest, no @-mentions).
func IsLegalBasisCheckRequest(query string) bool {
	return ClassifyLegalBasisRequest(query, 0).Check
}
