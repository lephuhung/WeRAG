package legalcatalog

import (
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// ResolveTwins matches an import against the catalogue so that one
// document written two ways ("02/2000/QÐ-BTC" with the Eth look-alike and
// "02/2000/QĐ-BTC", two vbpl ids) never becomes two rows under one key.
// existing holds the catalogue rows (id, number_key, doc_type, title,
// issued_on are read); docs are the import's rows, numbers already cleaned
// (CleanVBPLNumber) and keyed (CatalogNumberKey). A row is matched by id
// first, otherwise as the same document: same key, type class, issue day
// and title (CatalogTitleKey).
//
//   - A new id that is the same document as a catalogued or earlier
//     imported row is not imported: alias maps it to that row.
//   - A catalogued row whose key differs from the import's only by
//     look-alikes while another row holds the folded key (a twin migration
//     000175 left with its Ð key for a SuperAdmin decision) keeps its
//     number and key, so the import does not revive it under the shared
//     key; the rest of the row is updated. When it is the same document as
//     one of the key's rows, alias maps it there.
//
// Edges at an aliased id belong to its survivor (the import re-points
// them). keep is docs without the aliased new ids, in order.
func ResolveTwins(existing, docs []*types.LegalDocument) (keep []*types.LegalDocument, alias map[string]string) {
	byID := make(map[string]*types.LegalDocument, len(existing))
	byKey := map[string][]*types.LegalDocument{}
	for _, e := range existing {
		byID[e.ID] = e
		// a row still keyed with a look-alike is not a holder of the
		// folded key
		if e.NumberKey != "" && vietnamese_legal.CatalogNumberKey(e.NumberKey) == e.NumberKey {
			byKey[e.NumberKey] = append(byKey[e.NumberKey], e)
		}
	}
	alias = map[string]string{}
	keep = docs[:0:0]
	for _, d := range docs {
		cur := byID[d.ID]
		if cur == nil {
			if s := sameDocument(byKey[d.NumberKey], d); s != nil {
				alias[d.ID] = s.ID
				continue
			}
		} else if cur.NumberKey != d.NumberKey && vietnamese_legal.CatalogNumberKey(cur.NumberKey) == d.NumberKey &&
			holdsOther(byKey[d.NumberKey], d.ID) {
			d.DocumentNumber, d.NumberKey = cur.DocumentNumber, cur.NumberKey
			if s := sameDocument(byKey[vietnamese_legal.CatalogNumberKey(cur.NumberKey)], d); s != nil {
				alias[d.ID] = s.ID
			}
			keep = append(keep, d)
			continue
		}
		keep = append(keep, d)
		if d.NumberKey != "" && (cur == nil || cur.NumberKey != d.NumberKey) {
			byKey[d.NumberKey] = append(byKey[d.NumberKey], d)
		}
	}
	return keep, alias
}

// sameDocument returns the first of rows that is d under another id: same
// type class, the same known issue day and the same title key.
func sameDocument(rows []*types.LegalDocument, d *types.LegalDocument) *types.LegalDocument {
	for _, r := range rows {
		if r.ID != d.ID && r.IssuedOn != nil && d.IssuedOn != nil &&
			r.IssuedOn.Format("2006-01-02") == d.IssuedOn.Format("2006-01-02") &&
			vietnamese_legal.DocTypeClass(r.DocType) == vietnamese_legal.DocTypeClass(d.DocType) &&
			vietnamese_legal.CatalogTitleKey(r.Title) == vietnamese_legal.CatalogTitleKey(d.Title) {
			return r
		}
	}
	return nil
}

func holdsOther(rows []*types.LegalDocument, id string) bool {
	for _, r := range rows {
		if r.ID != id {
			return true
		}
	}
	return false
}
