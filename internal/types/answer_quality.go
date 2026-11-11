package types

import (
	"strings"
	"time"
)

// Answer-quality inbox: what admins read to see where the assistant falls
// short. Two lists over messages:
//   - disliked answers: assistant messages the session owner voted down
//     (Message.Feedback = MessageFeedbackDown), with the comment;
//   - ungrounded questions: assistant messages a gate answered without
//     evidence (MessageExecutionContext.Ungrounded).
//
// A Tenant Admin sees the rows of their own organization, a SuperAdmin
// those of every organization (optionally one). Members and guests never.
// An admin's cause tag and "reviewed" mark live in answer_quality_reviews
// (migration 000197), keyed by message and list: the user's message is
// never changed.

// Values of UngroundedMarker.Reason.
const (
	// UngroundedNoEvidence: the grounding gate replaced an answer no tool
	// evidence backed (agent), or retrieval found nothing and the fallback
	// answered (quick answer).
	UngroundedNoEvidence = "no_evidence"
	// UngroundedLegalDocumentMissing: the question names a legal document
	// the system holds no full text of; the turn answered with the
	// "requested"/"rejected" card.
	UngroundedLegalDocumentMissing = "legal_document_missing"
)

// UngroundedQueryMaxRunes bounds the question kept in the marker.
const UngroundedQueryMaxRunes = 1000

// UngroundedMarker is stored on an assistant message
// (MessageExecutionContext.Ungrounded) when the turn was answered by a
// gate for lack of evidence. A failed turn (TurnFailure) never carries one.
type UngroundedMarker struct {
	Reason string `json:"reason"`
	// Query is the user's question as asked.
	Query string `json:"query,omitempty"`
	// LegalRequest is the số hiệu of a legal-document request the turn
	// recorded and that is still open, "" when none.
	LegalRequest string `json:"legal_request,omitempty"`
}

// NewUngroundedMarker builds a marker, trimming the texts and capping the
// question at UngroundedQueryMaxRunes.
func NewUngroundedMarker(reason, query, legalRequest string) *UngroundedMarker {
	return &UngroundedMarker{
		Reason:       reason,
		Query:        truncateRunes(strings.TrimSpace(query), UngroundedQueryMaxRunes),
		LegalRequest: truncateRunes(strings.TrimSpace(legalRequest), 128),
	}
}

// Kinds of AnswerQualityReview (the list a review belongs to).
const (
	AnswerQualityKindDisliked   = "disliked"
	AnswerQualityKindUngrounded = "ungrounded"
)

// Cause tags an admin may set on a disliked answer.
const (
	AnswerQualityCauseWrongBasis  = "wrong_basis"  // sai căn cứ
	AnswerQualityCauseMissingInfo = "missing_info" // thiếu thông tin
	AnswerQualityCauseOffTopic    = "off_topic"    // trả lời lạc đề
	AnswerQualityCauseFormatError = "format_error" // lỗi định dạng
	AnswerQualityCauseOther       = "other"        // khác
)

// Cause tags an admin may set on an ungrounded question.
const (
	AnswerQualityCauseKBMissing     = "kb_missing"     // kho chưa có tài liệu
	AnswerQualityCauseRetrievalMiss = "retrieval_miss" // có tài liệu nhưng không tìm ra
	AnswerQualityCauseOutOfScope    = "out_of_scope"   // ngoài phạm vi
)

// ValidAnswerQualityCause reports whether cause may be set on a review of
// kind; "" (no tag) always may.
func ValidAnswerQualityCause(kind, cause string) bool {
	if cause == "" {
		return true
	}
	switch kind {
	case AnswerQualityKindDisliked:
		switch cause {
		case AnswerQualityCauseWrongBasis, AnswerQualityCauseMissingInfo, AnswerQualityCauseOffTopic,
			AnswerQualityCauseFormatError, AnswerQualityCauseOther:
			return true
		}
	case AnswerQualityKindUngrounded:
		switch cause {
		case AnswerQualityCauseKBMissing, AnswerQualityCauseRetrievalMiss, AnswerQualityCauseOutOfScope,
			AnswerQualityCauseOther:
			return true
		}
	}
	return false
}

// ValidAnswerQualityKind reports whether kind is one of the lists.
func ValidAnswerQualityKind(kind string) bool {
	return kind == AnswerQualityKindDisliked || kind == AnswerQualityKindUngrounded
}

// AnswerQualityNoteMaxRunes bounds an admin's note on a review.
const AnswerQualityNoteMaxRunes = 2000

