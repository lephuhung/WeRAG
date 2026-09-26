package types

import (
	"errors"
	"time"
)

// Abbreviation resolution contracts for the abbreviation-resolution gate.
// This schema is shared by the inspector, selector, coordinator and turn
// binding; consumers must not rename these interfaces. All serialized DTO
// fields carry explicit snake_case JSON tags because the resolution is
// persisted as a typed payload.

const (
	// MaxAbbreviationTerms bounds the number of distinct candidate terms in
	// one resolution. Breaching it blocks the turn instead of truncating.
	MaxAbbreviationTerms = 100
	// MaxAbbreviationShortFormRunes bounds a candidate short form (50 ký tự).
	MaxAbbreviationShortFormRunes = 50
	// MaxAbbreviationFullFormRunes bounds a user-supplied full form (255 ký tự).
	MaxAbbreviationFullFormRunes = 255
)

// Resolution statuses.
const (
	AbbreviationStatusReady           = "ready"
	AbbreviationStatusNeedsDefinition = "needs_definition"
	AbbreviationStatusBlockedError    = "blocked_error"
)

// Term sources.
const (
	AbbreviationSourceDictionaryActive = "dictionary_active"
	AbbreviationSourceUserCurrent      = "user_current_request"
)

// Suggestion lifecycle states (written by the coordinator; the inspector
// leaves them empty).
const (
	AbbreviationSuggestionPendingReview = "pending_review"
	AbbreviationSuggestionActive        = "active"
	AbbreviationSuggestionSaveFailed    = "save_failed"
)

// Resolution error codes for blocked_error.
const (
	AbbreviationErrorSelectionRequired       = "selection_required"
	AbbreviationErrorCandidateBudgetExceeded = "candidate_budget_exceeded"
	// AbbreviationErrorInvalidResolution marks a resolution that claims a
	// load-bearing state its contents cannot support (for example a ready
	// status whose effective query does not recompute). It is an internal
	// invariant failure, never a user-facing definition request.
	AbbreviationErrorInvalidResolution = "invalid_resolution"
)

var (
	ErrAbbreviationNotReady     = errors.New("abbreviation resolution is not ready")
	ErrAbbreviationConflict     = errors.New("abbreviation resolution conflicts with sealed state")
	ErrAbbreviationNotFound     = errors.New("abbreviation resolution not found")
	ErrAbbreviationBadSelection = errors.New("abbreviation selection is not a listed active meaning")
)

// AbbreviationOwner scopes a bound turn to its owning session and principal.
type AbbreviationOwner struct {
	TenantID    uint64 `json:"tenant_id"`
	SessionID   string `json:"session_id"`
	OwnerID     string `json:"owner_id"`
	PrincipalID string `json:"principal_id"`
}

// AbbreviationBinding identifies one user turn. RawQuery is the CURRENT user
// reply; on continuation it may differ from the resolution's root
// OriginalQuery, so the two must never be equated.
type AbbreviationBinding struct {
	Owner              AbbreviationOwner `json:"owner"`
	UserMessageID      string            `json:"user_message_id"`
	AssistantMessageID string            `json:"assistant_message_id"`
	RawQuery           string            `json:"raw_query"`
}

// AbbreviationOccurrence is one UTF-8 byte-offset span of a short form in
// the resolution's OriginalQuery.
type AbbreviationOccurrence struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// AbbreviationMeaning is one active dictionary meaning of a term.
type AbbreviationMeaning struct {
	ID          string `json:"id"`
	FullForm    string `json:"full_form"`
	Description string `json:"description,omitempty"`
}

// AbbreviationDefinition is user-supplied evidence for an unknown term: the
// full-form span lives in the CURRENT user's message, which on continuation
// may be a different message than the root question. SourceMessageID names
// the message carrying the definition; message provenance is validated by
// the parser/coordinator, which own the source text unavailable here.
type AbbreviationDefinition struct {
	ShortForm       string `json:"short_form"`
	FullForm        string `json:"full_form"`
	SourceMessageID string `json:"source_message_id"`
	Start           int    `json:"start"`
	End             int    `json:"end"`
}

