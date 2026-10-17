package repository

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestListReadableKnowledgeBasesPostgres runs the readable-KB statement on
// a real PostgreSQL (boolean binds, derived-table LEFT JOINs, timestamptz
// comparisons) in a disposable random schema. Skipped unless
// WEKNORA_MIGRATION_TEST_POSTGRES_DSN points at a disposable database.
func TestListReadableKnowledgeBasesPostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_MIGRATION_TEST_POSTGRES_DSN to run the disposable PostgreSQL readable-KB test")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := fmt.Sprintf("readable_kb_%d", time.Now().UnixNano())
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	sqlDB, err := admin.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, admin.Exec("SET search_path TO "+schema).Error)
	require.NoError(t, admin.AutoMigrate(&types.Tenant{}, &types.TenantMember{}, &types.KnowledgeBase{},
		&types.KBInvitation{}, &types.KBAccessGrant{}))
	runReadableCases(t, seedReadableFixture(t, admin))
}
