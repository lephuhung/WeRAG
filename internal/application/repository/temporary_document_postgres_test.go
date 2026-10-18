package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The parse-queue timestamps (started_at, ready_at) hold the instant
// whatever the server's zone (timestamptz since migration 157). Written as
// +08 wall clock into the former timestamp columns they looked 8 h in the
// future, so a hung parse held the owner's parse slot long after
// parseSlotStaleAfter. Skipped unless WEKNORA_MIGRATION_TEST_POSTGRES_DSN
// points at a database where a disposable schema may be created.
func TestTemporaryDocumentParseTimesUTCPostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_MIGRATION_TEST_POSTGRES_DSN to run the disposable PostgreSQL temporary-document test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	schema := fmt.Sprintf("temp_docs_%d", time.Now().UnixNano())
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	require.NoError(t, db.Exec("SET search_path TO "+schema).Error)
	require.NoError(t, db.Exec("SET TIME ZONE 'UTC'").Error)
	t.Cleanup(func() {
		_ = db.Exec("SET search_path TO public").Error
		_ = db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
	})
	require.NoError(t, db.Exec(`CREATE TABLE sessions (
		id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, user_id VARCHAR(36) NOT NULL
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE temporary_documents (
		id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, session_id VARCHAR(36) NOT NULL,
		status VARCHAR(16) NOT NULL, content TEXT, chunks JSONB NOT NULL DEFAULT '[]',
		image_refs JSONB NOT NULL DEFAULT '[]', metadata JSONB NOT NULL DEFAULT '{}',
		token_count INT NOT NULL DEFAULT 0, chunk_count INT NOT NULL DEFAULT 0, error_message TEXT,
		started_at TIMESTAMPTZ, ready_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMPTZ
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO sessions VALUES ('s1', 7, 'u1'), ('s2', 7, 'u1')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO temporary_documents (id, tenant_id, session_id, status) VALUES
		('d1', 7, 's1', 'uploaded'), ('d2', 7, 's2', 'uploaded')`).Error)

	repo := NewTemporaryDocumentRepository(db)
	ctx := context.Background()
	cst := time.FixedZone("CST", 8*3600)
	now := time.Now().In(cst)

	// a parse started 40 min ago (stale) and one just now
	require.NoError(t, repo.MarkProcessing(ctx, 7, "d1", now.Add(-40*time.Minute)))
	require.NoError(t, repo.MarkProcessing(ctx, 7, "d2", now))
	var wall string
	require.NoError(t, db.Raw(`SELECT to_char(started_at, 'YYYY-MM-DD HH24:MI') FROM temporary_documents WHERE id = 'd2'`).Scan(&wall).Error)
	require.Equal(t, now.UTC().Format("2006-01-02 15:04"), wall, "started_at holds the instant")

	n, err := repo.CountProcessingForSessionOwner(ctx, 7, "s1", now.Add(-30*time.Minute))
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "the stale parse no longer holds a slot; the fresh one (other session, same owner) does")

	require.NoError(t, repo.MarkReady(ctx, 7, "d2", "text", types.JSON("[]"), types.JSON("[]"), types.JSON("{}"), 1, 1, now))
	require.NoError(t, db.Raw(`SELECT to_char(ready_at, 'YYYY-MM-DD HH24:MI') FROM temporary_documents WHERE id = 'd2'`).Scan(&wall).Error)
	require.Equal(t, now.UTC().Format("2006-01-02 15:04"), wall, "ready_at holds the instant")
}
