package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type capturedLegalPromptModel struct {
	fakeAgentChatModel
	systemPrompt string
}

func (m *capturedLegalPromptModel) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	for _, message := range messages {
		if message.Role == "system" {
			m.systemPrompt = message.Content
			break
		}
	}
	return m.fakeAgentChatModel.ChatStream(ctx, messages, opts)
}

func TestBuiltinLegalPromptDisclosesNamesWithoutBodiesOrSandbox(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	model := &capturedLegalPromptModel{}
	cfg := &types.AgentConfig{SkillsEnabled: false, UseCustomSystemPrompt: true, SystemPrompt: "Chỉ dùng tài liệu của WeKnora."}
	engine, err := (&agentService{}).CreateAgentEngine(ctx, cfg, model, nil, nil, "sess", "msg")
	require.NoError(t, err)
	_, err = engine.Execute(ctx, "sess", "msg", "So sánh các văn bản", nil)
	require.NoError(t, err)
	for _, name := range bundledLegalSkillNames {
		require.Contains(t, model.systemPrompt, `path="skill://`+name+`/SKILL.md"`)
		require.Equal(t, 1, strings.Count(model.systemPrompt, `name="`+name+`"`))
	}
	require.Contains(t, model.systemPrompt, "Chỉ dùng tài liệu của WeKnora.")
	require.NotContains(t, model.systemPrompt, "Tóm tắt đúng nội dung đã đọc", "full instructions must be read on demand")
}
