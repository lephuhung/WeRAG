package repository

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type kbSubscriptionRepository struct {
	db *gorm.DB
}

// NewKBSubscriptionRepository constructs the repo against db.
func NewKBSubscriptionRepository(db *gorm.DB) interfaces.KBSubscriptionRepository {
	return &kbSubscriptionRepository{db: db}
}

func (r *kbSubscriptionRepository) Subscribe(ctx context.Context, sub *types.KBSubscription) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "kb_id"}, {Name: "tenant_id"}, {Name: "user_id"}},
			DoNothing: true,
		}).
		Create(sub).Error
}

func (r *kbSubscriptionRepository) Unsubscribe(ctx context.Context, kbID string, tenantID uint64, userID string) error {
	return r.db.WithContext(ctx).
		Where("kb_id = ? AND tenant_id = ? AND user_id = ?", kbID, tenantID, userID).
		Delete(&types.KBSubscription{}).Error
}

func (r *kbSubscriptionRepository) ListForReader(
	ctx context.Context, tenantID uint64, userID string,
) ([]*types.KBSubscription, error) {
	var rows []*types.KBSubscription
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND (user_id = '' OR user_id = ?)", tenantID, userID).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

func (r *kbSubscriptionRepository) DeleteByKnowledgeBaseID(ctx context.Context, kbID string) error {
	return r.db.WithContext(ctx).Where("kb_id = ?", kbID).Delete(&types.KBSubscription{}).Error
}

func (r *kbSubscriptionRepository) ListPublishedCatalog(
	ctx context.Context, keyword string, limit, offset int,
) ([]*types.KnowledgeBase, int64, error) {
	if limit <= 0 {
		limit = types.PublicCatalogDefaultPageSize
	}
	if offset < 0 {
		offset = 0
	}
	base := r.db.WithContext(ctx).Model(&types.KnowledgeBase{}).
		Where("is_temporary = ?", false).
		Where("owner_tenant_id <> 0 AND visibility = ?", string(types.KBVisibilityPublished))
	if q := strings.TrimSpace(keyword); q != "" {
		like := "%" + escapeLikeKeyword(q) + "%"
		base = base.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var kbs []*types.KnowledgeBase
	if err := catalogOrder(base).Limit(limit).Offset(offset).Find(&kbs).Error; err != nil {
		return nil, 0, err
	}
	return kbs, total, nil
}
