package service

import (
	"context"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// restrictedReadGrants is a grant service that also resolves live invites
// (like the real kbAccessGrantService), so service read checks see both
// ACL sources.
type restrictedReadGrants struct {
	fakeKBGrantService
	invites map[string]bool // kbID + "/" + userID
}

func (g *restrictedReadGrants) HasAcceptedInvite(_ context.Context, kbID, userID string) (bool, error) {
	return g.invites[kbID+"/"+userID], nil
}

func (g *restrictedReadGrants) HasLiveInvite(ctx context.Context, kbID, userID string) (bool, error) {
	return g.HasAcceptedInvite(ctx, kbID, userID)
}

func restrictedReadFixture() (*restrictedReadGrants, map[string]*types.KnowledgeBase) {
	kbs := map[string]*types.KnowledgeBase{
		"kb-r": {ID: "kb-r", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityRestricted},
		"kb-t": {ID: "kb-t", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant},
	}
	grants := &restrictedReadGrants{
		fakeKBGrantService: fakeKBGrantService{scopes: map[string]*types.KBScope{
			"kb-r": {TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityRestricted,
				MemberDefault: types.KBMemberDefaultContribute},
			"kb-t": tenantScope(1),
		}},
		invites: map[string]bool{},
	}
	return grants, kbs
}

func restrictedMemberCtx() context.Context {
	ctx := newSharedAccessContext() // tenant 1, user-1, Member
	return types.WithCaller(ctx, types.CallerFromContext(ctx))
}

// Search: HybridSearch's per-KB authorization rejects a restricted KB for
// an owner-tenant Member without an ACL (404, no existence leak) and
// admits it once an invite is live.
func TestSearchAuthorizationClosesRestrictedKB(t *testing.T) {
	grants, kbs := restrictedReadFixture()
	svc := &knowledgeBaseService{kbAccessGrantService: grants}
	ctx := restrictedMemberCtx()

	require.NoError(t, svc.authorizeKBAccess(ctx, []*types.KnowledgeBase{kbs["kb-t"]}))
	err := svc.authorizeKBAccess(ctx, []*types.KnowledgeBase{kbs["kb-t"], kbs["kb-r"]})
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok, "got %v", err)
	require.Equal(t, 404, appErr.HTTPCode)

	grants.invites["kb-r/user-1"] = true
	require.NoError(t, svc.authorizeKBAccess(ctx, []*types.KnowledgeBase{kbs["kb-r"]}))
}

// Chat KB selection: search targets are rebuilt per request, so a
// restricted KB is dropped without an ACL and a revoked invite stops
// retrieval on the next turn.
func TestChatSearchTargetsDropRestrictedKBWithoutACL(t *testing.T) {
	grants, kbs := restrictedReadFixture()
	session := &sessionService{
		knowledgeBaseService: &suggestionKBService{kbs: kbs},
		kbAccessGrantService: grants,
	}
	ctx := restrictedMemberCtx()
	targetIDs := func() []string {
		targets, err := session.buildSearchTargets(ctx, 1, []string{"kb-t", "kb-r"}, nil, nil)
		require.NoError(t, err)
		ids := make([]string, 0, len(targets))
		for _, target := range targets {
			ids = append(ids, target.KnowledgeBaseID)
		}
		return ids
	}
	require.Equal(t, []string{"kb-t"}, targetIDs())

	grants.invites["kb-r/user-1"] = true
	require.ElementsMatch(t, []string{"kb-t", "kb-r"}, targetIDs())

	delete(grants.invites, "kb-r/user-1")
	require.Equal(t, []string{"kb-t"}, targetIDs(), "a revoked invite stops retrieval immediately")
}

