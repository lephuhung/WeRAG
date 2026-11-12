package repository

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type userNotificationRepository struct {
	db *gorm.DB
}

// NewUserNotificationRepository creates the in-app notification repository.
func NewUserNotificationRepository(db *gorm.DB) interfaces.UserNotificationRepository {
	return &userNotificationRepository{db: db}
}

// createUserNotification inserts n on db (a transaction or not) unless its
// dedupe key exists; false = already there.
func createUserNotification(db *gorm.DB, n *types.UserNotification) (bool, error) {
	if n == nil || strings.TrimSpace(n.UserID) == "" || strings.TrimSpace(n.DedupeKey) == "" || n.Kind == "" {
		return false, errors.New("user notification: user, kind and dedupe key are required")
	}
	if !types.ValidUserNotificationLink(n.Link) {
		return false, fmt.Errorf("user notification: link %q is not an app path under %s", n.Link, types.UserNotificationLinkPrefix)
	}
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	res := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedupe_key"}}, DoNothing: true}).Create(n)
	return res.RowsAffected == 1, res.Error
}

func (r *userNotificationRepository) Create(ctx context.Context, n *types.UserNotification) (bool, error) {
	return createUserNotification(r.db.WithContext(ctx), n)
}

func (r *userNotificationRepository) List(
	ctx context.Context, userID string, unreadOnly bool, before *time.Time, limit int,
) ([]*types.UserNotification, error) {
	if limit <= 0 || limit > types.UserNotificationListMax {
		limit = types.UserNotificationListMax
	}
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if unreadOnly {
		q = q.Where("read_at IS NULL")
	}
	if before != nil {
		q = q.Where("created_at < ?", *before)
	}
	var rows []*types.UserNotification
	err := q.Order("created_at DESC").Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *userNotificationRepository) CountUnread(ctx context.Context, userID string) (int64, int64, error) {
	var row struct {
		Unread int64
		Unseen int64
	}
	err := r.db.WithContext(ctx).Model(&types.UserNotification{}).
		Select("COUNT(*) AS unread, COALESCE(SUM(CASE WHEN seen_at IS NULL THEN 1 ELSE 0 END), 0) AS unseen").
		Where("user_id = ? AND read_at IS NULL", userID).Scan(&row).Error
	return row.Unread, row.Unseen, err
}

func (r *userNotificationRepository) MarkSeen(ctx context.Context, userID string, ids []string, at time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&types.UserNotification{}).
		Where("user_id = ? AND id IN ? AND seen_at IS NULL", userID, ids).Update("seen_at", at)
	return res.RowsAffected, res.Error
}

