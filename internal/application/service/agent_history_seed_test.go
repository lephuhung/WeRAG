package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A session continued from another one starts from that one's summary: the
// seed leads the history as a compaction summary until the session has a
// checkpoint of its own.
func TestLoadAgentHistoryStartsFromTheContextSeed(t *testing.T) {
	repo := &historyRepo{rows: storedTurns(2)}

	got, _, err := LoadAgentHistory(context.Background(), repo, "s2", unlimitedBudget, false,
		WithHistorySeed("## Goal\nwhat the previous session established"))
	require.NoError(t, err)
	require.Len(t, got, 5)
	assert.Equal(t, chat.MessageKindCompactionSummary, got[0].Kind)
	assert.Contains(t, got[0].Content, "what the previous session established")
	assert.Equal(t, []string{"question 1", "answer 1", "question 2", "answer 2"}, contents(got[1:]))
}

// Once the session has its own checkpoint — which folded the seed in as the
// previous summary — the seed is not replayed a second time.
func TestLoadAgentHistoryCheckpointSupersedesTheSeed(t *testing.T) {
	rows := storedTurns(3)
	repo := &historyRepo{rows: rows, checkpoint: checkpointOn(rows[1], "## Goal\nseed plus turn one")}

	got, _, err := LoadAgentHistory(context.Background(), repo, "s2", unlimitedBudget, false,
		WithHistorySeed("## Goal\nthe old seed"))
	require.NoError(t, err)
	assert.Contains(t, got[0].Content, "seed plus turn one")
	for _, m := range got {
		assert.NotContains(t, m.Content, "the old seed")
	}
	// a blank seed is no seed
	got, _, err = LoadAgentHistory(context.Background(), &historyRepo{rows: storedTurns(1)}, "s3",
		unlimitedBudget, false, WithHistorySeed("  "))
	require.NoError(t, err)
	assert.Equal(t, []string{"question 1", "answer 1"}, contents(got))
}
