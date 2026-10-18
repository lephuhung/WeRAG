package types

import "time"

// LegalDocumentDraft is the reviewed-OCR path for a legal document vbpl.vn
// publishes only as a scanned PDF. The SuperAdmin's rule: a scan is never
// embedded as raw OCR. The PDF is fetched and OCRed into a draft, the
// SuperAdmin edits the draft in the embedded editor (spelling, the
// Phần/Chương/Mục/Điều structure) until it reads like an HTML original and
// approves it; only then is it converted to vbpl-style HTML (prov-* markup)
// and ingested like any HTML document. One draft per catalogue document
// (LegalDocumentID); a failed draft is deleted when fetched again.
type LegalDocumentDraft struct {
	ID string `json:"id" gorm:"type:varchar(36);primaryKey"`
	// RequestID is the request the draft was fetched for, "" when the feed
	// fetched it without one.
	RequestID       string `json:"request_id"        gorm:"type:varchar(36);not null;default:''"`
	LegalDocumentID string `json:"legal_document_id" gorm:"type:varchar(64);not null;uniqueIndex:ux_legal_document_drafts_doc"`
	DocumentNumber  string `json:"document_number"   gorm:"type:varchar(128);not null;default:''"`
	DocType         string `json:"doc_type"          gorm:"type:varchar(64);not null;default:''"`
	Title           string `json:"title"             gorm:"type:text;not null;default:''"`

	PDFFileName string `json:"pdf_file_name" gorm:"type:text;not null;default:''"`
	// PDFRef is the stored PDF (FileService reference), never served as is.
	PDFRef   string `json:"-"         gorm:"type:text;not null;default:''"`
	PDFBytes int64  `json:"pdf_bytes" gorm:"not null;default:0"`

	// OCRMarkdown is the raw OCR output, kept for audit and diff; list
	// answers leave it out.
	OCRMarkdown string `json:"ocr_markdown,omitempty" gorm:"type:text;not null;default:''"`
	OCRPages    int    `json:"ocr_pages"              gorm:"not null;default:0"`
	OCREngine   string `json:"ocr_engine"             gorm:"type:varchar(64);not null;default:''"`

	Status string `json:"status" gorm:"type:varchar(16);not null;default:'fetching';index:idx_legal_document_drafts_status"`
	Error  string `json:"error"  gorm:"type:text;not null;default:''"`

	// The hidden document-assistant session (platform organization, owned
	// by the SuperAdmin who opened the draft), its .docx attachment and the
	// document workspace the editor edits.
	SessionID    string `json:"session_id"    gorm:"type:varchar(36);not null;default:''"`
	AttachmentID string `json:"attachment_id" gorm:"type:varchar(36);not null;default:''"`
	WorkspaceID  string `json:"workspace_id"  gorm:"type:varchar(36);not null;default:''"`

	// Structure is the last LegalDraftStructure report.
	Structure JSON `json:"structure,omitempty" gorm:"type:jsonb"`

	ApprovedKnowledgeID string `json:"approved_knowledge_id" gorm:"type:varchar(36);not null;default:''"`
	ApprovedKBID        string `json:"approved_kb_id"        gorm:"column:approved_kb_id;type:varchar(36);not null;default:''"`

	CreatedBy  string     `json:"created_by"  gorm:"type:varchar(64);not null;default:''"`
	ReviewedBy string     `json:"reviewed_by" gorm:"type:varchar(64);not null;default:''"`
	ApprovedBy string     `json:"approved_by" gorm:"type:varchar(64);not null;default:''"`
	ApprovedAt *time.Time `json:"approved_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// TableName returns the table name for LegalDocumentDraft.
func (LegalDocumentDraft) TableName() string { return "legal_document_drafts" }

// Draft states.
const (
	LegalDraftFetching  = "fetching"  // the PDF is being downloaded
	LegalDraftOCR       = "ocr"       // the PDF is being OCRed
	LegalDraftReady     = "ready"     // OCR text ready, not opened yet
	LegalDraftReviewing = "reviewing" // opened in the editor
	LegalDraftApproved  = "approved"  // converted to HTML and ingested
	LegalDraftFailed    = "failed"
)

// LegalDraftSessionMarker starts the description of the hidden session a
// draft is edited in ("legal_draft:<draft id>"): no session list shows it.
const LegalDraftSessionMarker = "legal_draft:"

// LegalDraftSummary is a draft as a request row shows it.
type LegalDraftSummary struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	OCRPages int    `json:"ocr_pages"`
	Error    string `json:"error"`
}

// Summary returns the request-row view of d.
func (d *LegalDocumentDraft) Summary() *LegalDraftSummary {
	if d == nil {
		return nil
	}
	return &LegalDraftSummary{ID: d.ID, Status: d.Status, OCRPages: d.OCRPages, Error: d.Error}
}

// LegalDraftItem is one row of the draft list: the draft and the request
// it was fetched for (nil when none).
type LegalDraftItem struct {
	*LegalDocumentDraft
	Request *LegalDocumentRequest `json:"request"`
}

// LegalDraftViewer is who opens a draft's editor and from where.
type LegalDraftViewer struct {
	UserID   string
	UserName string
	// Lang is the editor language ("vi", "en").
	Lang string
	// Origin is the page that embeds the editor (the plugin's hostOrigin).
	Origin string
}

// LegalDraftOpenResult is what the review page needs to show a draft.
type LegalDraftOpenResult struct {
	Draft *LegalDocumentDraft `json:"draft"`
	// Editor is the ONLYOFFICE config; nil when the editor is not configured.
	Editor *DocumentEditorConfig `json:"editor"`
	// SessionID / DocumentID address the hidden session's workspace routes
	// (/sessions/:id/documents/:doc_id/...), called with
	// X-Tenant-ID: TenantID.
	SessionID  string `json:"session_id"`
	DocumentID string `json:"document_id"`
	TenantID   uint64 `json:"tenant_id"`
	EditorKey  string `json:"editor_key"`
	Revision   int    `json:"revision"`
	// PDFURL is a 2-hour link to the original scan (/r/<token>).
	PDFURL string `json:"pdf_url"`
}

// LegalDraftStructure is the structure report of a draft's current text.
type LegalDraftStructure struct {
	CheckedAt  time.Time `json:"checked_at"`
	Revision   int       `json:"revision"`
	Paragraphs int       `json:"paragraphs"`
	// Headings are the Phần/Chương/Mục/Điều headings in order; Parent is
	// the index (in Headings) of the enclosing one, -1 at the top.
	Headings     []LegalDraftHeading    `json:"headings"`
	Articles     []LegalDraftArticle    `json:"articles"`
	Subdivisions LegalDraftSubdivisions `json:"subdivisions"`
	PageMarkers  int                    `json:"page_markers"`
	Warnings     []LegalDraftWarning    `json:"warnings"`
	Blocking     int                    `json:"blocking"`
	Ready        bool                   `json:"ready_to_approve"`
}

// LegalDraftHeading is one structural heading of a draft.
type LegalDraftHeading struct {
	Level int `json:"level"` // 1 Phần, 2 Chương, 3 Mục, 4 Điều
	// Kind is "phan", "chuong", "muc" or "dieu".
	Kind      string `json:"kind"`
	Number    string `json:"number"`
	Title     string `json:"title"`
	Paragraph int    `json:"paragraph"`
	Parent    int    `json:"parent"`
}

// LegalDraftArticle is one Điều of a draft.
type LegalDraftArticle struct {
	Number    string `json:"number"`
	Title     string `json:"title"`
	Paragraph int    `json:"paragraph"`
}

// LegalDraftSubdivisions counts the khoản/điểm markers the legal parser
// accepted and rejected (out of sequence, outside a Điều).
type LegalDraftSubdivisions struct {
	KhoanCandidates int `json:"khoan_candidates"`
	KhoanAccepted   int `json:"khoan_accepted"`
	DiemCandidates  int `json:"diem_candidates"`
	DiemAccepted    int `json:"diem_accepted"`
	Rejected        int `json:"rejected"`
}

// LegalDraftWarning is one finding of a structure report. Paragraph is the
// paragraph index in the document (-1 when none); Paragraphs lists more
// positions of the same finding when it groups several.
type LegalDraftWarning struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"` // "error" (blocks approval), "warning", "info"
	Message    string `json:"message"`
	Paragraph  int    `json:"paragraph"`
	Paragraphs []int  `json:"paragraphs,omitempty"`
	Blocking   bool   `json:"blocking"`
}

// LegalDraftApproveResult is the answer of an approval.
type LegalDraftApproveResult struct {
	Draft *LegalDocumentDraft `json:"draft"`
	// Request is the request the draft was fetched for, now added; nil
	// when the draft had none.
	Request *LegalRequestItem `json:"request"`
	// Result is the ingest result: "created", "replaced" or "unchanged".
	Result      string `json:"result"`
	KnowledgeID string `json:"knowledge_id"`
	Articles    int    `json:"articles"`
}
