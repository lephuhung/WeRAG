package interfaces

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

// AbbreviationRepository defines persistence for the global abbreviation
// dictionary.
type AbbreviationRepository interface {
	// Create inserts a new abbreviation row.
	Create(ctx context.Context, abbr *types.Abbreviation) error
	// SuggestUnique returns the row of the same short/full pair (case- and
	// space-insensitive, an active one first) or inserts abbr as an
	// inactive suggestion.
	SuggestUnique(ctx context.Context, abbr *types.Abbreviation) (*types.Abbreviation, error)
	// SuggestUniqueCreated is SuggestUnique also reporting whether the row
	// was inserted by this call.
	SuggestUniqueCreated(ctx context.Context, abbr *types.Abbreviation) (*types.Abbreviation, bool, error)

	// GetByID retrieves one row. Returns nil, nil when absent.
	GetByID(ctx context.Context, id string) (*types.Abbreviation, error)

	// List returns rows matching the filters plus the total count.
	// search matches short_form OR full_form (case-insensitive substring);
	// isActive nil means "no filter".
	List(ctx context.Context, search string, isActive *bool, offset, limit int) ([]*types.Abbreviation, int64, error)

	// ListActive returns every active row (the expansion dictionary).
	ListActive(ctx context.Context) ([]*types.Abbreviation, error)

	// ListByShortForm returns all rows whose short_form equals the given
	// value case-insensitively (the disambiguation candidate list).
	ListByShortForm(ctx context.Context, shortForm string) ([]*types.Abbreviation, error)

	// Update saves changes to an existing row.
	Update(ctx context.Context, abbr *types.Abbreviation) error

	// Delete soft-deletes a row.
	Delete(ctx context.Context, id string) error
}

// AbbreviationService is the business-logic surface used by handlers and
// agent tools.
type AbbreviationService interface {
	// Suggest creates an inactive abbreviation suggestion on behalf of the
	// caller in ctx.
	Suggest(ctx context.Context, req *types.AbbreviationCreateRequest) (*types.Abbreviation, error)

	// Create inserts a row with an explicit active flag (admin path).
	Create(ctx context.Context, req *types.AbbreviationCreateRequest, active bool) (*types.Abbreviation, error)

	// Get returns one row or nil.
	Get(ctx context.Context, id string) (*types.Abbreviation, error)

	// List returns filtered rows plus total, pending rows first. offset is a
	// row offset (not a page) so a review screen can resume exactly after the
	// rows it approved in place.
	List(ctx context.Context, search string, isActive *bool, offset, limit int) ([]*types.Abbreviation, int64, error)

	// Update applies a partial update. Only admins may flip IsActive —
	// the handler enforces that before calling.
	Update(ctx context.Context, id string, req *types.AbbreviationUpdateRequest) (*types.Abbreviation, error)

	// Delete removes a row.
	Delete(ctx context.Context, id string) error

	// ListActive returns the active dictionary for expansion.
	ListActive(ctx context.Context) ([]*types.Abbreviation, error)

	// ListByShortForm returns every row for a short form (any active state).
	ListByShortForm(ctx context.Context, shortForm string) ([]*types.Abbreviation, error)

	// DetectCandidates decides which tokens of text are abbreviations to
	// look up. It never fails: a classifier error falls back to the
	// heuristic detection (zero value).
	DetectCandidates(ctx context.Context, text string) types.AbbreviationDetection
}

// AbbreviationFeedbackRepository stores answers to the chat gate's
// "unclear word" question as labelled training data.
type AbbreviationFeedbackRepository interface {
	Record(ctx context.Context, rows []types.AbbreviationTypoFeedback) error
	// Latest returns, per lower-case word, the tenant's latest typo, keep
	// or abbreviation answer.
	Latest(ctx context.Context, tenantID uint64, words []string) (map[string]types.AbbreviationTypoFeedback, error)
}

// DocumentSpellingFeedbackRepository stores verdicts on the document
// assistant's spelling underlines as labelled training data; the user's
// "not a mistake" rows also keep a text from being underlined again.
type DocumentSpellingFeedbackRepository interface {
	Record(ctx context.Context, rows []types.DocumentSpellingFeedback) error
	// Dismissed returns the tenant's "not a mistake" verdicts on the
	// paragraphs with these hashes (paragraph_hash and wrong only).
	Dismissed(ctx context.Context, tenantID uint64, paragraphHashes []string) ([]types.DocumentSpellingFeedback, error)
}

// DocumentFormatFeedbackRepository stores users' verdicts on the NĐ30
// format check reports as labelled data for tuning the checker.
type DocumentFormatFeedbackRepository interface {
	Record(ctx context.Context, row *types.DocumentFormatFeedback) error
}

// DocumentLabelRepository reads both feedback tables back as labels for
// the Tenant Admin / SuperAdmin review table and the JSONL training export.
type DocumentLabelRepository interface {
	// List returns a page of labels, newest first, and how many match;
	// ErrDocumentLabelPageTooDeep past the first 2,000 rows (use the export).
	List(ctx context.Context, q *types.DocumentLabelQuery) ([]types.DocumentLabel, int64, error)
	// Each calls fn for every matching label (spelling first, then format,
	// each oldest first), reading rows in batches; q.Offset/Limit are ignored.
	Each(ctx context.Context, q *types.DocumentLabelQuery, fn func(*types.DocumentLabel) error) error
	// Models lists the model names stored on the labels in scope.
	Models(ctx context.Context, tenantID uint64) ([]string, error)
}

// ErrDocumentLabelPageTooDeep: a label list page beyond the depth the
// review table pages to; the JSONL export has every row.
var ErrDocumentLabelPageTooDeep = errors.New("label page too deep: narrow the filters or use the export")
