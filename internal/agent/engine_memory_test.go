package agent

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// TestAgentMemoryLandsInTheSystemPrompt guards the reason memory rides in the
// system prompt at all: buildMessagesWithLLMContext drops system messages that
// arrive through history, so a memory injected as its own message would be
// silently discarded from the second turn onward.
func TestAgentMemoryLandsInTheSystemPrompt(t *testing.T) {
	engine := newTestEngine(t, nil)
	engine.SetMemoryPrompt(types.WrapMemoryForPrompt("Preferences:\n- Trả lời bằng tiếng Việt", ""))

	systemPrompt := engine.buildSystemPrompt(t.Context())
	require.Contains(t, systemPrompt, "Trả lời bằng tiếng Việt")
	require.Contains(t, systemPrompt, "<user_memory>")

	history := []chat.Message{
		{Role: "user", Content: "Câu hỏi lượt trước"},
		{Role: "assistant", Content: "Câu trả lời lượt trước"},
	}
	messages := engine.buildMessagesWithLLMContext(systemPrompt, "Câu hỏi lượt này", "test-session", history, nil)
	require.NotEmpty(t, messages)
	require.Equal(t, "system", messages[0].Role)
	require.Contains(t, messages[0].Content, "Trả lời bằng tiếng Việt")

	// And it appears exactly once, not once per history turn.
	require.Equal(t, 1, strings.Count(messages[0].Content, "<user_memory>"))
	for _, message := range messages[1:] {
		require.NotContains(t, message.Content, "<user_memory>")
	}
}

func TestAgentWithoutMemoryPromptIsUnchanged(t *testing.T) {
	engine := newTestEngine(t, nil)
	require.NotContains(t, engine.buildSystemPrompt(t.Context()), "<user_memory>")
}

func TestAgentMemoryPromptIsAppendedNotSubstituted(t *testing.T) {
	baseline := newTestEngine(t, nil).buildSystemPrompt(t.Context())

	engine := newTestEngine(t, nil)
	engine.SetMemoryPrompt(types.WrapMemoryForPrompt("About the user:\n- đang làm về hình ảnh y tế", ""))
	withMemory := engine.buildSystemPrompt(t.Context())

	require.Greater(t, len(withMemory), len(baseline))
	require.Contains(t, withMemory, "đang làm về hình ảnh y tế")
	// The tool-protocol section must still be there: memory is inserted before
	// it, so an appended block cannot push the protocol out of the prompt.
	require.Contains(t, withMemory, strings.TrimSpace(baseline[len(baseline)-40:]))
}
