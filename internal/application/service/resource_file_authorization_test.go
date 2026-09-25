package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// catalogFileGrants grants the caller's tenant viewer access to the "shared"
// KB and resolves that KB's owner tenant for the evidence path.
type catalogFileGrants struct{}

func (catalogFileGrants) ApprovedKBPermission(
	_ context.Context,
	kb string,
	_ uint64,
) (types.KBPermission, bool, error) {
	return types.KBPermissionViewer, kb == "shared", nil
}

func (catalogFileGrants) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return nil, nil
}

func (catalogFileGrants) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{{ID: "shared", TenantID: 7}}, nil
}

func TestCatalogFileAuthorizationRejectsLegacyTenantGrant(t *testing.T) {
	catalog, db := newResourceCatalogForTest(t)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{}, &types.WikiPage{}))
	ctx := newSharedAccessContext()
	const physical = "local://7/exports/private.png"
	ref, err := catalog.Register(ctx, 7, physical, interfaces.ResourceRegistration{})
	require.NoError(t, err)
	for _, kb := range []string{"private", "shared"} {
		require.NoError(t, db.Create(&types.KnowledgeBase{ID: kb, TenantID: 7}).Error)
		require.NoError(
			t,
			db.Create(&types.Knowledge{ID: kb + "-doc", TenantID: 7, KnowledgeBaseID: kb, Type: "file"}).Error,
		)
	}
	require.NoError(
		t,
		catalog.Bind(ctx, ref, types.ResourceOwnerKnowledge, "private-doc", types.ResourceRelationSourceFile),
	)
	text := "![private](" + ref + ") ![legacy](" + physical + ")"
	require.NoError(
		t,
		db.Create(
			&types.Chunk{ID: "chunk", TenantID: 7, KnowledgeBaseID: "shared", KnowledgeID: "shared-doc", Content: text},
		).Error,
	)
	require.NoError(
		t,
		db.Create(&types.WikiPage{ID: "wiki", TenantID: 7, KnowledgeBaseID: "shared", Content: text}).Error,
	)
	grant := &access.KBAccess{
		KnowledgeBase:     &types.KnowledgeBase{ID: "shared", TenantID: 7},
		Caller:            types.CallerFromContext(ctx),
		EffectiveTenantID: 7,
		Permission:        types.KBPermissionViewer,
	}
	lookup := catalog.(interfaces.KBResourceLookup)
	message := &types.Message{
		ID:                  "message",
		AgentTenantID:       1,
		Content:             text,
		KnowledgeReferences: []*types.SearchResult{{KnowledgeBaseID: "shared", Content: text}},
	}
	authorizer := access.MessageKBGrantAuthorizer{GrantGuard: catalogFileGrants{}, KBs: catalogFileGrants{}}
	check := func(allowed bool) {
		t.Helper()
		for _, path := range []string{ref, physical} {
			_, err := access.ResolveKBFile(ctx, grant, "shared", path, catalog, lookup)
			if allowed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, access.ErrForbidden)
			}
			_, err = access.AuthorizeMessageFile(ctx, message, path, catalog, authorizer)
			if allowed && path == ref {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, access.ErrForbidden)
			}
		}
	}
	check(false)
}
