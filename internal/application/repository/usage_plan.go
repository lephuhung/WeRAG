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

type usagePlanRepository struct {
	db *gorm.DB
}

// NewUsagePlanRepository constructs the repo against db.
func NewUsagePlanRepository(db *gorm.DB) interfaces.UsagePlanRepository {
	return &usagePlanRepository{db: db}
}

func (r *usagePlanRepository) conn(ctx context.Context) *gorm.DB {
	return connFromContext(ctx, r.db)
}

func (r *usagePlanRepository) ListPlans(ctx context.Context, includeInactive bool) ([]*types.UsagePlan, error) {
	q := r.conn(ctx).Order("sort_order ASC, created_at ASC")
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
	if err := r.conn(ctx).Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *usagePlanRepository) CreatePlan(ctx context.Context, plan *types.UsagePlan) error {
	return r.conn(ctx).Create(plan).Error
}

// UpdatePlan writes every column so a cleared description or a false
// is_active is saved too (Updates(struct) would skip zero values).
func (r *usagePlanRepository) UpdatePlan(ctx context.Context, plan *types.UsagePlan) error {
	plan.UpdatedAt = time.Now()
	return r.conn(ctx).Model(plan).Select("*").Omit("id", "created_at").Updates(plan).Error
}

func (r *usagePlanRepository) DeletePlan(ctx context.Context, id string) error {
	return r.conn(ctx).Where("id = ?", id).Delete(&types.UsagePlan{}).Error
}

func (r *usagePlanRepository) CountSubscriptionsByPlan(ctx context.Context, planID string) (int64, error) {
	var n int64
	err := r.conn(ctx).Model(&types.UsageSubscription{}).Where("plan_id = ?", planID).Count(&n).Error
	return n, err
}

func (r *usagePlanRepository) ListSubscriptions(
	ctx context.Context, filter types.UsageSubscriptionFilter,
) ([]*types.UsageSubscription, error) {
	q := r.conn(ctx).Preload("Plan").Order("created_at DESC")
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
	if err := r.conn(ctx).Preload("Plan").Where("id = ?", id).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *usagePlanRepository) GetActiveSubscription(
	ctx context.Context, scopeType, scopeID string,
) (*types.UsageSubscription, error) {
	var sub types.UsageSubscription
	err := r.conn(ctx).Preload("Plan").
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
	return r.conn(ctx).Omit("Plan").Create(sub).Error
}

func (r *usagePlanRepository) UpdateSubscription(ctx context.Context, sub *types.UsageSubscription) error {
	sub.UpdatedAt = time.Now()
	return r.conn(ctx).Model(sub).Select("*").Omit("id", "created_at", "Plan").Updates(sub).Error
}

func (r *usagePlanRepository) DeleteSubscription(ctx context.Context, id string) error {
	return r.conn(ctx).Where("id = ?", id).Delete(&types.UsageSubscription{}).Error
}

func (r *usagePlanRepository) ListSeats(ctx context.Context, subscriptionID string) ([]*types.UsageSeat, error) {
	var seats []*types.UsageSeat
	err := r.conn(ctx).Where("subscription_id = ?", subscriptionID).Order("assigned_at ASC").Find(&seats).Error
	if err != nil {
		return nil, err
	}
	return seats, nil
}

func (r *usagePlanRepository) CountSeats(ctx context.Context, subscriptionID string) (int64, error) {
	var n int64
	err := r.conn(ctx).Model(&types.UsageSeat{}).Where("subscription_id = ?", subscriptionID).Count(&n).Error
	return n, err
}

func (r *usagePlanRepository) HasSeat(ctx context.Context, subscriptionID, userID string) (bool, error) {
	var n int64
	err := r.conn(ctx).Model(&types.UsageSeat{}).
		Where("subscription_id = ? AND user_id = ?", subscriptionID, userID).Count(&n).Error
	return n > 0, err
}

func (r *usagePlanRepository) AddSeat(ctx context.Context, seat *types.UsageSeat) error {
	return r.conn(ctx).Create(seat).Error
}

func (r *usagePlanRepository) RemoveSeat(ctx context.Context, subscriptionID, userID string) error {
	return r.conn(ctx).
		Where("subscription_id = ? AND user_id = ?", subscriptionID, userID).Delete(&types.UsageSeat{}).Error
}

func (r *usagePlanRepository) ListSubscriptionAddons(
	ctx context.Context, subscriptionID string,
) ([]*types.UsageSubscriptionAddon, error) {
	var addons []*types.UsageSubscriptionAddon
	if err := r.conn(ctx).Where("subscription_id = ?", subscriptionID).
		Order("created_at ASC").Find(&addons).Error; err != nil {
		return nil, err
	}
	if len(addons) == 0 {
		return addons, nil
	}
	ids := make([]string, 0, len(addons))
	for _, a := range addons {
		ids = append(ids, a.AgentID)
	}
	var rows []struct {
		ID   string
		Name string
	}
	if err := r.conn(ctx).Table("custom_agents").Select("id, name").
		Where("id IN ? AND deleted_at IS NULL", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	for _, a := range addons {
		a.AgentName = names[a.AgentID]
	}
	return addons, nil
}

func (r *usagePlanRepository) UpsertSubscriptionAddon(ctx context.Context, addon *types.UsageSubscriptionAddon) error {
	now := time.Now()
	if addon.CreatedAt.IsZero() {
		addon.CreatedAt = now
	}
	addon.UpdatedAt = now
	return r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "subscription_id"}, {Name: "agent_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"starts_at", "ends_at", "price_vnd", "order_id", "granted_by", "updated_at"}),
	}).Create(addon).Error
}

func (r *usagePlanRepository) DeleteSubscriptionAddon(ctx context.Context, subscriptionID, agentID string) (bool, error) {
	res := r.conn(ctx).Where("subscription_id = ? AND agent_id = ?", subscriptionID, agentID).
		Delete(&types.UsageSubscriptionAddon{})
	return res.RowsAffected > 0, res.Error
}

func (r *usagePlanRepository) CustomAgentDistribution(ctx context.Context, agentID string) (string, bool, error) {
	var rows []struct{ Distribution string }
	if err := r.conn(ctx).Table("custom_agents").Select("distribution").
		Where("id = ? AND is_builtin = ? AND deleted_at IS NULL", agentID, false).
		Limit(1).Scan(&rows).Error; err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	return rows[0].Distribution, true, nil
}
