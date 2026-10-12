package vbpl

import (
	"regexp"
	"strings"
	"unicode"
)

// Amend is the provision a quoted span writes into another document:
// "3. Sửa đổi, bổ sung Điều 6 như sau: “Điều 6. …”" inside "Điều 1. Sửa
// đổi, bổ sung một số điều của Luật Phòng, chống thiên tai" amends Điều 6
// of Luật Phòng, chống thiên tai. Fields are empty when not stated.
type Amend struct {
	// Document names the amended document as the text gives it ("Luật
	// Phòng, chống thiên tai", "Nghị định số 100/2016/NĐ-CP").
	Document string `json:"document,omitempty"`
	Article  string `json:"article,omitempty"`
	Khoan    string `json:"khoan,omitempty"`
	Diem     string `json:"diem,omitempty"`
}

var (
	// amendVerbRe marks a lead-in that introduces amending text.
	amendVerbRe = regexp.MustCompile(`(?i)sửa đổi|bổ sung|thay thế|thay cụm|thay từ|được sửa|như sau`)
	// amendedDocRe captures the amended document named after của/tại/vào;
	// it stops before the issue date, "như sau", a colon or a parenthesis.
	amendedDocRe = regexp.MustCompile(`(?i)(?:của|tại|vào)\s+((?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định|Chỉ thị|Hiến pháp)\s[^:;(“"]*?)(?:\s+ngày\s+\d|\s+được sửa đổi|\s+đã được|\s+như sau|\s*[:;(“"]|\s*$)`)
	// clauseTargetRe finds "[điểm X] [khoản N] Điều M" in a lead-in.
	clauseTargetRe = regexp.MustCompile(`(?i)(?:điểm\s+([a-zđ])\s*,?\s+)?(?:khoản\s+(\d+[a-zđ]?)\s*,?\s+)?Điều\s+(\d+[a-zđ]?)`)
	khoanOnlyRe    = regexp.MustCompile(`(?i)khoản\s+(\d+[a-zđ]?)`)
	diemOnlyRe     = regexp.MustCompile(`(?i)điểm\s+([a-zđ])(?:[\s,;.)]|$)`)
	// quotedArticleRe / quotedClauseRe read the label the quoted text starts with.
	quotedArticleRe = regexp.MustCompile(`^[“"]?\s*Điều\s+(\d+[a-zđ]?)`)
	quotedClauseRe  = regexp.MustCompile(`^[“"]?\s*(\d+[a-zđ]?)\.\s`)
	quotedItemRe    = regexp.MustCompile(`^[“"]?\s*([a-zđ])\)\s`)
	// ownLabelRe strips a heading's own label ("Điều 1.", "3.", "a)").
	ownLabelRe = regexp.MustCompile(`^(?:Điều\s+\d+[a-zđ]?\s*[.:]?|\d+[a-zđ]?\.|[a-zđ]\))\s*`)
)

// amendTarget works out what the quoted span opened by opener amends, from
// the opener's own label and the lead-ins around it: the paragraph before
// the quote, the enclosing điểm, khoản and Điều headings, and finally the
// document title. It returns nil when no lead-in reads as an amendment.
func (b *builder) amendTarget(opener string) *Amend {
	var chain []string
	blocks := b.doc.Blocks
	if n := len(blocks); n > 0 && blocks[n-1].Kind == KindContent {
		chain = append(chain, blocks[n-1].Text)
	}
	for _, i := range []int{b.item, b.clause, b.article} {
		if i >= 0 {
			// the enclosing Điều's own label is not a target
			chain = append(chain, ownLabelRe.ReplaceAllString(blocks[i].Text, ""))
		}
	}
	if len(chain) == 0 || !amendVerbRe.MatchString(chain[0]) {
		return nil
	}
	a := &Amend{}
	opener = strings.TrimSpace(opener)
	if m := quotedArticleRe.FindStringSubmatch(opener); m != nil {
		a.Article = m[1]
	}
	// The nearest lead-in naming a Điều fixes the article; khoản / điểm come
	// with it or from a nearer lead-in that names only them.
	var nearKhoan, nearDiem string
	for _, t := range chain {
		if a.Article == "" {
			if m := clauseTargetRe.FindStringSubmatch(t); m != nil {
				a.Article, a.Khoan, a.Diem = m[3], m[2], strings.ToLower(m[1])
				if a.Khoan == "" {
					a.Khoan = nearKhoan
				}
				if a.Diem == "" {
					a.Diem = nearDiem
				}
			}
		}
		if a.Article == "" {
			if nearKhoan == "" {
				if m := khoanOnlyRe.FindStringSubmatch(t); m != nil {
					nearKhoan = m[1]
				}
			}
			if nearDiem == "" {
				if m := diemOnlyRe.FindStringSubmatch(t); m != nil {
					nearDiem = strings.ToLower(m[1])
				}
			}
		}
		if a.Document == "" {
			a.Document = amendedDocument(t)
		}
	}
	switch {
	case a.Article != "" && quotedArticleRe.MatchString(opener):
		a.Khoan, a.Diem = "", ""
	case a.Article != "":
		// The quote's own label wins when the lead-in names it: "Bổ sung
		// khoản 6 vào sau khoản 5 Điều 5" quoted as “6. …” adds khoản 6.
		if m := quotedClauseRe.FindStringSubmatch(opener); m != nil && mentions(chain[0], "khoản", m[1]) {
			a.Khoan, a.Diem = m[1], ""
		} else if m := quotedItemRe.FindStringSubmatch(opener); m != nil && mentions(chain[0], "điểm", m[1]) {
			a.Diem = strings.ToLower(m[1])
		}
	}
	if a.Document == "" {
		a.Document = b.titleAmendedDocument()
	}
	if a.Article == "" && a.Document == "" {
		return nil
	}
	return a
}

func amendedDocument(t string) string {
	m := amendedDocRe.FindStringSubmatch(t)
	if m == nil {
		return ""
	}
	return sentenceCase(strings.TrimRight(strings.TrimSpace(m[1]), " ,."))
}

// sentenceCase turns an all-caps name from a title ("NGHỊ ĐỊNH SỐ
// 100/2016/NĐ-CP") into "Nghị định số 100/2016/NĐ-CP", keeping the số hiệu.
func sentenceCase(s string) string {
	if !isUpperHeading(s) {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		if strings.ContainsAny(w, "0123456789/") {
			continue
		}
		w = strings.ToLower(w)
		if i == 0 {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			w = string(r)
		}
		words[i] = w
	}
	return strings.Join(words, " ")
}

// titleAmendedDocument reads the amended document from the title of an
// amending document ("Sửa đổi, bổ sung một số điều của Nghị định số
// 160/2007/NĐ-CP …"), when the title names exactly one.
func (b *builder) titleAmendedDocument() string {
	for _, blk := range b.doc.Blocks {
		if blk.Kind != KindPreamble {
			continue
		}
		if !amendVerbRe.MatchString(blk.Text) {
			continue
		}
		if doc := amendedDocument(blk.Text); doc != "" && !strings.Contains(strings.ToLower(doc), " và ") {
			return doc
		}
	}
	return ""
}

// mentions reports whether t names "<keyword> <label>" ("khoản 6").
func mentions(t, keyword, label string) bool {
	re := regexp.MustCompile(`(?i)` + keyword + `\s+` + regexp.QuoteMeta(label) + `(?:[\s,;.)]|$)`)
	return re.MatchString(t)
}
