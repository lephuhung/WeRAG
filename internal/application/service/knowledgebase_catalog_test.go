package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Task 4: catalog composition (owned + bounded public page + invites).
//
// - Ordinary tenant humans see own + public page + accepted invites, deduped.
// - API-key / synthetic / anonymous callers receive no implicit public rows.
// - Count/status enrichment uses each KB's stored data-scope tenant_id.
// - Dedicated public catalog is human-only (tenantless SuperAdmin included)
//   with bounded, normalized pagination.
// ---------------------------------------------------------------------------

type catalogCountCall struct {
	tenantID uint64
	kbID     string
}

type catalogKGRepo struct {
	interfaces.KnowledgeRepository
	calls []catalogCountCall
}

func (r *catalogKGRepo) CountKnowledgeByKnowledgeBaseID(_ context.Context, tenantID uint64, kbID string) (int64, error) {
	r.calls = append(r.calls, catalogCountCall{tenantID: tenantID, kbID: kbID})
	return 7, nil
}

func (r *catalogKGRepo) CountKnowledgeByStatus(
	_ context.Context, tenantID uint64, kbID string, _ []string,
) (int64, error) {
	r.calls = append(r.calls, catalogCountCall{tenantID: tenantID, kbID: kbID})
	return 0, nil
}

type catalogChunkRepo struct {
	interfaces.ChunkRepository
	calls []catalogCountCall
}

func (r *catalogChunkRepo) CountChunksByKnowledgeBaseID(_ context.Context, tenantID uint64, kbID string) (int64, error) {
	r.calls = append(r.calls, catalogCountCall{tenantID: tenantID, kbID: kbID})
	return 3, nil
}

type catalogInviteService struct {
	interfaces.KBInvitationService
	ids []string
}

func (s *catalogInviteService) InvitedKBIDs(context.Context, string) ([]string, error) {
	return s.ids, nil
}

func catalogHumanCtx(tenant uint64, user string) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant)
	ctx = context.WithValue(ctx, types.UserIDContextKey, user)
	return types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: user, Role: types.TenantRoleMember})
}

func catalogAPIKeyCtx() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "system-1", Role: types.TenantRoleAdmin})
	return types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{
		KeyID: 7, Name: "k", KnowledgeBaseIDs: types.StringArray{"own-a"},
		Capabilities: types.StringArray{string(types.APIKeyCapabilityRetrieve)},
	})
}

func newCatalogService(
	repo *fakeKBRepo, kg *catalogKGRepo, chunk *catalogChunkRepo, invites *catalogInviteService,
) *knowledgeBaseService {
	return &knowledgeBaseService{repo: repo, kgRepo: kg, chunkRepo: chunk, kbInviteService: invites}
}

func catalogRow(id string, dataTenant, owner uint64, vis types.KBVisibility, kbType string) *types.KnowledgeBase {
	if kbType == "" {
		kbType = types.KnowledgeBaseTypeDocument
	}
	return &types.KnowledgeBase{
		ID: id, Name: id, Type: kbType,
		TenantID: dataTenant, OwnerTenantID: owner, Visibility: vis,
	}
}

func withSource(kb *types.KnowledgeBase, source access.AccessSource) *types.KnowledgeBase {
	kb.AccessSource = string(source)
	return kb
}

