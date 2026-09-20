package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// AbbreviationRepository defines persistence for the global abbreviation
// dictionary.
type AbbreviationRepository interface {
	// Create inserts a new abbreviation row.
	Create(ctx context.Context, abbr *types.Abbreviation) error

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

	// List returns filtered rows plus total.
	List(ctx context.Context, search string, isActive *bool, page, perPage int) ([]*types.Abbreviation, int64, error)

	// Update applies a partial update. Only admins may flip IsActive —
	// the handler enforces that before calling.
	Update(ctx context.Context, id string, req *types.AbbreviationUpdateRequest) (*types.Abbreviation, error)

	// Delete removes a row.
	Delete(ctx context.Context, id string) error

	// ListActive returns the active dictionary for expansion.
	ListActive(ctx context.Context) ([]*types.Abbreviation, error)

	// ListByShortForm returns every row for a short form (any active state).
	ListByShortForm(ctx context.Context, shortForm string) ([]*types.Abbreviation, error)
}
