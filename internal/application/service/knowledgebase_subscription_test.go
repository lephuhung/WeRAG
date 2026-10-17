package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type fakeSubscriptionRepo struct {
	interfaces.KBSubscriptionRepository
	rows map[string]*types.KBSubscription // key kb|tenant|user
}

func subKey(kbID string, tenantID uint64, userID string) string {
	return kbID + "|" + itoaU(tenantID) + "|" + userID
}

func itoaU(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

func (r *fakeSubscriptionRepo) Subscribe(_ context.Context, sub *types.KBSubscription) error {
	r.rows[subKey(sub.KBID, sub.TenantID, sub.UserID)] = sub
	return nil
}

func (r *fakeSubscriptionRepo) Unsubscribe(_ context.Context, kbID string, tenantID uint64, userID string) error {
	delete(r.rows, subKey(kbID, tenantID, userID))
	return nil
}

func (r *fakeSubscriptionRepo) ListForReader(_ context.Context, tenantID uint64, userID string) ([]*types.KBSubscription, error) {
	var out []*types.KBSubscription
	for _, row := range r.rows {
		if row.TenantID == tenantID && (row.UserID == "" || row.UserID == userID) {
			out = append(out, row)
		}
	}
	return out, nil
}

func memberCtx(tenantID uint64, userID string, role types.TenantRole) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	return types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: userID, Role: role})
}

func publishedFixture() (*knowledgeBaseService, *fakeKBRepo, *fakeSubscriptionRepo) {
	repo := newFakeKBRepo()
	published := &types.KnowledgeBase{ID: "kb-tinh", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityPublished}
	private := &types.KnowledgeBase{ID: "kb-rieng", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant}
	repo.rows[published.ID] = published
	repo.rows[private.ID] = private
	repo.byIDs = map[string]*types.KnowledgeBase{published.ID: published, private.ID: private}
	subs := &fakeSubscriptionRepo{rows: map[string]*types.KBSubscription{}}
	return &knowledgeBaseService{repo: repo, kgRepo: &catalogKGRepo{}, subscriptionRepo: subs}, repo, subs
}

func requireHTTPCode(t *testing.T, err error, httpCode int) {
	t.Helper()
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok, "want app error, got %v", err)
	require.Equal(t, httpCode, appErr.HTTPCode)
}

func TestSubscribePublishedKB(t *testing.T) {
	svc, _, subs := publishedFixture()
	member := memberCtx(2, "canbo-sob", types.TenantRoleMember)
	admin := memberCtx(2, "admin-sob", types.TenantRoleAdmin)

	require.NoError(t, svc.SubscribeKnowledgeBase(member, "kb-tinh", false))
	require.Contains(t, subs.rows, subKey("kb-tinh", 2, "canbo-sob"))

	requireHTTPCode(t, svc.SubscribeKnowledgeBase(member, "kb-tinh", true), 403)
	require.NoError(t, svc.SubscribeKnowledgeBase(admin, "kb-tinh", true))
	require.Contains(t, subs.rows, subKey("kb-tinh", 2, ""))

	requireHTTPCode(t, svc.SubscribeKnowledgeBase(member, "kb-rieng", false), 400)
	requireHTTPCode(t, svc.SubscribeKnowledgeBase(memberCtx(1, "u1", types.TenantRoleMember), "kb-tinh", false), 400)
	requireHTTPCode(t, svc.SubscribeKnowledgeBase(memberCtx(2, "system-2", types.TenantRoleMember), "kb-tinh", false), 403)

	require.NoError(t, svc.UnsubscribeKnowledgeBase(member, "kb-tinh", false))
	require.NotContains(t, subs.rows, subKey("kb-tinh", 2, "canbo-sob"))
}

func TestListKnowledgeBasesIncludesOnlySubscribedPublished(t *testing.T) {
	svc, repo, _ := publishedFixture()
	reader := memberCtx(2, "canbo-sob", types.TenantRoleMember)
	// The readable set holds the published KB of tenant 1 (every human may
	// view it); the subscription only decides whether it is listed.
	published := *repo.rows["kb-tinh"]
	published.AccessSource = "published"
	repo.readable = []*types.KnowledgeBase{&published}

	listed, err := svc.ListKnowledgeBases(reader)
	require.NoError(t, err)
	require.Empty(t, listed, "an unsubscribed published KB stays out of the default scope")

	require.NoError(t, svc.SubscribeKnowledgeBase(reader, "kb-tinh", false))
	listed, err = svc.ListKnowledgeBases(reader)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "kb-tinh", listed[0].ID)
	require.True(t, listed[0].SubscribedByMe)

	// Withdrawn: the KB is no longer readable, so the subscription alone
	// never lists it.
	repo.readable = nil
	listed, err = svc.ListKnowledgeBases(reader)
	require.NoError(t, err)
	require.Empty(t, listed, "a withdrawn KB leaves the scope even with a subscription")
}

func TestPublishTenantKBByOwnerAdmin(t *testing.T) {
	svc, repo, _ := publishedFixture()
	owner := memberCtx(1, "admin-tinh", types.TenantRoleAdmin)

	kb, err := svc.SetKnowledgeBaseVisibility(owner, "kb-rieng", types.KBVisibilityPublished, 0)
	require.NoError(t, err)
	require.Equal(t, types.KBVisibilityPublished, kb.Visibility)
	require.Equal(t, uint64(1), kb.OwnerTenantID, "publishing keeps the owner")

	kb, err = svc.SetKnowledgeBaseVisibility(owner, "kb-rieng", types.KBVisibilityTenant, 0)
	require.NoError(t, err)
	require.Equal(t, types.KBVisibilityTenant, kb.Visibility)

	_, err = svc.SetKnowledgeBaseVisibility(memberCtx(1, "m1", types.TenantRoleMember), "kb-rieng", types.KBVisibilityPublished, 0)
	requireHTTPCode(t, err, 403)
	_, err = svc.SetKnowledgeBaseVisibility(memberCtx(2, "admin-sob", types.TenantRoleAdmin), "kb-rieng", types.KBVisibilityPublished, 0)
	requireHTTPCode(t, err, 403)
	_, err = svc.SetKnowledgeBaseVisibility(owner, "kb-rieng", types.KBVisibilityPublished, 2)
	requireHTTPCode(t, err, 400)
	require.Equal(t, types.KBVisibilityTenant, repo.rows["kb-rieng"].Visibility)

	super := context.WithValue(memberCtx(3, "super", types.TenantRoleMember), types.SystemAdminContextKey, true)
	_, err = svc.SetKnowledgeBaseVisibility(super, "kb-tinh", types.KBVisibilityPublic, 0)
	requireHTTPCode(t, err, 400)
}