// The readable set (owned, public, invites, grants, dedupe, temporary rows)
// is one repository statement now (see repository
// TestListReadableKnowledgeBases*). The service passes the caller's
// classification and returns the rows with their access_source.
func TestListKnowledgeBases_ReturnsReadableRowsForHumanMember(t *testing.T) {
	repo := newFakeKBRepo()
	repo.readable = []*types.KnowledgeBase{
		withSource(catalogRow("own-a", 1, 1, types.KBVisibilityTenant, ""), access.SourceTenantScope),
		withSource(catalogRow("pub-a", 0, 0, types.KBVisibilityPublic, ""), access.SourcePublic),
		withSource(catalogRow("inv-fresh", 9, 9, types.KBVisibilityRestricted, ""), access.SourceACLUser),
	}
	svc := newCatalogService(repo, &catalogKGRepo{}, &catalogChunkRepo{}, &catalogInviteService{})

	got, err := svc.ListKnowledgeBases(catalogHumanCtx(1, "user-a"))
	require.NoError(t, err)
	sources := map[string]string{}
	for _, kb := range got {
		sources[kb.ID] = kb.AccessSource
	}
	assert.Equal(t, map[string]string{
		"own-a": "tenant_scope", "pub-a": "public", "inv-fresh": "acl_user",
	}, sources)
	require.Len(t, repo.readers, 1)
	assert.Equal(t, types.KBReader{
		TenantID: 1, UserID: "user-a", Human: true, TenantMember: true,
	}, repo.readers[0])
}

func TestCatalogComposition_NoPublicForMachineCallers(t *testing.T) {
	// Machine callers are classified non-human, so the repository returns
	// only their own tenant's rows (never public, published or ACL rows).
	synthCtx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	synthCtx = types.WithCaller(synthCtx, types.Caller{TenantID: 1, UserID: "system-1", Role: types.TenantRoleAdmin})
	for _, ctx := range []context.Context{catalogAPIKeyCtx(), synthCtx} {
		repo := newFakeKBRepo()
		svc := newCatalogService(repo, &catalogKGRepo{}, &catalogChunkRepo{}, &catalogInviteService{})
		_, err := svc.ListKnowledgeBases(ctx)
		require.NoError(t, err)
		require.Len(t, repo.readers, 1)
		assert.False(t, repo.readers[0].Human, "machine principals never list as humans")
		assert.False(t, repo.readers[0].SuperAdmin)
		assert.Equal(t, uint64(1), repo.readers[0].TenantID)
	}
}

