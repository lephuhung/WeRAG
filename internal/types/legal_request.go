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

	// ClaimedBy/ClaimedAt are the operator lease: one person is fetching this
	// document right now (the external-source feed has several operators).
	// The lease expires after LegalRequestClaimLease so a closed browser does
	// not block the queue forever, and any decision clears it.
	ClaimedBy string     `json:"claimed_by" gorm:"type:varchar(36);not null;default:''"`
	ClaimedAt *time.Time `json:"claimed_at"`

	Detail    JSON      `json:"detail"     gorm:"type:jsonb"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Opened (not stored) is set by LegalRequestRepository.Record when the
	// call opened the request: created it, reopened it after it was added,
	// or was the first real request of a row queued with nobody asking.
	Opened bool `json:"-" gorm:"-"`
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
	// LegalRequestAutoIngest: vbpl.vn publishes the HTML full text, the
	// backend worker (service.LegalAutoIngest) adds it to its public KB.
	// Operators do not see these rows.
	LegalRequestAutoIngest = "auto_ingest"
)

// LegalRequestOpenStatuses are the states a request is still waiting in:
// asking again bumps it, any ingest of its document closes it.
var LegalRequestOpenStatuses = []string{
	LegalRequestRequested, LegalRequestFetched, LegalRequestReviewing, LegalRequestAutoIngest,
}

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
	// RequesterCount is how many people asked (legal_request_requesters);
	// who they are is not shown.
	RequesterCount int `json:"requester_count"`
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

// LegalRequestSourceNoFullText marks a request nobody asked for: the nightly
// import queued a document in force that vbpl.vn publishes no full text for.
// Its RequestCount stays 0 until a user really asks.
const LegalRequestSourceNoFullText = "vbpl_no_text"

// LegalExpiredRetentionDaysSettingKey: how many days a fully expired
// document keeps its full text in a public KB; its catalogue row stays.
const LegalExpiredRetentionDaysSettingKey = "legal.expired_retention_days"

// LegalExpiredRetentionDefaultDays is that setting's default.
const LegalExpiredRetentionDefaultDays = 15

// LegalRequestSourceHTML marks a request the nightly import queued for the
// auto-ingest worker: vbpl.vn has the full text, no public KB holds it.
const LegalRequestSourceHTML = "vbpl_html"

// LegalRequestDetailAutoIngestSkip is the Detail key the worker sets when it
// will not retry a document (no KB for its type, the portal refused): the
// reason. LegalRequestDetailAutoIngestError holds the last transient failure.
const (
	LegalRequestDetailAutoIngestSkip  = "auto_ingest_skip"
	LegalRequestDetailAutoIngestError = "auto_ingest_error"
)

// LegalRequestDetailTVPLNotFound is the Detail key an operator sets when the
// document is not on thuvienphapluat.vn either: {at, by, note}.
const LegalRequestDetailTVPLNotFound = "tvpl_not_found"

// LegalRequestDetailPortal is the Detail key holding what vbpl.vn offers
// for the document (service.LegalPortalAvailability).
const LegalRequestDetailPortal = "portal"

// Legal request decision actions.
const (
	LegalRequestActionIngest = "ingest"
	LegalRequestActionReject = "reject"
	LegalRequestActionAdded  = "added"
	// LegalRequestActionNotOnTVPL records that the operator looked the
	// document up on thuvienphapluat.vn and it is not there. The request
	// stays open: another source or a SuperAdmin may still add it.
	LegalRequestActionNotOnTVPL = "not_on_tvpl"
	// LegalRequestActionTVPLRecheck undoes it (a misclick, or TVPL has it now).
	LegalRequestActionTVPLRecheck = "tvpl_recheck"
	// LegalRequestActionClaim / Release take (or drop) the operator lease on
	// one request, so several operators do not fetch the same document.
	LegalRequestActionClaim   = "claim"
	LegalRequestActionRelease = "release"
)

// LegalRequestClaimLease is how long a claim keeps other operators out. A
// claim is refreshed by claiming again; it never outlives the lease.
const LegalRequestClaimLease = 30 * time.Minute

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

// LegalRequestRequester is one person who asked for a request's document
// (migration 000195): the request itself keeps only the last asker
// (Last*), so the earlier ones would be lost. One row per (RequestID,
// UserID): the same person asking again moves the row to the latest
// conversation and, when they were already told, makes them pending again
// (they ask because they still miss it). Requests nobody asked for (the
// nightly import, a feed push) have no requester.
//
// NotifiedAt is set once the person was told the document is available
// (service.LegalRequestNotifier, an in-app notification), NotifyStatus says
// how (LegalRequesterNotifyInApp). UpdatedAt moves only when the person asks
// (again): it keys the notification to the ask.
type LegalRequestRequester struct {
	ID        string  `json:"id"         gorm:"type:varchar(36);primaryKey"`
	RequestID string  `json:"request_id" gorm:"type:varchar(36);not null;uniqueIndex:ux_legal_request_requesters_user,priority:1"`
	TenantID  uint64  `json:"tenant_id"  gorm:"not null;default:0"`
	UserID    string  `json:"user_id"    gorm:"type:varchar(36);not null;uniqueIndex:ux_legal_request_requesters_user,priority:2"`
	SessionID string  `json:"session_id" gorm:"type:varchar(36);not null;default:''"`
	MessageID *string `json:"message_id" gorm:"type:varchar(36)"`
	// Channel is where the person asked (LegalRequesterChannel*, or the
	// chat request's channel as sent).
	Channel      string     `json:"channel"       gorm:"type:varchar(32);not null;default:''"`
	NotifiedAt   *time.Time `json:"notified_at"`
	NotifyStatus string     `json:"notify_status" gorm:"type:varchar(16);not null;default:''"`
	// NotifyAttempts counts the sweeps that failed to write the notice; at
	// LegalRequesterMaxNotifyAttempts the row is closed as failed.
	NotifyAttempts int       `json:"notify_attempts" gorm:"not null;default:0"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName returns the table name for LegalRequestRequester.
