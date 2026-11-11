package tools

import (
	"context"
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
// A check is "pure" unless the request also asks something else (a named
// law, "… và cho biết mức phạt", "tóm tắt", a second question): the report
// alone answers a pure one. An impure one still runs the check but keeps
// the grounding gate: the rest needs a search.

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
	// a document named by its kind: "căn cứ pháp lý của quyết định này"
	// (only next to "căn cứ": "quyết định này còn hiệu lực không" may mean
	// a decision discussed earlier)
	legalBasisTypedDocRe = regexp.MustCompile(`\b(quyet dinh|cong van|bao cao|ke hoach|thong bao|de an|nghi quyet|hop dong|to trinh|chuong trinh|phuong an) nay\b`)
	// the căn cứ part of the document: "phần căn cứ"
	legalBasisSectionRe = regexp.MustCompile(`\bphan can cu\b`)
	// the documents it cites: "văn bản viện dẫn", "căn cứ pháp lý được dẫn",
	// "các văn bản được nêu"
	legalBasisCitedRe = regexp.MustCompile(`\b(van ban|can cu|can cu phap ly)( duoc| da)? (vien dan|dan chieu|trich dan|dan|neu|neu ra|nhac den|de cap)\b|\bvien dan\b|\bdan chieu\b`)
	// "căn cứ", "căn cứ pháp lý", "căn cứ ban hành" (legalBasisTermOther
	// rejects a "căn cứ" of something else: "căn cứ ly hôn", "căn cứ tính
	// thuế")
	legalBasisTermRe = regexp.MustCompile(`\bcan cu( phap ly| ban hanh| phap luat)?\b`)
	// asking for a check
	legalBasisVerbRe = regexp.MustCompile(`\b(ra soat|soat|kiem tra|doi chieu|xem lai|xem giup|xem ho|danh gia|check)\b`)
	// asking to underline the citations: "gạch chân các văn bản hết hiệu lực"
	legalBasisMarkRe = regexp.MustCompile(`\b(gach chan|danh dau)\b.*\b(van ban|vien dan|can cu)\b`)
	// asking whether they are still in force (a bare "hiệu lực" is not:
	// "hiệu lực hồi tố")
	legalBasisStillRe = regexp.MustCompile(`\b(con|het) hieu luc\b`)
	// "hiệu lực văn bản (này)", "hiệu lực các căn cứ" after a check verb
	legalBasisValidityOfRe = regexp.MustCompile(`\bhieu luc (cua )?(cac )?(van ban|can cu)\b`)
	legalBasisValidityRe   = regexp.MustCompile(`\bhieu luc\b`)
	// a named law: a số hiệu ("30/2020/NĐ-CP", "1461/QĐ-TTg") or a type with
	// a number or a year ("Nghị định 30", "Luật Đất đai 2013", "Thông tư 01")
	legalBasisNamedLawRe = regexp.MustCompile(`\d+/(\d{4}/)?[a-z]|\b(luat|nghi dinh|thong tu|quyet dinh|nghi quyet|chi thi|phap lenh|bo luat|nd|tt|qd)\b[^,.;?!]{0,40}?\b\d{1,4}\b`)
	// what makes it a question about the law or something else:
	//   - a question form ("là gì", "như thế nào", "quy định", "cách …");
	//   - a date or term ("đến khi nào"): the document itself answers it;
	//   - a provision ("điều 5", "khoản 2");
	//   - a draft law ("dự thảo Luật Đất đai");
	//   - a legal situation ("… thì áp dụng văn bản nào", "có vi phạm",
	//     "bị xử phạt", "xử lý", "áp dụng");
	//   - "hiệu lực hồi tố";
	//   - a question about citing ("có được / có nên viện dẫn", "trách
	//     nhiệm", "tài liệu này nói gì", "làm hết hiệu lực những văn bản nào").
	legalBasisBailRe = regexp.MustCompile(`\b(la gi|nghia la|nhu the nao|the nao|ra sao|quy dinh|cach)\b|` +
		`\b(khi nao|ngay nao|bao gio|bao lau|thoi han|tu ngay|den ngay)\b|` +
		`\b(dieu|khoan|diem) \d+|` +
		`\bdu thao (luat|bo luat|nghi dinh|thong tu|quyet dinh|nghi quyet|phap lenh|chi thi)\b|` +
		`\b(khi|neu)\b.*\bthi\b|\bthi (ap dung|xu ly|sao|phai)\b|\bco vi pham\b|\bbi xu phat\b|\bxu ly\b|\bap dung\b|` +
		`\bhieu luc hoi to\b|\b(cua|cho) viec\b|` +
		`\b(co nen|co duoc|duoc khong|trach nhiem|noi gi|lam het hieu luc|co hop le|co sao khong)\b`)
	// a spelling request bails unless a legal-basis phrase is there too
	// ("kiểm tra căn cứ pháp lý … rồi sửa lỗi chính tả": a check, not pure)
	legalBasisSpellingRe = regexp.MustCompile(`\bchinh ta\b`)
	legalBasisStrongRe   = regexp.MustCompile(`\bcan cu phap ly\b|\bvien dan\b|\bdan chieu\b|\b(con|het) hieu luc\b`)
	// "… còn hiệu lực không" about something that is not a legal document:
	// a contract's or a licence's own term
	legalBasisNonLegalObjRe = regexp.MustCompile(`\b(hop dong|giay phep|giay chung nhan|chung chi|chung nhan|ho chieu|bao hiem|cccd|cmnd|can cuoc|giay to|so do|bang lai|the)\b`)
	// another question beside the check: not pure
	legalBasisOtherRe = regexp.MustCompile(`\b(tom tat|giai thich|so sanh|muc phat|bao nhieu|chinh ta|phan tich|dich sang|thu tuc|tham quyen)\b|` +
		`\b(va|roi|dong thoi|sau do|ngoai ra|kem)\b.*\b(cho biet|soan|viet|sua loi|sua lai|sua chinh ta|dich|tra cuu|tim|tinh)\b`)
	legalBasisHandleRe = regexp.MustCompile(`^vb\d+$`)
)

