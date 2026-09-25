package repository

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ownerTestDDL mirrors knowledgeBasesTestDDL plus the Task 1 ownership
// column (see migrations/versioned/000113). It lives here so the RED run
// exercises the new column/model mapping before the shared fixture is
// updated; the shared DDL is updated to match during GREEN.
const ownerTestDDL = `
CREATE TABLE IF NOT EXISTS knowledge_bases (
    profile_config TEXT,
    generated_profile TEXT,
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    tenant_id INTEGER NOT NULL,
    owner_tenant_id INTEGER NOT NULL DEFAULT 0,
    type VARCHAR(32) NOT NULL DEFAULT 'document',
    visibility VARCHAR(16) NOT NULL DEFAULT 'tenant',
    org_id INTEGER NULL,
    chunking_config TEXT NOT NULL DEFAULT '{}',
    image_processing_config TEXT NOT NULL DEFAULT '{}',
    embedding_model_id VARCHAR(64) NOT NULL,
    summary_model_id VARCHAR(64) NOT NULL,
    cos_config TEXT NOT NULL DEFAULT '{}',
    storage_provider_config TEXT DEFAULT NULL,
    vlm_config TEXT NOT NULL DEFAULT '{}',
    extract_config TEXT NULL DEFAULT NULL,
    faq_config TEXT,
    question_generation_config TEXT NULL,
    auto_tag_config TEXT NULL,
    is_temporary BOOLEAN NOT NULL DEFAULT 0,
    is_pinned INTEGER NOT NULL DEFAULT 0,
    pinned_at DATETIME NULL,
    asr_config TEXT,
    vector_store_id VARCHAR(36),
    storage_backend_id VARCHAR(36),
    wiki_config TEXT,
    indexing_strategy TEXT,
    creator_id VARCHAR(36),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);
`

func setupOwnerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(ownerTestDDL).Error)
	return db
}

// TestKnowledgeBase_OwnerTenantID_Roundtrip proves the model field persists
// to the owner_tenant_id column and distinguishes ownership from the
// data-scope tenant_id (platform-owned public KB keeps its data scope).
func TestKnowledgeBase_OwnerTenantID_Roundtrip(t *testing.T) {
	db := setupOwnerTestDB(t)

	tenantOwned := &types.KnowledgeBase{
		ID: uuid.New().String(), Name: "tenant-kb",
		TenantID: 7, OwnerTenantID: 7,
		EmbeddingModelID: "e", SummaryModelID: "s",
	}
	require.NoError(t, db.Create(tenantOwned).Error)

	platformOwned := &types.KnowledgeBase{
		ID: uuid.New().String(), Name: "public-kb",
		TenantID: 7, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
		EmbeddingModelID: "e", SummaryModelID: "s",
	}
	require.NoError(t, db.Create(platformOwned).Error)

	var got types.KnowledgeBase
	require.NoError(t, db.First(&got, "id = ?", tenantOwned.ID).Error)
	assert.Equal(t, uint64(7), got.OwnerTenantID)
	assert.Equal(t, uint64(7), got.TenantID)

	var gotPublic types.KnowledgeBase
	require.NoError(t, db.First(&gotPublic, "id = ?", platformOwned.ID).Error)
	assert.Equal(t, uint64(0), gotPublic.OwnerTenantID)
	assert.Equal(t, uint64(7), gotPublic.TenantID, "data scope must not follow ownership")
}

// TestKnowledgeBase_OwnerBackfill_KeepsTenantID replays the 000113 backfill
// statement shape: legacy rows (owner default 0) gain owner = tenant while
// tenant_id values do not change.
func TestKnowledgeBase_OwnerBackfill_KeepsTenantID(t *testing.T) {
	db := setupOwnerTestDB(t)

	ids := map[uint64]string{7: uuid.New().String(), 9: uuid.New().String()}
	for tenant, id := range ids {
		kb := &types.KnowledgeBase{
			ID: id, Name: "legacy-kb",
			TenantID: tenant,
			EmbeddingModelID: "e", SummaryModelID: "s",
		}
		require.NoError(t, db.Create(kb).Error)
	}

	require.NoError(t, db.Exec(
		"UPDATE knowledge_bases SET owner_tenant_id = tenant_id WHERE owner_tenant_id = 0 AND tenant_id <> 0",
	).Error)

	for tenant, id := range ids {
		var owner, dataScope uint64
		require.NoError(t, db.Table("knowledge_bases").
			Where("id = ?", id).Select("owner_tenant_id").Scan(&owner).Error)
		require.NoError(t, db.Table("knowledge_bases").
			Where("id = ?", id).Select("tenant_id").Scan(&dataScope).Error)
		assert.Equal(t, tenant, owner, "backfill must copy the owning tenant")
		assert.Equal(t, tenant, dataScope, "backfill must not rewrite the data scope")
	}
}

// TestKnowledgeBase_OwnerVisibilityInvariant_RejectedByModel proves invalid
// owner/visibility combinations are rejected at the model layer on SQLite
// (the Postgres CHECK in 000113 cannot execute here, so Go validation is
// the enforced path under test).
func TestKnowledgeBase_OwnerVisibilityInvariant_RejectedByModel(t *testing.T) {
	invalid := []*types.KnowledgeBase{
		{TenantID: 7, OwnerTenantID: 7, Visibility: types.KBVisibilityPublic},
		{TenantID: 7, OwnerTenantID: 0, Visibility: types.KBVisibilityTenant},
	}
	for _, kb := range invalid {
		assert.Error(t, kb.ValidateOwnership(),
			"owner=%d visibility=%q must be rejected", kb.OwnerTenantID, kb.Visibility)
	}

	valid := []*types.KnowledgeBase{
		{TenantID: 7, OwnerTenantID: 7, Visibility: types.KBVisibilityTenant},
		{TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
		{TenantID: 7, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
	}
	for _, kb := range valid {
		assert.NoError(t, kb.ValidateOwnership(),
			"owner=%d visibility=%q must be accepted", kb.OwnerTenantID, kb.Visibility)
	}
}

// TestGetKBScopeByID_ProjectsOwnerTenantID proves the access-scope
// projection carries both the owner (authorization) and the data-scope
// tenant (execution) without loading the full row.
func TestGetKBScopeByID_ProjectsOwnerTenantID(t *testing.T) {
	db := setupOwnerTestDB(t)
	repo := &knowledgeBaseRepository{db: db}
	ctx := t.Context()

	kb := &types.KnowledgeBase{
		ID: uuid.New().String(), Name: "scoped-kb",
		TenantID: 7, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
		EmbeddingModelID: "e", SummaryModelID: "s",
	}
	require.NoError(t, db.Create(kb).Error)

	scope, err := repo.GetKBScopeByID(ctx, kb.ID)
	require.NoError(t, err)
	require.NotNil(t, scope)
	assert.Equal(t, uint64(0), scope.OwnerTenantID)
	assert.Equal(t, uint64(7), scope.TenantID)
	assert.Equal(t, types.KBVisibilityPublic, scope.Visibility)

	missing, err := repo.GetKBScopeByID(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
