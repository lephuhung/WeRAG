package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

type cohortSettings struct {
	days, max int64
	confirmed string
}

func (s *cohortSettings) GetInt(_ context.Context, key, _ string, def int64) int64 {
	switch key {
	case types.LegalExpiredRetentionDaysSettingKey:
		return s.days
	case types.LegalExpiredRetentionCohortMaxSettingKey:
		return s.max
	}
	return def
}

func (s *cohortSettings) GetString(_ context.Context, key, _ string, def string) string {
	if key == types.LegalExpiredRetentionConfirmedDaysSettingKey {
		return s.confirmed
	}
	return def
}

// More copies turning fully expired on one Vietnam day than cohort_max: that
// day's texts stay and the pass reports it until a SuperAdmin confirms the
// day; the other days' texts go as usual.
func TestLegalExpiredRetentionHoldsAWaveOfExpiry(t *testing.T) {
	f := newRetentionFixture(t, 15)
	ctx := context.Background()
	set := &cohortSettings{days: 15, max: 2}
	f.w.settings = set
	// three copies whose clocks started 2026-11-01 20:00 UTC (02/11 in Vietnam)
	for i := 1; i <= 3; i++ {
		require.NoError(t, f.db.Exec(fmt.Sprintf(`INSERT INTO legal_documents VALUES ('w%d', 'vbpl', 'Hết hiệu lực toàn bộ',
			'%d/2019/ND-CP', 'kw%d', 'Nghị định', 'Chính phủ', '2019-01-05')`, i, i, i)).Error)
		require.NoError(t, f.db.Exec(fmt.Sprintf(`INSERT INTO knowledges VALUES ('k-w%d', 'pub',
			'{"external_id":"w%d","eff_expired_at":"2026-11-01T20:00:00Z"}', 'completed', NULL)`, i, i)).Error)
	}
	f.w.Tick(ctx) // starts the clock of k-exp at 2026-11-09
	f.now = f.now.AddDate(0, 0, 16)

	require.Equal(t, 1, f.w.Tick(ctx))
	require.Equal(t, []string{"k-exp"}, f.removed, "another day's text goes")
	beat, ok := legalWorkerBeat(types.LegalSyncWorkerExpiredRetention)
	require.True(t, ok)
	require.Contains(t, beat.LastError, "3 văn bản cùng chuyển hết hiệu lực ngày 02/11/2026: chờ SuperAdmin xác nhận")
	require.NotNil(t, beat.FailingSince, "the pass counts as failing: the tick-error alert fires after an hour")

	set.confirmed = "2026-10-30, 2026-11-02"
	require.Equal(t, 3, f.w.Tick(ctx))
	require.ElementsMatch(t, []string{"k-exp", "k-w1", "k-w2", "k-w3"}, f.removed)
	beat, _ = legalWorkerBeat(types.LegalSyncWorkerExpiredRetention)
	require.Empty(t, beat.LastError)
	require.Nil(t, beat.FailingSince)
}

func TestLegalExpiredRetentionCohortWithinTheLimit(t *testing.T) {
	f := newRetentionFixture(t, 15)
	ctx := context.Background()
	f.w.settings = &cohortSettings{days: 15, max: 0} // < 1: the default (100)
	f.w.Tick(ctx)
	f.now = f.now.AddDate(0, 0, 16)
	require.Equal(t, 1, f.w.Tick(ctx))
	beat, _ := legalWorkerBeat(types.LegalSyncWorkerExpiredRetention)
	require.Empty(t, beat.LastError)
}
