package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/compaction"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

// storedTurns is n stored question/answer turns of roughly answerRepeat
// words of answer each, tagged with their turn IDs like LoadAgentHistory does.
func storedTurns(n, answerRepeat int) []chat.Message {
	var out []chat.Message
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("turn-%02d", i)
		out = append(out,
			chat.Message{Role: "user", Content: fmt.Sprintf("question %d", i), TurnID: id},
			chat.Message{Role: "assistant", Content: strings.Repeat(fmt.Sprintf("answer %d text ", i), answerRepeat), TurnID: id},
		)
	}
	return out
}

func collectCompactions(bus *event.EventBus) (*[]event.ContextCompactedData, *sync.Mutex) {
	var (
		mu  sync.Mutex
		out []event.ContextCompactedData
	)
	bus.On(event.EventContextCompacted, func(_ context.Context, evt event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		out = append(out, evt.Data.(event.ContextCompactedData))
		return nil
	})
	return &out, &mu
}

func TestHistoryKeepRecentTokens(t *testing.T) {
	require.Equal(t, 8192, HistoryKeepRecentTokens(32768))
	require.Equal(t, 16384, HistoryKeepRecentTokens(65536))
	require.Equal(t, minHistoryKeepRecentTokens, HistoryKeepRecentTokens(1000))
}

// History under the plan's budget is left alone: no summarization call.
func TestCompactHistoryToBudgetLeavesSmallHistoryAlone(t *testing.T) {
	llm := &summarizerChat{}
	engine := newTestEngine(t, llm, withMaxContextTokens(250000), withMaxCompletionTokens(4000))
	history := storedTurns(3, 50)
	got := engine.CompactHistoryToBudget(context.Background(), history, 32768)
	require.Equal(t, history, got)
	require.Zero(t, llm.calls)
}

// History over the budget — far below the window, so mid-turn compaction
// would never fire — is summarized before the turn: the result fits the
// budget, the summary is checkpointed on a stored turn boundary and the UI
// gets the usual context_compacted event. Nothing is dropped: everything
// before the retained tail went into the summarizer.
func TestCompactHistoryToBudgetSummarizesOverflowAndCheckpoints(t *testing.T) {
	llm := &summarizerChat{}
	engine := newTestEngine(t, llm, withMaxContextTokens(250000), withMaxCompletionTokens(4000))
	sink := &recordingCheckpointSink{}
	engine.SetContextCheckpointSink(sink)
	events, mu := collectCompactions(engine.eventBus)

	history := storedTurns(40, 400)
	const budget = 32768
	before := engine.tokenEstimator.EstimateMessages(history)
	require.Greater(t, before, budget, "fixture must exceed the budget")
	require.False(t, engine.compactor.Settings().ShouldCompact(before),
		"fixture must stay under the window threshold: only the budget asks for compaction")

	got := engine.CompactHistoryToBudget(context.Background(), history, budget)
	after := engine.tokenEstimator.EstimateMessages(got)
	require.LessOrEqual(t, after, budget)
	require.Equal(t, chat.MessageKindCompactionSummary, got[0].Kind, "the summary leads the history")
	require.Equal(t, "turn-39", got[len(got)-1].TurnID, "the newest turn survives verbatim")
	require.Positive(t, llm.calls)

	require.Len(t, sink.turnIDs, 1)
	require.Equal(t, got[1].TurnID, nextTurnAfter(history, sink.turnIDs[0]),
		"the checkpoint covers exactly what the summary replaced")

	mu.Lock()
	require.Len(t, *events, 1)
	require.Equal(t, string(compaction.ReasonHistoryBudget), (*events)[0].Reason)
	mu.Unlock()

	// The compacted history is under budget, so the next turn does not
	// compact again on the same content.
	calls := llm.calls
	again := engine.CompactHistoryToBudget(context.Background(), got, budget)
	require.Equal(t, got, again)
	require.Equal(t, calls, llm.calls)
}

// A single stored turn larger than the whole budget: the turn is split, its
// head summarized, and the tool outputs of the retained tail trimmed so the
// turn starts within budget — once, without looping on compaction.
func TestCompactHistoryToBudgetHandlesOneTurnLargerThanBudget(t *testing.T) {
	llm := &summarizerChat{}
	engine := newTestEngine(t, llm, withMaxContextTokens(250000), withMaxCompletionTokens(4000))
	sink := &recordingCheckpointSink{}
	engine.SetContextCheckpointSink(sink)

	history := []chat.Message{{Role: "user", Content: "research everything", TurnID: "big"}}
	body := strings.Repeat("retrieved passage text ", 2500) // one result near the keep-recent size
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("call-%d", i)
		history = append(history,
			chat.Message{Role: "assistant", TurnID: "big", ToolCalls: []chat.ToolCall{{
				ID: id, Type: "function", Function: chat.FunctionCall{Name: "search_knowledge", Arguments: `{}`},
			}}},
			chat.Message{Role: "tool", TurnID: "big", Name: "search_knowledge", ToolCallID: id, Content: body},
		)
	}
	// The turn answered through its tools: it ends on one result larger
	// than the whole budget, which the cut point cannot split.
	history = append(history,
		chat.Message{Role: "assistant", TurnID: "big", ToolCalls: []chat.ToolCall{{
			ID: "call-last", Type: "function", Function: chat.FunctionCall{Name: "read_document", Arguments: `{}`},
		}}},
		chat.Message{Role: "tool", TurnID: "big", Name: "read_document", ToolCallID: "call-last",
			Content: strings.Repeat("full document text ", 12000)},
	)

	const budget = 32768
	before := engine.tokenEstimator.EstimateMessages(history)
	require.Greater(t, before, 2*budget)

	got := engine.CompactHistoryToBudget(context.Background(), history, budget)
	require.LessOrEqual(t, engine.tokenEstimator.EstimateMessages(got), budget)
	require.LessOrEqual(t, llm.calls, 2, "one history summary and at most one turn-prefix summary")
	require.Empty(t, sink.turnIDs, "a cut inside the only stored turn has no boundary to checkpoint")
	// tool results stay paired with their calls
	for i, m := range got {
		if m.Role == "tool" {
			require.NotEmpty(t, got[i-1].ToolCalls, "tool result %d lost its call", i)
		}
	}
}

// nextTurnAfter is the turn that follows turnID in history.
func nextTurnAfter(history []chat.Message, turnID string) string {
	seen := false
	for _, m := range history {
		if seen && m.TurnID != turnID {
			return m.TurnID
		}
		if m.TurnID == turnID {
			seen = true
		}
	}
	return ""
}