// AnswerQualityReview is an admin's verdict on one row of a list
// (migration 000197). TenantID is the organization of the message's
// session, so a Tenant Admin only ever writes their own.
type AnswerQualityReview struct {
	MessageID  string     `json:"message_id"  gorm:"type:varchar(36);primaryKey"`
	Kind       string     `json:"kind"        gorm:"type:varchar(16);primaryKey"`
	TenantID   uint64     `json:"tenant_id"   gorm:"not null;default:0"`
	Cause      string     `json:"cause"       gorm:"type:varchar(32);not null;default:''"`
	Note       string     `json:"note"        gorm:"type:text;not null;default:''"`
	Reviewed   bool       `json:"reviewed"    gorm:"not null;default:false"`
	ReviewedBy string     `json:"reviewed_by" gorm:"type:varchar(36);not null;default:''"`
	ReviewedAt *time.Time `json:"reviewed_at"`
	UpdatedBy  string     `json:"updated_by"  gorm:"type:varchar(36);not null;default:''"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// TableName returns the table name.
func (AnswerQualityReview) TableName() string { return "answer_quality_reviews" }

// AnswerQualityFilter narrows a list. TenantID 0 with AllTenants means
// every organization (SuperAdmin only); the service sets both from the
// caller, never from the request alone.
type AnswerQualityFilter struct {
	TenantID   uint64
	AllTenants bool
	From       *time.Time // created (disliked: voted) at or after
	To         *time.Time // before
	AgentID    string
	// HasComment keeps disliked answers with a non-empty comment.
	HasComment bool
	// Reason keeps ungrounded rows of one reason.
	Reason string
	// Reviewed: nil = all, true = reviewed only, false = not reviewed.
	Reviewed *bool
	Offset   int
	Limit    int
}

// AnswerQualityListMax caps a page.
const AnswerQualityListMax = 100

// AnswerQualityAnswerMaxRunes caps the answer text a list row carries.
const AnswerQualityAnswerMaxRunes = 4000

// AnswerQualityRow is one row of either list.
type AnswerQualityRow struct {
	MessageID    string     `json:"message_id"`
	SessionID    string     `json:"session_id"`
	CreatedAt    time.Time  `json:"created_at"`
	TenantID     uint64     `json:"tenant_id"`
	TenantName   string     `json:"tenant_name"`
	UserID       string     `json:"user_id"`
	UserName     string     `json:"user_name"`
	AgentID      string     `json:"agent_id"`
	AgentName    string     `json:"agent_name"`
	Question     string     `json:"question"`
	Answer       string     `json:"answer"`
	AnswerCut    bool       `json:"answer_truncated,omitempty"`
	References   int        `json:"references"`
	Feedback     int        `json:"feedback,omitempty"`
	Comment      string     `json:"comment,omitempty"`
	FeedbackAt   *time.Time `json:"feedback_at,omitempty"`
	Reason       string     `json:"reason,omitempty"`
	LegalRequest string     `json:"legal_request,omitempty"`
	// Review is the admin's verdict, nil when none.
	Review *AnswerQualityReviewView `json:"review,omitempty"`
}

// AnswerQualityReviewView is a review as a list row shows it.
type AnswerQualityReviewView struct {
	Cause        string     `json:"cause"`
	Note         string     `json:"note"`
	Reviewed     bool       `json:"reviewed"`
	ReviewedBy   string     `json:"reviewed_by"`
	ReviewerName string     `json:"reviewer_name"`
	ReviewedAt   *time.Time `json:"reviewed_at"`
}

// AnswerQualityAgent is an agent seen in a list (the agent filter's
// choices).
type AnswerQualityAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AnswerQualityPage is a page of a list.
type AnswerQualityPage struct {
	Items  []*AnswerQualityRow   `json:"items"`
	Total  int64                 `json:"total"`
	Agents []*AnswerQualityAgent `json:"agents"`
}

// UngroundedQuestionGroup is one normalized question of the ungrounded
// list, grouped over the filter's period.
type UngroundedQuestionGroup struct {
	Key string `json:"key"`
	// Sample is the latest wording.
	Sample        string    `json:"sample"`
	Count         int       `json:"count"`
	Organizations int       `json:"organizations"`
	LastAt        time.Time `json:"last_at"`
	// Reasons counts the rows per reason.
	Reasons map[string]int `json:"reasons"`
	// LegalRequest is a số hiệu one of the rows recorded a request for.
	LegalRequest string `json:"legal_request,omitempty"`
	// MessageID is the latest row (to open it in the list).
	MessageID string `json:"message_id"`
}

// UngroundedGroupPage is a page of grouped questions. Truncated says the
// period held more rows than were grouped (the newest are kept).
type UngroundedGroupPage struct {
	Items     []*UngroundedQuestionGroup `json:"items"`
	Total     int                        `json:"total"`
	Rows      int                        `json:"rows"`
	Truncated bool                       `json:"truncated,omitempty"`
}

// UngroundedGroupScanMax caps the rows one grouping reads.
const UngroundedGroupScanMax = 20000

// AnswerQualityReviewInput is an admin's change to a review.
type AnswerQualityReviewInput struct {
	Kind     string  `json:"kind"`
	Cause    *string `json:"cause"`
	Note     *string `json:"note"`
	Reviewed *bool   `json:"reviewed"`
}
