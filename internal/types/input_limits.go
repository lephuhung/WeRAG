package types

// Input gates: page caps an upload must pass before it is parsed, so one
// file cannot keep the parser or the OCR model busy for minutes. The caps
// are runtime system settings (SuperAdmin → limits) read on every upload.
// A knowledge-base upload is parsed in the background and may be large; a
// chat attachment or a document-assistant file is parsed while the user
// waits, so it is capped tightly. 0 disables a cap.
const (
	// Knowledge-base uploads.
	SettingLimitsKBPDFTextPages = "limits.kb.pdf_text_pages"
	SettingLimitsKBPDFScanPages = "limits.kb.pdf_scan_pages"
	SettingLimitsKBDocxPages    = "limits.kb.docx_pages"
	// Chat attachments and document-assistant files.
	SettingLimitsChatPDFTextPages = "limits.chat.pdf_text_pages"
	SettingLimitsChatPDFScanPages = "limits.chat.pdf_scan_pages"
	SettingLimitsChatDocxPages    = "limits.chat.docx_pages"

	DefaultLimitsKBPDFTextPages   = 500
	DefaultLimitsKBPDFScanPages   = 200
	DefaultLimitsKBDocxPages      = 300
	DefaultLimitsChatPDFTextPages = 20
	DefaultLimitsChatPDFScanPages = 6
	DefaultLimitsChatDocxPages    = 15

	// MinParsedTextRunes is the least text a parsed upload must yield to be
	// usable: below it (an unreadable scan, a photo, an empty file) the
	// upload is marked failed instead of ready, so the user learns at once
	// that nothing was read rather than asking about it in vain.
	MinParsedTextRunes = 50
)

// InputGateContext says which caps apply to an upload.
type InputGateContext string

const (
	InputGateKnowledgeBase InputGateContext = "kb"
	InputGateChat          InputGateContext = "chat"
)

// Reasons carried in the error envelope's details.reason so a client can
// tell the refusals apart without parsing the message.
const (
	InputGateReasonTooManyPages = "too_many_pages"
	InputGateReasonNoText       = "no_text"
)

// InputGateDetails is the details object of a page-cap refusal.
type InputGateDetails struct {
	Reason string `json:"reason"`
	// Kind is pdf_text, pdf_scan or docx.
	Kind  string `json:"kind"`
	Pages int    `json:"pages"`
	Limit int    `json:"limit"`
	// Estimated is set when the DOCX page count was derived from its text.
	Estimated bool `json:"estimated,omitempty"`
}
