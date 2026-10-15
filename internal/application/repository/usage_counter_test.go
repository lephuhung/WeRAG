package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newUsageCounterTestRepo(t *testing.T) (*usageCounterRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.UsageCounter{}))
	require.NoError(t, db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, usage_metered_at DATETIME NULL)`).Error)
	return &usageCounterRepository{db: db}, db
}

func TestUsageCounterAddAccumulatesAndMergesDuplicates(t *testing.T) {
	repo, _ := newUsageCounterTestRepo(t)
	ctx := context.Background()

	inc := func(scope, id, metric, period string, d int64) types.UsageIncrement {
		return types.UsageIncrement{ScopeType: scope, ScopeID: id, Metric: metric, PeriodKey: period, Delta: d}
	}
	// Two increments of the same key in one batch must not trip the
	// "cannot affect row a second time" rule: they are merged first.
	require.NoError(t, repo.Add(ctx, []types.UsageIncrement{
		inc("tenant", "1", "tokens", "2026-10", 100),
		inc("tenant", "1", "tokens", "2026-10", 50),
		inc("user", "u1", "questions", "2026-10-13", 1),
		inc("user", "", "questions", "2026-10-13", 1), // dropped: empty scope id
	}))
	require.NoError(t, repo.Add(ctx, []types.UsageIncrement{
		inc("tenant", "1", "tokens", "2026-10", 25),
	}))

	rows, err := repo.Get(ctx, "tenant", "1", []string{"2026-10"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(175), rows[0].Value)

	rows, err = repo.Get(ctx, "user", "u1", []string{"2026-10-13", "2026-10"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(1), rows[0].Value)

	sums, err := repo.SumByPeriod(ctx, "tenant", "2026-10")
	require.NoError(t, err)
	require.Len(t, sums, 1)
	require.Equal(t, "tokens", sums[0].Metric)
	require.Equal(t, int64(175), sums[0].Value)
}

func TestUsageCounterAddMessageUsageOnce(t *testing.T) {
	repo, db := newUsageCounterTestRepo(t)
	ctx := context.Background()
	require.NoError(t, db.Exec(`INSERT INTO messages (id) VALUES ('m1')`).Error)
	incs := []types.UsageIncrement{{ScopeType: "tenant", ScopeID: "1", Metric: "tokens", PeriodKey: "2026-10", Delta: 50}}

	claimed, err := repo.AddMessageUsage(ctx, "m1", incs)
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = repo.AddMessageUsage(ctx, "m1", incs)
	require.NoError(t, err)
	require.False(t, claimed, "second save of the same message must not count again")

	claimed, err = repo.AddMessageUsage(ctx, "missing", incs)
	require.NoError(t, err)
	require.False(t, claimed)

	rows, err := repo.Get(ctx, "tenant", "1", []string{"2026-10"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(50), rows[0].Value)
}

func TestUsageCounterFailedAddLeavesMessageUncounted(t *testing.T) {
	repo, db := newUsageCounterTestRepo(t)
	ctx := context.Background()
	require.NoError(t, db.Exec(`INSERT INTO messages (id) VALUES ('m1')`).Error)
	incs := []types.UsageIncrement{{ScopeType: "tenant", ScopeID: "1", Metric: "tokens", PeriodKey: "2026-10", Delta: 50}}

	// the counters cannot be written: the claim rolls back with them
	require.NoError(t, db.Migrator().RenameTable("usage_counters", "usage_counters_away"))
	_, err := repo.AddMessageUsage(ctx, "m1", incs)
	require.Error(t, err)
	require.NoError(t, db.Migrator().RenameTable("usage_counters_away", "usage_counters"))

	claimed, err := repo.AddMessageUsage(ctx, "m1", incs)
	require.NoError(t, err)
	require.True(t, claimed, "the retry counts the message the failed save could not")
	rows, err := repo.Get(ctx, "tenant", "1", []string{"2026-10"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(50), rows[0].Value)
}
