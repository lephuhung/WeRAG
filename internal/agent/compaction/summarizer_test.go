package compaction

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optsRecordingChat records the options of every summarization request.
type optsRecordingChat struct {
	stubChat
	opts []*chat.ChatOptions
}

func (o *optsRecordingChat) ChatStream(
	ctx context.Context, messages []chat.Message, opts *chat.ChatOptions,
) (<-chan types.StreamResponse, error) {
	o.opts = append(o.opts, opts)
	return o.stubChat.ChatStream(ctx, messages, opts)
}

// A reasoning model left to its default thinks through the completion
// budget and stops on the length cap: summaries ask for thinking off.
func TestSummarizationTurnsThinkingOff(t *testing.T) {
	model := &optsRecordingChat{stubChat: stubChat{response: "## Goal\nsummary", finishReason: "stop"}}
	c := New(model, newEstimator(t), testSettings())
	_, err := c.Compact(context.Background(), reactTurn(12), ReasonThreshold)
	require.NoError(t, err)
	require.NotEmpty(t, model.opts)
	for _, o := range model.opts {
		require.NotNil(t, o.Thinking)
		assert.False(t, *o.Thinking)
	}
}

// One summarization request is sized by the summarizer's window, not the
// answering model's, when another model summarizes.
func TestSummarizerInputBudgetUsesTheSummarizerWindow(t *testing.T) {
	est := newEstimator(t)
	answering := testSettings() // 40000 window
	small := answering
	small.SummarizerWindow = 20000
	large := answering
	large.SummarizerWindow = 120000

	base := answering.summarizerInputBudget(est, "", 1024)
	assert.Less(t, small.summarizerInputBudget(est, "", 1024), base)
	assert.Greater(t, large.summarizerInputBudget(est, "", 1024), base)
	// The compaction threshold still follows the answering model's window.
	assert.Equal(t, answering.Threshold(), large.Threshold())
}

// A separate summarizer that fails leaves one attempt to the answering
// model before the summary degrades to the raw archive.
func TestSeparateSummarizerFallsBackToTheAnsweringModel(t *testing.T) {
	answering := &stubChat{response: "## Goal\nfrom the answering model", finishReason: "stop"}
	summarizer := &stubChat{err: errors.New("model offline")}
	c := New(answering, newEstimator(t), testSettings()).WithSummarizer(summarizer, "small-model", 32000)
	require.Equal(t, 32000, c.Settings().SummarizerWindow)

	result, err := c.Compact(context.Background(), reactTurn(12), ReasonThreshold)
	require.NoError(t, err)
	assert.False(t, result.Degraded)
	assert.Contains(t, result.Summary, "from the answering model")
	assert.Equal(t, maxSummarizationAttempts, summarizer.calls)
	assert.Equal(t, 1, answering.calls)
}

// Both failing degrades as before, and the process counts it.
func TestDegradedCompactionsAreCounted(t *testing.T) {
	_, degradedBefore := Stats()
	c := New(&stubChat{err: errors.New("down")}, newEstimator(t), testSettings()).
		WithSummarizer(&stubChat{err: errors.New("also down")}, "small-model", 32000)
	result, err := c.Compact(context.Background(), reactTurn(12), ReasonThreshold)
	require.NoError(t, err)
	assert.True(t, result.Degraded)
	_, degradedAfter := Stats()
	assert.Equal(t, degradedBefore+1, degradedAfter)
}
