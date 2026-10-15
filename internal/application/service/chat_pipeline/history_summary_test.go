package chatpipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// summaryMessages serves n stored rounds and, optionally, a context summary
// covering rounds up to coveredThrough.
type summaryMessages struct {
	interfaces.MessageService
	rows           []*types.Message
	summary        string
	coveredThrough time.Time
}

func (m *summaryMessages) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	return m.rows, nil
}

func (m *summaryMessages) LatestContextSummary(context.Context, string) (string, time.Time, error) {
	return m.summary, m.coveredThrough, nil
}

var summaryBase = time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

func storedRounds(n int) []*types.Message {
	var rows []*types.Message
	for i := 1; i <= n; i++ {
		at := summaryBase.Add(time.Duration(i) * time.Minute)
		req := fmt.Sprintf("r%d", i)
		rows = append(rows,
			&types.Message{RequestID: req, Role: "user", Content: fmt.Sprintf("q%d", i), CreatedAt: at},
			&types.Message{RequestID: req, Role: "assistant", Content: fmt.Sprintf("a%d", i), CreatedAt: at.Add(time.Second)},
		)
	}
	return rows
}

func queries(h []*types.History) []string {
	out := make([]string, len(h))
	for i, x := range h {
		out[i] = x.Query
	}
	return out
}

// Without a summary the pipeline keeps its raw rounds as before.
func TestLoadHistoryAndSummaryWithoutSummary(t *testing.T) {
	ms := &summaryMessages{rows: storedRounds(8)}
	h, summary, err := loadHistoryAndSummary(context.Background(), ms, "s", 5, 20)
	require.NoError(t, err)
	assert.Empty(t, summary)
	assert.Equal(t, []string{"q4", "q5", "q6", "q7", "q8"}, queries(h))
}

// With a checkpoint: the summary plus the last 3 raw rounds, none of them a
// round the checkpoint covers.
func TestLoadHistoryAndSummaryUsesSummaryAndThreeRounds(t *testing.T) {
	rows := storedRounds(8)
	ms := &summaryMessages{rows: rows, summary: "## Goal\nearlier", coveredThrough: rows[11].CreatedAt} // a6
	h, summary, err := loadHistoryAndSummary(context.Background(), ms, "s", 5, 20)
	require.NoError(t, err)
	assert.Equal(t, "## Goal\nearlier", summary)
	assert.Equal(t, []string{"q7", "q8"}, queries(h), "round 6 is covered by the summary")

	ms.coveredThrough = rows[3].CreatedAt // a2
	h, _, err = loadHistoryAndSummary(context.Background(), ms, "s", 5, 20)
	require.NoError(t, err)
	assert.Equal(t, []string{"q6", "q7", "q8"}, queries(h))

	// a continued session's seed covers no round of this session
	ms.coveredThrough = time.Time{}
	h, _, err = loadHistoryAndSummary(context.Background(), ms, "s", 5, 20)
	require.NoError(t, err)
	assert.Len(t, h, 3)
}

// The answer prompt carries the summary before the raw rounds, and the
// rewrite prompt sees it too.
func TestSummaryReachesAnswerAndRewritePrompts(t *testing.T) {
	cm := &types.ChatManage{}
	cm.History = []*types.History{{Query: "q8", Answer: "a8"}}
	cm.HistorySummary = "## Goal\nearlier"
	cm.UserContent = "and then?"
	msgs := prepareMessagesWithHistory(cm)
	require.Len(t, msgs, 5)
	assert.Equal(t, "system", msgs[0].Role)
	assert.Equal(t, chat.MessageKindCompactionSummary, msgs[1].Kind)
	assert.Contains(t, msgs[1].Content, "earlier")
	assert.Equal(t, "q8", msgs[2].Content)

	text := formatConversationHistory(cm.HistorySummary, cm.History)
	assert.True(t, strings.Index(text, "earlier") < strings.Index(text, "q8"))
	assert.Contains(t, formatConversationHistory("only a summary", nil), "only a summary")
	assert.Empty(t, formatConversationHistory("", nil))
}