// Batch: /knowledge/batch without kb_id resolves documents through the
// same read check; documents of a restricted KB are not returned.
func TestKnowledgeBatchHidesRestrictedKBDocuments(t *testing.T) {
	grants, _ := restrictedReadFixture()
	svc, db := newKnowledgeSharedAccessService(t, grants)
	seedKnowledge(t, db, &types.Knowledge{ID: "doc-t", TenantID: 1, KnowledgeBaseID: "kb-t", Type: "file"})
	seedKnowledge(t, db, &types.Knowledge{ID: "doc-r", TenantID: 1, KnowledgeBaseID: "kb-r", Type: "file"})

	rows, err := svc.GetKnowledgeBatchWithSharedAccess(restrictedMemberCtx(), 1, []string{"doc-t", "doc-r"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "doc-t", rows[0].ID)
}

// Citations: persisted knowledge references (with chunk snippets) of a KB
// the caller can no longer read are hidden when history loads; an invite
// brings them back and its revocation hides them again.
func TestHistoryHidesCitationsOfUnreadableKB(t *testing.T) {
	grants, _ := restrictedReadFixture()
	svc := &messageService{kbAccessGrantService: grants}
	ctx := restrictedMemberCtx()
	load := func() []string {
		messages := []*types.Message{{Role: "assistant", KnowledgeReferences: types.References{
			{ID: "c-t", KnowledgeBaseID: "kb-t", Content: "tenant snippet"},
			{ID: "c-r", KnowledgeBaseID: "kb-r", Content: "restricted snippet"},
			{ID: "c-attach", Content: "chat attachment, not KB-bound"},
		}}}
		svc.hideUnreadableReferences(ctx, messages)
		ids := []string{}
		for _, ref := range messages[0].KnowledgeReferences {
			ids = append(ids, ref.ID)
		}
		return ids
	}
	require.Equal(t, []string{"c-t", "c-attach"}, load())
	grants.invites["kb-r/user-1"] = true
	require.Equal(t, []string{"c-t", "c-r", "c-attach"}, load())
	delete(grants.invites, "kb-r/user-1")
	require.Equal(t, []string{"c-t", "c-attach"}, load())
}

// citationKnowledgeService resolves documents to their KB for the history
// citation filter; unknown IDs are deleted documents.
type citationKnowledgeService struct {
	interfaces.KnowledgeService
	docs map[string]*types.Knowledge
}

func (s *citationKnowledgeService) GetKnowledgeByIDOnly(_ context.Context, id string) (*types.Knowledge, error) {
	if k, ok := s.docs[id]; ok {
		return k, nil
	}
	return nil, apperrors.NewNotFoundError("knowledge not found")
}

// Citations that are not KB-bound (web search results, whose KnowledgeID is
// their URL) stay; KB-bound ones follow the read check, whether the KB is
// recorded or resolved through the document; a document that is gone hides
// its citation.
func TestHistoryKeepsWebCitationsAndHidesDanglingDocuments(t *testing.T) {
	grants, _ := restrictedReadFixture()
	svc := &messageService{
		kbAccessGrantService: grants,
		knowService: &citationKnowledgeService{docs: map[string]*types.Knowledge{
			"doc-t": {ID: "doc-t", TenantID: 1, KnowledgeBaseID: "kb-t"},
			"doc-r": {ID: "doc-r", TenantID: 1, KnowledgeBaseID: "kb-r"},
		}},
	}
	messages := []*types.Message{{Role: "assistant", KnowledgeReferences: types.References{
		{ID: "c-web", KnowledgeID: "https://example.vn/a", MatchType: types.MatchTypeWebSearch,
			ChunkType: string(types.ChunkTypeWebSearch), KnowledgeSource: "web_search", Content: "web"},
		{ID: "c-web-agent", KnowledgeID: "https://example.com", ChunkType: string(types.ChunkTypeWebSearch),
			Content: "agent web result, URL as knowledge id"},
		{ID: "c-readable", KnowledgeID: "doc-t", Content: "readable KB via document"},
		{ID: "c-unreadable", KnowledgeID: "doc-r", Content: "restricted KB via document"},
		{ID: "c-dangling", KnowledgeID: "doc-gone", Content: "deleted document"},
	}}}
	svc.hideUnreadableReferences(restrictedMemberCtx(), messages)
	ids := []string{}
	for _, ref := range messages[0].KnowledgeReferences {
		ids = append(ids, ref.ID)
	}
	require.Equal(t, []string{"c-web", "c-web-agent", "c-readable"}, ids)
}
