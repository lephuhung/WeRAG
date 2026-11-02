package vbpl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// The "WeRAG legal HTML v1" format (docs/legal-document-feed-spec.md §9.2)
// lets a source other than vbpl.vn push a document: the prov-* classes of
// vbpl for its structure, an id on every Điều / khoản / điểm, and on each
// provision another document acts on a data-amend attribute holding a JSON
// array of AmendMark.

// LegalHTMLFormatV1 is the metadata "format" value of the v1 format.
const LegalHTMLFormatV1 = "werag-legal-html/1"

// AmendAttr is the attribute carrying the marks of a provision.
const AmendAttr = "data-amend"

// amendMaxPerProvision bounds the marks read on one provision.
const amendMaxPerProvision = 32

// AmendMark is one change another document (B) makes to the provision
// holding the mark, as the pushing source records it.
type AmendMark struct {
	Op              string `json:"op"`
	DocNumber       string `json:"doc_number,omitempty"`
	IssueDate       string `json:"issue_date,omitempty"`
	Issuer          string `json:"issuer,omitempty"`
	LegalDocumentID string `json:"legal_document_id,omitempty"`
	Path            string `json:"path,omitempty"`
	EffFrom         string `json:"eff_from,omitempty"`
	EffTo           string `json:"eff_to,omitempty"`
	Note            string `json:"note,omitempty"`
}

// AmendOpCodes maps the v1 operations to the vbpl relation codes stored in
// legal_provision_links.relation_code.
var AmendOpCodes = map[string]int{
	"sua_doi_bo_sung": EditorCodeSuaDoiBS,
	"sua_doi":         AmendCodeSuaDoi,
	"bo_sung":         EditorCodeBoSung,
	"thay_the":        EditorCodeThayThe,
	"bai_bo":          EditorCodeBaiBo,
	"tam_ngung":       EditorCodeTamNgung,
	"dinh_chi":        EditorCodeDinhChi,
	"dinh_chinh":      EditorCodeDinhChinh,
	"giai_thich":      EditorCodeGiaiThich,
	"huong_dan":       AmendCodeHuongDan,
}

// Relation codes of the v1 format beyond the editors' tags (same numbers
// as the vbpl.vn document relations).
const (
	AmendCodeHuongDan = 9  // quy định chi tiết, hướng dẫn thi hành
	AmendCodeSuaDoi   = 16 // sửa đổi
)

// AmendMarkedProvision is the marks found on one element.
type AmendMarkedProvision struct {
	ElementID string
	Marks     []AmendMark
}

// AmendMarkup is what ReadAmendMarkup found in a page.
type AmendMarkup struct {
	Provisions []AmendMarkedProvision
	// Problems are marks that could not be read (element id or reason).
	Problems []AmendMarkProblem
}

// AmendMarkProblem is one unreadable mark.
type AmendMarkProblem struct {
	ElementID string `json:"element_id"`
	Reason    string `json:"reason"`
}

// ReadAmendMarkup reads every data-amend attribute of a page, in document
// order. An element without an id, a value that is not a JSON array of
// marks, or a mark with an unknown op is reported as a problem and skipped.
func ReadAmendMarkup(raw []byte) (*AmendMarkup, error) {
	root, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	out := &AmendMarkup{}
	var visit func(n *html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Head, atom.Style, atom.Script, atom.Noscript, atom.Template:
				return
			}
			if val, ok := attrOK(n, AmendAttr); ok {
				out.read(attr(n, "id"), val)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(root)
	return out, nil
}

func (m *AmendMarkup) read(id, val string) {
	id = strings.TrimSpace(id)
	if id == "" {
		m.Problems = append(m.Problems, AmendMarkProblem{Reason: "data-amend on an element without id"})
		return
	}
	var marks []AmendMark
	if err := json.Unmarshal([]byte(val), &marks); err != nil {
		m.Problems = append(m.Problems, AmendMarkProblem{ElementID: id, Reason: "data-amend is not a JSON array of marks: " + err.Error()})
		return
	}
	var kept []AmendMark
	for i, mk := range marks {
		mk = mk.trimmed()
		if _, ok := AmendOpCodes[mk.Op]; !ok {
			m.Problems = append(m.Problems, AmendMarkProblem{ElementID: id, Reason: fmt.Sprintf("mark %d: unknown op %q", i+1, mk.Op)})
			continue
		}
		if mk.DocNumber == "" && mk.LegalDocumentID == "" {
			m.Problems = append(m.Problems, AmendMarkProblem{ElementID: id, Reason: fmt.Sprintf("mark %d: doc_number or legal_document_id is required", i+1)})
			continue
		}
		if len(kept) >= amendMaxPerProvision {
			m.Problems = append(m.Problems, AmendMarkProblem{ElementID: id, Reason: fmt.Sprintf("more than %d marks", amendMaxPerProvision)})
			break
		}
		kept = append(kept, mk)
	}
	if len(kept) > 0 {
		m.Provisions = append(m.Provisions, AmendMarkedProvision{ElementID: id, Marks: kept})
	}
}

func (mk AmendMark) trimmed() AmendMark {
	mk.Op = strings.ToLower(strings.TrimSpace(mk.Op))
	mk.DocNumber = strings.TrimSpace(mk.DocNumber)
	mk.IssueDate = strings.TrimSpace(mk.IssueDate)
	mk.Issuer = strings.TrimSpace(mk.Issuer)
	mk.LegalDocumentID = strings.TrimSpace(mk.LegalDocumentID)
	mk.Path = strings.ToLower(strings.TrimSpace(mk.Path))
	mk.EffFrom = strings.TrimSpace(mk.EffFrom)
	mk.EffTo = strings.TrimSpace(mk.EffTo)
	mk.Note = strings.TrimSpace(mk.Note)
	return mk
}

func attrOK(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}
