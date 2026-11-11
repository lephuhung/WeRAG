package repository

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The lists' Postgres branch (JSONB operators, jsonb_array_length) and
// migration 000197 (its partial indexes must be valid on the real column
// types). Runs only on a disposable database.
func TestAnswerQualityPostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL DSN")
	}
	for _, name := range []string{"disliked", "ungrounded"} {
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
			require.NoError(t, err)
			for _, table := range []string{"answer_quality_reviews", "messages", "sessions", "tenants", "users", "custom_agents"} {
				require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+table+" CASCADE").Error)
			}
			f := answerQualityFixtureOn(t, db)
			// production column types (000000_init, 000067)
			require.NoError(t, db.Exec(`ALTER TABLE messages ALTER COLUMN knowledge_references TYPE JSONB USING knowledge_references::jsonb`).Error)
			require.NoError(t, db.Exec(`ALTER TABLE messages ALTER COLUMN execution_context TYPE JSONB USING execution_context::jsonb`).Error)
			require.NoError(t, db.Exec("DROP TABLE answer_quality_reviews").Error)
			_, file, _, _ := runtime.Caller(0)
			up, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", "versioned", "000197_answer_quality_reviews.up.sql"))
			require.NoError(t, err)
			require.NoError(t, db.Exec(string(up)).Error)
			if name == "disliked" {
				checkAnswerQualityDisliked(t, f)
			} else {
				checkAnswerQualityUngrounded(t, f)
			}
			down, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", "versioned", "000197_answer_quality_reviews.down.sql"))
			require.NoError(t, err)
			require.NoError(t, db.Exec(string(down)).Error)
		})
	}
}
