package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func llmChatFixture() (*ModelHandler, *usageModelService) {
	models := &usageModelService{
		models: []*types.Model{
			{ID: "embed", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, IsDefault: true},
			{ID: "first", Name: "first-model", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive},
			{ID: "qwen", Name: "Qwen/Qwen3.6-35B-A3B-FP8", Type: types.ModelTypeKnowledgeQA,
				Status: types.ModelStatusActive, IsDefault: true,
				Parameters: types.ModelParameters{APIKey: "sk-secret", BaseURL: "http://10.0.0.1/v1"}},
			{ID: "broken", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatus("download_failed")},
		},
		chat: &usageChatModel{result: &types.ChatResponse{
			Content: `{"labels":{}}`, FinishReason: "stop",
			Usage: types.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}},
	}
	return &ModelHandler{service: models}, models
}

func llmChatRequest(h *ModelHandler, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Next()
	})
	r.POST("/llm/chat", h.LLMChat)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/llm/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestLLMChatUsesWorkspaceDefaultChatModel(t *testing.T) {
	h, models := llmChatFixture()
	w := llmChatRequest(h, `{"messages":[{"role":"system","content":"s"},{"role":"user","content":"u"}],
		"temperature":0,"json":true}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "qwen", models.selected)

	var resp struct {
		Data LLMChatResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "qwen", resp.Data.ModelID)
	require.Equal(t, "Qwen/Qwen3.6-35B-A3B-FP8", resp.Data.ModelName)
	require.Equal(t, `{"labels":{}}`, resp.Data.Content)
	require.Equal(t, 15, resp.Data.Usage.TotalTokens)
	require.NotContains(t, w.Body.String(), "sk-secret")
	require.NotContains(t, w.Body.String(), "10.0.0.1")

	opts := models.chat.options
	require.False(t, *opts.Thinking, "thinking defaults to off")
	require.Zero(t, opts.Temperature)
	require.Equal(t, llmChatDefaultMaxToks, opts.MaxTokens)
	require.NotEmpty(t, opts.Format, "json:true requests a JSON object")
	require.Empty(t, opts.Tools)
	require.Len(t, models.chat.messages, 2)
	require.Equal(t, "system", models.chat.messages[0].Role)
}

func TestLLMChatModelSelection(t *testing.T) {
	for _, tc := range []struct {
		name, modelID, want string
		status              int
	}{
		{"explicit default keyword", "default", "qwen", http.StatusOK},
		{"explicit chat model", "first", "first", http.StatusOK},
		{"not a chat model", "embed", "", http.StatusBadRequest},
		{"inactive model", "broken", "", http.StatusBadRequest},
		{"unknown model", "nope", "", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, models := llmChatFixture()
			w := llmChatRequest(h, `{"model_id":"`+tc.modelID+`","messages":[{"role":"user","content":"u"}]}`)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.want, models.selected)
		})
	}
}

func TestLLMChatFallsBackToFirstActiveChatModel(t *testing.T) {
	h, models := llmChatFixture()
	models.models[2].IsDefault = false
	w := llmChatRequest(h, `{"messages":[{"role":"user","content":"u"}]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "first", models.selected)
}

func TestLLMChatRejectsInvalidRequests(t *testing.T) {
	big := strings.Repeat("x", llmChatMaxContentChars+1)
	for _, tc := range []struct{ name, body string }{
		{"no messages", `{"messages":[]}`},
		{"bad role", `{"messages":[{"role":"tool","content":"x"}]}`},
		{"too large", `{"messages":[{"role":"user","content":"` + big + `"}]}`},
		{"temperature", `{"messages":[{"role":"user","content":"x"}],"temperature":3}`},
		{"max tokens", `{"messages":[{"role":"user","content":"x"}],"max_tokens":999999}`},
		{"not json", `nope`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, models := llmChatFixture()
			w := llmChatRequest(h, tc.body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Empty(t, models.selected, "no model is called for a rejected request")
		})
	}
}

func TestLLMChatNoChatModelConfigured(t *testing.T) {
	h, models := llmChatFixture()
	models.models = models.models[:1] // embedding only
	w := llmChatRequest(h, `{"messages":[{"role":"user","content":"u"}]}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "Configure an active chat model")
}

func TestLLMChatUpstreamFailure(t *testing.T) {
	h, models := llmChatFixture()
	models.chat.result = nil
	models.chat.err = errors.New("dial tcp 10.0.0.1: connection refused")
	w := llmChatRequest(h, `{"messages":[{"role":"user","content":"u"}],"thinking":true}`)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.NotContains(t, w.Body.String(), "10.0.0.1", "upstream errors must not leak model endpoints")
	require.True(t, *models.chat.options.Thinking)
}
