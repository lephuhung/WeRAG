package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

// Two pools upsert the same open digest while it is read now and then:
// there is never more than one unread digest, and no event is lost (the
// counts of all the digests add up to the events).
func TestUserNotificationOpenDigestPostgres(t *testing.T) {
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
	schema := fmt.Sprintf("user_notif_digest_%d", time.Now().UnixNano())
	require.NoError(t, setup.Exec("CREATE SCHEMA "+schema).Error)
	require.NoError(t, setup.Exec("SET search_path TO "+schema).Error)
	t.Cleanup(func() {
		_ = setup.Exec("SET search_path TO public").Error
		_ = setup.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
	})
	require.NoError(t, setup.AutoMigrate(&types.UserNotification{}))

	repos := make([]*userNotificationRepository, 2)
	for i := range repos {
		config, err := pgx.ParseConfig(dsn)
		require.NoError(t, err)
		config.RuntimeParams["search_path"] = schema
		conn := stdlib.OpenDB(*config)
		t.Cleanup(func() { _ = conn.Close() })
		pool, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
		require.NoError(t, err)
		repos[i] = &userNotificationRepository{db: pool}
	}
	ctx := context.Background()

	// phase 1: concurrent upserts only → one digest counting them all
	const workers = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = repos[i%2].UpsertOpenDigest(ctx, digestFor("a", time.Now()), addOne)
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	rows, err := repos[0].List(ctx, "a", false, nil, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, workers, digestCount(t, rows[0]))

	// phase 2: upserts racing reads
	_, err = repos[0].MarkAllRead(ctx, "a", time.Now())
	require.NoError(t, err)
	const events = 40
	start = make(chan struct{})
	errs = make([]error, events)
	for i := 0; i < events; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = repos[i%2].UpsertOpenDigest(ctx, digestFor("b", time.Now()), addOne)
		}(i)
	}
	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if list, err := repos[1].List(ctx, "b", true, nil, 10); err == nil {
				for _, n := range list {
					_, _ = repos[1].MarkRead(ctx, "b", n.ID, time.Now())
				}
			}
			var unread int64
			_ = setup.Model(&types.UserNotification{}).Where("user_id = ? AND read_at IS NULL", "b").Count(&unread).Error
			if unread > 1 {
				t.Errorf("%d unread digests at once", unread)
			}
		}
	}()
	close(start)
	wg.Wait()
	close(stop)
	readers.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	rows, err = repos[0].List(ctx, "b", false, nil, 100)
	require.NoError(t, err)
	total, unread := 0, 0
	for _, n := range rows {
		total += digestCount(t, n)
		if n.ReadAt == nil {
			unread++
		}
	}
	require.LessOrEqual(t, unread, 1)
	require.Equal(t, events, total, "no event lost")
}
