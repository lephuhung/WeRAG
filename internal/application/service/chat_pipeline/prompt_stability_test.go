package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// DeepSeek serves a cached prefix only when the whole prefix matches a
// persisted cache unit, and it bills a cached input token at 1/50 to 1/100 of
// an uncached one. That makes the shape of the request a cost decision, not a
// style one: everything that changes per turn has to sit AFTER the history, in
// the user turn, or every turn pays for the whole conversation again.
//
// This is the guard for the two blocks that used to break it in this path —
// the long-term memory envelope and the legal catalogue, both recalled for the
// current query and both previously appended to the system prompt.
func TestMessagesKeepPerTurnBlocksOutOfTheSystemPrompt(t *testing.T) {
	build := func(query, memory, catalog string) []chat.Message {
		chatManage := &types.ChatManage{}
		chatManage.Query = query
		chatManage.UserContent = query
		chatManage.SummaryConfig.Prompt = "Bạn là một trợ lý."
		chatManage.MemoryPrompt = memory
		chatManage.LegalCatalogPrompt = catalog
		chatManage.History = []*types.History{
			{Query: "Câu hỏi cũ", Answer: "Câu trả lời cũ"},
		}
		return prepareMessagesWithHistory(chatManage)
	}

	first := build("Câu hỏi lượt này",
		types.WrapMemoryForPrompt("Preferences:\n- Trả lời bằng tiếng Việt", ""), "")
	second := build("Một câu hỏi khác hoàn toàn",
		types.WrapMemoryForPrompt("Preferences:\n- Trả lời bằng tiếng Việt",
			"Relevant:\n- người dùng đang xem hồ sơ đất đai"),
		"\n\n## Legal catalogue\n- Luật Đất đai 2024")

	// The head of the request — the system message — is byte-identical, which
	// is what lets the provider's cache cover the history below it.
	require.Equal(t, first[0].Content, second[0].Content)
	require.Equal(t, "system", first[0].Role)

	// Nothing recalled for the query leaked into it.
	for _, leaked := range []string{"<user_memory>", "Preferences:", "Legal catalogue", "hồ sơ đất đai"} {
		require.NotContains(t, first[0].Content, leaked)
		require.NotContains(t, second[0].Content, leaked)
	}

	// The blocks ride with the question instead, exactly once.
	for _, messages := range [][]chat.Message{first, second} {
		last := messages[len(messages)-1]
		require.Equal(t, "user", last.Role)
		require.Equal(t, 1, strings.Count(last.Content, "<user_memory>"))
	}
	require.Contains(t, second[len(second)-1].Content, "hồ sơ đất đai")
	require.Contains(t, second[len(second)-1].Content, "Luật Đất đai 2024")
	require.Contains(t, second[len(second)-1].Content, "Một câu hỏi khác hoàn toàn")

	// And the prefix fingerprint the usage log quotes is the same for both
	// turns: one grep can now tell a stable prefix from a churning one.
	firstPrefix := chat.PromptPrefixFingerprint(first, nil)
	secondPrefix := chat.PromptPrefixFingerprint(second, nil)
	require.Equal(t, firstPrefix, secondPrefix,
		"[LLM Usage] prompt_prefix must not change between turns of one session")
}

// The requester's profile is the one per-person block that may stay in the
// system prompt: it does not change between turns, so it costs nothing there.
func TestUserContextStaysInTheSystemPrompt(t *testing.T) {
	chatManage := &types.ChatManage{}
	chatManage.Query = "Hỏi"
	chatManage.UserContent = "Hỏi"
	chatManage.SummaryConfig.Prompt = "Bạn là một trợ lý."
	chatManage.UserContextPrompt = "\n\n## User context\n- Work unit: Sở Tư pháp"

	messages := prepareMessagesWithHistory(chatManage)
	require.Contains(t, messages[0].Content, "Sở Tư pháp")
	require.NotContains(t, messages[len(messages)-1].Content, "Sở Tư pháp")
}
