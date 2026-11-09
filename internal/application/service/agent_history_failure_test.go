package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// A turn the model failed to answer never reaches the next turn's agent
// history: neither its empty (or broken-off) answer nor its question.
func TestCompleteHistoryTurnsSkipsFailedTurns(t *testing.T) {
	at := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	failed := &types.Message{ID: "a2", RequestID: "r2", Role: "assistant", IsCompleted: true, CreatedAt: at.Add(3 * time.Second)}
	failed.ExecutionContext.TurnFailure = types.TurnFailureModelUnavailable
	rows := []*types.Message{
		{ID: "u1", RequestID: "r1", Role: "user", IsCompleted: true, CreatedAt: at},
		{ID: "a1", RequestID: "r1", Role: "assistant", IsCompleted: true, Content: "trả lời", CreatedAt: at.Add(time.Second)},
		{ID: "u2", RequestID: "r2", Role: "user", IsCompleted: true, CreatedAt: at.Add(2 * time.Second)},
		failed,
	}
	turns := completeHistoryTurns(rows, true)
	require.Len(t, turns, 1)
	require.Equal(t, "a1", turns[0].assistant.ID)

	// the slim copy the replay groups keeps the mark
	replay := &historyReplay{full: map[string]*types.Message{}, withContent: map[string]bool{}}
	slim := make([]*types.Message, 0, len(rows))
	for _, r := range rows {
		slim = append(slim, replay.track(r))
	}
	require.Len(t, completeHistoryTurns(slim, true), 1)
}

func TestMessageAfterFindExposesFailure(t *testing.T) {
	m := &types.Message{}
	m.ExecutionContext.TurnFailure = types.TurnFailureModelInterrupted
	require.NoError(t, m.AfterFind(nil))
	require.Equal(t, types.TurnFailureModelInterrupted, m.Failure)
	require.True(t, m.TurnFailed())
}
