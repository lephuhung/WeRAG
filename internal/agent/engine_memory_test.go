package agent

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// TestAgentMemoryLandsInTheTurnNotTheSystemPrompt guards where the long-term
// memory envelope rides: in the per-turn user turn, exactly once per turn.
//
// It used to live in the system prompt, for a good reason — memory injected as
// its own message would be dropped from the second turn onward, because
// buildMessagesWithLLMContext discards system messages that arrive through
// history. Putting it in the current user turn keeps it on every turn (the turn
// is rebuilt each time) without paying for it in the cached prefix: because it
// is recalled by this question, a system-prompt block would change with every
// turn and invalidate the provider's prompt cache for the whole history below
// it (see docs/llm-cache-and-cost.md).
func TestAgentMemoryLandsInTheTurnNotTheSystemPrompt(t *testing.T) {
	engine := newTestEngine(t, nil)
	engine.SetMemoryPrompt(types.WrapMemoryForPrompt("Preferences:\n- Trả lời bằng tiếng Việt", ""))

	systemPrompt := engine.buildSystemPrompt(t.Context())
	require.NotContains(t, systemPrompt, "<user_memory>", "memory must not sit in the cacheable prefix")
	require.NotContains(t, systemPrompt, "Trả lời bằng tiếng Việt")

	history := []chat.Message{
		{Role: "user", Content: "Câu hỏi lượt trước"},
		{Role: "assistant", Content: "Câu trả lời lượt trước"},
	}
	messages := engine.buildMessagesWithLLMContext(systemPrompt, "Câu hỏi lượt này", "test-session", history, nil)
	require.NotEmpty(t, messages)
	require.Equal(t, "system", messages[0].Role)

	// The memory is in the turn the model is answering, and only there: not in
	// the system message and not repeated per history turn.
	last := messages[len(messages)-1]
	require.Equal(t, "user", last.Role)
	require.Contains(t, last.Content, "<user_memory>")
	require.Contains(t, last.Content, "Trả lời bằng tiếng Việt")
	require.Equal(t, 1, strings.Count(last.Content, "<user_memory>"))
	require.Less(t, strings.Index(last.Content, "<user_memory>"), strings.Index(last.Content, "Câu hỏi lượt này"),
		"memory is background for the question that follows it")
	for _, message := range messages[:len(messages)-1] {
		require.NotContains(t, message.Content, "<user_memory>")
	}
}

// The whole point of the move: two turns of one session, with different
// queries AND different recalled memory, must produce a byte-identical system
// prompt. That string is the head of the provider's cached prefix.
func TestSystemPromptIsStableAcrossTurnsWithDifferentMemory(t *testing.T) {
	engine := newTestEngine(t, nil)
	const question = "Câu hỏi lượt này"

	engine.SetMemoryPrompt(types.WrapMemoryForPrompt("Preferences:\n- Trả lời bằng tiếng Việt", ""))
	first := engine.buildSystemPrompt(t.Context())
	firstMessages := engine.buildMessagesWithLLMContext(first, question, "s", nil, nil)

	engine.SetMemoryPrompt(types.WrapMemoryForPrompt(
		"Preferences:\n- Trả lời bằng tiếng Việt", "Relevant:\n- người dùng đang xem hồ sơ đất đai"))
	second := engine.buildSystemPrompt(t.Context())

	require.Equal(t, first, second, "a per-turn memory recall must not change the system prompt")
	require.NotEqual(t, firstMessages[len(firstMessages)-1].Content,
		engine.RenderUserTurnContent("s", question), "the turn itself does carry the new memory")
	require.Contains(t, engine.RenderUserTurnContent("s", question), "hồ sơ đất đai")
}

func TestAgentWithoutMemoryPromptIsUnchanged(t *testing.T) {
	engine := newTestEngine(t, nil)
	require.NotContains(t, engine.buildSystemPrompt(t.Context()), "<user_memory>")
	require.NotContains(t, engine.RenderUserTurnContent("s", "hello"), "<user_memory>")
}

// A memory block must never displace the turn's own content: the query stays,
// and so does the tool protocol at the end of the system prompt.
func TestAgentMemoryPromptDoesNotDisplaceTheTurn(t *testing.T) {
	engine := newTestEngine(t, nil)
	baseline := engine.RenderUserTurnContent("s", "câu hỏi gốc")

	engine.SetMemoryPrompt(types.WrapMemoryForPrompt("About the user:\n- đang làm về hình ảnh y tế", ""))
	withMemory := engine.RenderUserTurnContent("s", "câu hỏi gốc")

	require.Greater(t, len(withMemory), len(baseline))
	require.Contains(t, withMemory, "đang làm về hình ảnh y tế")
	require.Contains(t, withMemory, "câu hỏi gốc")
	require.True(t, strings.HasSuffix(strings.TrimSpace(withMemory), "câu hỏi gốc"),
		"the question is the last thing the model reads")

	// The system prompt keeps its protocol section (the section that used to
	// follow memory) and never grows a memory block.
	systemPrompt := engine.buildSystemPrompt(t.Context())
	protocolOnly := newTestEngine(t, nil).buildSystemPrompt(t.Context())
	require.Equal(t, protocolOnly, systemPrompt)
}
