package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// splitAbbreviationMigrationStatements splits the new migration files into
// executable statements. Neither file contains triggers or embedded
// semicolons, so a semicolon split with comment/empty filtering is exact.
func splitAbbreviationMigrationStatements(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []string
	for _, part := range strings.Split(string(raw), ";") {
		var kept []string
		for _, line := range strings.Split(part, "\n") {
			if idx := strings.Index(line, "--"); idx >= 0 {
				line = line[:idx]
			}
			if strings.TrimSpace(line) != "" {
				kept = append(kept, line)
			}
		}
		if stmt := strings.TrimSpace(strings.Join(kept, "\n")); stmt != "" {
			out = append(out, stmt)
		}
	}
	require.NotEmpty(t, out, "migration must contain statements: %s", path)
	return out
}

func openAbbreviationMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count))
	return count == 1
}

// TestMigrationAbbreviationTurnStatesSQLite applies the real 000031 SQLite
// migration up, down, and up again, proving the schema (tables, CHECKs,
// unique root, partial one-waiting index) without touching legacy tables.
func TestMigrationAbbreviationTurnStatesSQLite(t *testing.T) {
	root := sqliteRepoRoot(t)
	up := filepath.Join(root, "migrations", "sqlite", "000031_abbreviation_turn_states.up.sql")
	down := filepath.Join(root, "migrations", "sqlite", "000031_abbreviation_turn_states.down.sql")
	db := openAbbreviationMigrationDB(t)

	apply := func(path string) {
		t.Helper()
		for _, stmt := range splitAbbreviationMigrationStatements(t, path) {
			_, err := db.Exec(stmt)
			require.NoError(t, err, "migration statement: %s", stmt)
		}
	}
	apply(up)

	for _, table := range []string{
		"abbreviation_turn_states", "abbreviation_turn_messages", "abbreviation_suggestion_locks",
	} {
		require.True(t, tableExists(t, db, table), "table %s must exist", table)
	}

	// The partial one-waiting index exists with its WHERE clause.
	var indexSQL sql.NullString
	require.NoError(t, db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_abbreviation_one_waiting'").Scan(&indexSQL))
	require.True(t, indexSQL.Valid)
	require.Contains(t, strings.ToUpper(indexSQL.String), "WHERE")
	require.Contains(t, indexSQL.String, "awaiting_definition")

	// Minimal truthful parents for the link foreign keys.
	_, err := db.Exec(`CREATE TABLE sessions (
		id VARCHAR(36) PRIMARY KEY, tenant_id INTEGER NOT NULL,
		user_id VARCHAR(512), deleted_at DATETIME)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE messages (
		id VARCHAR(36) PRIMARY KEY, session_id VARCHAR(36) NOT NULL,
		role VARCHAR(50) NOT NULL, deleted_at DATETIME)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO sessions (id, tenant_id, user_id) VALUES ('s', 7, 'alice')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (id, session_id, role) VALUES
		('u1', 's', 'user'), ('def-1', 's', 'user')`)
	require.NoError(t, err)

	// The state CHECK rejects unknown states when every other column is
	// valid (no NOT NULL red herring).
	_, err = db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('bad', 7, 's', 'u1', 'napping', 1, '2026-09-27 01:00:00')`)
	require.Error(t, err, "state CHECK must reject unknown states")
	require.Contains(t, err.Error(), "chk_abbreviation_turn_states_state")

	// The version CHECK rejects zero versions on an otherwise valid row.
	_, err = db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('bad', 7, 's', 'u1', 'inspecting', 0, '2026-09-27 01:00:00')`)
	require.Error(t, err, "version CHECK must reject zero versions")
	require.Contains(t, err.Error(), "chk_abbreviation_turn_states_version")

	// A fully valid row succeeds, proving the negatives above test the
	// intended constraints.
	_, err = db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('r1', 7, 's', 'u1', 'inspecting', 1, '2026-09-27 01:00:00')`)
	require.NoError(t, err)

	// The unique root blocks a second turn for the same question.
	_, err = db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('r2', 7, 's', 'u1', 'inspecting', 1, '2026-09-27 01:00:00')`)
	require.Error(t, err, "unique root must block a second turn for one question")

	// The partial unique blocks a second awaiting turn in one session.
	_, err = db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('w1', 8, 't', 'u1', 'awaiting_definition', 1, '2026-09-27 01:00:00')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('w2', 8, 't', 'u2', 'awaiting_definition', 1, '2026-09-27 01:00:00')`)
	require.Error(t, err, "partial unique must block a second awaiting turn")

	// A fully valid link succeeds, proving the negatives test intent.
	_, err = db.Exec(`INSERT INTO abbreviation_turn_messages (message_id, request_id, role)
		VALUES ('def-1', 'r1', 'definition')`)
	require.NoError(t, err)

	// The message-link role CHECK rejects unknown roles with valid FK
	// parents, so the failure cannot come from an unrelated error.
	_, err = db.Exec(`INSERT INTO abbreviation_turn_messages (message_id, request_id, role)
		VALUES ('u1', 'r1', 'bogus')`)
	require.Error(t, err, "link role CHECK must reject unknown roles")
	require.Contains(t, err.Error(), "chk_abbreviation_turn_messages_role")

	// Foreign keys: missing parents fail, cascades clean up links.
	_, err = db.Exec(`INSERT INTO abbreviation_turn_messages (message_id, request_id, role)
		VALUES ('ghost', 'r1', 'definition')`)
	require.Error(t, err, "link to a missing message must fail")
	_, err = db.Exec(`INSERT INTO abbreviation_turn_messages (message_id, request_id, role)
		VALUES ('u1', 'ghost-turn', 'definition')`)
	require.Error(t, err, "link to a missing turn must fail")
	_, err = db.Exec(`DELETE FROM abbreviation_turn_states WHERE id = 'r1'`)
	require.NoError(t, err)
	var linksLeft int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM abbreviation_turn_messages`).Scan(&linksLeft))
	require.Zero(t, linksLeft, "deleting the turn must cascade its links")

	// Legacy rows survive the down/up cycle untouched.
	_, err = db.Exec(`INSERT INTO abbreviation_turn_states
		(id, tenant_id, session_id, root_user_message_id, state, version, expires_at)
		VALUES ('r3', 7, 's', 'u1', 'inspecting', 1, '2026-09-27 01:00:00')`)
	require.NoError(t, err)

	// Down drops only the new tables; up again restores them. Legacy
	// session/message rows survive the cycle untouched.
	apply(down)
	for _, table := range []string{
		"abbreviation_turn_states", "abbreviation_turn_messages", "abbreviation_suggestion_locks",
	} {
		require.False(t, tableExists(t, db, table), "down must drop %s", table)
	}
	var legacySessions, legacyMessages int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&legacySessions))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&legacyMessages))
	require.Equal(t, 1, legacySessions, "down must preserve legacy sessions")
	require.Equal(t, 2, legacyMessages, "down must preserve legacy messages")
	apply(up)
	for _, table := range []string{
		"abbreviation_turn_states", "abbreviation_turn_messages", "abbreviation_suggestion_locks",
	} {
		require.True(t, tableExists(t, db, table), "second up must restore %s", table)
	}
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&legacySessions))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&legacyMessages))
	require.Equal(t, 1, legacySessions)
	require.Equal(t, 2, legacyMessages)
}