func TestListKnowledgeBases_SuperAdminKeepsEveryPublishedRow(t *testing.T) {
	repo := newFakeKBRepo()
	repo.readable = []*types.KnowledgeBase{
		withSource(catalogRow("other-published", 5, 5, types.KBVisibilityPublished, ""), access.SourceSuperAdmin),
		withSource(catalogRow("other-restricted", 5, 5, types.KBVisibilityRestricted, ""), access.SourceSuperAdmin),
	}
	svc := newCatalogService(repo, &catalogKGRepo{}, &catalogChunkRepo{}, &catalogInviteService{})
	ctx := context.WithValue(catalogHumanCtx(1, "root"), types.SystemAdminContextKey, true)
	got, err := svc.ListKnowledgeBases(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.True(t, repo.readers[0].SuperAdmin)
}

func TestCatalogCounts_UseDataScopeTenant(t *testing.T) {
	repo := newFakeKBRepo()
	repo.readable = []*types.KnowledgeBase{
		withSource(catalogRow("own-a", 1, 1, types.KBVisibilityTenant, ""), access.SourceTenantScope),
		withSource(catalogRow("inv-foreign", 9, 9, types.KBVisibilityTenant, ""), access.SourceACLUser),
	}
	kg, chunk := &catalogKGRepo{}, &catalogChunkRepo{}
	svc := newCatalogService(repo, kg, chunk, &catalogInviteService{})

	got, err := svc.ListKnowledgeBases(catalogHumanCtx(1, "user-a"))
	require.NoError(t, err)
	require.Len(t, got, 2)
	byID := map[string]*types.KnowledgeBase{}
	for _, kb := range got {
		byID[kb.ID] = kb
	}
	assert.Equal(t, int64(7), byID["inv-foreign"].KnowledgeCount)
	// Every count/status probe for the foreign invite must target its own
	// stored data scope (9), never the requesting tenant (1).
	for _, c := range append(append([]catalogCountCall{}, kg.calls...), chunk.calls...) {
		if c.kbID == "inv-foreign" {
			assert.Equal(t, uint64(9), c.tenantID, "count probe must use the KB data scope")
		} else {
			assert.Equal(t, uint64(1), c.tenantID)
		}
	}
}

func TestListPublicCatalog_TenantlessNonAdminAdmitted(t *testing.T) {
	repo := newFakeKBRepo()
	repo.catalogItems = []*types.KnowledgeBase{
		catalogRow("pub-a", 0, 0, types.KBVisibilityPublic, ""),
	}
	repo.catalogTotal = 1
	svc := newCatalogService(repo, &catalogKGRepo{}, &catalogChunkRepo{}, &catalogInviteService{})

	// Pinned decision: any authenticated human — including a tenantless
	// non-admin with no workspace — may discover the public catalog. Only
	// the main tenant list requires an active tenant.
	ctx := types.WithCaller(context.Background(),
		types.Caller{TenantID: 0, UserID: "user-plain", Role: types.TenantRoleMember})
	items, total, err := svc.ListPublicCatalog(ctx, 1, 10, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
}

func TestListPublicCatalog_ExtremePageSaturatesOffset(t *testing.T) {
	repo := newFakeKBRepo()
	repo.catalogItems = []*types.KnowledgeBase{}
	repo.catalogTotal = 3
	svc := newCatalogService(repo, &catalogKGRepo{}, &catalogChunkRepo{}, &catalogInviteService{})

	// MaxInt page with max page size: the offset must saturate instead of
	// wrapping (on 32-bit builds the product overflows int and would
	// collapse to the first page). The saturated page is valid and empty;
	// the total still counts the catalog.
	maxInt := int(^uint(0) >> 1)
	items, total, err := svc.ListPublicCatalog(catalogHumanCtx(1, "user-a"), maxInt, 200, "")
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Empty(t, items)
	assert.Equal(t, maxInt, repo.catalogOffset,
		"extreme pages saturate the offset instead of wrapping")
}

func TestListPublicCatalog_HumanOnlyWithPagination(t *testing.T) {
	newSvc := func() (*fakeKBRepo, *knowledgeBaseService) {
		repo := newFakeKBRepo()
		repo.catalogItems = []*types.KnowledgeBase{catalogRow("pub-a", 0, 0, types.KBVisibilityPublic, "")}
		repo.catalogTotal = 42
		return repo, newCatalogService(repo, &catalogKGRepo{}, &catalogChunkRepo{}, &catalogInviteService{})
	}

	// Tenantless explicit SuperAdmin discovers the public page.
	repo, svc := newSvc()
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 0, UserID: "super-1", Role: types.TenantRoleMember})
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	items, total, err := svc.ListPublicCatalog(ctx, 2, 10, "")
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	require.Len(t, items, 1)
	assert.Equal(t, 10, repo.catalogLimit)
	assert.Equal(t, 10, repo.catalogOffset, "page 2 of size 10 skips 10")

	// Ordinary tenant human with keyword.
	repo, svc = newSvc()
	items, total, err = svc.ListPublicCatalog(catalogHumanCtx(1, "user-a"), 1, 10, "law")
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	require.Len(t, items, 1)
	assert.Equal(t, "law", repo.catalogKeyword)

	// Anonymous (no identity at all) is denied without panic.
	_, svc = newSvc()
	_, _, err = svc.ListPublicCatalog(context.Background(), 1, 10, "")
	require.Error(t, err, "anonymous must not discover the public catalog")

	// API keys receive no implicit public rows.
	_, svc = newSvc()
	_, _, err = svc.ListPublicCatalog(catalogAPIKeyCtx(), 1, 10, "")
	require.Error(t, err, "API-key principals must not use the human catalog")

	// Pagination normalizes: page 0 → 1, oversize → clamp.
	repo, svc = newSvc()
	_, _, err = svc.ListPublicCatalog(catalogHumanCtx(1, "user-a"), 0, 100000, "")
	require.NoError(t, err)
	assert.Equal(t, 1, repo.catalogPage)
	assert.LessOrEqual(t, repo.catalogLimit, 200)
}
