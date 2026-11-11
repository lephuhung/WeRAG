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
	MarkRead(ctx context.Context, userID, id string, at time.Time) (bool, error)
	// MarkAllRead reads every unread notification of the user.
	MarkAllRead(ctx context.Context, userID string, at time.Time) (int64, error)
}
