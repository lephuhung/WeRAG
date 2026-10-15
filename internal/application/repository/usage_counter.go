package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type usageCounterRepository struct {
	db *gorm.DB
}

// NewUsageCounterRepository constructs the repo against db.
func NewUsageCounterRepository(db *gorm.DB) interfaces.UsageCounterRepository {
	return &usageCounterRepository{db: db}
}

type usageCounterKey struct {
	scopeType, scopeID, metric, periodKey string
}

// Add is one multi-row INSERT ... ON CONFLICT DO UPDATE value = value + new.
// Duplicate keys are merged first: Postgres refuses to touch the same row
// twice in one upsert.
func (r *usageCounterRepository) Add(ctx context.Context, increments []types.UsageIncrement) error {
	merged := make(map[usageCounterKey]int64, len(increments))
	order := make([]usageCounterKey, 0, len(increments))
	for _, inc := range increments {
		if inc.Delta == 0 || inc.ScopeType == "" || inc.ScopeID == "" || inc.Metric == "" || inc.PeriodKey == "" {
			continue
		}
		key := usageCounterKey{inc.ScopeType, inc.ScopeID, inc.Metric, inc.PeriodKey}
		if _, seen := merged[key]; !seen {
			order = append(order, key)
		}
		merged[key] += inc.Delta
	}
	if len(order) == 0 {
		return nil
	}
	now := time.Now()
	rows := make([]types.UsageCounter, 0, len(order))
	for _, key := range order {
		rows = append(rows, types.UsageCounter{
			ScopeType: key.scopeType,
			ScopeID:   key.scopeID,
			Metric:    key.metric,
			PeriodKey: key.periodKey,
			Value:     merged[key],
			UpdatedAt: now,
		})
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "scope_type"}, {Name: "scope_id"}, {Name: "metric"}, {Name: "period_key"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"value":      gorm.Expr("usage_counters.value + excluded.value"),
				"updated_at": now,
			}),
		}).
		Create(&rows).Error
}

func (r *usageCounterRepository) Get(
	ctx context.Context, scopeType, scopeID string, periodKeys []string,
) ([]*types.UsageCounter, error) {
	if len(periodKeys) == 0 {
		return nil, nil
	}
	var rows []*types.UsageCounter
	err := r.db.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ? AND period_key IN ?", scopeType, scopeID, periodKeys).
		Find(&rows).Error
	return rows, err
}

func (r *usageCounterRepository) SumByPeriod(
	ctx context.Context, scopeType, periodKey string,
) ([]*types.UsageCounter, error) {
	var rows []*types.UsageCounter
	err := r.db.WithContext(ctx).
		Model(&types.UsageCounter{}).
		Select("metric, SUM(value) AS value").
		Where("scope_type = ? AND period_key = ?", scopeType, periodKey).
		Group("metric").
		Scan(&rows).Error
	return rows, err
}

func (r *usageCounterRepository) DeleteScope(ctx context.Context, scopeType, scopeID string) error {
	if scopeType != types.UsageScopeSession || scopeID == "" {
		return fmt.Errorf("usage counters: only session scopes can be reset, not %q", scopeType)
	}
	return r.db.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ?", scopeType, scopeID).
		Delete(&types.UsageCounter{}).Error
}

// ClaimMessageUsage flips messages.usage_metered_at from NULL exactly once.
// The column is deliberately not on types.Message: UpdateMessage writes the
// struct with Updates(), and a mapped field would race with this claim.
func (r *usageCounterRepository) ClaimMessageUsage(ctx context.Context, messageID string) (bool, error) {
	res := r.db.WithContext(ctx).Exec(
		"UPDATE messages SET usage_metered_at = ? WHERE id = ? AND usage_metered_at IS NULL",
		time.Now(), messageID,
	)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