// TestMigrationAbbreviationTurnStatesParity asserts the versioned 000114
// PostgreSQL migration carries the same contract as its SQLite mirror
// (static evidence; live PG runs only with a disposable DSN).
func TestMigrationAbbreviationTurnStatesParity(t *testing.T) {
	root := sqliteRepoRoot(t)
	pg, err := os.ReadFile(filepath.Join(root, "migrations", "versioned",
		"000114_abbreviation_turn_states.up.sql"))
	require.NoError(t, err)
	lite, err := os.ReadFile(filepath.Join(root, "migrations", "sqlite",
		"000031_abbreviation_turn_states.up.sql"))
	require.NoError(t, err)

	states := []string{"inspecting", "awaiting_definition", "ready", "running",
		"completed", "blocked_error", "cancelled", "expired"}
	roles := []string{"root", "definition", "clarification", "answer"}
	for _, want := range states {
		require.Contains(t, string(pg), "'"+want+"'", "pg must admit state %s", want)
		require.Contains(t, string(lite), "'"+want+"'", "sqlite must admit state %s", want)
	}
	for _, want := range roles {
		require.Contains(t, string(pg), "'"+want+"'", "pg must admit role %s", want)
		require.Contains(t, string(lite), "'"+want+"'", "sqlite must admit role %s", want)
	}
	require.Contains(t, string(pg), "JSONB", "pg payload must be typed JSONB")
	require.Contains(t, string(lite), "payload TEXT", "sqlite payload must be TEXT JSON")
	for _, want := range []string{"owner_id VARCHAR(512)", "principal_id VARCHAR(512)"} {
		require.Contains(t, string(pg), want, "pg must size %s to session owner scope", want)
		require.Contains(t, string(lite), want, "sqlite must size %s to session owner scope", want)
	}
	for _, want := range []string{
		"abbreviation_turn_states", "abbreviation_turn_messages",
		"abbreviation_suggestion_locks", "idx_abbreviation_turn_root",
		"idx_abbreviation_one_waiting",
	} {
		require.Contains(t, string(pg), want)
		require.Contains(t, string(lite), want)
	}

	down, err := os.ReadFile(filepath.Join(root, "migrations", "versioned",
		"000114_abbreviation_turn_states.down.sql"))
	require.NoError(t, err)
	for _, table := range []string{
		"abbreviation_turn_messages", "abbreviation_suggestion_locks", "abbreviation_turn_states",
	} {
		require.Contains(t, string(down), "DROP TABLE IF EXISTS "+table)
	}
	for _, legacy := range []string{"DROP TABLE abbreviations", "DROP TABLE sessions", "DROP TABLE messages"} {
		require.NotContains(t, string(down), legacy, "down must not touch legacy tables")
	}
}
