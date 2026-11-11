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
	return addUsageCounters(r.db.WithContext(ctx), increments)
}

func addUsageCounters(db *gorm.DB, increments []types.UsageIncrement) error {
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
	return db.
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

func (r *usageCounterRepository) GetMany(
	ctx context.Context, scopeType string, scopeIDs, periodKeys []string,
) ([]*types.UsageCounter, error) {
	if len(scopeIDs) == 0 || len(periodKeys) == 0 {
		return nil, nil
	}
	var rows []*types.UsageCounter
	err := r.db.WithContext(ctx).
		Where("scope_type = ? AND scope_id IN ? AND period_key IN ?", scopeType, scopeIDs, periodKeys).
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

// AddMessageUsage flips messages.usage_metered_at from NULL and adds the
// message's increments in one transaction: a failed add leaves the message
// unmarked, so a later save counts it, and a message saved several times
// is counted once. The column is deliberately not on types.Message:
// UpdateMessage writes the struct with Updates(), and a mapped field would
// race with this claim.
func (r *usageCounterRepository) AddMessageUsage(
	ctx context.Context, messageID string, increments []types.UsageIncrement,
) (bool, error) {
	claimed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			"UPDATE messages SET usage_metered_at = ? WHERE id = ? AND usage_metered_at IS NULL",
			time.Now(), messageID,
		)
		if res.Error != nil || res.RowsAffected != 1 {
			return res.Error
		}
		if err := addUsageCounters(tx, increments); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

var _ interfaces.UsageMemberReportRepository = (*usageCounterRepository)(nil)

// ListMemberCounters implements interfaces.UsageMemberReportRepository.
// The prefix is digits and a colon, so LIKE needs no escaping; the leading
// primary-key column (scope_type) keeps the scan to member rows.
func (r *usageCounterRepository) ListMemberCounters(
	ctx context.Context, tenantID uint64, monthKey string,
) ([]*types.UsageCounter, error) {
	if tenantID == 0 || monthKey == "" {
		return nil, nil
	}
	var rows []*types.UsageCounter
	err := r.db.WithContext(ctx).
		Where("scope_type = ? AND period_key = ? AND scope_id LIKE ?",
			types.UsageScopeMember, monthKey, types.UsageMemberScopePrefix(tenantID)+"%").
		Find(&rows).Error
	return rows, err
}

// memberMessageUsageRow is one session owner's sums in SumMessageUsageByUser.
type memberMessageUsageRow struct {
	UserID           string
	Questions        int64
	PromptTokens     int64
	CompletionTokens int64
	Tokens           int64
}

// SumMessageUsageByUser implements interfaces.UsageMemberReportRepository.
// The sums are cast back to BIGINT (Postgres widens SUM(bigint) to numeric).
// Tokens follow the meter: total_tokens, or prompt + completion when the
// provider reported no total.
func (r *usageCounterRepository) SumMessageUsageByUser(
	ctx context.Context, tenantID uint64, from, to time.Time,
) (map[string]types.UsagePeriodTotals, error) {
	pg := r.db.Dialector.Name() == "postgres"
	field := func(name string) string {
		if pg {
			return "COALESCE((m.usage->>'" + name + "')::bigint, 0)"
		}
		return "COALESCE(CAST(json_extract(m.usage, '$." + name + "') AS INTEGER), 0)"
	}
	prompt, completion, total := field("prompt_tokens"), field("completion_tokens"), field("total_tokens")
	// as migration 000136's backfill: only an object carries token figures
	assistant := "m.role = 'assistant' AND m.usage IS NOT NULL"
	if pg {
		assistant += " AND jsonb_typeof(m.usage) = 'object'"
	} else {
		assistant += " AND json_valid(m.usage) AND json_type(m.usage) = 'object'"
	}
	query := "SELECT s.user_id AS user_id," +
		" CAST(SUM(CASE WHEN m.role = 'user' THEN 1 ELSE 0 END) AS BIGINT) AS questions," +
		" CAST(SUM(CASE WHEN " + assistant + " THEN " + prompt + " ELSE 0 END) AS BIGINT) AS prompt_tokens," +
		" CAST(SUM(CASE WHEN " + assistant + " THEN " + completion + " ELSE 0 END) AS BIGINT) AS completion_tokens," +
		" CAST(SUM(CASE WHEN " + assistant + " THEN (CASE WHEN " + total + " > 0 THEN " + total +
		" ELSE " + prompt + " + " + completion + " END) ELSE 0 END) AS BIGINT) AS tokens" +
		" FROM messages m JOIN sessions s ON s.id = m.session_id" +
		" WHERE s.tenant_id = ? AND m.created_at >= ? AND m.created_at < ?" +
		" AND s.user_id IS NOT NULL AND s.user_id <> ''" +
		" GROUP BY s.user_id"
	var rows []memberMessageUsageRow
	// UTC instants: SQLite compares the stored text, Postgres the instant.
	if err := r.db.WithContext(ctx).Raw(query, tenantID, from.UTC(), to.UTC()).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]types.UsagePeriodTotals, len(rows))
	for _, row := range rows {
		out[row.UserID] = types.UsagePeriodTotals{
			Questions: row.Questions, Tokens: row.Tokens,
			PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens,
		}
	}
	return out, nil
}
