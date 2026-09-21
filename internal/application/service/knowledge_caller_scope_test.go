package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// callerScopeGrants records the grantee tenant of every grant lookup so the
// test can prove checks always run against the original caller — never the
// execution tenant of a shared execution context. Tenant 2 holds an
// approved grant on the third tenant's "onward" KB; caller tenant 1 does
// not, and must not inherit it.
type callerScopeGrants struct {
	interfaces.KBAccessGrantService
	callers []uint64
}

func (s *callerScopeGrants) ApprovedKBPermission(
	_ context.Context,
	kbID string,
	grantee uint64,
) (types.KBPermission, bool, error) {
	s.callers = append(s.callers, grantee)
	return types.KBPermissionViewer, kbID == "onward" && grantee == 2, nil
}

func (s *callerScopeGrants) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return nil, nil
}

func TestSharedExecutionFiltersDocumentsChunksAndSearchScope(t *testing.T) {
	grants := &callerScopeGrants{}
	svc, db := newKnowledgeSharedAccessService(t, grants)
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	kbs := []*types.KnowledgeBase{
		{ID: "own", TenantID: 1},
		{ID: "granted", TenantID: 2},
		{ID: "private", TenantID: 2},
		{ID: "onward", TenantID: 3},
	}
	var ids []string
	for _, kb := range kbs {
		ids = append(ids, kb.ID)
		seedKnowledge(t, db, &types.Knowledge{ID: kb.ID, TenantID: kb.TenantID, KnowledgeBaseID: kb.ID, Type: "file"})
		require.NoError(
			t,
			db.Create(
				&types.Chunk{
					ID:              kb.ID,
					TenantID:        kb.TenantID,
					KnowledgeBaseID: kb.ID,
					KnowledgeID:     kb.ID,
					Content:         kb.ID,
				},
			).Error,
		)
	}
	search := &knowledgeBaseService{
		kgRepo:               svc.repo,
		chunkRepo:            repository.NewChunkRepository(db),
		kbAccessGrantService: grants,
	}
	kbService := &suggestionKBService{kbs: make(map[string]*types.KnowledgeBase)}
	for _, kb := range kbs {
		kbService.kbs[kb.ID] = kb
	}
	session := &sessionService{knowledgeBaseService: kbService, knowledgeService: svc, kbAccessGrantService: grants}
	base := newSharedAccessContext()
	grant := &access.KBAccess{
		KnowledgeBase:     kbs[1],
		Caller:            types.CallerFromContext(base),
		EffectiveTenantID: 2,
		Permission:        types.KBPermissionViewer,
	}
	for _, execution := range []uint64{1, 2, 3} {
		ctx := logger.CloneContext(types.WithExecutionTenant(grant.Context(base), execution))
		rows, err := svc.GetKnowledgeBatchWithSharedAccess(ctx, execution, ids)
		require.NoError(t, err)
		got := make([]string, 0, len(rows))
		for _, row := range rows {
			got = append(got, row.ID)
		}
		require.ElementsMatch(t, []string{"own", "granted"}, got)
		knowledgeMap, err := search.fetchKnowledgeDataWithShared(ctx, execution, ids)
		require.NoError(t, err)
		require.Len(t, knowledgeMap, 2)
		require.NotNil(t, knowledgeMap["own"])
		require.NotNil(t, knowledgeMap["granted"])
		chunks, err := search.listChunksByIDWithShared(ctx, execution, ids)
		require.NoError(t, err)
		got = got[:0]
		for _, chunk := range chunks {
			got = append(got, chunk.ID)
		}
		require.ElementsMatch(t, []string{"own", "granted"}, got)
		require.NoError(t, search.authorizeKBAccess(ctx, kbs[:2]))
		require.Error(t, search.authorizeKBAccess(ctx, kbs[2:3]), "execution tenant is not ownership")
		require.Error(t, search.authorizeKBAccess(ctx, kbs[3:]), "owner tenant grants are not inherited")
		targets, err := session.buildSearchTargets(
			ctx,
			execution,
			ids,
			nil,
			[]types.TagScope{{KnowledgeBaseID: "private", TagIDs: []string{"tag"}}},
		)
		require.NoError(t, err)
		require.Len(t, targets, 2, "denied KBs must not become search or tag targets")
		got = got[:0]
		for _, target := range targets {
			got = append(got, target.KnowledgeBaseID)
		}
		require.ElementsMatch(t, []string{"own", "granted"}, got)
		_, err = resolveKBReadTenant(ctx, kbs[1], grants)
		require.NoError(t, err, "FAQ/tag reads accept the exact upstream grant")
		_, err = resolveKBReadTenant(ctx, kbs[2], grants)
		require.Error(t, err, "FAQ/tag reads cannot access another KB in the execution tenant")
	}
	for _, caller := range grants.callers {
		require.Equal(t, uint64(1), caller)
	}
}

type callerScopeKBList struct {
	interfaces.KnowledgeBaseService
	tenant uint64
}

func (s *callerScopeKBList) ListKnowledgeBases(ctx context.Context) ([]*types.KnowledgeBase, error) {
	s.tenant = types.MustTenantIDFromContext(ctx)
	return []*types.KnowledgeBase{{ID: "own", TenantID: s.tenant, Type: types.KnowledgeBaseTypeDocument}}, nil
}

type callerScopeSearchRepo struct {
	interfaces.KnowledgeRepository
	scopes []types.KnowledgeSearchScope
}

func (r *callerScopeSearchRepo) SearchKnowledgeInScopes(
	_ context.Context,
	scopes []types.KnowledgeSearchScope,
	_ string,
	_, _ int,
	_ []string,
) ([]*types.Knowledge, bool, int64, error) {
	r.scopes = scopes
	return nil, false, 0, nil
}

func TestDocumentSearchListsCallerTenantAfterExecutionSwitch(t *testing.T) {
	kbs := &callerScopeKBList{}
	repo := &callerScopeSearchRepo{}
	svc := &knowledgeService{kbService: kbs, repo: repo}
	ctx := types.WithExecutionTenant(newSharedAccessContext(), 2)
	_, _, _, err := svc.SearchKnowledge(ctx, "query", 0, 10, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), kbs.tenant)
	require.Equal(t, []types.KnowledgeSearchScope{{TenantID: 1, KBID: "own"}}, repo.scopes)
	require.Equal(t, uint64(2), types.MustTenantIDFromContext(ctx), "search must not mutate the parent context")
}

func TestAPIKeyScopeDenialSurfacesAsForbidden(t *testing.T) {
	ctx := types.WithTenantAPIKeyScope(
		newSharedAccessContext(),
		types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"private"}},
	)
	search := &knowledgeBaseService{}
	app, ok := apperrors.IsAppError(
		search.authorizeKBAccess(ctx, []*types.KnowledgeBase{{ID: "selected", TenantID: 2}}),
	)
	require.True(t, ok)
	require.Equal(t, apperrors.ErrForbidden, app.Code, "API-key scope denial must not become a lookup failure")
}
