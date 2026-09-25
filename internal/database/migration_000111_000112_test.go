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

// execVersionedUp executes the data statements of a versioned migration
// file against SQLite. DO/NOTICE blocks and bare SELECTs are
// Postgres-only scaffolding and are skipped; the UPDATE statements that
// carry the actual narrowing must be plain SQL valid on both engines.
func execVersionedUp(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(sqliteRepoRoot(t), "migrations", "versioned", name))
	require.NoError(t, err)
	// Strip -- line comments first: they may contain semicolons.
	var code strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	for _, stmt := range strings.Split(code.String(), ";") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		// DO $$ ... END $$ notice blocks and bare SELECTs are
		// Postgres-only scaffolding.
		if strings.HasPrefix(upper, "DO ") || strings.Contains(trimmed, "$$") ||
			strings.HasPrefix(upper, "SELECT ") || strings.HasPrefix(upper, "END") {
			continue
		}
		require.NoError(t, db.Exec(trimmed).Error, "statement in %s", name)
	}
}

func TestMigration000111RevokesPublicKBsAndGrants(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_bases (
		id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT,
		visibility VARCHAR(16) NOT NULL DEFAULT 'tenant',
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE kb_access_grants (
		id VARCHAR(36) PRIMARY KEY, kb_id VARCHAR(36) NOT NULL,
		owner_tenant_id BIGINT NOT NULL, grantee_tenant_id BIGINT NOT NULL,
		status VARCHAR(16) NOT NULL DEFAULT 'pending',
		responded_at TIMESTAMP WITH TIME ZONE,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMP WITH TIME ZONE
	)`).Error)

	require.NoError(t, db.Exec(
		`INSERT INTO knowledge_bases (id, tenant_id, visibility) VALUES
		('kb-pub', 1, 'public'), ('kb-tenant', 1, 'tenant')`).Error)
	require.NoError(t, db.Exec(
		`INSERT INTO kb_access_grants (id, kb_id, owner_tenant_id, grantee_tenant_id, status) VALUES
		('g-pending', 'kb-pub', 1, 2, 'pending'),
		('g-approved', 'kb-tenant', 1, 3, 'approved'),
		('g-rejected', 'kb-tenant', 1, 4, 'rejected'),
		('g-revoked', 'kb-tenant', 1, 5, 'revoked')`).Error)

	execVersionedUp(t, db, "000111_revoke_public_kbs_and_grants.up.sql")

	var publicCount int64
	require.NoError(t, db.Table("knowledge_bases").Where("visibility = 'public'").Count(&publicCount).Error)
	require.Zero(t, publicCount, "no public KB may survive migration 000111")
	var vis string
	require.NoError(t, db.Table("knowledge_bases").Where("id = 'kb-pub'").Select("visibility").Scan(&vis).Error)
	require.Equal(t, "tenant", vis)

	var liveCount int64
	require.NoError(t, db.Table("kb_access_grants").
		Where("status IN ('pending','approved') AND deleted_at IS NULL").Count(&liveCount).Error)
	require.Zero(t, liveCount, "no live tenant-wide grant may survive migration 000111")
	var status string
	require.NoError(t, db.Table("kb_access_grants").Where("id = 'g-approved'").Select("status").Scan(&status).Error)
	require.Equal(t, "revoked", status)
	require.NoError(t, db.Table("kb_access_grants").Where("id = 'g-pending'").Select("status").Scan(&status).Error)
	require.Equal(t, "revoked", status)
	// Terminal rows are untouched.
	require.NoError(t, db.Table("kb_access_grants").Where("id = 'g-rejected'").Select("status").Scan(&status).Error)
	require.Equal(t, "rejected", status)
}

func TestMigration000112CollapsesToAdminMember(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE tenant_members (
		id INTEGER PRIMARY KEY AUTOINCREMENT, user_id VARCHAR(36) NOT NULL,
		tenant_id BIGINT NOT NULL, role VARCHAR(20) NOT NULL DEFAULT 'member',
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMP WITH TIME ZONE
	)`).Error)

	require.NoError(t, db.Exec(
		`INSERT INTO tenant_members (user_id, tenant_id, role) VALUES
		('u-owner', 1, 'owner'), ('u-admin', 1, 'admin'),
		('u-member', 1, 'member'), ('u-contrib', 1, 'contributor'),
		('u-viewer', 1, 'viewer'), ('u-bogus', 1, 'superuser')`).Error)

	execVersionedUp(t, db, "000112_three_role_model.up.sql")

	rows := []struct {
		UserID string
		Role   string
	}{}
	require.NoError(t, db.Table("tenant_members").Select("user_id, role").Scan(&rows).Error)
	got := map[string]string{}
	for _, r := range rows {
		got[r.UserID] = r.Role
	}
	require.Equal(t, map[string]string{
		"u-owner": "admin", "u-admin": "admin", "u-member": "member",
		"u-contrib": "member", "u-viewer": "member", "u-bogus": "member",
	}, got)
}
