package chat

import (
	"slices"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatOptionsCompletionBudget(t *testing.T) {
	assert.Zero(t, (*ChatOptions)(nil).CompletionBudget())
	assert.Zero(t, (&ChatOptions{}).CompletionBudget())
	assert.Equal(t, 128, (&ChatOptions{MaxTokens: 128}).CompletionBudget())
	assert.Equal(t, 256, (&ChatOptions{MaxCompletionTokens: 256}).CompletionBudget())
	assert.Equal(t, 256, (&ChatOptions{MaxTokens: 128, MaxCompletionTokens: 256}).CompletionBudget())
}

func TestWireCompletionTokenField(t *testing.T) {
	// Every AllProviders() name must be classified here so a new vendor cannot
	// silently inherit the default. Match Pi's polarity: default
	// max_completion_tokens; self-hosted generic stays on max_tokens.
	want := map[provider.ProviderName]completionTokenField{
		provider.ProviderGeneric:      completionTokenFieldMaxTokens,
		provider.ProviderOpenAI:       completionTokenFieldMaxCompletionTokens,
		provider.ProviderWeKnoraCloud: completionTokenFieldMaxCompletionTokens,
	}
	// Decision-only providers (jev, clef) never serve chat completions.
	var chatProviders []provider.ProviderName
	for _, name := range provider.AllProviders() {
		if p, ok := provider.Get(name); ok && slices.Contains(p.Info().ModelTypes, types.ModelTypeDecision) {
			continue
		}
		chatProviders = append(chatProviders, name)
	}
	require.Len(t, want, len(chatProviders), "classify every non-decision AllProviders() name")
	for _, name := range chatProviders {
		field, ok := want[name]
		require.True(t, ok, "classify %s in TestWireCompletionTokenField", name)
		assert.Equal(t, field, wireCompletionTokenField(name, "any"), string(name))
	}

	// GPT-5 / o-series always use the modern field, even on a max_tokens provider.
	assert.Equal(t, completionTokenFieldMaxCompletionTokens,
		wireCompletionTokenField(provider.ProviderGeneric, "gpt-5-mini"))
}

func TestBuildChatCompletionRequest_OneWireTokenField(t *testing.T) {
	messages := []Message{{Role: "user", Content: "hello"}}

	t.Run("generic vLLM sends max_tokens from MaxTokens-only callers", func(t *testing.T) {
		c := newOutboundChat(t, string(provider.ProviderGeneric), "qwen3", nil)
		req := c.BuildChatCompletionRequest(messages, &ChatOptions{MaxTokens: 2048}, false)
		assert.Equal(t, 2048, req.MaxTokens)
		assert.Zero(t, req.MaxCompletionTokens)
	})

	t.Run("openai gpt-4o sends max_completion_tokens", func(t *testing.T) {
		c := newOutboundChat(t, string(provider.ProviderOpenAI), "gpt-4o", nil)
		req := c.BuildChatCompletionRequest(messages, &ChatOptions{MaxTokens: 128}, false)
		assert.Zero(t, req.MaxTokens)
		assert.Equal(t, 128, req.MaxCompletionTokens)
	})
}

func TestOllamaBuildChatRequestUsesCompletionBudget(t *testing.T) {
	c := &OllamaChat{modelName: "llama3"}
	req := c.buildChatRequest(
		[]Message{{Role: "user", Content: "hi"}},
		&ChatOptions{MaxCompletionTokens: 512},
		false,
	)
	assert.Equal(t, 512, req.Options["num_predict"])
}

func TestBuildLangfuseModelParamsUsesCompletionBudget(t *testing.T) {
	params := buildLangfuseModelParams(&ChatOptions{MaxTokens: 128, MaxCompletionTokens: 256})
	assert.Equal(t, 256, params["max_completion_tokens"])
	_, hasLegacy := params["max_tokens"]
	assert.False(t, hasLegacy)
}
