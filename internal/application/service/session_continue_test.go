package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type createOnlySessionRepo struct {
	interfaces.SessionRepository
	created []*types.Session
}

func (r *createOnlySessionRepo) Create(_ context.Context, s *types.Session) (*types.Session, error) {
	cp := *s
	cp.ID = "new-session"
	r.created = append(r.created, &cp)
	return &cp, nil
}

// The continued session starts from the old one's checkpoint plus the turns
// the checkpoint does not cover, and keeps owner and input-bar state.
func TestContinueSessionSeedsWithCheckpointAndRecentTurns(t *testing.T) {
	rows := storedTurns(4)
	messages := &historyRepo{rows: rows, checkpoint: checkpointOn(rows[3], "## Goal\nturns one and two")}
	sessions := &createOnlySessionRepo{}
	svc := &sessionService{sessionRepo: sessions, messageRepo: messages}
	state := &types.SessionLastRequestState{AgentID: "agent-1"}

	got, err := svc.ContinueSession(context.Background(), &types.Session{
		ID: "old", TenantID: 7, UserID: "u1", Title: "Hỏi về luật", LastRequestState: state,
	})
	require.NoError(t, err)
	assert.Equal(t, "new-session", got.ID)
	assert.Equal(t, uint64(7), got.TenantID)
	assert.Equal(t, "u1", got.UserID)
	assert.Equal(t, "Hỏi về luật (tiếp)", got.Title)
	assert.Same(t, state, got.LastRequestState)

	seed := got.ContextSeed
	assert.True(t, strings.HasPrefix(seed, "## Goal\nturns one and two"), seed)
	assert.Contains(t, seed, "User: question 3\nAssistant: answer 3")
	assert.Contains(t, seed, "User: question 4\nAssistant: answer 4")
	assert.NotContains(t, seed, "question 2", "turns the checkpoint covers are not quoted again")
	assert.Less(t, strings.Index(seed, "question 3"), strings.Index(seed, "question 4"))
}

// A session that never compacted is continued from excerpts of its newest
// turns, and a long session's seed stays bounded.
func TestContinueSessionWithoutCheckpointQuotesNewestTurns(t *testing.T) {
	messages := &historyRepo{rows: storedTurnsOfSize(20, 3000)}
	svc := &sessionService{sessionRepo: &createOnlySessionRepo{}, messageRepo: messages}

	got, err := svc.ContinueSession(context.Background(), &types.Session{ID: "old", TenantID: 7})
	require.NoError(t, err)
	assert.Contains(t, got.ContextSeed, "question 20")
	assert.NotContains(t, got.ContextSeed, "question 12\n", "only the newest turns are quoted")
	assert.LessOrEqual(t, len([]rune(got.ContextSeed)), continuationSeedRunes+10)
	assert.Empty(t, got.Title)
}

// A sub-chat names its document chat's root, also when opened from another
// sub-chat; a plain continuation names none. The title is marked once.
func TestOpenSubChatNamesTheRootChat(t *testing.T) {
	rows := storedTurns(2)
	svc := &sessionService{sessionRepo: &createOnlySessionRepo{}, messageRepo: &historyRepo{rows: rows}}
	ctx := context.Background()

	got, err := svc.OpenSubChat(ctx, &types.Session{ID: "root", TenantID: 7, UserID: "u1", Title: "Soạn công văn"})
	require.NoError(t, err)
	assert.Equal(t, "root", got.ChatRootID)
	assert.Equal(t, "Soạn công văn (tiếp)", got.Title)
	assert.NotEmpty(t, got.ContextSeed, "it starts from the chat it was opened from")

	got, err = svc.OpenSubChat(ctx, &types.Session{ID: "sub-1", ChatRootID: "root", TenantID: 7, UserID: "u1", Title: "Soạn công văn (tiếp)"})
	require.NoError(t, err)
	assert.Equal(t, "root", got.ChatRootID)
	assert.Equal(t, "Soạn công văn (tiếp)", got.Title)

	got, err = svc.ContinueSession(ctx, &types.Session{ID: "plain", TenantID: 7, UserID: "u1"})
	require.NoError(t, err)
	assert.Empty(t, got.ChatRootID)
}
