package service

import (
	"context"
	"testing"
	"time"

	agenttoken "github.com/Tencent/WeKnora/internal/agent/token"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingHistoryRepo is historyRepo with the optional turn counter.
type countingHistoryRepo struct {
	historyRepo
}

func (r *countingHistoryRepo) CountCompletedTurnsThrough(
	_ context.Context, _ string, at time.Time, id string,
) (int64, error) {
	var n int64
	boundary := &types.Message{ID: id, CreatedAt: at}
	for _, m := range r.rows {
		if m.Role == "assistant" && m.IsCompleted && sortsAtOrBefore(m, boundary) {
			n++
		}
	}
	return n, nil
}

type windowModels struct {
	interfaces.ModelService
	window int
}

func (m windowModels) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	return &types.Model{ID: id, Parameters: types.ModelParameters{ContextWindow: m.window}}, nil
}

type previewGuard struct {
	interfaces.UsageGuard
	preview *types.UsageSessionPreview
	gotMode types.SessionCapMode
}

func (g *previewGuard) PreviewSession(
	_ context.Context, _ uint64, _, _ string, capMode types.SessionCapMode,
) (*types.UsageSessionPreview, error) {
	g.gotMode = capMode
	return g.preview, nil
}

func newContextUsageService(repo interfaces.MessageRepository, guard interfaces.UsageGuard) *SessionContextUsageService {
	return NewSessionContextUsageService(nil, repo, nil, windowModels{window: 250000}, nil, guard, nil)
}

// estimateLoaded prices what LoadAgentHistory returns, the way the engine's
// first compaction check would (no calibration in these fixtures).
func estimateLoaded(t *testing.T, repo interfaces.MessageRepository, seed string) int {
	t.Helper()
	msgs, _, err := LoadAgentHistory(context.Background(), repo, "s1", 250000, false, WithHistorySeed(seed))
	require.NoError(t, err)
	est, err := agenttoken.NewEstimator()
	require.NoError(t, err)
	return est.EstimateMessages(msgs)
}

func TestContextUsageWithoutCheckpoint(t *testing.T) {
	rows := storedTurns(3)
	last := rows[len(rows)-1]
	last.ModelID = "m1"
	last.Usage = &types.TokenUsage{PromptTokens: 1200, CompletionTokens: 300}
	last.AgentSteps = types.AgentSteps{{Iteration: 0}, {Iteration: 1}, {Iteration: 1}}
	repo := &countingHistoryRepo{historyRepo{rows: rows}}

	got, err := newContextUsageService(repo, nil).Usage(context.Background(), &types.Session{ID: "s1"})
	require.NoError(t, err)

	assert.Equal(t, types.ContextUsageModeAgent, got.Mode)
	assert.Equal(t, 250000, got.ModelWindow)
	assert.Equal(t, 250000, got.HistoryBudget, "no plan: the window is the budget")
	assert.Equal(t, estimateLoaded(t, repo, ""), got.HistoryTokens, "the estimate is what a turn loads")
	assert.Positive(t, got.HistoryTokens)
	assert.Zero(t, got.SummaryTokens)
	assert.False(t, got.Compacted)
	assert.Zero(t, got.CompactedTurns)
	require.NotNil(t, got.LastTurn)
	assert.Equal(t, types.SessionContextLastTurn{PromptTokens: 1200, CompletionTokens: 300, Rounds: 2}, *got.LastTurn)
	assert.False(t, got.Enforced)
}

func TestContextUsageFromCheckpoint(t *testing.T) {
	rows := storedTurns(4)
	repo := &countingHistoryRepo{historyRepo{rows: rows, checkpoint: checkpointOn(rows[3], "## Goal\nturns one and two")}}

	got, err := newContextUsageService(repo, nil).Usage(context.Background(), &types.Session{ID: "s1"})
	require.NoError(t, err)

	assert.True(t, got.Compacted)
	assert.False(t, got.Seeded)
	assert.Equal(t, 2, got.CompactedTurns, "the checkpoint on turn 2 covers turns 1 and 2")
	assert.Positive(t, got.SummaryTokens)
	assert.Greater(t, got.HistoryTokens, got.SummaryTokens, "turns 3 and 4 follow the summary")
	assert.Equal(t, estimateLoaded(t, repo, ""), got.HistoryTokens)
	assert.Nil(t, got.LastTurn, "the fixtures record no usage")
}

func TestContextUsageFromSeedOnly(t *testing.T) {
	repo := &countingHistoryRepo{historyRepo{rows: storedTurns(1)}}
	session := &types.Session{ID: "s1", ContextSeed: "## Goal\nthe earlier session"}

	got, err := newContextUsageService(repo, nil).Usage(context.Background(), session)
	require.NoError(t, err)

	assert.True(t, got.Seeded)
	assert.True(t, got.Compacted)
	assert.Zero(t, got.CompactedTurns, "the seed covers no turn of this session")
	assert.Positive(t, got.SummaryTokens)
	assert.Equal(t, estimateLoaded(t, repo, session.ContextSeed), got.HistoryTokens)
}

func TestContextUsageEmptySession(t *testing.T) {
	repo := &countingHistoryRepo{}

	got, err := newContextUsageService(repo, nil).Usage(context.Background(), &types.Session{ID: "s1"})
	require.NoError(t, err)

	assert.Zero(t, got.HistoryTokens)
	assert.Nil(t, got.LastTurn)
	assert.Equal(t, types.DefaultMaxContextTokens, got.ModelWindow, "no turn names a model yet")
}

func TestContextUsageAppliesThePlan(t *testing.T) {
	repo := &countingHistoryRepo{historyRepo{rows: storedTurns(1)}}
	guard := &previewGuard{preview: &types.UsageSessionPreview{
		Enforced: true, PlanName: "Cơ bản",
		Features: types.UsagePlanFeatures{HistoryTokens: 1000, TurnTokenCap: 300000},
		Session:  types.UsageSessionStatus{Turns: 12, MaxTurns: 15, Tokens: 81234, Warning: true},
	}}
	rows := repo.rows
	rows[len(rows)-1].ModelID = "m1"

	got, err := newContextUsageService(repo, guard).Usage(context.Background(), &types.Session{ID: "s1"})
	require.NoError(t, err)

	assert.True(t, got.Enforced)
	assert.Equal(t, "Cơ bản", got.PlanName)
	assert.Equal(t, types.MinHistoryTokens, got.HistoryBudget, "the plan's budget is clamped to the floor")
	assert.Equal(t, int64(300000), got.TurnTokenCap)
	assert.Equal(t, types.SessionContextSessionStatus{Turns: 12, MaxTurns: 15, Tokens: 81234, Warning: true}, got.Session)
	assert.Equal(t, types.SessionCapHard, guard.gotMode)
}
