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

	// Questions: the longest question a chat turn accepts, and whether a
	// short generic question over a large corpus ("luật đất đai") is
	// answered with a card asking the user to narrow it.
	SettingLimitsChatQuestionMaxRunes  = "limits.chat.question_max_runes"
	SettingLimitsChatBroadQuestionGate = "limits.chat.broad_question_gate"
	// Parsing: how many uploads of one user may be parsed at the same time;
	// the rest wait in the queue (see temporaryDocumentService.Process and
	// knowledgeService.ProcessDocument).
	SettingLimitsParseMaxConcurrentPerUser = "limits.parse.max_concurrent_per_user"

	DefaultLimitsChatQuestionMaxRunes      = 4000
	DefaultLimitsParseMaxConcurrentPerUser = 3

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
	// InputGateDocument is the file the document assistant opens to edit
	// (role "target"). The assistant reads it section by section, so it
	// takes the knowledge-base caps, not the tight chat-attachment ones.
	InputGateDocument InputGateContext = "document"
)

// Reasons carried in the error envelope's details.reason so a client can
// tell the refusals apart without parsing the message.
const (
	InputGateReasonTooManyPages    = "too_many_pages"
	InputGateReasonNoText          = "no_text"
	InputGateReasonQuestionTooLong = "question_too_long"
)

// QuestionLengthDetails is the details object of a too-long question.
type QuestionLengthDetails struct {
	Reason string `json:"reason"`
	Runes  int    `json:"runes"`
	Limit  int    `json:"limit"`
}

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