// legalBasisTermOther are the words that, right after "căn cứ (pháp lý)"
// in the same clause, make it a "căn cứ" of something else: grounds for a
// divorce, a penalty, a tax ("căn cứ ly hôn", "căn cứ xử phạt", "căn cứ
// tính thuế", "căn cứ để …"). Anything else — "cho tôi", "văn bản này", the
// end of the clause — is the document's căn cứ.
var legalBasisTermOther = []string{
	"ly hon", "xu phat", "tinh", "khoi kien", "khoi to", "khang cao", "khang nghi", "mien", "ap dung",
	"cham dut", "sa thai", "doi", "cap", "thanh lap", "khoa hoc", "thuc tien", "thuc te", "de", "vao",
	"theo", "cho viec", "ve", "xac dinh", "ket luan", "bo tri", "tuyen", "huy", "giai quyet", "boi thuong",
}

// legalBasisFillerWords are the words a bare check is made of: the check,
// the document, politeness ("có căn cứ nào hết hiệu lực không").
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
	// thanks, listing what the check found
	"cam": true, "thank": true, "thanks": true, "you": true, "nhieu": true, "xin": true, "liet": true, "ke": true,
	"bi": true, "thay": true, "the": true, "sua": true, "sung": true, "bai": true, "ngung": true,
	"deu": true, "nua": true, "luon": true,
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

// legalBasisTermOK reports whether no "căn cứ" of the folded clause c is
// followed, in the clause, by a word of legalBasisTermOther.
func legalBasisTermOK(c string) bool {
	for _, loc := range legalBasisTermRe.FindAllStringIndex(c, -1) {
		rest := " " + strings.TrimSpace(c[loc[1]:]) + " "
		for _, w := range legalBasisTermOther {
			if strings.HasPrefix(rest, " "+w+" ") {
				return false
			}
		}
	}
	return true
}

// legalBasisClauseRe cuts a request into clauses at punctuation (before
// folding, which drops it): "Kiểm tra căn cứ pháp lý. Cảm ơn".
var legalBasisClauseRe = regexp.MustCompile(`[.,;:!?…\n]+|\s[–—-]\s`)

// legalBasisRelated reports whether a folded clause belongs to the check:
// it carries a check word, the căn cứ, the cited documents, their validity
// or the document.
func legalBasisRelated(c string, mentioned int) bool {
	return legalBasisVerbRe.MatchString(c) || legalBasisTermRe.MatchString(c) || legalBasisCitedRe.MatchString(c) ||
		legalBasisValidityRe.MatchString(c) || legalBasisDocRe.MatchString(c) || legalBasisTypedDocRe.MatchString(c) ||
		legalBasisSectionRe.MatchString(c) || legalBasisMarkRe.MatchString(c) || mentioned > 0 && strings.Contains(c, "@")
}

