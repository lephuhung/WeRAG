package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type feedbackSessionRepo struct {
	interfaces.SessionRepository
	err error
}

func (r *feedbackSessionRepo) Get(context.Context, uint64, string, string) (*types.Session, error) {
	return &types.Session{}, r.err
}

type feedbackMessageRepo struct {
	interfaces.MessageRepository
	msg     *types.Message
	updates int
	vote    int
	note    string
	at      *time.Time
}

func (r *feedbackMessageRepo) GetMessage(context.Context, string, string) (*types.Message, error) {
	return r.msg, nil
}

func (r *feedbackMessageRepo) UpdateMessageFeedback(
	_ context.Context, _, _ string, feedback int, comment string, at *time.Time,
) error {
	r.updates++
	r.vote, r.note, r.at = feedback, comment, at
	return nil
}

func feedbackCtx() context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, uint64(10000))
}

func TestSetMessageFeedbackStoresVote(t *testing.T) {
	repo := &feedbackMessageRepo{msg: &types.Message{ID: "m1", Role: "assistant", IsCompleted: true}}
	s := &messageService{messageRepo: repo, sessionRepo: &feedbackSessionRepo{}}

	got, err := s.SetMessageFeedback(feedbackCtx(), "s1", "m1", types.MessageFeedbackDown, "sai")
	require.NoError(t, err)
	require.Equal(t, 1, repo.updates)
	require.Equal(t, types.MessageFeedbackDown, repo.vote)
	require.Equal(t, "sai", repo.note)
	require.NotNil(t, repo.at)
	require.Equal(t, types.MessageFeedbackDown, got.Feedback)
}

func TestSetMessageFeedbackClearDropsNoteAndTime(t *testing.T) {
	repo := &feedbackMessageRepo{msg: &types.Message{ID: "m1", Role: "assistant", IsCompleted: true}}
	s := &messageService{messageRepo: repo, sessionRepo: &feedbackSessionRepo{}}

	_, err := s.SetMessageFeedback(feedbackCtx(), "s1", "m1", 0, "ignored")
	require.NoError(t, err)
	require.Equal(t, 0, repo.vote)
	require.Empty(t, repo.note)
	require.Nil(t, repo.at)
}

func TestSetMessageFeedbackOnlyOnFinishedAnswers(t *testing.T) {
	for name, msg := range map[string]*types.Message{
		"user message":   {ID: "m1", Role: "user", IsCompleted: true},
		"still incoming": {ID: "m1", Role: "assistant", IsCompleted: false},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &feedbackMessageRepo{msg: msg}
			s := &messageService{messageRepo: repo, sessionRepo: &feedbackSessionRepo{}}
			_, err := s.SetMessageFeedback(feedbackCtx(), "s1", "m1", types.MessageFeedbackUp, "")
			require.ErrorIs(t, err, apperrors.ErrFeedbackNotAllowed)
			require.Zero(t, repo.updates)
		})
	}
}

func TestSetMessageFeedbackRequiresVisibleSession(t *testing.T) {
	repo := &feedbackMessageRepo{msg: &types.Message{ID: "m1", Role: "assistant", IsCompleted: true}}
	s := &messageService{messageRepo: repo, sessionRepo: &feedbackSessionRepo{err: apperrors.ErrSessionNotFound}}

	_, err := s.SetMessageFeedback(feedbackCtx(), "s1", "m1", types.MessageFeedbackUp, "")
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)
	require.Zero(t, repo.updates)
}

func TestForkedCopiesDoNotInheritFeedback(t *testing.T) {
	at := time.Now()
	copies := copyMessagesInto("fork", []*types.Message{
		{ID: "m1", Role: "assistant", Feedback: types.MessageFeedbackUp, FeedbackComment: "tốt", FeedbackAt: &at},
	})
	require.Len(t, copies, 1)
	require.Zero(t, copies[0].Feedback)
	require.Empty(t, copies[0].FeedbackComment)
	require.Nil(t, copies[0].FeedbackAt)
}
