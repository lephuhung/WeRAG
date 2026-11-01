package session

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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

// /knowledge-chat never runs a Guest's turn: its quick-answer pipeline
// would not be held to the public-lookup agent.
func TestKnowledgeQARefusesThePublicPortal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey,
		&types.Tenant{ID: 10012, Kind: types.TenantKindPublicPortal})
	c.Request = httptest.NewRequest(http.MethodPost, "/knowledge-chat/s1", nil).WithContext(ctx)
	(&Handler{}).KnowledgeQA(c)
	require.Len(t, c.Errors, 1)
	var appErr *errors.AppError
	require.True(t, stderrors.As(c.Errors[0].Err, &appErr))
	require.Equal(t, http.StatusBadRequest, appErr.HTTPCode)
}
