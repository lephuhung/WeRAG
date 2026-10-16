package types

import (
	"strings"
	"unicode/utf8"
)

// DocumentTemplateRequest asks for a new NĐ30 document drafted from chat
// into an empty Word document (the Word add-in's contract
// docs/backend-document-template-contract.md). Follow-up turns of one
// request carry the same RequestID; the answer is the structured
// prepare_document_template result, never document ops.
type DocumentTemplateRequest struct {
	SchemaVersion int    `json:"schema_version"`
	RequestID     string `json:"request_id"`
	TemplateID    string `json:"template_id"`
	DocumentID    string `json:"document_id"`
}

// DocumentTemplateToolName is the tool name of the structured answer.
const DocumentTemplateToolName = "prepare_document_template"

// DocumentTemplateNames are the document types the add-in can build, by
// template ID.
var DocumentTemplateNames = map[string]string{
	"cong-van":   "Công văn",
	"ke-hoach":   "Kế hoạch",
	"bao-cao":    "Báo cáo",
	"to-trinh":   "Tờ trình",
	"thong-bao":  "Thông báo",
	"quyet-dinh": "Quyết định",
}

// Normalized returns the request when it is well formed (schema 1, a known
// template, IDs of sane length), else nil.
func (r *DocumentTemplateRequest) Normalized() *DocumentTemplateRequest {
	if r == nil || r.SchemaVersion != 1 {
		return nil
	}
	out := DocumentTemplateRequest{
		SchemaVersion: 1,
		RequestID:     strings.TrimSpace(r.RequestID),
		TemplateID:    strings.TrimSpace(r.TemplateID),
		DocumentID:    strings.TrimSpace(r.DocumentID),
	}
	if _, ok := DocumentTemplateNames[out.TemplateID]; !ok {
		return nil
	}
	for _, id := range []string{out.RequestID, out.DocumentID} {
		if id == "" || utf8.RuneCountInString(id) > 128 || strings.ContainsAny(id, " \t\r\n") {
			return nil
		}
	}
	return &out
}
