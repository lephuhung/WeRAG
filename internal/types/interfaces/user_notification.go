package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// UserNotificationRepository stores the in-app notifications
// (types.UserNotification). Every read and write is scoped to one user.
type UserNotificationRepository interface {
	// Create inserts n unless a row with its DedupeKey exists; false =
	// already there.
	Create(ctx context.Context, n *types.UserNotification) (bool, error)
	// List returns the user's notifications, newest first: only unread
	// ones when unreadOnly, only those created before `before` when set,
	// at most limit.
	List(ctx context.Context, userID string, unreadOnly bool, before *time.Time, limit int) ([]*types.UserNotification, error)
	// CountUnread counts the user's unread notifications and, of those,
	// the ones no toast was shown for yet.
	CountUnread(ctx context.Context, userID string) (unread, unseen int64, err error)
	// MarkSeen sets seen_at on the user's notifications among ids that
	// have none; returns how many.
	MarkSeen(ctx context.Context, userID string, ids []string, at time.Time) (int64, error)
	// MarkRead sets read_at (and seen_at when unset) on one of the user's
	// notifications; false when it is not the user's or does not exist.
	// Reading an open digest frees its key (MarkAllRead too).
	MarkRead(ctx context.Context, userID, id string, at time.Time) (bool, error)
	// MarkAllRead reads every unread notification of the user.
	MarkAllRead(ctx context.Context, userID string, at time.Time) (int64, error)
	// UpsertOpenDigest keeps one unread digest per n.DedupeKey, which must be
	// an open key (types.UserNotificationOpenKey): it inserts n when no
	// unread row holds the key; otherwise it locks that row, calls
	// merge(prev, next) with next a copy of n to fill in next's title, body,
	// link and payload from both, and saves them into the row, moving it to
	// the top (created_at = next.CreatedAt) and clearing seen_at so it is
	// toasted again. Safe under concurrency: two calls never leave two unread
	// rows. n gets the row's id; true = inserted.
	UpsertOpenDigest(ctx context.Context, n *types.UserNotification, merge func(prev, next *types.UserNotification)) (bool, error)
	// CloseOpenDigests reads every user's open digest of kind last updated
	// before `before` (a digest updated later reports something newer);
	// returns how many.
	CloseOpenDigests(ctx context.Context, kind string, before, at time.Time) (int64, error)
	// LatestOfKind returns the user's newest notification of kind, read
	// or not (nil when none).
	LatestOfKind(ctx context.Context, userID, kind string) (*types.UserNotification, error)
	// SetPayload replaces the payload of the user's notification id (no
	// other column changes: it is not toasted again).
	SetPayload(ctx context.Context, userID, id string, payload types.JSON) error
	// PurgeRead deletes the notifications read before readBefore, batch
	// rows at a time; returns how many.
	PurgeRead(ctx context.Context, readBefore time.Time, batch int) (int64, error)
}
