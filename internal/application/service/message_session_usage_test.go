package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type seedSessionRepo struct {
	interfaces.SessionRepository
	cleared []string
}

func (r *seedSessionRepo) Get(_ context.Context, tenantID uint64, _ string, id string) (*types.Session, error) {
	return &types.Session{ID: id, TenantID: tenantID}, nil
}

func (r *seedSessionRepo) ClearContextSeed(_ context.Context, _ uint64, id string) error {
	r.cleared = append(r.cleared, id)
	return nil
}

type clearingMessageRepo struct {
	interfaces.MessageRepository
	deleted []string
}

func (r *clearingMessageRepo) DeleteMessagesBySessionID(_ context.Context, id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func (r *clearingMessageRepo) GetKnowledgeIDsBySessionID(context.Context, string) ([]string, error) {
	return nil, nil
}

func (r *clearingMessageRepo) CreateMessage(_ context.Context, m *types.Message) (*types.Message, error) {
	cp := *m
	cp.ID = "m-" + m.Content
	return &cp, nil
}

type recordingMeter struct {
	interfaces.UsageMeter
	questionSessions []string
	reset            []string
}

func (m *recordingMeter) RecordQuestion(_ context.Context, _ uint64, _ string, sessionID string) {
	m.questionSessions = append(m.questionSessions, sessionID)
}

func (m *recordingMeter) ResetSession(_ context.Context, sessionID string) {
	m.reset = append(m.reset, sessionID)
}

func tenantCtx() context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
}

// Clearing a session starts it over: its session counters are reset and a
// continued session stops replaying the summary it was seeded with.
func TestClearSessionMessagesResetsCountersAndSeed(t *testing.T) {
	sessions := &seedSessionRepo{}
	messages := &clearingMessageRepo{}
	meter := &recordingMeter{}
	svc := &messageService{sessionRepo: sessions, messageRepo: messages, usageMeter: meter}

	require.NoError(t, svc.ClearSessionMessages(tenantCtx(), "s1"))
	assert.Equal(t, []string{"s1"}, messages.deleted)
	assert.Equal(t, []string{"s1"}, meter.reset)
	assert.Equal(t, []string{"s1"}, sessions.cleared)
}

// Only the user row that opens a turn counts toward the session's turn cap;
// a steered row still counts as a question for the day/month counters.
func TestSteeredUserRowsDoNotCountAsSessionTurns(t *testing.T) {
	meter := &recordingMeter{}
	svc := &messageService{sessionRepo: &seedSessionRepo{}, messageRepo: &clearingMessageRepo{}, usageMeter: meter}

	_, err := svc.CreateMessage(tenantCtx(), &types.Message{SessionID: "s1", Role: "user", Content: "open"})
	require.NoError(t, err)
	_, err = svc.CreateMessage(types.WithSteeredMessage(tenantCtx()),
		&types.Message{SessionID: "s1", Role: "user", Content: "steer"})
	require.NoError(t, err)

	assert.Equal(t, []string{"s1", ""}, meter.questionSessions,
		"both rows are questions; only the first counts for the session")
}
