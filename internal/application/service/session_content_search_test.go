package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type contentSearchRepo struct {
	interfaces.SessionRepository
	got  *types.SessionContentSearchQuery
	hits []*types.SessionContentHit
}

func (r *contentSearchRepo) SearchContent(_ context.Context, q *types.SessionContentSearchQuery) ([]*types.SessionContentHit, bool, error) {
	r.got = q
	return r.hits, true, nil
}

// The search runs in the caller's own scope (tenant and owner from the
// context, never from the request), validates its input and sends a
// snippet instead of the message.
func TestSearchSessionContentScopeAndSnippet(t *testing.T) {
	repo := &contentSearchRepo{hits: []*types.SessionContentHit{{
		SessionID: "s1", Content: "Hỏi về ĐIỀU 5 của luật", MessageID: "m1",
	}}}
	svc := &sessionService{sessionRepo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "alice")

	// a decomposed (NFD) query is composed like stored text
	hits, more, err := svc.SearchSessionContent(ctx, "  \u0111i\u00ea\u0300u 5 ", "", 2, 10)
	require.NoError(t, err)
	require.True(t, more)
	require.Equal(t, &types.SessionContentSearchQuery{
		TenantID: 7, UserID: "alice", Query: "điều 5", Offset: 10, Limit: 10,
	}, repo.got)
	require.Len(t, hits, 1)
	require.Empty(t, hits[0].Content, "the message itself is not sent")
	require.Equal(t, types.Snippet{Before: "Hỏi về ", Match: "ĐIỀU 5", After: " của luật"}, hits[0].Snippet)

	_, _, err = svc.SearchSessionContent(ctx, "điều", "WORD", 1, 500)
	require.NoError(t, err)
	require.Equal(t, types.SessionSourceWord, repo.got.Source)
	require.Equal(t, 20, repo.got.Limit, "page size is capped")

	for _, bad := range []struct{ q, source string }{{"a", ""}, {"điều", "api"}, {"điều", "telegram"}} {
		_, _, err = svc.SearchSessionContent(ctx, bad.q, bad.source, 1, 10)
		require.Error(t, err, bad)
	}
}

// Without an owner (no user in the context) nothing is searched.
func TestSearchSessionContentWithoutOwnerFindsNothing(t *testing.T) {
	repo := &contentSearchRepo{hits: []*types.SessionContentHit{{SessionID: "s1"}}}
	svc := &sessionService{sessionRepo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	hits, more, err := svc.SearchSessionContent(ctx, "điều 5", "", 1, 10)
	require.NoError(t, err)
	require.False(t, more)
	require.Empty(t, hits)
	require.Nil(t, repo.got, "the repository is not asked")
}

type slowSearchRepo struct{ interfaces.SessionRepository }

func (slowSearchRepo) SearchContent(ctx context.Context, _ *types.SessionContentSearchQuery) ([]*types.SessionContentHit, bool, error) {
	<-ctx.Done()
	return nil, false, ctx.Err()
}

// A search past its deadline answers 504 with a hint to narrow the query.
func TestSearchSessionContentTimeoutIs504(t *testing.T) {
	svc := &sessionService{sessionRepo: slowSearchRepo{}}
	parent, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ctx := context.WithValue(parent, types.TenantIDContextKey, uint64(7))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "alice")
	_, _, err := svc.SearchSessionContent(ctx, "điều 5", "", 1, 10)
	var appErr *apperrors.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusGatewayTimeout, appErr.HTTPCode)
}
