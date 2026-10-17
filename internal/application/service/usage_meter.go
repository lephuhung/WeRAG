package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// usageMeter implements interfaces.UsageMeter on top of usage_counters.
//
// Phase 1 of usage packages only observes: nothing here blocks a turn. The
// pre-flight checks of phase 2 will read the same counters.
type usageMeter struct {
	repo interfaces.UsageCounterRepository
	now  func() time.Time
}

// NewUsageMeter creates the meter.
func NewUsageMeter(repo interfaces.UsageCounterRepository) interfaces.UsageMeter {
	return &usageMeter{repo: repo, now: time.Now}
}

// usageScopes lists the counters a turn belongs to: always the workspace,
// plus the person when userID is a real account (the IM and API paths run
// as synthetic system-<tenant> users, which only the workspace scope sees).
func usageScopes(tenantID uint64, userID string) [][2]string {
	scopes := make([][2]string, 0, 2)
	if tenantID != 0 {
		scopes = append(scopes, [2]string{types.UsageScopeTenant, types.UsageTenantScopeID(tenantID)})
	}
	if userID != "" && !types.IsSyntheticUserID(userID) {
		scopes = append(scopes, [2]string{types.UsageScopeUser, userID})
	}
	return scopes
}

// buildUsageIncrements expands metric deltas over every scope and period.
// A turn of a known session also counts toward that session's lifetime
// counters (scope "session", period "all"), which only the per-session caps
// read.
func buildUsageIncrements(
	tenantID uint64, userID, sessionID string, at time.Time, deltas map[string]int64,
) []types.UsageIncrement {
	keys := types.UsagePeriodKeysAt(at)
	scopes := usageScopes(tenantID, userID)
	out := make([]types.UsageIncrement, 0, (len(scopes)*2+1)*len(deltas))
	for metric, delta := range deltas {
		if delta <= 0 {
			continue
		}
		for _, scope := range scopes {
			for _, period := range []string{keys.Day, keys.Month} {
				out = append(out, types.UsageIncrement{
					ScopeType: scope[0], ScopeID: scope[1], Metric: metric, PeriodKey: period, Delta: delta,
				})
			}
		}
		if sessionID != "" {
			out = append(out, types.UsageIncrement{
				ScopeType: types.UsageScopeSession, ScopeID: sessionID, Metric: metric,
				PeriodKey: types.UsagePeriodAll, Delta: delta,
			})
		}
	}
	return out
}

func (m *usageMeter) add(ctx context.Context, increments []types.UsageIncrement) {
	if len(increments) == 0 {
		return
	}
	// The turn is over or persisting; a client disconnect must not lose the count.
	if err := m.repo.Add(context.WithoutCancel(ctx), increments); err != nil {
		logger.Warnf(ctx, "usage meter: failed to add %d counters: %v", len(increments), err)
	}
}

func (m *usageMeter) RecordQuestion(ctx context.Context, tenantID uint64, userID, sessionID string) {
	m.add(ctx, buildUsageIncrements(tenantID, userID, sessionID, m.now(), map[string]int64{
		types.UsageMetricQuestions: 1,
	}))
}

func (m *usageMeter) RecordMessageUsage(ctx context.Context, msg *types.Message) {
	if msg == nil || msg.ID == "" || msg.Role != "assistant" || msg.Usage == nil {
		return
	}
	u := msg.Usage
	total := u.TotalTokens
	if total <= 0 {
		total = u.PromptTokens + u.CompletionTokens
	}
	if total <= 0 {
		return
	}
	tenantID, _ := types.TenantIDFromContext(ctx)
	userID, _ := types.UserIDFromContext(ctx)
	increments := buildUsageIncrements(tenantID, userID, msg.SessionID, m.now(), map[string]int64{
		types.UsageMetricTokens:           int64(total),
		types.UsageMetricPromptTokens:     int64(u.PromptTokens),
		types.UsageMetricCompletionTokens: int64(u.CompletionTokens),
	})
	// The mark and the counters commit together: a failure leaves the
	// message uncounted for the next save to count. A client disconnect
	// must not lose the count.
	if _, err := m.repo.AddMessageUsage(context.WithoutCancel(ctx), msg.ID, increments); err != nil {
		logger.Warnf(ctx, "usage meter: counting message %s failed: %v", msg.ID, err)
	}
}

func (m *usageMeter) RecordOCRUsage(
	ctx context.Context, tenantID uint64, userID, sessionID string, pages int, usage types.VLMUsage,
) {
	total := usage.Total()
	if total <= 0 && pages <= 0 {
		return
	}
	m.add(context.WithoutCancel(ctx), buildUsageIncrements(tenantID, userID, sessionID, m.now(), map[string]int64{
		types.UsageMetricTokens:           int64(total),
		types.UsageMetricPromptTokens:     int64(usage.PromptTokens),
		types.UsageMetricCompletionTokens: int64(usage.CompletionTokens),
		types.UsageMetricOCRTokens:        int64(total),
		types.UsageMetricOCRPages:         int64(pages),
	}))
}

func (m *usageMeter) ResetSession(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}
	if err := m.repo.DeleteScope(context.WithoutCancel(ctx), types.UsageScopeSession, sessionID); err != nil {
		logger.Warnf(ctx, "usage meter: failed to reset session %s counters: %v", sessionID, err)
	}
}

func (m *usageMeter) scopeSummary(ctx context.Context, scopeType, scopeID string, keys types.UsagePeriodKeys) (*types.UsageScopeSummary, error) {
	rows, err := m.repo.Get(ctx, scopeType, scopeID, []string{keys.Day, keys.Month})
	if err != nil {
		return nil, err
	}
	summary := &types.UsageScopeSummary{
		ScopeType: scopeType,
		ScopeID:   scopeID,
		Today:     types.UsagePeriodTotals{PeriodKey: keys.Day},
		Month:     types.UsagePeriodTotals{PeriodKey: keys.Month},
	}
	for _, row := range rows {
		switch row.PeriodKey {
		case keys.Day:
			summary.Today.AddMetric(row.Metric, row.Value)
		case keys.Month:
			summary.Month.AddMetric(row.Metric, row.Value)
		}
	}
	return summary, nil
}

func (m *usageMeter) Summary(ctx context.Context, tenantID uint64, userID string) (*types.UsageSummary, error) {
	keys := types.UsagePeriodKeysAt(m.now())
	out := &types.UsageSummary{Timezone: types.UsageTimezone}
	for _, scope := range usageScopes(tenantID, userID) {
		s, err := m.scopeSummary(ctx, scope[0], scope[1], keys)
		if err != nil {
			return nil, err
		}
		if scope[0] == types.UsageScopeTenant {
			out.Tenant = s
		} else {
			out.User = s
		}
	}
	return out, nil
}

func (m *usageMeter) PlatformTotals(ctx context.Context) (*types.UsageStats, error) {
	keys := types.UsagePeriodKeysAt(m.now())
	out := &types.UsageStats{
		Timezone: types.UsageTimezone,
		Today:    types.UsagePeriodTotals{PeriodKey: keys.Day},
		Month:    types.UsagePeriodTotals{PeriodKey: keys.Month},
	}
	for _, period := range []struct {
		key    string
		totals *types.UsagePeriodTotals
	}{{keys.Day, &out.Today}, {keys.Month, &out.Month}} {
		rows, err := m.repo.SumByPeriod(ctx, types.UsageScopeTenant, period.key)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			period.totals.AddMetric(row.Metric, row.Value)
		}
	}
	return out, nil
}
