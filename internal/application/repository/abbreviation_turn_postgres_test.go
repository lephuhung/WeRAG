package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// requirePGErrorCode asserts a PostgreSQL error identity (unique 23505,
// check 23514, FK 23503) so constraint coverage cannot pass on unrelated
// failures.
func requirePGErrorCode(t *testing.T, err error, code string, msg string) {
	t.Helper()
	require.Error(t, err, msg)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, msg)
	require.Equal(t, code, pgErr.Code, msg)
}

func TestAbbreviationTurnPostgresLockSQL(t *testing.T) {
	sqliteDB, _ := newAbbreviationTurnRepo(t)
	conn, err := sqliteDB.DB()
	require.NoError(t, err)
	pg, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true})
	require.NoError(t, err)
	statement := pg.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []map[string]any
		return turnMessagesForUpdate(tx, []string{"a1", "u1"}).Find(&rows)
	})
	require.Contains(t, statement, `ORDER BY id FOR UPDATE`)
	require.Contains(t, statement, `"messages"`)
}

// TestAbbreviationTurnPostgres runs the versioned 000114 migration and the
// turn repository against a real PostgreSQL in a disposable random schema.
// Skipped unless WEKNORA_MIGRATION_TEST_POSTGRES_DSN points at a disposable
// database; a skip is reported as a skip, never as PG evidence. It never
// reads .env or production credentials and never touches shared schemas:
// only the new migration runs, over a truthful minimal sessions/messages
// baseline (owner and role columns included), and the schema is dropped at
// the end. One pooled connection keeps SET search_path valid on every
// statement; the concurrent block therefore proves the version predicate
// elects one winner under serialization, not a multi-connection race.
func TestAbbreviationTurnPostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_MIGRATION_TEST_POSTGRES_DSN to run the disposable PostgreSQL turn test")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	schema := fmt.Sprintf("abbr_turn_%d", time.Now().UnixNano())
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	require.NoError(t, db.Exec("SET search_path TO "+schema).Error)
	t.Cleanup(func() {
		_ = db.Exec("SET search_path TO public").Error
		_ = db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
	})

	// Truthful minimal baseline the ownership checks and FKs need.
	require.NoError(t, db.Exec(`CREATE TABLE sessions (
		id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
		user_id VARCHAR(512), deleted_at TIMESTAMPTZ
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE messages (
		id VARCHAR(36) PRIMARY KEY, session_id VARCHAR(36) NOT NULL,
		role VARCHAR(50) NOT NULL, deleted_at TIMESTAMPTZ
	)`).Error)

	// Only the new migration runs here, never the full chain.
	for _, stmt := range splitMigrationStatements(t, filepath.Join(
		root, "migrations", "versioned", "000114_abbreviation_turn_states.up.sql")) {
		require.NoError(t, db.Exec(stmt).Error, "migration statement: %s", stmt)
	}
	for _, table := range []string{
		"abbreviation_turn_states", "abbreviation_turn_messages", "abbreviation_suggestion_locks",
	} {
		var count int64
		require.NoError(t, db.Raw(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
			schema, table).Scan(&count).Error)
		require.Equal(t, int64(1), count, "table %s must exist", table)
	}
	var payloadType string
	require.NoError(t, db.Raw(
		`SELECT data_type FROM information_schema.columns
		 WHERE table_schema = ? AND table_name = 'abbreviation_turn_states' AND column_name = 'payload'`,
		schema).Scan(&payloadType).Error)
	require.Equal(t, "jsonb", payloadType, "payload must be typed JSONB on PostgreSQL")

	// Seeds precede every repository call: ownership checks read them.
	ctx := context.Background()
	owner := types.AbbreviationOwner{TenantID: 7, SessionID: "s", OwnerID: "alice", PrincipalID: "web_user:alice"}
	require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id, user_id) VALUES
		('s', 7, 'alice'), ('s-other', 7, 'bob')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO messages (id, session_id, role) VALUES
		('u1', 's', 'user'), ('u2', 's', 'user'), ('u3', 's', 'user'),
		('u4', 's', 'user'), ('u5', 's', 'user'),
		('def-1', 's', 'user'), ('a1', 's', 'assistant'), ('u-other', 's-other', 'user')`).Error)
	repo := NewAbbreviationTurnRepository(db)

	row := &types.AbbreviationTurnState{
		ID: "r", TenantID: 7, SessionID: "s", OwnerID: "alice",
		PrincipalID: "web_user:alice", RootUserMessageID: "u1",
		State: "inspecting", Version: 1, ExpiresAt: time.Now().Add(24 * time.Hour),
		Payload: types.AbbreviationTurnPayload{
			Resolution: types.AbbreviationResolution{
				OriginalQuery: "ATTT là gì", Status: "needs_definition",
				UnknownTerms: []string{"ATTT"},
			},
		},
	}
	require.NoError(t, repo.Begin(ctx, owner, row))
	row.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, owner, 1, row)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.LinkMessages(ctx, owner, "r", []types.AbbreviationMessageLink{
		{MessageID: "u1", RequestID: "r", Role: "root"},
		{MessageID: "def-1", RequestID: "r", Role: "definition"},
	}))
	resolved, err := repo.ByMessages(ctx, owner, []string{"u1", "def-1"})
	require.NoError(t, err)
	require.Len(t, resolved, 2)

	// Ownership: a foreign owner cannot begin on this session or read the turn.
	foreign := types.AbbreviationOwner{TenantID: 7, SessionID: "s", OwnerID: "mallory", PrincipalID: "web_user:mallory"}
	foreignRow := &types.AbbreviationTurnState{
		ID: "rx", TenantID: 7, SessionID: "s", OwnerID: "mallory",
		PrincipalID: "web_user:mallory", RootUserMessageID: "u1",
		State: "inspecting", Version: 1, ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.Error(t, repo.Begin(ctx, foreign, foreignRow))
	_, err = repo.Get(ctx, foreign, "r")
	require.ErrorIs(t, err, types.ErrAbbreviationNotFound)

	// Stale CAS loses; partial replies persist on awaiting_definition.
	row.State = "awaiting_definition"
	stale, err := repo.CompareAndSwap(ctx, owner, 1, row)
	require.NoError(t, err)
	require.False(t, stale, "version already advanced past 1")
	row.State = "awaiting_definition"
	ok, err = repo.CompareAndSwap(ctx, owner, 2, row)
	require.NoError(t, err)
	require.True(t, ok)

	// Concurrent swaps elect exactly one winner through the version predicate.
	var wg sync.WaitGroup
	wins := make([]bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			attempt := *row
			attempt.State = "ready"
			ok, err := repo.CompareAndSwap(ctx, owner, 3, &attempt)
			if err == nil {
				wins[n] = ok
			}
		}(i)
	}
	wg.Wait()
	require.True(t, wins[0] != wins[1], "exactly one concurrent CAS must win")

	// A live awaiting turn justifies the one-waiting violation below.
	waiting := &types.AbbreviationTurnState{
		ID: "r3", TenantID: 7, SessionID: "s", OwnerID: "alice",
		PrincipalID: "web_user:alice", RootUserMessageID: "u2",
		State: "inspecting", Version: 1, ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, repo.Begin(ctx, owner, waiting))
	waiting.State = "awaiting_definition"
	ok, err = repo.CompareAndSwap(ctx, owner, 1, waiting)
	require.NoError(t, err)
	require.True(t, ok)

	// Constraint invariants with distinct otherwise-valid rows and asserted
	// PostgreSQL error identities. The valid Begin/link calls above are the
	// matching success controls.
	dupeRoot := &types.AbbreviationTurnState{
		ID: "r-dupe", TenantID: 7, SessionID: "s", OwnerID: "alice",
		PrincipalID: "web_user:alice", RootUserMessageID: "u1",
		State: "inspecting", Version: 1, ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	requirePGErrorCode(t, func() error { return repo.Begin(ctx, owner, dupeRoot) }(),
		"23505", "unique root must block a second turn for one question")
	requirePGErrorCode(t, db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('w2', 7, 's', 'u3', 'awaiting_definition', 1, NOW() + INTERVAL '1 hour')`).Error,
		"23505", "partial unique must block a second awaiting turn")
	requirePGErrorCode(t, db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('bad', 7, 's', 'u4', 'napping', 1, NOW() + INTERVAL '1 hour')`).Error,
		"23514", "state CHECK must reject unknown states")
	requirePGErrorCode(t, db.Exec(`INSERT INTO abbreviation_turn_messages (message_id, request_id, role)
		VALUES ('ghost', 'r', 'definition')`).Error,
		"23503", "link FK must reject missing messages")
	// The bogus role uses an otherwise-valid unlinked message, so only the
	// CHECK can fail; the valid links above prove the success control.
	requirePGErrorCode(t, db.Exec(`INSERT INTO abbreviation_turn_messages (message_id, request_id, role)
		VALUES ('u5', 'r', 'bogus')`).Error,
		"23514", "link role CHECK must reject unknown roles")

	// Down migration removes only the new tables.
	for _, stmt := range splitMigrationStatements(t, filepath.Join(
		root, "migrations", "versioned", "000114_abbreviation_turn_states.down.sql")) {
		require.NoError(t, db.Exec(stmt).Error, "down statement: %s", stmt)
	}
	for _, table := range []string{
		"abbreviation_turn_states", "abbreviation_turn_messages", "abbreviation_suggestion_locks",
	} {
		var count int64
		require.NoError(t, db.Raw(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
			schema, table).Scan(&count).Error)
		require.Zero(t, count, "down migration must drop %s", table)
	}
}