func (LegalRequestRequester) TableName() string { return "legal_request_requesters" }

// Where a requester asked.
const (
	LegalRequesterChannelWeb       = "web"
	LegalRequesterChannelWordAddin = "word_addin"
	// LegalRequesterChannelPortal: a Guest in the public portal.
	LegalRequesterChannelPortal = "portal"
)

// LegalRequestRequester.NotifyStatus values (NotifiedAt is set with each).
const (
	// LegalRequesterNotifyInApp: the in-app notification was written
	// (types.UserNotification).
	LegalRequesterNotifyInApp = "in_app"
	// LegalRequesterNotifyOutOfScope: the person asked while the request was
	// added with a copy their agent does not read; this closing tells them
	// nothing (they are told when asking again reopens the request and a
	// copy is added again).
	LegalRequesterNotifyOutOfScope = "out_of_scope"
	// LegalRequesterNotifyFailed: writing the notice failed
	// LegalRequesterMaxNotifyAttempts times; not retried.
	LegalRequesterNotifyFailed = "failed"
)

// LegalRequesterMaxNotifyAttempts caps the sweeps that try one notice.
const LegalRequesterMaxNotifyAttempts = 20

// LegalRequestAddedNotice is a requester due the "document added" notice:
// the requester row with what the notice names of its request.
type LegalRequestAddedNotice struct {
	RequesterID string
	RequestID   string
	TenantID    uint64
	UserID      string
	SessionID   string
	Channel     string
	// AskedAt is the requester row's updated_at: asking again moves it, so
	// it keys the notice to the ask, not to the closing.
	AskedAt        time.Time
	DocumentNumber string
	Title          string
	DecidedAt      time.Time
	// SessionLive: the conversation still exists (not deleted).
	SessionLive bool
	// WordFileID is the Word file the conversation is bound to, "" when none.
	WordFileID string
}

// LegalRequestNoticeWindow bounds how long after a request was closed as
// added its requesters are still notified: a document added weeks ago (or
// whose parsing finished much later) is no news any more.
const LegalRequestNoticeWindow = 7 * 24 * time.Hour
