package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildFallbackMessages_PrependsSystemAndEndsWithUser guards the model
// fallback path: the LLM input must start with a system message (the fallback
// instruction) and end with the user's question, with history in between.
// Previously the fallback dropped the system role entirely, producing a
// [user, assistant, user] input with no system message.
func TestBuildFallbackMessages_PrependsSystemAndEndsWithUser(t *testing.T) {
	cm := &types.ChatManage{}
	cm.Query = "Nội dung tệp này"
	cm.History = []*types.History{
		{Query: "Câu hỏi trước", Answer: "Câu trả lời trước"},
	}

	msgs := buildFallbackMessages(cm, "No content directly matched...\n\nUser question: Nội dung tệp này")

	require.GreaterOrEqual(t, len(msgs), 2)
	assert.Equal(t, "system", msgs[0].Role, "fallback input must start with a system message")
	assert.Contains(t, msgs[0].Content, "No content directly matched")

	// History is replayed between system and the trailing user turn.
	assert.Equal(t, "user", msgs[1].Role)
	assert.Equal(t, "Câu hỏi trước", msgs[1].Content)
	assert.Equal(t, "assistant", msgs[2].Role)
	assert.Equal(t, "Câu trả lời trước", msgs[2].Content)

	last := msgs[len(msgs)-1]
	assert.Equal(t, "user", last.Role, "generation must be prompted by a trailing user turn")
	assert.Equal(t, "Nội dung tệp này", last.Content)
}

// TestBuildFallbackMessages_PrefersRewriteQuery verifies the trailing user turn
// uses the rewritten query when available.
func TestBuildFallbackMessages_PrefersRewriteQuery(t *testing.T) {
	cm := &types.ChatManage{}
	cm.Query = "Nó thế nào"
	cm.RewriteQuery = "Hiệu năng mô hình Hunyuan thế nào"

	msgs := buildFallbackMessages(cm, "fallback instruction")

	last := msgs[len(msgs)-1]
	assert.Equal(t, "user", last.Role)
	assert.Equal(t, "Hiệu năng mô hình Hunyuan thế nào", last.Content)
}

// TestBuildFallbackMessages_EmptyPromptSkipsSystem ensures we don't inject an
// empty system message when there is no fallback instruction to carry.
func TestBuildFallbackMessages_EmptyPromptSkipsSystem(t *testing.T) {
	cm := &types.ChatManage{}
	cm.Query = "hello"

	msgs := buildFallbackMessages(cm, "   ")

	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "hello", msgs[0].Content)
}

// TestBuildFallbackMessages_AttachesImagesToUserTurn confirms images ride on the
// trailing user message only when the chat model supports vision.
func TestBuildFallbackMessages_AttachesImagesToUserTurn(t *testing.T) {
	cm := &types.ChatManage{}
	cm.Query = "Xem hình"
	cm.Images = []string{"https://example.com/a.png"}

	cm.ChatModelSupportsVision = false
	noVision := buildFallbackMessages(cm, "fallback")
	assert.Empty(t, noVision[len(noVision)-1].Images)

	cm.ChatModelSupportsVision = true
	withVision := buildFallbackMessages(cm, "fallback")
	assert.Equal(t, cm.Images, withVision[len(withVision)-1].Images)
}

func TestPrepareFallbackMessagesKeepsHistoricalSourcesNonCitable(t *testing.T) {
	cm := &types.ChatManage{}
	cm.Query = "follow-up"
	cm.History = []*types.History{{
		Query: "previous",
		Answer: `Previous <kb doc="Legacy" chunk_id="legacy-chunk" kb_id="legacy-kb" /> ` +
			`<web url="https://example.com/previous" title="Previous source" />`,
	}}

	messages, refs := prepareFallbackMessages(cm, "legacy fallback prompt")
	require.Contains(t, messages[0].Content, "Source handling protocol")
	require.Equal(t, `Previous <ref id="c1"/> <ref id="w1"/>`, messages[2].Content)
	// Fallback has no current retrieval evidence. Historical handles are kept
	// for navigation but must not authorize citations in the new answer.
	raw := `Answer <ref id="c1"/><ref id="w1"/>`
	require.Equal(t, "Answer ", refs.DecodeOutputText(raw))
	decoder := refs.StreamDecoder()
	streamed := decoder.Feed(`Answer <ref id="c`) +
		decoder.Feed(`1"/><ref id="w`) + decoder.Feed(`1"/>`) + decoder.Flush()
	require.Equal(t, "Answer ", streamed)
}

func TestPrepareFallbackMessagesSuppressesCitationsWhenDisabled(t *testing.T) {
	disabled := false
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{CitationEnabled: &disabled}}
	cm.Query = "follow-up"
	cm.History = []*types.History{{
		Query:  "previous",
		Answer: `Previous <kb doc="Legacy" chunk_id="legacy-chunk" />`,
	}}

	messages, refs := prepareFallbackMessages(cm, "legacy fallback prompt")
	require.Contains(t, messages[0].Content, "Source citations are disabled")
	require.Equal(t, `Previous <ref id="c1"/>`, messages[2].Content)
	require.Equal(t, "answer ", refs.DecodeOutputText(`answer <ref id="c1"/>`))
}
