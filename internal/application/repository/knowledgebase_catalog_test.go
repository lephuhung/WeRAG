package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Task 4: owner-scoped catalog queries. The public catalog is keyed on
// owner_tenant_id = 0 AND visibility = public — never on the data-scope
// tenant_id — so converted rows (platform-owned, foreign data scope) are
// included and malformed owner/visibility pairs are excluded.

// seedCatalogTenant creates a tenant row directly (fixtures predate the
// create/delete serialization protocol and bypass it intentionally).
func seedCatalogTenant(t *testing.T, db *gorm.DB, name string) uint64 {
	t.Helper()
	tenant := &types.Tenant{Name: name, Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	return tenant.ID
}

func seedCatalogKB(
	t *testing.T, db *gorm.DB, id string, dataTenant, owner uint64, vis types.KBVisibility, temp bool,
) {
	t.Helper()
	require.NoError(t, db.Create(&types.KnowledgeBase{
		ID: id, Name: "kb-" + id,
		TenantID: dataTenant, OwnerTenantID: owner, Visibility: vis,
		IsTemporary: temp,
	}).Error)
}

func catalogIDs(kbs []*types.KnowledgeBase) []string {
	out := make([]string, 0, len(kbs))
	for _, kb := range kbs {
		out = append(out, kb.ID)
	}
	return out
}

func TestListOwnedKnowledgeBases_OwnerScoped(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewKnowledgeBaseRepository(db)

	t1 := seedCatalogTenant(t, db, "t1")
	t2 := seedCatalogTenant(t, db, "t2")
	seedCatalogKB(t, db, "own-a", t1, t1, types.KBVisibilityTenant, false)
	seedCatalogKB(t, db, "own-b", t1, t1, types.KBVisibilityTenant, false)
	// Converted row: owned by t1, platform data scope.
	seedCatalogKB(t, db, "own-converted", 0, t1, types.KBVisibilityTenant, false)
	// Legacy pre-backfill row: no owner but tenant visibility + data scope.
	seedCatalogKB(t, db, "own-legacy", t1, 0, types.KBVisibilityTenant, false)
	// Foreign tenant-owned row must never enter.
	seedCatalogKB(t, db, "foreign", t2, t2, types.KBVisibilityTenant, false)
	// Platform-owned public row is catalog, not owned.
	seedCatalogKB(t, db, "pub", 0, 0, types.KBVisibilityPublic, false)
	// Temporary and soft-deleted rows excluded.
	seedCatalogKB(t, db, "temp", t1, t1, types.KBVisibilityTenant, true)
	seedCatalogKB(t, db, "gone", t1, t1, types.KBVisibilityTenant, false)
	require.NoError(t, db.Where("id = ?", "gone").Delete(&types.KnowledgeBase{}).Error)

	got, err := repo.ListOwnedKnowledgeBases(ctx, t1)
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"own-a", "own-b", "own-converted", "own-legacy"}, catalogIDs(got))
}

func TestListPlatformPublicCatalog_OwnerAndVisibility(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewKnowledgeBaseRepository(db)

	t1 := seedCatalogTenant(t, db, "t1")
	seedCatalogKB(t, db, "pub-a", 0, 0, types.KBVisibilityPublic, false)
	// Converted row keeps a foreign data scope: still platform catalog.
	seedCatalogKB(t, db, "pub-converted", t1, 0, types.KBVisibilityPublic, false)
	// Malformed owner/visibility pair: nonzero owner with public visibility
	// is never platform catalog.
	seedCatalogKB(t, db, "pub-malformed", t1, t1, types.KBVisibilityPublic, false)
	// Tenant rows, temporary and soft-deleted public rows excluded.
	seedCatalogKB(t, db, "tenant", t1, t1, types.KBVisibilityTenant, false)
	seedCatalogKB(t, db, "pub-temp", 0, 0, types.KBVisibilityPublic, true)
	seedCatalogKB(t, db, "pub-gone", 0, 0, types.KBVisibilityPublic, false)
	require.NoError(t, db.Where("id = ?", "pub-gone").Delete(&types.KnowledgeBase{}).Error)

	got, total, err := repo.ListPlatformPublicCatalog(ctx, "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.ElementsMatch(t, []string{"pub-a", "pub-converted"}, catalogIDs(got))
}

func TestListPlatformPublicCatalog_PaginationStable(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewKnowledgeBaseRepository(db)

	for i := 0; i < 5; i++ {
		seedCatalogKB(t, db, fmt.Sprintf("pub-%d", i), 0, 0, types.KBVisibilityPublic, false)
	}

	// Page through with limit 2: pages partition the catalog without
	// overlap, and the union in order equals the full ordered list.
	full, total, err := repo.ListPlatformPublicCatalog(ctx, "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, full, 5)

	var paged []string
	for _, offset := range []int{0, 2, 4} {
		items, total, err := repo.ListPlatformPublicCatalog(ctx, "", 2, offset)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total, "every page reports the full total")
		paged = append(paged, catalogIDs(items)...)
	}
	assert.Equal(t, catalogIDs(full), paged, "paged union must equal the stable full order")
}

func TestListPlatformPublicCatalog_ExtremeOffsetIsValidEmptyPage(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewKnowledgeBaseRepository(db)

	seedCatalogKB(t, db, "pub-a", 0, 0, types.KBVisibilityPublic, false)

	// An absurd offset is a valid empty page: total still counts the
	// catalog, no rows return, and nothing falls back to the first page.
	got, total, err := repo.ListPlatformPublicCatalog(ctx, "", 200, int(^uint(0)>>1))
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Empty(t, got)
}

func TestListPlatformPublicCatalog_KeywordFilter(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewKnowledgeBaseRepository(db)

	seedCatalogKB(t, db, "pub-law", 0, 0, types.KBVisibilityPublic, false)
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id = ?", "pub-law").
		Updates(map[string]any{"name": "Civil Code", "description": "public laws"}).Error)
	seedCatalogKB(t, db, "pub-other", 0, 0, types.KBVisibilityPublic, false)
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id = ?", "pub-other").
		Updates(map[string]any{"name": "Cookbook", "description": "recipes"}).Error)

	got, total, err := repo.ListPlatformPublicCatalog(ctx, "civil", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, got, 1)
	assert.Equal(t, "pub-law", got[0].ID)
}
