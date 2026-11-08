package legalcatalog

import (
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

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
