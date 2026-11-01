package session

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type publicKBListStub struct {
	interfaces.KnowledgeBaseService
	kbs []*types.KnowledgeBase
	err error
}

func (s *publicKBListStub) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return s.kbs, s.err
}

// A Guest's turn is refused, before it is metered, when the public lookup
// has no platform public knowledge base to read: none at all, only tenant
// or published ones, or a listing error. It never falls back to plain chat.
func TestPublicLookupWithoutKnowledge(t *testing.T) {
	agent := enforcePublicLookupConfig(&types.CustomAgent{ID: types.BuiltinPublicLookupID})
	rag := types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	public := &types.KnowledgeBase{ID: "pub", Visibility: types.KBVisibilityPublic, IndexingStrategy: rag}
	unsearchable := &types.KnowledgeBase{ID: "pub-raw", Visibility: types.KBVisibilityPublic}
	tenantKB := &types.KnowledgeBase{ID: "own", Visibility: types.KBVisibilityPublic, OwnerTenantID: 7, IndexingStrategy: rag}

	refused := func(stub *publicKBListStub) {
		t.Helper()
		h := &Handler{knowledgebaseService: stub}
		err := h.publicLookupWithoutKnowledge(context.Background(), agent)
		var appErr *errors.AppError
		require.True(t, stderrors.As(err, &appErr))
		require.Equal(t, errors.ErrAgentUnavailable, appErr.Code)
		require.Equal(t, types.PublicLookupNoKnowledgeMessage, appErr.Message)
	}
	refused(&publicKBListStub{})
	refused(&publicKBListStub{kbs: []*types.KnowledgeBase{tenantKB, unsearchable}})
	refused(&publicKBListStub{kbs: []*types.KnowledgeBase{public}, err: stderrors.New("db down")})

	h := &Handler{knowledgebaseService: &publicKBListStub{kbs: []*types.KnowledgeBase{tenantKB, public}}}
	require.NoError(t, h.publicLookupWithoutKnowledge(context.Background(), agent))
}
