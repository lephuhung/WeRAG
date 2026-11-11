package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestApplyScopeBrief(t *testing.T) {
	brief := func(_ context.Context, q string) string {
		if q == "phá sản?" {
			return "BRIEF"
		}
		return ""
	}
	msgs := func() []chat.Message {
		return []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "turn content"}}
	}

	engine := newTestEngine(t, &mockChat{}, withMaxIterations(20))
	m := msgs()
	engine.applyScopeBrief(context.Background(), "phá sản?", m)
	assert.Equal(t, "turn content", m[1].Content, "no brief installed")
	assert.Equal(t, wrapUpNudgeRound, engine.wrapUpAfter())

	engine.SetScopeBrief(brief)
	m = msgs()
	engine.applyScopeBrief(context.Background(), "góp vốn?", m)
	assert.Equal(t, "turn content", m[1].Content, "no match")
	assert.Equal(t, wrapUpNudgeRound, engine.wrapUpAfter())

	m = msgs()
	engine.applyScopeBrief(context.Background(), "phá sản?", m)
	assert.Equal(t, "turn content\n\nBRIEF", m[1].Content)
	assert.Equal(t, scopeWrapUpRound, engine.wrapUpAfter())

	// the image path keeps Content plus Images: it gets the brief
	img := newTestEngine(t, &mockChat{}, withMaxIterations(20))
	img.SetScopeBrief(brief)
	m = []chat.Message{{Role: "user", Content: "turn content", Images: []string{"https://x/img.png"}}}
	img.applyScopeBrief(context.Background(), "phá sản?", m)
	assert.Equal(t, "turn content\n\nBRIEF", m[0].Content)
	assert.Equal(t, []string{"https://x/img.png"}, m[0].Images)

	// a multi-part user message is left alone
	other := newTestEngine(t, &mockChat{}, withMaxIterations(20))
	other.SetScopeBrief(brief)
	m = []chat.Message{{Role: "user", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "x"}}}}
	other.applyScopeBrief(context.Background(), "phá sản?", m)
	assert.Empty(t, m[0].Content)
	assert.Equal(t, wrapUpNudgeRound, other.wrapUpAfter())
}

// A scope-briefed turn is nudged after scopeWrapUpRound rounds, with the
// round count in the text.
func TestScopeBriefMovesTheWrapUpNudge(t *testing.T) {
	engine := newTestEngine(t, &mockChat{}, withMaxIterations(20))
	engine.wrapUpRound = scopeWrapUpRound
	msgs := []chat.Message{{Role: "user", Content: "q"}, {Role: "tool", Content: "r"}}
	early := &types.AgentState{CurrentRound: scopeWrapUpRound - 1}
	assert.False(t, engine.appendWrapUpNudge(context.Background(), early, &msgs))
	state := &types.AgentState{CurrentRound: scopeWrapUpRound}
	require.True(t, engine.appendWrapUpNudge(context.Background(), state, &msgs))
	require.Len(t, msgs, 3)
	assert.Equal(t, fmt.Sprintf(wrapUpNudgeFormat, scopeWrapUpRound), msgs[2].Content)
	assert.Contains(t, msgs[2].Content, "Bạn đã dùng 3 vòng")

	// a turn capped at that many rounds is not nudged
	short := newTestEngine(t, &mockChat{}, withMaxIterations(scopeWrapUpRound))
	short.wrapUpRound = scopeWrapUpRound
	msgs = []chat.Message{{Role: "user", Content: "q"}, {Role: "tool", Content: "r"}}
	assert.False(t, short.appendWrapUpNudge(context.Background(), state, &msgs))
}
