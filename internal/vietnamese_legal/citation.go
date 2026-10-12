package vietnamese_legal

import (
	"net/url"
	"strings"
)

// VBPLDocumentURL is the public vbpl.vn page of a document; ?go=<provision
// id> scrolls it to one Điều / khoản / điểm.
const VBPLDocumentURL = "https://vbpl.vn/van-ban/chi-tiet/"

// maxCitedAmends bounds the amended provisions named in one citation.
const maxCitedAmends = 5

// Citation names where the chunk sits in its document, the way a lawyer
// cites it: "khoản 2 Điều 14", "điểm a khoản 2 Điều 14", "Điều 1 (khoản 3,
// 4, 5)", plus the phụ lục / attached document and, for amending text, the
// provisions it amends: "khoản 6 Điều 1 (sửa đổi Điều 9 Luật Phòng, chống
// thiên tai)". Empty when the chunk carries no structure.
func (m *LegalMetadata) Citation() string {
	if m == nil {
		return ""
	}
	var cite string
	switch {
	case len(m.ArticleNos) == 0:
	case len(m.ArticleNos) > 1:
		cite = "Điều " + strings.Join(m.ArticleNos, ", ")
	case len(m.KhoanNos) == 1 && len(m.DiemLabels) == 1:
		cite = "điểm " + m.DiemLabels[0] + " khoản " + m.KhoanNos[0] + " Điều " + m.ArticleNos[0]
	case len(m.KhoanNos) == 1:
		cite = "khoản " + m.KhoanNos[0] + " Điều " + m.ArticleNos[0]
	case len(m.KhoanNos) > 1:
		cite = "Điều " + m.ArticleNos[0] + " (khoản " + strings.Join(m.KhoanNos, ", ") + ")"
	default:
		cite = "Điều " + m.ArticleNos[0]
	}
	if m.Appendix != "" {
		if cite == "" {
			cite = m.Appendix
		} else {
			cite += " – " + m.Appendix
		}
	}
	if amended := amendsText(m.Amends); amended != "" && cite != "" {
		cite += " (sửa đổi " + amended + ")"
	}
	return cite
}

// amendsText lists amended provisions, naming each document once after its
// run of provisions: "khoản 1 Điều 3, Điều 6 Luật Phòng, chống thiên tai".
func amendsText(amends []AmendRef) string {
	var groups []string
	var clauses []string
	doc := ""
	flush := func() {
		if len(clauses) == 0 && doc == "" {
			return
		}
		g := strings.Join(clauses, ", ")
		if doc != "" {
			g = strings.TrimSpace(g + " " + doc)
		}
		groups = append(groups, g)
		clauses, doc = nil, ""
	}
	for i, a := range amends {
		if i == maxCitedAmends {
			flush()
			groups = append(groups, "…")
			return strings.Join(groups, "; ")
		}
		if i > 0 && a.Document != doc {
			flush()
		}
		doc = a.Document
		if c := (AmendRef{Article: a.Article, Khoan: a.Khoan, Diem: a.Diem}).String(); c != "" {
			clauses = append(clauses, c)
		}
	}
	flush()
	return strings.Join(groups, "; ")
}

// SourceURL deep-links the chunk on its source portal: the vbpl.vn page with
// ?go= at the narrowest provision the chunk holds alone (the điểm, else the
// khoản, else the Điều). Empty when the chunk has no vbpl source.
func (m *LegalMetadata) SourceURL() string {
	if m == nil || m.Source != "vbpl" || m.SourceDocID == "" {
		return ""
	}
	base := VBPLDocumentURL + url.PathEscape(m.SourceDocID)
	if id := m.provisionID(); id != "" {
		return base + "?go=" + url.QueryEscape(id)
	}
	return base
}

func (m *LegalMetadata) provisionID() string {
	if len(m.ProvisionIDs) == 0 || len(m.ArticleNos) > 1 {
		return ""
	}
	if len(m.KhoanNos) == 1 {
		key := "khoan:" + m.KhoanNos[0]
		if len(m.DiemLabels) == 1 {
			if id := m.ProvisionIDs[key+"/diem:"+m.DiemLabels[0]]; id != "" {
				return id
			}
		}
		if id := m.ProvisionIDs[key]; id != "" {
			return id
		}
	}
	return m.ProvisionIDs["dieu"]
}

// SectionURL deep-links one khoản / điểm of the chunk's Điều (a lookup of
// "khoản 2 Điều 14" in a chunk holding khoản 1–3); empty when the chunk has
// no vbpl id for it.
func (m *LegalMetadata) SectionURL(khoan, diem string) string {
	if m == nil || m.Source != "vbpl" || m.SourceDocID == "" || khoan == "" {
		return ""
	}
	key := "khoan:" + khoan
	if diem != "" {
		key += "/diem:" + diem
	}
	id := m.ProvisionIDs[key]
	if id == "" {
		return ""
	}
	return VBPLDocumentURL + url.PathEscape(m.SourceDocID) + "?go=" + url.QueryEscape(id)
}
