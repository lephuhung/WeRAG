package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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
	res := db.Model(&types.UserNotification{}).Where("user_id = ? AND read_at IS NULL", userID).Update("read_at", at)
	return res.RowsAffected, res.Error
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
