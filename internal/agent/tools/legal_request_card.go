package tools

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// A question that names a full số hiệu the system holds no full text of is
// answered in code, without a model, by this card: the document is
// missing and a request to add it went to the SuperAdmin queue (see
// service.legalMissingDocumentGate). A fully expired document is not
// requested: the card says so and points at what replaced it.

// LegalRequestType is the display_type (and pipeline tool name) of the card.
const LegalRequestType = "legal_document_request"

// Modes of the legal request card.
const (
	LegalRequestModeRequested = "requested"
	LegalRequestModeRejected  = "rejected"
	LegalRequestModeExpired   = "expired"
)

// LegalRequestCard is the card: the số hiệu as written, the catalogue
// documents it names and what happened to the request.
type LegalRequestCard struct {
	Mode string
	// Reference is the số hiệu as the user wrote it.
	Reference string
	// DocType is the type word the user wrote before it ("Nghị định"), if any.
	DocType   string
	Documents []*types.LegalDocument
	// RequestCount is the stored request's count (requested mode).
	RequestCount int
	DecisionNote string
	// Replacement is the document replacing an expired one, if known.
	Replacement *types.LegalDocument
}

// LegalDocTypeBefore returns the catalogue doc type whose word ends the
// text before a số hiệu ("Nghị định số " → "Nghị định"), or "".
func LegalDocTypeBefore(prefix string) string {
	p := strings.ToLower(strings.TrimSpace(prefix))
	p = strings.TrimSpace(strings.TrimSuffix(p, "số"))
	for _, t := range legalTypeWords {
		if strings.HasSuffix(p, t.word) {
			return t.docType
		}
	}
	return ""
}

func legalRequestDoc(d *types.LegalDocument) map[string]interface{} {
	return map[string]interface{}{
		"id":              d.ID,
		"document_number": d.DocumentNumber,
		"title":           d.Title,
		"doc_type":        d.DocType,
		"issuer":          d.Issuer,
		"status":          d.Status,
		"issued_on":       legalISODate(d.IssuedOn),
		"url":             vietnamese_legal.VBPLDocumentURL + d.ID,
	}
}

// Data is the card payload (display_type legal_document_request).
func (c *LegalRequestCard) Data() map[string]interface{} {
	docs := make([]map[string]interface{}, 0, len(c.Documents))
	for _, d := range c.Documents {
		docs = append(docs, legalRequestDoc(d))
	}
	data := map[string]interface{}{
		"display_type":  LegalRequestType,
		"mode":          c.Mode,
		"reference":     c.Reference,
		"doc_type":      c.DocType,
		"documents":     docs,
		"request_count": c.RequestCount,
		"decision_note": c.DecisionNote,
	}
	if c.Replacement != nil {
		data["replacement"] = map[string]interface{}{
			"id":              c.Replacement.ID,
			"document_number": c.Replacement.DocumentNumber,
			"title":           c.Replacement.Title,
			"doc_type":        c.Replacement.DocType,
			"url":             vietnamese_legal.VBPLDocumentURL + c.Replacement.ID,
		}
	}
	return data
}

// label names the document: "Nghị định 53/2022/NĐ-CP".
func (c *LegalRequestCard) label() string {
	docType, number := c.DocType, c.Reference
	if len(c.Documents) > 0 {
		if docType == "" {
			docType = c.Documents[0].DocType
		}
		if n := strings.TrimSpace(c.Documents[0].DocumentNumber); n != "" {
			number = n
		}
	}
	return strings.TrimSpace(docType + " " + number)
}

// Message is the answer shown with the card (and alone in clients without it).
func (c *LegalRequestCard) Message() string {
	label := c.label()
	var title string
	if len(c.Documents) == 1 {
		title = strings.TrimSpace(c.Documents[0].Title)
	}
	named := label
	if title != "" {
		named += " (" + title + ")"
	}
	switch c.Mode {
	case LegalRequestModeExpired:
		text := named + " đã hết hiệu lực toàn bộ"
		if len(c.Documents) == 1 && c.Documents[0].ExpiredOn != nil {
			text += " từ ngày " + c.Documents[0].ExpiredOn.Format("02/01/2006")
		}
		text += " theo danh mục văn bản pháp luật, nên không được đưa vào kho tri thức."
		if r := c.Replacement; r != nil {
			repl := strings.TrimSpace(r.DocType + " " + r.DocumentNumber)
			if t := strings.TrimSpace(r.Title); t != "" {
				repl += " (" + t + ")"
			}
			text += " Văn bản thay thế là " + repl + "; bạn có thể hỏi về văn bản này."
		}
		return text
	case LegalRequestModeRejected:
		text := "Hệ thống chưa có toàn văn " + named + ". Đề nghị bổ sung văn bản này đã được quản trị viên xem xét và chưa được chấp nhận"
		if note := strings.TrimSpace(c.DecisionNote); note != "" {
			text += ": " + strings.TrimRight(note, ".") + "."
		} else {
			text += "."
		}
		return text
	default:
		count := ""
		if c.RequestCount > 1 {
			count = fmt.Sprintf(" (lần thứ %d)", c.RequestCount)
		}
		return "Hệ thống chưa có toàn văn " + named + ". Đã ghi nhận đề nghị bổ sung" + count +
			" tới quản trị viên; khi văn bản được bổ sung, bạn hỏi lại sẽ có câu trả lời."
	}
}
