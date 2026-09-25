package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// portableStatements113 extracts the SQLite-portable statements (backfill
// UPDATEs and CREATE INDEX) from the Postgres-dialect 000113 up migration.
// Postgres-only scaffolding — DO $$ notice blocks, ADD COLUMN IF NOT
// EXISTS, and ALTER TABLE ... ADD CONSTRAINT — is intentionally skipped:
// SQLite cannot execute those, so the CHECK invariant is covered by Go
// model validation (see types.ValidateOwnership) instead.
func portableStatements113(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(
		sqliteRepoRoot(t), "migrations", "versioned",
		"000113_platform_public_knowledge_bases.up.sql"))
	require.NoError(t, err, "000113 up migration must exist")
	var code strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	var out []string
	for _, stmt := range strings.Split(code.String(), ";") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "UPDATE ") || strings.HasPrefix(upper, "CREATE INDEX ") {
			out = append(out, trimmed)
		}
	}
	require.NotEmpty(t, out, "000113 must contain portable UPDATE/CREATE INDEX statements")
	return out
}

func TestMigration000113BackfillsOwnerPreservesTenant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// Pre-migration shape plus the new column at its rollout default,
	// mirroring what the Postgres ADD COLUMN ... DEFAULT 0 produces.
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_bases (
		id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
		owner_tenant_id BIGINT NOT NULL DEFAULT 0,
		visibility VARCHAR(16) NOT NULL DEFAULT 'tenant',
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error)

	require.NoError(t, db.Exec(
		`INSERT INTO knowledge_bases (id, tenant_id, visibility) VALUES
		('kb-a', 7, 'tenant'), ('kb-b', 9, 'tenant'), ('kb-c', 7, 'tenant')`).Error)

	for _, stmt := range portableStatements113(t) {
		require.NoError(t, db.Exec(stmt).Error, "portable statement: %s", stmt)
	}

	type row struct {
		ID       string
		TenantID uint64
		Owner    uint64
	}
	var rows []row
	require.NoError(t, db.Table("knowledge_bases").
		Select("id, tenant_id, owner_tenant_id AS owner").Scan(&rows).Error)
	got := map[string]row{}
	for _, r := range rows {
		got[r.ID] = r
	}
	require.Len(t, got, 3)
	for id, tenant := range map[string]uint64{"kb-a": 7, "kb-b": 9, "kb-c": 7} {
		require.Equal(t, tenant, got[id].TenantID, "%s tenant_id must not change", id)
		require.Equal(t, tenant, got[id].Owner, "%s owner must backfill from tenant_id", id)
	}

	// Every surviving row must satisfy the owner/visibility invariant the
	// Postgres CHECK enforces (tenant visibility requires a nonzero owner).
	var violations int64
	require.NoError(t, db.Table("knowledge_bases").
		Where("NOT ((visibility = 'public' AND owner_tenant_id = 0) OR " +
			"(visibility = 'tenant' AND owner_tenant_id > 0))").
		Count(&violations).Error)
	require.Zero(t, violations, "backfilled rows must satisfy the owner/visibility invariant")

	// The catalog index serving owner + visibility queries must exist.
	var idxCount int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?",
		"idx_knowledge_bases_owner_visibility").Scan(&idxCount).Error)
	require.Equal(t, int64(1), idxCount)
}
