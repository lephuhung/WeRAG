package chat

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClampPromptCacheKey(t *testing.T) {
	assert.Equal(t, "", clampPromptCacheKey(""))
	assert.Equal(t, "sess-1", clampPromptCacheKey("sess-1"))
	long := strings.Repeat("a", 80)
	got := clampPromptCacheKey(long)
	assert.Equal(t, 64, len([]rune(got)))
	assert.Equal(t, strings.Repeat("a", 64), got)
}

func TestApplyPromptCacheToJSONBody_OpenAIKey(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hi"},
		},
	}
	policy := promptCachePolicyFor(provider.ProviderOpenAI, "")
	body, forceRaw, err := applyPromptCacheToJSONBody(req, policy, "sess-abc", CacheRetentionShort)
	require.NoError(t, err)
	require.True(t, forceRaw)
	payload := body.(map[string]any)
	assert.Equal(t, "sess-abc", payload["prompt_cache_key"])
	assert.NotContains(t, payload, "prompt_cache_retention")
}

func TestApplyPromptCacheToJSONBody_OpenAILongRetention(t *testing.T) {
	req := openai.ChatCompletionRequest{Model: "gpt-4o"}
	policy := promptCachePolicyFor(provider.ProviderOpenAI, "")
	body, forceRaw, err := applyPromptCacheToJSONBody(req, policy, "sess-abc", CacheRetentionLong)
	require.NoError(t, err)
	require.True(t, forceRaw)
	payload := body.(map[string]any)
	assert.Equal(t, "24h", payload["prompt_cache_retention"])
}

func TestApplyPromptCacheToJSONBody_NoneLeavesBodyUntouched(t *testing.T) {
	req := &openai.ChatCompletionRequest{Model: "gpt-4o"}
	policy := promptCachePolicyFor(provider.ProviderOpenAI, "")
	body, forceRaw, err := applyPromptCacheToJSONBody(req, policy, "sess-abc", CacheRetentionNone)
	require.NoError(t, err)
	assert.False(t, forceRaw)
	assert.Equal(t, req, body)
}

func TestBuildOutbound_OpenAIPromptCacheKeyFromSession(t *testing.T) {
	c := newOutboundChat(t, string(provider.ProviderOpenAI), "gpt-4o", nil)
	ctx := types.WithSessionID(context.Background(), "sess-live")
	body, _, useRaw, err := c.buildOutbound(ctx, []Message{{Role: "user", Content: "hi"}}, &ChatOptions{}, false)
	require.NoError(t, err)
	require.True(t, useRaw)
	payload := body.(map[string]any)
	assert.Equal(t, "sess-live", payload["prompt_cache_key"])
}

func TestAttachPromptCacheHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", nil)
	require.NoError(t, err)
	attachPromptCacheHeaders(req, promptCachePolicy{sendAffinity: true}, "sess-1")
	assert.Equal(t, "sess-1", req.Header.Get("session_id"))
	assert.Equal(t, "sess-1", req.Header.Get("x-client-request-id"))
	assert.Equal(t, "sess-1", req.Header.Get("x-session-affinity"))
}
