package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAbbreviationSuggestUniqueReusesActiveAndPending(t *testing.T) {
	db, repo := newAbbrevRepo(t)
	require.NoError(t, db.Exec(`CREATE TABLE abbreviation_suggestion_locks ("key" VARCHAR(64) PRIMARY KEY)`).Error)
	ctx := context.Background()
	active := &types.Abbreviation{ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true}
	pending := &types.Abbreviation{ShortForm: "ATTT", FullForm: "An toàn thực phẩm"}
	require.NoError(t, repo.Create(ctx, active))
	require.NoError(t, repo.Create(ctx, pending))
	for _, tc := range []struct {
		full     string
		original *types.Abbreviation
	}{
		{" an TOÀN THÔNG TIN ", active},
		{"An toàn thực phẩm", pending},
	} {
		got, err := repo.SuggestUnique(ctx, &types.Abbreviation{ShortForm: " attt ", FullForm: tc.full})
		require.NoError(t, err)
		require.Equal(t, tc.original.ID, got.ID)
		require.Equal(t, tc.original.IsActive, got.IsActive)
	}
	created, err := repo.SuggestUnique(ctx, &types.Abbreviation{ShortForm: "ATTT", FullForm: "An toàn công nghiệp"})
	require.NoError(t, err)
	require.False(t, created.IsActive)
	require.NotEqual(t, active.ID, created.ID)
	require.NotEqual(t, pending.ID, created.ID)
}

func TestAbbreviationSuggestUniqueConcurrent(t *testing.T) {
	db, repo := newAbbrevRepo(t)
	require.NoError(t, db.Exec(`CREATE TABLE abbreviation_suggestion_locks ("key" VARCHAR(64) PRIMARY KEY)`).Error)
	var wg sync.WaitGroup
	rows := make([]*types.Abbreviation, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rows[n], errs[n] = repo.SuggestUnique(context.Background(), &types.Abbreviation{
				ShortForm: "ATTT", FullForm: "An toàn thông tin", SuggestedBy: "alice",
			})
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, rows[0].ID, rows[1].ID)
	var total int64
	require.NoError(t, db.Model(&types.Abbreviation{}).Count(&total).Error)
	require.Equal(t, int64(1), total)
}
