package types

import "time"

// Answers to the gate's "unclear word" question (AbbreviationTypoFeedback.Choice).
const (
	AbbreviationFeedbackTypo         = "typo"         // "X → y": the user confirmed a correction
	AbbreviationFeedbackKeep         = "keep"         // "X → X": the word is right as typed
	AbbreviationFeedbackAbbreviation = "abbreviation" // "X = meaning"
	AbbreviationFeedbackRewrite      = "rewrite"      // the user picked the rewritten question
	AbbreviationFeedbackOther        = "other"        // anything else, sent as a new question
)

// AbbreviationTypoFeedback is one answer to the gate's question about an
// unclear word: what was offered (the spell-check guess, the rewritten
// question) and what the user chose. The rows are user-labelled training
// data for the spell-check model.
type AbbreviationTypoFeedback struct {
	ID              uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	TenantID        uint64    `json:"tenant_id"`
	SessionID       string    `json:"session_id"`
	RequestID       string    `json:"request_id"`
	UserMessageID   string    `json:"user_message_id"`
	OriginalQuery   string    `json:"original_query"`
	Word            string    `json:"word"`
	SpellcheckGuess string    `json:"spellcheck_guess"`
	Rewrite         string    `json:"rewrite"`
	Choice          string    `json:"choice"`
	Answer          string    `json:"answer"`
	CreatedAt       time.Time `json:"created_at"`
}

func (AbbreviationTypoFeedback) TableName() string { return "abbreviation_typo_feedback" }
