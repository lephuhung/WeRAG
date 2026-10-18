package types

import (
	"context"
	"time"
)

// LegalDocumentRequest is a request to add the full text of a legal
// document the system lacks. A chat turn that names a full số hiệu the
// knowledge bases do not hold records one (service.legalMissingDocumentGate);
// a SuperAdmin ingests it from vbpl.vn, adds it by hand or rejects it.
//
// One row per (NumberKey, LegalDocumentID): LegalDocumentID is the
// catalogue (vbpl.vn) id when the catalogue knows the document, "" when
// it does not. Source says where the identity came from ("catalogue", ""
// unknown; later other sources), Detail keeps a source's evidence, so a
// new source needs no new columns.
type LegalDocumentRequest struct {
	ID              string `json:"id"                gorm:"type:varchar(36);primaryKey"`
	NumberKey       string `json:"number_key"        gorm:"type:varchar(128);not null;uniqueIndex:ux_legal_document_requests_doc,priority:1"`
	DocumentNumber  string `json:"document_number"   gorm:"type:varchar(128);not null;default:''"`
	DocType         string `json:"doc_type"          gorm:"type:varchar(64);not null;default:''"`
	Title           string `json:"title"             gorm:"type:text;not null;default:''"`
	LegalDocumentID string `json:"legal_document_id" gorm:"type:varchar(64);not null;default:'';uniqueIndex:ux_legal_document_requests_doc,priority:2"`
	Source          string `json:"source"            gorm:"type:varchar(32);not null;default:''"`
	Status          string `json:"status"            gorm:"type:varchar(16);not null;default:'requested';index:idx_legal_document_requests_status"`
	RequestCount    int    `json:"request_count"     gorm:"not null;default:1"`

	FirstRequestedAt time.Time `json:"first_requested_at" gorm:"not null"`
	LastRequestedAt  time.Time `json:"last_requested_at"  gorm:"not null;index:idx_legal_document_requests_last_requested_at"`
	LastTenantID     uint64    `json:"last_tenant_id"     gorm:"not null;default:0"`
	LastUserID       string    `json:"last_user_id"       gorm:"type:varchar(36);not null;default:''"`
	LastSessionID    string    `json:"last_session_id"    gorm:"type:varchar(36);not null;default:''"`
	LastQuery        string    `json:"last_query"         gorm:"type:text;not null;default:''"`

	KnowledgeID     string `json:"knowledge_id"      gorm:"type:varchar(36);not null;default:''"`
	KnowledgeBaseID string `json:"knowledge_base_id" gorm:"type:varchar(36);not null;default:''"`

	DecidedBy    string     `json:"decided_by"    gorm:"type:varchar(36);not null;default:''"`
	DecidedAt    *time.Time `json:"decided_at"`
	DecisionNote string     `json:"decision_note" gorm:"type:text;not null;default:''"`

	Detail    JSON      `json:"detail"     gorm:"type:jsonb"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for LegalDocumentRequest.
func (LegalDocumentRequest) TableName() string { return "legal_document_requests" }

// Legal document request states.
const (
	LegalRequestRequested = "requested"
	// LegalRequestFetched: vbpl.vn has only a scanned PDF, which was fetched
	// into a draft (LegalDocumentDraft) waiting for the SuperAdmin's review.
	LegalRequestFetched = "fetched"
	// LegalRequestReviewing: the draft is open in the editor.
	LegalRequestReviewing = "reviewing"
	LegalRequestAdded     = "added"
	LegalRequestRejected  = "rejected"
)

// LegalRequestOpenStatuses are the states a request is still waiting in:
// asking again bumps it, any ingest of its document closes it.
var LegalRequestOpenStatuses = []string{LegalRequestRequested, LegalRequestFetched, LegalRequestReviewing}

// IsLegalRequestOpen reports whether status is one of LegalRequestOpenStatuses.
func IsLegalRequestOpen(status string) bool {
	for _, s := range LegalRequestOpenStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// Legal document request sources.
const (
	// LegalRequestSourceCatalogue: the legal catalogue knows the document
	// (LegalDocumentID is its vbpl.vn id).
	LegalRequestSourceCatalogue = "catalogue"
)

// LegalRequestQueryMaxRunes caps the stored question.
const LegalRequestQueryMaxRunes = 500

// LegalRequestItem is one row of the SuperAdmin queue: the request, the
// catalogue document when known and the public KB suggested for it.
type LegalRequestItem struct {
	*LegalDocumentRequest
	Document *LegalRequestDocument `json:"document,omitempty"`
	// SuggestedKBID is the public KB named for the document's type
	// ("VBPL – Nghị định"), "" when none is.
	SuggestedKBID string `json:"suggested_kb_id"`
	// KnowledgeParseStatus is knowledges.parse_status of KnowledgeID
	// ("pending", "processing", "completed", "failed"), "" when no document
	// is linked: an ingested PDF scan is OCRed asynchronously.
	KnowledgeParseStatus string `json:"knowledge_parse_status"`
	// Draft is the OCR draft of the document's scanned PDF, nil when none
	// was fetched.
	Draft *LegalDraftSummary `json:"draft"`
}

// LegalRequestDocument is the catalogue view of a requested document.
type LegalRequestDocument struct {
	ID             string `json:"id"`
	DocumentNumber string `json:"document_number"`
	Title          string `json:"title"`
	DocType        string `json:"doc_type"`
	Issuer         string `json:"issuer"`
	Status         string `json:"status"`
	IssuedOn       string `json:"issued_on"` // YYYY-MM-DD
	URL            string `json:"url"`
}

// LegalRequestDecision is the SuperAdmin's answer for one request.
type LegalRequestDecision struct {
	// Action is "ingest" (fetch from vbpl.vn into KnowledgeBaseID), "reject"
	// or "added" (added by hand).
	Action          string `json:"action"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeID     string `json:"knowledge_id"`
	Note            string `json:"note"`
}

// LegalRequestDetailPortal is the Detail key holding what vbpl.vn offers
// for the document (service.LegalPortalAvailability).
const LegalRequestDetailPortal = "portal"

// Legal request decision actions.
const (
	LegalRequestActionIngest = "ingest"
	LegalRequestActionReject = "reject"
	LegalRequestActionAdded  = "added"
)

// LegalPublicKB is a platform public knowledge base a request can be
// ingested into.
type LegalPublicKB struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type legalRequestRecordedKey struct{}

// WithLegalRequestRecorded marks a turn for which a request to add the
// named document was recorded although the turn still runs (the catalogue
// does not know the number): the grounding gate's fixed answer then says
// so (agent.ungroundedAnswerFor).
func WithLegalRequestRecorded(ctx context.Context, documentNumber string) context.Context {
	return context.WithValue(ctx, legalRequestRecordedKey{}, documentNumber)
}

// LegalRequestRecordedFromContext returns the số hiệu WithLegalRequestRecorded
// stored, or "".
func LegalRequestRecordedFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(legalRequestRecordedKey{}).(string)
	return v
}
