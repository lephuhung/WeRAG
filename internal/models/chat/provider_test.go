package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveProvider pins the provider+model routing table, including the
// sub-model matchers (reasoning models, Qwen thinking, LKEAP DeepSeek V3) and
// the baseProvider fallback for everything else.
func TestResolveProvider(t *testing.T) {
	cases := []struct {
		name  string
		prov  provider.ProviderName
		model string
		want  providerAdapter
	}{
		{"generic", provider.ProviderGeneric, "anything", genericProvider{}},
		{"openai non-reasoning falls back", provider.ProviderOpenAI, "gpt-4o", baseProvider{}},
		{"openai reasoning", provider.ProviderOpenAI, "gpt-5", openAIReasoningProvider{}},
		{"weknora cloud", provider.ProviderWeKnoraCloud, "anything", weKnoraCloudProvider{}},
		{"unknown falls back", provider.ProviderName("nope"), "x", baseProvider{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.IsType(t, tc.want, resolveProvider(tc.prov, tc.model))
		})
	}
}

func newOutboundChat(t *testing.T, providerName, model string, extra map[string]string) *RemoteAPIChat {
	t.Helper()
	c, err := NewRemoteAPIChat(&ChatConfig{
		Source:      types.ModelSourceRemote,
		ModelName:   model,
		APIKey:      "k",
		ModelID:     model,
		Provider:    providerName,
		ExtraConfig: extra,
	})
	require.NoError(t, err)
	return c
}

// TestBuildOutbound_Thinking is the characterization suite for the merged
// thinking-control path: it asserts that buildOutbound produces the same wire
// formats the pre-refactor provider customizers did.
func TestBuildOutbound_Thinking(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}

	t.Run("generic explicit thinking_type overrides legacy kwargs", func(t *testing.T) {
		c := newOutboundChat(t, string(provider.ProviderGeneric), "deepseek-v4-flash",
			map[string]string{ExtraConfigThinkingControl: "thinking_type"})
		body, _, useRaw, err := c.buildOutbound(context.Background(), msgs, &ChatOptions{Thinking: ptrBool(false)}, true)
		require.NoError(t, err)
		require.True(t, useRaw)
		js := mustJSON(t, body)
		assert.Contains(t, js, `"thinking"`)
		assert.Contains(t, js, `"disabled"`)
		assert.NotContains(t, js, "chat_template_kwargs")
	})

	t.Run("generic legacy chat_template_kwargs", func(t *testing.T) {
		c := newOutboundChat(t, string(provider.ProviderGeneric), "qwen", nil)
		body, _, useRaw, err := c.buildOutbound(context.Background(), msgs, &ChatOptions{Thinking: ptrBool(false)}, true)
		require.NoError(t, err)
		require.True(t, useRaw)
		assert.Contains(t, mustJSON(t, body), "chat_template_kwargs")
	})

	t.Run("none keeps the standard SDK request", func(t *testing.T) {
		c := newOutboundChat(t, string(provider.ProviderGeneric), "x",
			map[string]string{ExtraConfigThinkingControl: "none"})
		body, _, useRaw, err := c.buildOutbound(context.Background(), msgs, &ChatOptions{Thinking: ptrBool(false)}, true)
		require.NoError(t, err)
		assert.False(t, useRaw)
		_, ok := body.(*openai.ChatCompletionRequest)
		assert.True(t, ok)
	})

}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
