package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type emailNotificationRepository struct {
	db *gorm.DB
}

// NewEmailNotificationRepository creates the repository.
func NewEmailNotificationRepository(db *gorm.DB) interfaces.EmailNotificationRepository {
	return &emailNotificationRepository{db: db}
}

func (r *emailNotificationRepository) Claim(ctx context.Context, n *types.EmailNotification) (bool, error) {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dedupe_key"}},
		DoNothing: true,
	}).Create(n)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *emailNotificationRepository) Release(ctx context.Context, dedupeKey string) error {
	return r.db.WithContext(ctx).Where("dedupe_key = ?", dedupeKey).Delete(&types.EmailNotification{}).Error
}

func (r *emailNotificationRepository) ReleaseStale(ctx context.Context, kind string, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("kind = ? AND recipients = ? AND created_at < ?", kind, types.EmailRecipientsPending, before).
		Delete(&types.EmailNotification{})
	return res.RowsAffected, res.Error
}

func (r *emailNotificationRepository) SetRecipients(ctx context.Context, dedupeKey string, n int) error {
	return r.db.WithContext(ctx).Model(&types.EmailNotification{}).
		Where("dedupe_key = ?", dedupeKey).Update("recipients", n).Error
}

func (r *emailNotificationRepository) ListSubscriptionsEndingBetween(
	ctx context.Context, from, to time.Time,
) ([]*types.UsageSubscription, error) {
	var subs []*types.UsageSubscription
	err := r.db.WithContext(ctx).Preload("Plan").
		Where("status = ? AND ends_at IS NOT NULL AND ends_at > ? AND ends_at <= ?",
			types.UsageSubscriptionActive, from, to).
		Order("ends_at").Find(&subs).Error
	return subs, err
}