// AbbreviationTerm is one candidate short form and its resolution state.
// Key is strings.ToLower(strings.TrimSpace(ShortForm)).
type AbbreviationTerm struct {
	ShortForm        string                   `json:"short_form"`
	Key              string                   `json:"key"`
	Occurrences      []AbbreviationOccurrence `json:"occurrences"`
	Meanings         []AbbreviationMeaning    `json:"meanings"`
	SelectedID       string                   `json:"selected_id,omitempty"`
	FullForm         string                   `json:"full_form"`
	Source           string                   `json:"source"`
	Definition       *AbbreviationDefinition  `json:"definition,omitempty"`
	SuggestionID     string                   `json:"suggestion_id,omitempty"`
	SuggestionStatus string                   `json:"suggestion_status,omitempty"`
}

// AbbreviationResolution is the per-turn resolution state. Lifecycle fields
// (RequestID, message IDs, Version, ExpiresAt) are assigned by the
// coordinator; Inspect leaves them at zero values.
type AbbreviationResolution struct {
	RequestID            string             `json:"request_id,omitempty"`
	RootUserMessageID    string             `json:"root_user_message_id,omitempty"`
	CurrentUserMessageID string             `json:"current_user_message_id,omitempty"`
	Version              uint64             `json:"version"`
	OriginalQuery        string             `json:"original_query"`
	EffectiveQuery       string             `json:"effective_query,omitempty"`
	Status               string             `json:"status"`
	ErrorCode            string             `json:"error_code,omitempty"`
	Terms                []AbbreviationTerm `json:"terms"`
	UnknownTerms         []string           `json:"unknown_terms"`
	ExpiresAt            time.Time          `json:"expires_at"`
}

// AbbreviationPublicTerm is the shared public projection of one resolved
// term. It omits Definition, Occurrences and owner/principal evidence.
type AbbreviationPublicTerm struct {
	ShortForm        string `json:"short_form"`
	FullForm         string `json:"full_form"`
	Source           string `json:"source"`
	SelectedID       string `json:"selected_id,omitempty"`
	SuggestionStatus string `json:"suggestion_status"`
}

// AbbreviationPublicState is the shared public projection of a resolution,
// used by events and history. It omits private evidence/owner/scope.
type AbbreviationPublicState struct {
	RequestID         string                   `json:"request_id"`
	RootUserMessageID string                   `json:"root_user_message_id"`
	Status            string                   `json:"status"`
	Version           uint64                   `json:"version"`
	UnknownTerms      []string                 `json:"unknown_terms"`
	Terms             []AbbreviationPublicTerm `json:"terms"`
	ExpiresAt         time.Time                `json:"expires_at"`
}

// PublicState projects the shareable subset of the resolution. Slices are
// deep-copied so callers cannot mutate internal state through the projection.
func (r AbbreviationResolution) PublicState() AbbreviationPublicState {
	terms := make([]AbbreviationPublicTerm, 0, len(r.Terms))
	for _, t := range r.Terms {
		terms = append(terms, AbbreviationPublicTerm{
			ShortForm:        t.ShortForm,
			FullForm:         t.FullForm,
			Source:           t.Source,
			SelectedID:       t.SelectedID,
			SuggestionStatus: t.SuggestionStatus,
		})
	}
	unknown := make([]string, len(r.UnknownTerms))
	copy(unknown, r.UnknownTerms)
	return AbbreviationPublicState{
		RequestID:         r.RequestID,
		RootUserMessageID: r.RootUserMessageID,
		Status:            r.Status,
		Version:           r.Version,
		UnknownTerms:      unknown,
		Terms:             terms,
		ExpiresAt:         r.ExpiresAt,
	}
}
