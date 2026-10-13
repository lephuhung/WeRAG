package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type usageBillingRepository struct {
	db *gorm.DB
}

// NewUsageBillingRepository constructs the repo against db.
func NewUsageBillingRepository(db *gorm.DB) interfaces.UsageBillingRepository {
	return &usageBillingRepository{db: db}
}

func (r *usageBillingRepository) ListPromos(ctx context.Context, includeInactive bool) ([]*types.UsagePromoCode, error) {
	q := r.db.WithContext(ctx).Order("created_at DESC")
	if !includeInactive {
		q = q.Where("is_active = ?", true)
	}
	var out []*types.UsagePromoCode
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *usageBillingRepository) GetPromo(ctx context.Context, id string) (*types.UsagePromoCode, error) {
	var p types.UsagePromoCode
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *usageBillingRepository) GetPromoByCode(ctx context.Context, code string) (*types.UsagePromoCode, error) {
	var p types.UsagePromoCode
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *usageBillingRepository) CreatePromo(ctx context.Context, promo *types.UsagePromoCode) error {
	return r.db.WithContext(ctx).Create(promo).Error
}

// UpdatePromo writes every column except the use counter, which only
// AddPromoUse touches so concurrent orders cannot lose increments.
func (r *usageBillingRepository) UpdatePromo(ctx context.Context, promo *types.UsagePromoCode) error {
	promo.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(promo).Select("*").
		Omit("id", "created_at", "used_count").Updates(promo).Error
}

func (r *usageBillingRepository) DeletePromo(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.UsagePromoCode{}).Error
}

func (r *usageBillingRepository) AddPromoUse(ctx context.Context, id string, delta int) error {
	return r.db.WithContext(ctx).Model(&types.UsagePromoCode{}).Where("id = ?", id).
		UpdateColumn("used_count", gorm.Expr("GREATEST(used_count + ?, 0)", delta)).Error
}

func (r *usageBillingRepository) CreateOrder(ctx context.Context, order *types.UsageOrder) error {
	return r.db.WithContext(ctx).Create(order).Error
}

func (r *usageBillingRepository) GetOrder(ctx context.Context, id string) (*types.UsageOrder, error) {
	var o types.UsageOrder
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *usageBillingRepository) GetOrderByCode(ctx context.Context, code string) (*types.UsageOrder, error) {
	var o types.UsageOrder
	if err := r.db.WithContext(ctx).Where("order_code = ?", code).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *usageBillingRepository) ListOrders(ctx context.Context, filter types.UsageOrderFilter) ([]*types.UsageOrder, error) {
	q := r.db.WithContext(ctx).Order("created_at DESC")
	if filter.ScopeType != "" {
		q = q.Where("scope_type = ?", filter.ScopeType)
	}
	if filter.ScopeID != "" {
		q = q.Where("scope_id = ?", filter.ScopeID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.CreatedBy != "" {
		q = q.Where("created_by = ?", filter.CreatedBy)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	var out []*types.UsageOrder
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *usageBillingRepository) UpdateOrder(ctx context.Context, order *types.UsageOrder) error {
	order.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(order).Select("*").Omit("id", "created_at").Updates(order).Error
}

func (r *usageBillingRepository) ExpirePendingOrders(ctx context.Context, now time.Time) ([]*types.UsageOrder, error) {
	var due []*types.UsageOrder
	err := r.db.WithContext(ctx).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at < ?", types.UsageOrderPending, now).
		Find(&due).Error
	if err != nil || len(due) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(due))
	for _, o := range due {
		ids = append(ids, o.ID)
	}
	err = r.db.WithContext(ctx).Model(&types.UsageOrder{}).
		Where("id IN ? AND status = ?", ids, types.UsageOrderPending).
		Updates(map[string]interface{}{"status": types.UsageOrderExpired, "updated_at": now}).Error
	if err != nil {
		return nil, err
	}
	for _, o := range due {
		o.Status = types.UsageOrderExpired
	}
	return due, nil
}
