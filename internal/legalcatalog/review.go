package legalcatalog

import (
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// SameIssue reports whether two catalogue rows of one số hiệu are the same
// document: same type class, same issue day, issuers that agree. The title
// is left out on purpose: the portal's second copy often has a bare title
// ("Thông tư 25/2026/TT-BGDĐT"), which SameDocument cannot match.
func SameIssue(a, b *types.LegalDocument) bool {
	return a.IssuedOn != nil && b.IssuedOn != nil &&
		a.IssuedOn.Format("2006-01-02") == b.IssuedOn.Format("2006-01-02") &&
		vietnamese_legal.DocTypeClass(a.DocType) == vietnamese_legal.DocTypeClass(b.DocType) &&
		IssuersAgree(a.Issuer, b.Issuer)
}

// ReviewDiff returns the portal values of the fields a reviewer curates
// that differ from the reviewed row, keyed by JSON field name.
func ReviewDiff(cur, in *types.LegalDocument) map[string]string {
	diff := map[string]string{}
	cmp := func(name, a, b string) {
		if strings.TrimSpace(a) != strings.TrimSpace(b) {
			diff[name] = b
		}
	}
	day := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format("2006-01-02")
	}
	cmp("document_number", cur.DocumentNumber, in.DocumentNumber)
	cmp("title", cur.Title, in.Title)
	cmp("doc_type", cur.DocType, in.DocType)
	cmp("issuer", cur.Issuer, in.Issuer)
	cmp("issued_on", day(cur.IssuedOn), day(in.IssuedOn))
	cmp("effective_on", day(cur.EffectiveOn), day(in.EffectiveOn))
	cmp("expired_on", day(cur.ExpiredOn), day(in.ExpiredOn))
	cmp("status", cur.Status, in.Status)
	return diff
}

// KeepReviewedWhereBlank fills the curated fields the portal left blank in
// in from the reviewed row: a blank is missing data, not a correction.
func KeepReviewedWhereBlank(in, cur *types.LegalDocument) {
	text := func(in *string, cur string) {
		if strings.TrimSpace(*in) == "" {
			*in = cur
		}
	}
	if strings.TrimSpace(in.DocumentNumber) == "" {
		in.DocumentNumber, in.NumberKey = cur.DocumentNumber, cur.NumberKey
	}
	text(&in.Title, cur.Title)
	text(&in.DocType, cur.DocType)
	text(&in.Issuer, cur.Issuer)
	text(&in.Status, cur.Status)
	day := func(in **time.Time, cur *time.Time) {
		if *in == nil {
			*in = cur
		}
	}
	day(&in.IssuedOn, cur.IssuedOn)
	day(&in.EffectiveOn, cur.EffectiveOn)
	day(&in.ExpiredOn, cur.ExpiredOn)
}