// ClassifyLegalBasisRequest reads a request: whether it asks to check the
// legal basis of the open document (every document it cites) and whether
// that is all it asks. mentioned is how many documents the user named with
// @ in this turn (they point at a document as "văn bản này" does).
func ClassifyLegalBasisRequest(query string, mentioned int) LegalBasisIntent {
	all := legalBasisFold(query)
	if all == "" || strings.Contains(all, "the thuc") || legalBasisBailRe.MatchString(all) {
		// a format check has its own route (check_document_format); a
		// question about the law anywhere in the request goes to the search
		return LegalBasisIntent{}
	}
	if legalBasisSpellingRe.MatchString(all) && !legalBasisStrongRe.MatchString(all) {
		return LegalBasisIntent{}
	}
	// the clauses of the check, and whether another clause asks something
	// else (a clause of thanks or politeness does not)
	var related []string
	other := false
	for _, raw := range legalBasisClauseRe.Split(query, -1) {
		c := legalBasisFold(raw)
		switch {
		case c == "":
		case legalBasisRelated(c, mentioned):
			if !legalBasisTermOK(c) {
				return LegalBasisIntent{}
			}
			related = append(related, c)
		case !legalBasisAllFiller(c):
			other = true
		}
	}
	if len(related) == 0 {
		return LegalBasisIntent{}
	}
	f := strings.Join(related, " ")
	doc := legalBasisDocRe.MatchString(f) || mentioned > 0 || legalBasisSectionRe.MatchString(f)
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
	// "hợp đồng / giấy phép … còn hiệu lực": the object is no legal document
	nonLegal := !term && !cited && legalBasisNonLegalObjRe.MatchString(f)
	check := false
	switch {
	case nonLegal:
	case cited && (verb || still):
		// "các văn bản viện dẫn còn hiệu lực không", "rà soát hiệu lực các
		// văn bản được nêu"
		check = true
	case doc && still && !term:
		// "kiểm tra văn bản này còn hiệu lực không" (answered with its
		// citations' statuses), "thông tư trong văn bản này còn hiệu lực
		// không" — not "hợp đồng trong file này còn hiệu lực không"
		check = true
	case verb && (doc && legalBasisValidityRe.MatchString(f) || legalBasisValidityOfRe.MatchString(f)):
		// "kiểm tra hiệu lực văn bản này", "rà soát hiệu lực văn bản"
		check = true
	case term && verb:
		// "rà soát (giúp tôi) căn cứ pháp lý", "nhờ bạn kiểm tra các căn cứ
		// pháp lý trong văn bản", "tóm tắt dự thảo và kiểm tra căn cứ"
		check = true
	case term && still && (doc || legalBasisTypedDocRe.MatchString(f) || legalBasisAllFiller(f)):
		// "căn cứ pháp lý của quyết định này còn hiệu lực không", "có căn
		// cứ nào hết hiệu lực không", "phần căn cứ có văn bản nào hết hiệu lực"
		check = true
	case term && (doc || legalBasisTypedDocRe.MatchString(f)) && legalBasisValidityRe.MatchString(f):
		check = true
	case legalBasisMarkRe.MatchString(f) && still:
		// "gạch chân các văn bản hết hiệu lực" (the card underlines on a click)
		check = true
	case term && legalBasisTypedDocRe.MatchString(f) && legalBasisAllFillerBut(f, legalBasisTypedDocRe):
		// "căn cứ pháp lý của quyết định này"
		check = true
	}
	if !check {
		return LegalBasisIntent{}
	}
	pure := !named && !other && !legalBasisOtherRe.MatchString(all)
	return LegalBasisIntent{Check: true, Pure: pure}
}

// legalBasisAllFillerBut is legalBasisAllFiller once re's matches are cut.
func legalBasisAllFillerBut(f string, re *regexp.Regexp) bool {
	return legalBasisAllFiller(re.ReplaceAllString(f, " "))
}

// IsLegalBasisCheckRequest reports whether query asks to check the legal
// basis of the open document (ClassifyLegalBasisRequest, no @-mentions).
func IsLegalBasisCheckRequest(query string) bool {
	return ClassifyLegalBasisRequest(query, 0).Check
}

type explicitMentionsKey struct{}

// WithExplicitMentions records how many documents the user named with @
// in this turn (attachments that name an open tab are not counted).
func WithExplicitMentions(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, explicitMentionsKey{}, n)
}

// ExplicitMentionsFromContext is the count WithExplicitMentions recorded, 0.
func ExplicitMentionsFromContext(ctx context.Context) int {
	n, _ := ctx.Value(explicitMentionsKey{}).(int)
	return n
}
