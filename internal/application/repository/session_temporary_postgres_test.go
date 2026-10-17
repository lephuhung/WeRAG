package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestTemporarySessionsPostgres runs migration 000153 and the temporary
// session paths against a real PostgreSQL in a disposable random schema.
// Skipped unless WEKNORA_MIGRATION_TEST_POSTGRES_DSN points at a disposable
// database; only the new migration runs, over a minimal baseline of the
// tables its purge function reads, and the schema is dropped at the end.
func TestTemporarySessionsPostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_MIGRATION_TEST_POSTGRES_DSN to run the disposable PostgreSQL temporary-session test")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	schema := fmt.Sprintf("temp_sessions_%d", time.Now().UnixNano())
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	require.NoError(t, db.Exec("SET search_path TO "+schema).Error)
	t.Cleanup(func() {
		_ = db.Exec("SET search_path TO public").Error
		_ = db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
	})

	require.NoError(t, db.Exec(`CREATE TABLE sessions (
		id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
		chat_root_id VARCHAR(36) NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE messages (
		id VARCHAR(36) PRIMARY KEY, session_id VARCHAR(36) NOT NULL, deleted_at TIMESTAMPTZ
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE document_workspaces (
		id VARCHAR(36) PRIMARY KEY, session_id VARCHAR(36) NOT NULL,
		updated_at TIMESTAMP, deleted_at TIMESTAMP
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE temporary_documents (
		id VARCHAR(36) PRIMARY KEY, session_id VARCHAR(36) NOT NULL, created_at TIMESTAMP
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE usage_counters (
		scope_type VARCHAR(32) NOT NULL, scope_id VARCHAR(64) NOT NULL
	)`).Error)

	// One Exec, like golang-migrate: the file holds $$-quoted function bodies.
	up, err := os.ReadFile(filepath.Join(root, "migrations", "versioned", "000153_temporary_sessions.up.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)

	ctx := context.Background()
	isTemporary := func(id string) bool {
		var v bool
		require.NoError(t, db.Raw("SELECT is_temporary FROM sessions WHERE id = ?", id).Scan(&v).Error)
		return v
	}
	isDeleted := func(id string) bool {
		var v bool
		require.NoError(t, db.Raw("SELECT deleted_at IS NOT NULL FROM sessions WHERE id = ?", id).Scan(&v).Error)
		return v
	}

	t.Run("a message keeps the session and its document root", func(t *testing.T) {
		require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id, is_temporary) VALUES ('root', 1, TRUE)`).Error)
		require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id, chat_root_id, is_temporary)
			VALUES ('child', 1, 'root', TRUE), ('other', 1, '', TRUE)`).Error)
		require.NoError(t, keepTemporarySession(db, "child"))
		require.False(t, isTemporary("child"))
		require.False(t, isTemporary("root"))
		require.True(t, isTemporary("other"), "an unrelated session stays temporary")
		require.NoError(t, keepTemporarySession(db, ""))
	})

	t.Run("a stale Save does not set the flag back", func(t *testing.T) {
		require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id, is_temporary) VALUES ('saved', 1, TRUE)`).Error)
		var stale types.Session
		require.NoError(t, db.Table("sessions").Select("id, tenant_id, is_temporary").
			Where("id = ?", "saved").Take(&stale).Error)
		require.True(t, stale.IsTemporary)
		require.NoError(t, keepTemporarySession(db, "saved"))
		// the copy loaded before the first message is written back
		require.NoError(t, db.Model(&stale).Select("is_temporary", "updated_at").Updates(&stale).Error)
		require.False(t, isTemporary("saved"))
	})

	t.Run("purge removes only idle temporary sessions", func(t *testing.T) {
		require.NoError(t, db.Exec(`UPDATE sessions SET is_temporary = FALSE`).Error)
		// back-dated fixtures would be swept by the insert trigger itself
		require.NoError(t, db.Exec(`ALTER TABLE sessions DISABLE TRIGGER sessions_purge_expired_temporary`).Error)
		defer func() {
			require.NoError(t, db.Exec(`ALTER TABLE sessions ENABLE TRIGGER sessions_purge_expired_temporary`).Error)
		}()
		old, fresh := "NOW() - INTERVAL '2 hours'", "NOW()"
		for id, at := range map[string]string{
			"plain-old": old, "plain-fresh": fresh, "doc-recent": old, "doc-old": "NOW() - INTERVAL '30 hours'",
			"chatted-old": old, "upload-recent": old,
		} {
			require.NoError(t, db.Exec(fmt.Sprintf(`INSERT INTO sessions (id, tenant_id, is_temporary, created_at, updated_at)
				VALUES ('%s', 1, TRUE, %s, %s)`, id, at, at)).Error)
		}
		require.NoError(t, db.Exec(`INSERT INTO document_workspaces (id, session_id, updated_at) VALUES
			('w1', 'doc-recent', (NOW() - INTERVAL '3 hours') AT TIME ZONE 'UTC'),
			('w2', 'doc-old', (NOW() - INTERVAL '30 hours') AT TIME ZONE 'UTC')`).Error)
		require.NoError(t, db.Exec(`INSERT INTO temporary_documents (id, session_id, created_at) VALUES
			('d1', 'upload-recent', (NOW() - INTERVAL '10 minutes') AT TIME ZONE 'UTC')`).Error)
		require.NoError(t, db.Exec(`INSERT INTO messages (id, session_id) VALUES ('m1', 'chatted-old')`).Error)
		require.NoError(t, db.Exec(`INSERT INTO usage_counters (scope_type, scope_id) VALUES
			('session', 'plain-old'), ('user', 'plain-old'), ('session', 'plain-fresh')`).Error)

		removed, err := PurgeExpiredTemporarySessions(ctx, db, time.Hour, 24*time.Hour)
		require.NoError(t, err)
		require.Equal(t, 2, removed)
		require.True(t, isDeleted("plain-old"))
		require.True(t, isDeleted("doc-old"))
		require.False(t, isDeleted("plain-fresh"))
		require.False(t, isDeleted("doc-recent"), "an open document gets the long TTL")
		require.False(t, isDeleted("upload-recent"), "a recent upload counts as activity")
		require.False(t, isDeleted("chatted-old"), "a session with messages is never purged")

		var counters []string
		require.NoError(t, db.Raw(`SELECT scope_type || ':' || scope_id FROM usage_counters ORDER BY 1`).
			Scan(&counters).Error)
		require.Equal(t, []string{"session:plain-fresh", "user:plain-old"}, counters)
	})

	t.Run("a new session triggers the purge", func(t *testing.T) {
		require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id, is_temporary, created_at, updated_at)
			VALUES ('idle', 1, TRUE, NOW() - INTERVAL '3 hours', NOW() - INTERVAL '3 hours')`).Error)
		require.True(t, isDeleted("idle"), "the insert itself sweeps an idle temporary session")
		require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id, is_temporary) VALUES ('new', 1, TRUE)`).Error)
		require.False(t, isDeleted("new"))
	})
}
