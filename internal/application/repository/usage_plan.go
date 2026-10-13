package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type usagePlanRepository struct {
	db *gorm.DB
}

// NewUsagePlanRepository constructs the repo against db.
func NewUsagePlanRepository(db *gorm.DB) interfaces.UsagePlanRepository {
	return &usagePlanRepository{db: db}
}

func (r *usagePlanRepository) ListPlans(ctx context.Context, includeInactive bool) ([]*types.UsagePlan, error) {
	q := r.db.WithContext(ctx).Order("sort_order ASC, created_at ASC")
	if !includeInactive {
		q = q.Where("is_active = ?", true)
	}
	var plans []*types.UsagePlan
	if err := q.Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *usagePlanRepository) GetPlan(ctx context.Context, id string) (*types.UsagePlan, error) {
	var plan types.UsagePlan
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *usagePlanRepository) CreatePlan(ctx context.Context, plan *types.UsagePlan) error {
	return r.db.WithContext(ctx).Create(plan).Error
}

// UpdatePlan writes every column so a cleared description or a false
// is_active is saved too (Updates(struct) would skip zero values).
func (r *usagePlanRepository) UpdatePlan(ctx context.Context, plan *types.UsagePlan) error {
	plan.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(plan).Select("*").Omit("id", "created_at").Updates(plan).Error
}

func (r *usagePlanRepository) DeletePlan(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.UsagePlan{}).Error
}

func (r *usagePlanRepository) CountSubscriptionsByPlan(ctx context.Context, planID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&types.UsageSubscription{}).Where("plan_id = ?", planID).Count(&n).Error
	return n, err
}

func (r *usagePlanRepository) ListSubscriptions(
	ctx context.Context, filter types.UsageSubscriptionFilter,
) ([]*types.UsageSubscription, error) {
	q := r.db.WithContext(ctx).Preload("Plan").Order("created_at DESC")
	if filter.ScopeType != "" {
		q = q.Where("scope_type = ?", filter.ScopeType)
	}
	if filter.ScopeID != "" {
		q = q.Where("scope_id = ?", filter.ScopeID)
	}
	if filter.PlanID != "" {
		q = q.Where("plan_id = ?", filter.PlanID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	var subs []*types.UsageSubscription
	if err := q.Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *usagePlanRepository) GetSubscription(ctx context.Context, id string) (*types.UsageSubscription, error) {
	var sub types.UsageSubscription
	if err := r.db.WithContext(ctx).Preload("Plan").Where("id = ?", id).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *usagePlanRepository) GetActiveSubscription(
	ctx context.Context, scopeType, scopeID string,
) (*types.UsageSubscription, error) {
	var sub types.UsageSubscription
	err := r.db.WithContext(ctx).Preload("Plan").
		Where("scope_type = ? AND scope_id = ? AND status = ?", scopeType, scopeID, types.UsageSubscriptionActive).
		First(&sub).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *usagePlanRepository) CreateSubscription(ctx context.Context, sub *types.UsageSubscription) error {
	return r.db.WithContext(ctx).Omit("Plan").Create(sub).Error
}

func (r *usagePlanRepository) UpdateSubscription(ctx context.Context, sub *types.UsageSubscription) error {
	sub.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(sub).Select("*").Omit("id", "created_at", "Plan").Updates(sub).Error
}

func (r *usagePlanRepository) DeleteSubscription(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.UsageSubscription{}).Error
}

func (r *usagePlanRepository) ListSeats(ctx context.Context, subscriptionID string) ([]*types.UsageSeat, error) {
	var seats []*types.UsageSeat
	err := r.db.WithContext(ctx).Where("subscription_id = ?", subscriptionID).Order("assigned_at ASC").Find(&seats).Error
	if err != nil {
		return nil, err
	}
	return seats, nil
}

func (r *usagePlanRepository) CountSeats(ctx context.Context, subscriptionID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&types.UsageSeat{}).Where("subscription_id = ?", subscriptionID).Count(&n).Error
	return n, err
}

func (r *usagePlanRepository) HasSeat(ctx context.Context, subscriptionID, userID string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&types.UsageSeat{}).
		Where("subscription_id = ? AND user_id = ?", subscriptionID, userID).Count(&n).Error
	return n > 0, err
}

func (r *usagePlanRepository) AddSeat(ctx context.Context, seat *types.UsageSeat) error {
	return r.db.WithContext(ctx).Create(seat).Error
}

func (r *usagePlanRepository) RemoveSeat(ctx context.Context, subscriptionID, userID string) error {
	return r.db.WithContext(ctx).
		Where("subscription_id = ? AND user_id = ?", subscriptionID, userID).Delete(&types.UsageSeat{}).Error
}
