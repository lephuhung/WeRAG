// Package legalcatalog turns vbpl.vn snapshots into the platform-wide legal
// catalogue (legal_documents / legal_document_relations).
package legalcatalog

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"golang.org/x/text/unicode/norm"
)

// vbplRelation maps a vbpl.vn relation label to the catalogue relation.
// A row (doc, other, label) on the portal is listed from doc's page. The
// action labels ("Thay thế", "Bãi bỏ", …) say what doc does to other; the
// "Văn bản …" labels give other's role towards doc, so most of them are
// reversed. Checked on the 2026-07 snapshot by pairing each edge with the
// label shown on the other document's page (e.g. "Thay thế" ↔ "Văn bản quy
// định hết hiệu lực" 28k times).
var vbplRelations = map[string]struct {
	rel     types.LegalRelation
	reverse bool
}{
	"Thay thế":             {types.LegalRelationReplaces, false},
	"Bãi bỏ":               {types.LegalRelationRepeals, false},
	"Văn bản hết hiệu lực": {types.LegalRelationExpires, false},
	"Văn bản quy định hết hiệu lực":         {types.LegalRelationExpires, true},
	"Văn bản bị hết hiệu lực 1 phần":        {types.LegalRelationPartiallyExpires, false},
	"Văn bản quy định hết hiệu lực 1 phần":  {types.LegalRelationPartiallyExpires, true},
	"Tạm ngưng hiệu lực":                    {types.LegalRelationSuspends, false},
	"Đình chỉ thi hành":                     {types.LegalRelationSuspends, false},
	"Văn bản bị đình chỉ":                   {types.LegalRelationSuspends, false},
	"Văn bản bị đình chỉ 1 phần":            {types.LegalRelationSuspends, false},
	"Văn bản đình chỉ":                      {types.LegalRelationSuspends, true},
	"Văn bản đình chỉ 1 phần":               {types.LegalRelationSuspends, true},
	"Sửa đổi, bổ sung":                      {types.LegalRelationAmends, false},
	"Văn bản được sửa đổi":                  {types.LegalRelationAmends, false},
	"Văn bản được bổ sung":                  {types.LegalRelationAmends, false},
	"Văn bản sửa đổi":                       {types.LegalRelationAmends, true},
	"Văn bản bổ sung":                       {types.LegalRelationAmends, true},
	"Quy định chi tiết, hướng dẫn thi hành": {types.LegalRelationGuides, false},
	"Hướng dẫn áp dụng":                     {types.LegalRelationGuides, false},
	"Văn bản được HD, QĐ chi tiết":          {types.LegalRelationGuides, false},
	"Văn bản HD, QĐ chi tiết":               {types.LegalRelationGuides, true},
	"Giải thích":                            {types.LegalRelationInterprets, false},
	"Hợp nhất":                              {types.LegalRelationConsolidates, false},
	"Đính chính":                            {types.LegalRelationCorrects, false},
	"Căn cứ":                                {types.LegalRelationBasedOn, false},
	"Văn bản căn cứ":                        {types.LegalRelationBasedOn, false},
	"Dẫn chiếu":                             {types.LegalRelationReferences, false},
	"Văn bản dẫn chiếu":                     {types.LegalRelationReferences, false},
	"Văn bản liên quan khác":                {types.LegalRelationRelated, false},
}

// VBPLRelationEdge turns one vbpl.vn row (doc listed the label for other)
// into a directed catalogue edge from → to. ok is false for labels that
// carry no legal effect ("Bản dịch", "Công bố") or are unknown.
func VBPLRelationEdge(docID, otherID, label string) (from, to string, rel types.LegalRelation, ok bool) {
	m, found := vbplRelations[strings.TrimSpace(norm.NFC.String(label))]
	if !found {
		return "", "", "", false
	}
	if m.reverse {
		return otherID, docID, m.rel, true
	}
	return docID, otherID, m.rel, true
}

// CleanVBPLNumber strips the wording vbpl.vn sometimes leaves around a số
// ký hiệu ("số 99/2025/QH15", "Luật số: 118/2025/QH15", "'41/2025/NQ-HĐND").
// Values the number parser cannot read (old "01 /CT-UB", "Không số") are
// kept as written. The look-alike "Ð" of old rows becomes "Đ", and a
// dotted number keeps its prefix ("66.12/2026/NQ-CP", not "12/2026/NQ-CP").
func CleanVBPLNumber(raw string) string {
	raw = strings.NewReplacer("Ð", "Đ", "ð", "đ").Replace(raw)
	if cits := vietnamese_legal.FindCitations(raw); len(cits) > 0 &&
		cits[0].Kind == vietnamese_legal.CitationFull && strings.Contains(cits[0].No, ".") {
		return cits[0].Number
	}
	if n := vietnamese_legal.NormalizeOwnDocumentNumber(vietnamese_legal.RecoverDocumentNumber(raw)); n != "" {
		return n
	}
	return strings.TrimSpace(raw)
}
