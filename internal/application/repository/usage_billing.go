package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

func (r *usageBillingRepository) conn(ctx context.Context) *gorm.DB {
	return connFromContext(ctx, r.db)
}

// InTransaction runs fn in one transaction shared by this repository and
// the usage plan repository (they read it from ctx).
func (r *usageBillingRepository) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return runInTransaction(ctx, r.db, fn)
}

// LockScope serialises the orders of one scope until the transaction ends,
// so two confirmations cannot both extend the same subscription from the
// same end date.
func (r *usageBillingRepository) LockScope(ctx context.Context, scopeType, scopeID string) error {
	return r.conn(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
		"usage-scope:"+scopeType+":"+scopeID).Error
}

func (r *usageBillingRepository) ListPromos(ctx context.Context, includeInactive bool) ([]*types.UsagePromoCode, error) {
	q := r.conn(ctx).Order("created_at DESC")
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
	if err := r.conn(ctx).Where("id = ?", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *usageBillingRepository) GetPromoByCode(ctx context.Context, code string) (*types.UsagePromoCode, error) {
	var p types.UsagePromoCode
	err := r.conn(ctx).Where("code = ?", code).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *usageBillingRepository) CreatePromo(ctx context.Context, promo *types.UsagePromoCode) error {
	return r.conn(ctx).Create(promo).Error
}

// UpdatePromo writes every column except the use counter, which only
// AddPromoUse touches so concurrent orders cannot lose increments.
func (r *usageBillingRepository) UpdatePromo(ctx context.Context, promo *types.UsagePromoCode) error {
	promo.UpdatedAt = time.Now()
	return r.conn(ctx).Model(promo).Select("*").
		Omit("id", "created_at", "used_count").Updates(promo).Error
}

func (r *usageBillingRepository) DeletePromo(ctx context.Context, id string) error {
	return r.conn(ctx).Where("id = ?", id).Delete(&types.UsagePromoCode{}).Error
}

func (r *usageBillingRepository) AddPromoUse(ctx context.Context, id string, delta int) error {
	return r.conn(ctx).Model(&types.UsagePromoCode{}).Where("id = ?", id).
		UpdateColumn("used_count", gorm.Expr("GREATEST(used_count + ?, 0)", delta)).Error
}

// ReservePromoUse takes one use of the code only while uses remain: the
// check and the increment are one UPDATE, so concurrent orders cannot both
// take the last use.
func (r *usageBillingRepository) ReservePromoUse(ctx context.Context, id string) (bool, error) {
	res := r.conn(ctx).Model(&types.UsagePromoCode{}).
		Where("id = ? AND (max_uses <= 0 OR used_count < max_uses)", id).
		UpdateColumn("used_count", gorm.Expr("used_count + 1"))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *usageBillingRepository) CreateOrder(ctx context.Context, order *types.UsageOrder) error {
	return r.conn(ctx).Create(order).Error
}

func (r *usageBillingRepository) GetOrder(ctx context.Context, id string) (*types.UsageOrder, error) {
	var o types.UsageOrder
	if err := r.conn(ctx).Where("id = ?", id).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOrderForUpdate reads an order and locks its row until the
// transaction ends.
func (r *usageBillingRepository) GetOrderForUpdate(ctx context.Context, id string) (*types.UsageOrder, error) {
	var o types.UsageOrder
	err := r.conn(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&o).Error
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *usageBillingRepository) GetOrderByCode(ctx context.Context, code string) (*types.UsageOrder, error) {
	var o types.UsageOrder
	if err := r.conn(ctx).Where("order_code = ?", code).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *usageBillingRepository) ListOrders(ctx context.Context, filter types.UsageOrderFilter) ([]*types.UsageOrder, error) {
	q := r.conn(ctx).Order("created_at DESC")
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
	return r.conn(ctx).Model(order).Select("*").Omit("id", "created_at").Updates(order).Error
}

// ExpirePendingOrders flips due pending orders in one UPDATE and returns
// only the rows this call flipped (RETURNING): a concurrent call never
// gets the same order, so its promo use is released once.
func (r *usageBillingRepository) ExpirePendingOrders(ctx context.Context, now time.Time) ([]*types.UsageOrder, error) {
	var expired []*types.UsageOrder
	err := r.conn(ctx).Model(&expired).Clauses(clause.Returning{}).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at < ?", types.UsageOrderPending, now).
		Updates(map[string]interface{}{"status": types.UsageOrderExpired, "updated_at": now}).Error
	if err != nil {
		return nil, err
	}
	return expired, nil
}
