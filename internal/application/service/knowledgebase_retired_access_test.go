package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type retiredAccessListRepo struct {
	*fakeKBRepo
	publicListCalled bool
	getIDsCalled     bool
}

func (r *retiredAccessListRepo) ListPublicKnowledgeBasesExcept(context.Context, uint64) ([]*types.KnowledgeBase, error) {
	r.publicListCalled = true
	return nil, nil
}

func (r *retiredAccessListRepo) GetKnowledgeBaseByIDs(context.Context, []string) ([]*types.KnowledgeBase, error) {
	r.getIDsCalled = true
	return nil, nil
}

type retiredAccessGrantList struct {
	interfaces.KBAccessGrantService
	called bool
}

func (s *retiredAccessGrantList) GrantedKBIDs(context.Context, uint64) ([]string, error) {
	s.called = true
	return []string{"legacy-grant-kb"}, nil
}

// A machine (non-human) context lists only its own tenant's KBs: no legacy
// public discovery and no tenant-wide grants.
func TestListKnowledgeBasesNonHumanDoesNotExpandAccess(t *testing.T) {
	repo := &retiredAccessListRepo{fakeKBRepo: newFakeKBRepo()}
	grants := &retiredAccessGrantList{}
	svc := &knowledgeBaseService{repo: repo, kbAccessGrantService: grants}

	listed, err := svc.ListKnowledgeBases(ctxWithTenant(1))
	require.NoError(t, err)
	require.Empty(t, listed)
	require.False(t, repo.publicListCalled, "retired public KBs must not be discovered across tenants")
	require.False(t, grants.called, "tenant-wide grants are for human callers only")
	require.False(t, repo.getIDsCalled, "granted KB IDs must not be loaded for a non-human caller")
}
