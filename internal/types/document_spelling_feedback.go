package types

import "time"

// Verdicts on a spelling underline (DocumentSpellingFeedback.Verdict).
const (
	// SpellingVerdictNotError: the flagged text is right as written.
	SpellingVerdictNotError = "not_error"
)

// Who gave the verdict (DocumentSpellingFeedback.Source).
const (
	SpellingFeedbackSourceUser     = "user"     // the spelling list's "not a mistake"
	SpellingFeedbackSourceVerifier = "verifier" // the verifier model rejected the finding
)

// DocumentSpellingFeedback is one verdict on a spelling finding of the
// document assistant: the paragraph, the flagged text, the spell-check
// model's correction and who said what about it. The user's rows also keep
// the text from being underlined again in a paragraph with the same text
// (ParagraphHash, see tools.SpellingParagraphHash) in the tenant.
type DocumentSpellingFeedback struct {
	ID             uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	TenantID       uint64    `json:"tenant_id"`
	UserID         string    `json:"user_id"`
	SessionID      string    `json:"session_id"`
	DocumentID     string    `json:"document_id"`
	Paragraph      string    `json:"paragraph"`
	ParagraphHash  string    `json:"paragraph_hash"`
	Wrong          string    `json:"wrong"`
	Suggestion     string    `json:"suggestion"`
	TextOccurrence int       `json:"text_occurrence"`
	Verdict        string    `json:"verdict"`
	Source         string    `json:"source"`
	Model          string    `json:"model"`
	Note           string    `json:"note"`
	CreatedAt      time.Time `json:"created_at"`
}

func (DocumentSpellingFeedback) TableName() string { return "document_spelling_feedback" }
