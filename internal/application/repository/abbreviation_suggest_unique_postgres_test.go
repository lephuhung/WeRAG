package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAbbreviationSuggestUniquePostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL DSN")
	}
	setup, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	setupConn, err := setup.DB()
	require.NoError(t, err)
	setupConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = setupConn.Close() })
	schema := fmt.Sprintf("abbr_suggest_%d", time.Now().UnixNano())
	require.NoError(t, setup.Exec("CREATE SCHEMA "+schema).Error)
	require.NoError(t, setup.Exec("SET search_path TO "+schema).Error)
	t.Cleanup(func() {
		_ = setup.Exec("SET search_path TO public").Error
		_ = setup.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
	})
	require.NoError(t, setup.AutoMigrate(&types.Abbreviation{}))
	require.NoError(t, setup.Exec(`CREATE TABLE abbreviation_suggestion_locks ("key" VARCHAR(64) PRIMARY KEY)`).Error)

	repos := make([]*abbreviationRepository, 2)
	for i := range repos {
		config, err := pgx.ParseConfig(dsn)
		require.NoError(t, err)
		config.RuntimeParams["search_path"] = schema
		conn := stdlib.OpenDB(*config)
		t.Cleanup(func() { _ = conn.Close() })
		pool, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
		require.NoError(t, err)
		repos[i] = &abbreviationRepository{db: pool}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	rows := make([]*types.Abbreviation, 2)
	errs := make([]error, 2)
	for i := range repos {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			rows[n], errs[n] = repos[n].SuggestUnique(context.Background(), &types.Abbreviation{ShortForm: "ATTT", FullForm: "An toàn thông tin"})
		}(i)
	}
	close(start)
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, rows[0].ID, rows[1].ID)
	var count int64
	require.NoError(t, setup.Model(&types.Abbreviation{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
