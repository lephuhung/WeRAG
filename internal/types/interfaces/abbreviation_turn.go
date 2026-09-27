package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// AbbreviationTurnRepository persists scoped clarification-turn state for
// the abbreviation-resolution gate. Owner scope appears on EVERY operation;
// there is no unscoped public GetByID.
type AbbreviationTurnRepository interface {
	// Begin cancels the session's previous awaiting_definition turn (when
	// this is a new question) and inserts the root inspecting turn, in one
	// transaction. Resume paths never call Begin.
	Begin(ctx context.Context, owner types.AbbreviationOwner, row *types.AbbreviationTurnState) error

	// Get returns one turn by ID within the owner scope, or
	// types.ErrAbbreviationNotFound when absent, out of scope, or bound to
	// a soft-deleted session/root message.
	Get(ctx context.Context, owner types.AbbreviationOwner, id string) (*types.AbbreviationTurnState, error)

	// Awaiting returns the session's live awaiting_definition turn (expiry
	// in the future, session and root message not soft-deleted), or nil
	// when there is none. Domain absence hides the turn; infrastructure
	// failures are returned, never mistaken for absence.
	Awaiting(ctx context.Context, owner types.AbbreviationOwner, now time.Time) (*types.AbbreviationTurnState, error)

	// CompareAndSwap validates the state transition, then swaps state,
	// payload, error code and message links for the expected version,
	// bumping version by one. Expiry is never overwritten. A lost race or
	// out-of-scope row reports (false, nil); malformed input errors.
	CompareAndSwap(ctx context.Context, owner types.AbbreviationOwner, expected uint64, row *types.AbbreviationTurnState) (bool, error)

	// LinkMessages transactionally links messages to a turn. Every message
	// must belong to the owner session and be live; cross-session messages
	// are refused. Forks never copy links implicitly.
	LinkMessages(ctx context.Context, owner types.AbbreviationOwner, requestID string, links []types.AbbreviationMessageLink) error

	// ByMessages resolves live-linked turns keyed by message ID, within the
	// owner scope. Messages that are missing, soft-deleted, or bound to a
	// soft-deleted session are absent from the result. Infrastructure
	// failures abort the call with no partial map.
	ByMessages(ctx context.Context, owner types.AbbreviationOwner, messageIDs []string) (map[string]*types.AbbreviationTurnState, error)

	// CancelByMessages cancels the non-terminal turns linked to the given
	// messages, within the owner scope.
	CancelByMessages(ctx context.Context, owner types.AbbreviationOwner, messageIDs []string) error

	// CancelSession cancels every non-terminal turn of the owner session.
	CancelSession(ctx context.Context, owner types.AbbreviationOwner) error
}