func (r *userNotificationRepository) MarkRead(ctx context.Context, userID, id string, at time.Time) (bool, error) {
	var n types.UserNotification
	err := r.db.WithContext(ctx).Select("id", "read_at", "seen_at").
		Where("id = ? AND user_id = ?", id, userID).Take(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	updates := map[string]any{}
	if n.ReadAt == nil {
		updates["read_at"] = at
	}
	if n.SeenAt == nil {
		updates["seen_at"] = at
	}
	if n.ReadAt == nil {
		// an open digest's key is freed for the next one
		updates["dedupe_key"] = gorm.Expr(types.UserNotificationRotateOpenKeySQL)
	}
	if len(updates) == 0 {
		return true, nil
	}
	return true, r.db.WithContext(ctx).Model(&types.UserNotification{}).
		Where("id = ? AND user_id = ?", id, userID).Updates(updates).Error
}

func (r *userNotificationRepository) MarkAllRead(ctx context.Context, userID string, at time.Time) (int64, error) {
	db := r.db.WithContext(ctx)
	if err := db.Model(&types.UserNotification{}).
		Where("user_id = ? AND read_at IS NULL AND seen_at IS NULL", userID).Update("seen_at", at).Error; err != nil {
		return 0, err
	}
	res := db.Model(&types.UserNotification{}).Where("user_id = ? AND read_at IS NULL", userID).
		Updates(map[string]any{"read_at": at, "dedupe_key": gorm.Expr(types.UserNotificationRotateOpenKeySQL)})
	return res.RowsAffected, res.Error
}

// errOpenDigestMoved: the open digest was read (its key rotated) between
// the insert attempt and the lock; the upsert starts over.
var errOpenDigestMoved = errors.New("user notification: open digest moved")

// upsertOpenDigestAttempts bounds the retries of UpsertOpenDigest after a
// digest was read meanwhile; upsertOpenDigestLockRetries those after
// SQLite's table lock (unit tests only: Postgres waits on the lock).
const (
	upsertOpenDigestAttempts    = 5
	upsertOpenDigestLockRetries = 50
)

func (r *userNotificationRepository) UpsertOpenDigest(
	ctx context.Context, n *types.UserNotification, merge func(prev, next *types.UserNotification),
) (bool, error) {
	if n == nil || !types.IsUserNotificationOpenKey(n.DedupeKey) {
		return false, errors.New("user notification: an open digest key is required")
	}
	if merge == nil {
		return false, errors.New("user notification: merge is required")
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	draft := *n
	var lastErr error
	for attempt, locked := 0, 0; attempt < upsertOpenDigestAttempts && locked < upsertOpenDigestLockRetries; {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		next := draft
		created := false
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// the unique key makes a concurrent insert wait for this one
			// (Postgres) and then find the row below
			ok, err := createUserNotification(tx, &next)
			if err != nil || ok {
				created = ok
				return err
			}
			q := tx
			if tx.Dialector.Name() == "postgres" {
				q = q.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			var prev types.UserNotification
			err = q.Where("dedupe_key = ? AND read_at IS NULL", draft.DedupeKey).Take(&prev).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// read in between (the key moved with it), or a read row
				// still holds the key: freed below, outside this rollback
				return errOpenDigestMoved
			}
			if err != nil {
				return err
			}
			if prev.UserID != draft.UserID || prev.Kind != draft.Kind {
				return fmt.Errorf("user notification: open key %q held by another user or kind", draft.DedupeKey)
			}
			next = draft
			merge(&prev, &next)
			if !types.ValidUserNotificationLink(next.Link) {
				return fmt.Errorf("user notification: link %q is not an app path under %s", next.Link, types.UserNotificationLinkPrefix)
			}
			next.ID = prev.ID
			// on top of the list again, and toasted again
			return tx.Model(&types.UserNotification{}).Where("id = ?", prev.ID).Updates(map[string]any{
				"tenant_id":  next.TenantID,
				"title":      next.Title,
				"body":       next.Body,
				"link":       next.Link,
				"payload":    next.Payload,
				"created_at": next.CreatedAt,
				"seen_at":    nil,
			}).Error
		})
		if err == nil {
			*n = next
			return created, nil
		}
		lastErr = err
		switch {
		case errors.Is(err, errOpenDigestMoved):
			attempt++
			// a read row holding the open key (written before reads rotated
			// keys, or by hand) would block every insert: free the key in a
			// committed step of its own
			if ferr := r.db.WithContext(ctx).Model(&types.UserNotification{}).
				Where("dedupe_key = ? AND read_at IS NOT NULL", draft.DedupeKey).
				Update("dedupe_key", gorm.Expr(types.UserNotificationRotateOpenKeySQL)).Error; ferr != nil {
				if !isSQLiteLockError(ferr) {
					return false, ferr
				}
				locked++
			}
		case isSQLiteLockError(err):
			locked++
			time.Sleep(time.Duration(1+rand.IntN(10)) * time.Millisecond)
		default:
			return false, err
		}
	}
	return false, lastErr
}

func (r *userNotificationRepository) CloseOpenDigests(ctx context.Context, kind string, before, at time.Time) (int64, error) {
	if kind == "" {
		return 0, errors.New("user notification: kind is required")
	}
	res := r.db.WithContext(ctx).Model(&types.UserNotification{}).
		Where("kind = ? AND read_at IS NULL AND created_at < ? AND dedupe_key LIKE ?", kind, before, "%:open").
		Updates(map[string]any{
			"read_at":    at,
			"seen_at":    gorm.Expr("COALESCE(seen_at, ?)", at),
			"dedupe_key": gorm.Expr(types.UserNotificationRotateOpenKeySQL),
		})
	return res.RowsAffected, res.Error
}

func (r *userNotificationRepository) LatestOfKind(ctx context.Context, userID, kind string) (*types.UserNotification, error) {
	var n types.UserNotification
	err := r.db.WithContext(ctx).Where("user_id = ? AND kind = ?", userID, kind).
		Order("created_at DESC").Order("id DESC").Take(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// isSQLiteLockError: the in-memory SQLite of the unit tests refused a
// concurrent write (shared-cache table lock or busy); worth a retry.
func isSQLiteLockError(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked)
}

func (r *userNotificationRepository) PurgeRead(ctx context.Context, readBefore time.Time, batch int) (int64, error) {
	if batch <= 0 {
		batch = 1000
	}
	var total int64
	for {
		// DELETE … LIMIT is not portable: delete a batch of ids at a time
		res := r.db.WithContext(ctx).Exec(`DELETE FROM user_notifications WHERE id IN (
			SELECT id FROM user_notifications WHERE read_at IS NOT NULL AND read_at < ? LIMIT ?)`, readBefore, batch)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < int64(batch) || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}
