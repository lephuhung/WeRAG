package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func sessionCounterRows(t *testing.T, repo *usageCounterRepository, scopeType, scopeID string) int {
	t.Helper()
	rows, err := repo.Get(context.Background(), scopeType, scopeID, []string{types.UsagePeriodAll, "2026-10"})
	require.NoError(t, err)
	return len(rows)
}

// Deleting a session drops its lifetime counters (they only serve its cap)
// and leaves other sessions' and the workspace's counters alone.
func TestSessionDeleteDropsSessionCounters(t *testing.T) {
	repo, db := newSessionRepositoryForTest(t)
	require.NoError(t, db.AutoMigrate(&types.UsageCounter{}))
	counters := NewUsageCounterRepository(db).(*usageCounterRepository)
	ctx := context.Background()
	a := createSessionForTest(t, db, 1, "alice")
	b := createSessionForTest(t, db, 1, "alice")
	c := createSessionForTest(t, db, 1, "alice")
	var incs []types.UsageIncrement
	for _, id := range []string{a.ID, b.ID, c.ID} {
		incs = append(incs, types.UsageIncrement{ScopeType: types.UsageScopeSession, ScopeID: id,
			Metric: types.UsageMetricQuestions, PeriodKey: types.UsagePeriodAll, Delta: 3})
	}
	incs = append(incs, types.UsageIncrement{ScopeType: types.UsageScopeTenant, ScopeID: "1",
		Metric: types.UsageMetricQuestions, PeriodKey: "2026-10", Delta: 9})
	require.NoError(t, counters.Add(ctx, incs))

	_, err := repo.Delete(ctx, 1, "alice", a.ID)
	require.NoError(t, err)
	require.Zero(t, sessionCounterRows(t, counters, types.UsageScopeSession, a.ID))
	require.Equal(t, 1, sessionCounterRows(t, counters, types.UsageScopeSession, b.ID))

	_, err = repo.BatchDelete(ctx, 1, "alice", []string{b.ID})
	require.NoError(t, err)
	require.Zero(t, sessionCounterRows(t, counters, types.UsageScopeSession, b.ID))

	_, err = repo.DeleteAllByTenantID(ctx, 1, "alice")
	require.NoError(t, err)
	require.Zero(t, sessionCounterRows(t, counters, types.UsageScopeSession, c.ID))
	require.Equal(t, 1, sessionCounterRows(t, counters, types.UsageScopeTenant, "1"),
		"workspace counters are history and stay")
}

func TestSessionClearContextSeed(t *testing.T) {
	repo, db := newSessionRepositoryForTest(t)
	s := createSessionForTest(t, db, 1, "alice")
	require.NoError(t, db.Model(&types.Session{}).Where("id = ?", s.ID).Update("context_seed", "## Goal\nold").Error)

	require.NoError(t, repo.ClearContextSeed(context.Background(), 2, s.ID))
	var got types.Session
	require.NoError(t, db.First(&got, "id = ?", s.ID).Error)
	require.Equal(t, "## Goal\nold", got.ContextSeed, "another tenant cannot clear it")

	require.NoError(t, repo.ClearContextSeed(context.Background(), 1, s.ID))
	require.NoError(t, db.First(&got, "id = ?", s.ID).Error)
	require.Empty(t, got.ContextSeed)
}
