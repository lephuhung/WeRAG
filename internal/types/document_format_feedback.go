package types

import (
	"encoding/json"
	"time"
)

// Verdicts on a format check report (DocumentFormatFeedback.Verdict).
const (
	// FormatVerdictFalseAlarm: the finding is wrong (FindingID names it).
	FormatVerdictFalseAlarm = "false_alarm"
	// FormatVerdictNotND30: the document is no văn bản hành chính under
	// NĐ30 (a tố tụng / nghiệp vụ form, a đơn …) and was checked as one.
	FormatVerdictNotND30 = "not_nd30"
	// FormatVerdictWrongType: the detected document type is wrong.
	FormatVerdictWrongType = "wrong_type"
	// FormatVerdictOther: anything else, said in Note.
	FormatVerdictOther = "other"
)

// ValidFormatVerdict reports whether v is one of the verdicts above.
func ValidFormatVerdict(v string) bool {
	switch v {
	case FormatVerdictFalseAlarm, FormatVerdictNotND30, FormatVerdictWrongType, FormatVerdictOther:
		return true
	}
	return false
}

// DocumentFormatFeedback is one user verdict on the NĐ30 format check of a
// working document: the whole report (FindingID "") or one finding. It
// keeps the report as given (Report) and the checked file's resource ref,
// as labelled data for tuning the checker; nothing is acted on.
type DocumentFormatFeedback struct {
	ID               uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	TenantID         uint64          `json:"tenant_id"`
	UserID           string          `json:"user_id"`
	SessionID        string          `json:"session_id"`
	DocumentID       string          `json:"document_id"`
	FileName         string          `json:"file_name"`
	DocumentRef      string          `json:"document_ref"`
	DocumentRevision *int            `json:"document_revision"`
	ReportID         string          `json:"report_id"`
	DocumentType     string          `json:"document_type"`
	Scope            string          `json:"scope"`
	FindingID        string          `json:"finding_id"`
	CheckID          string          `json:"check_id"`
	FindingTitle     string          `json:"finding_title"`
	Verdict          string          `json:"verdict"`
	Note             string          `json:"note"`
	Model            string          `json:"model"`
	Report           json.RawMessage `json:"report" gorm:"type:jsonb"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (DocumentFormatFeedback) TableName() string { return "document_format_feedback" }
