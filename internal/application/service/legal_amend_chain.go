package service

import (
	"regexp"
	"strings"
)

// Amendments of an amending document: C changes the provision of B that
// wrote into A ("sửa đổi khoản 2 Điều 1 Nghị định B", where that khoản
// rewrites khoản 5 Điều 8 of A). The change is traced back to A (plan
// §4.7: "Sửa của văn bản sửa đổi → lần về A") as a derived edge, always
// tier 2 and labelled LegalAmendSourceChain; what cannot be traced
// mechanically stays an UNRESOLVED edge on A's provision.

var innerQuoteRe = regexp.MustCompile(`[“"]([^”"]{3,})[”"]`)

// TraceLegalAmendChain derives C → A edges from C's edges on B (cEdges,
// Target = B) and B's edges on A carrying their Origin in B (bEdges).
func TraceLegalAmendChain(cEdges, bEdges []LegalAmendEdge) []LegalAmendEdge {
	var out []LegalAmendEdge
	for _, c := range cEdges {
		if c.Rejected != "" || c.Provision.Article == "" {
			continue
		}
		for _, b := range bEdges {
			if b.Rejected != "" || b.Origin.Article == "" || b.Amender.CatalogID == "" ||
				b.Amender.CatalogID != c.Target.CatalogID || !c.Provision.normalized().covers(b.Origin.normalized()) {
				continue
			}
			d := LegalAmendEdge{
				Op: LegalOpKhac, Amender: c.Amender, Target: b.Target, Provision: b.Provision, NewLabel: b.NewLabel,
				Origin: c.Origin, EffFrom: c.EffFrom, Tier: LegalTierUnverified,
				Sources: append(append([]string(nil), c.Sources...), LegalAmendSourceChain),
				LeadIn:  c.LeadIn, LeadInText: c.LeadInText, ChunkHash: c.ChunkHash,
				TextWithheld: c.TextWithheld, Unresolved: c.Unresolved,
			}
			via := b.Origin.String() + " " + b.Amender.label() + " (văn bản sửa đổi) bị " + string(c.Op) + " bởi " + c.Amender.label()
			switch {
			case c.phrase() && b.carriesText() && b.Op != LegalOpThayCumTu && len(findPhrase(b.QuoteText, c.From)) > 0:
				// the words were written into A by B's quoted text
				d.Op, d.From, d.To, d.Provision = c.Op, c.From, c.To, legalChainProvision(b)
				d.NewLabel = ""
			case c.phrase() && b.phrase() && b.To != "" && samePhrase(b.To, c.From):
				// B put b.To into A; C changes those words
				d.Op, d.From, d.To = c.Op, b.To, c.To
			case c.Op == LegalOpSuaDoi && c.QuoteText != "":
				inner := innerQuoteRe.FindAllStringSubmatch(c.QuoteText, -1)
				if len(inner) == 1 && b.carriesText() && b.Op != LegalOpThayCumTu {
					d.Op, d.QuoteText = b.Op, strings.TrimSpace(inner[0][1])
				} else if d.Unresolved == "" {
					d.Unresolved = via + "; không tách được lời văn mới cho văn bản gốc"
				}
			default:
				if d.Unresolved == "" {
					d.Unresolved = via + "; cần đọc văn bản " + c.Amender.label()
				}
			}
			out = append(out, d)
		}
	}
	return out
}

// legalChainProvision is the provision of A a B edge writes (a bo_sung
// writes its new label).
func legalChainProvision(b LegalAmendEdge) LegalProvisionRef {
	if b.Op == LegalOpBoSung && b.NewLabel != "" {
		return legalAddedProvision(b.Provision, b.NewLabel)
	}
	return b.Provision
}
