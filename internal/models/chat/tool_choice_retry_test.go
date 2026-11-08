package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsThinkingToolChoiceError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "deepseek 400 wrapped by go-openai",
			err: errors.New("error, status code: 400, status: 400 Bad Request, " +
				"message: Thinking mode does not support this tool_choice (request_id: 07b7c8a6)"),
			want: true,
		},
		{
			name: "raw HTTP 400 body",
			err:  errors.New(`API request failed with status 400: {"error":{"message":"thinking mode does not support this tool_choice"}}`),
			want: true,
		},
		{name: "unrelated 400", err: errors.New("error, status code: 400, message: invalid model"), want: false},
		{name: "tool_choice without thinking", err: errors.New("error, status code: 400, message: tools do not support this tool_choice"), want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isThinkingToolChoiceError(tc.err))
		})
	}
}

func TestForcedToolChoice(t *testing.T) {
	assert.False(t, forcedToolChoice(nil))
	assert.False(t, forcedToolChoice(&ChatOptions{}))
	assert.False(t, forcedToolChoice(&ChatOptions{ToolChoice: "auto"}))
	assert.False(t, forcedToolChoice(&ChatOptions{ToolChoice: "none"}))
	assert.True(t, forcedToolChoice(&ChatOptions{ToolChoice: "search_knowledge"}))
	assert.True(t, forcedToolChoice(&ChatOptions{ToolChoice: "required"}))
}

func TestWithoutForcedToolChoiceKeepsOtherFields(t *testing.T) {
	thinking := false
	opts := &ChatOptions{
		Temperature:         0.2,
		MaxCompletionTokens: 128,
		Thinking:            &thinking,
		ToolChoice:          "search_knowledge",
		Tools:               []Tool{{Type: "function", Function: FunctionDef{Name: "search_knowledge"}}},
	}
	retry := withoutForcedToolChoice(opts)
	require.NotNil(t, retry)
	assert.Empty(t, retry.ToolChoice)
	assert.Equal(t, 0.2, retry.Temperature)
	assert.Equal(t, 128, retry.MaxCompletionTokens)
	assert.Equal(t, opts.Tools, retry.Tools)
	// The caller's options are untouched.
	assert.Equal(t, "search_knowledge", opts.ToolChoice)
	assert.Nil(t, withoutForcedToolChoice(nil))
}

// TestDeepSeekThinkingField pins when the DeepSeek wire format is selected:
// the official endpoint gets {"thinking":{"type":...}} (which is what lets the
// forced first round run), always-thinking models keep the adapter default, and
// an explicit extra_config override still wins.
func TestDeepSeekThinkingField(t *testing.T) {
	thinking := false
	msgs := []Message{{Role: "user", Content: "hi"}}

	cases := []struct {
		name        string
		provider    string
		baseURL     string
		model       string
		extraConfig map[string]string
		wantField   bool
	}{
		{
			name:      "provider deepseek",
			provider:  "deepseek",
			baseURL:   "https://api.deepseek.com/v1",
			model:     "deepseek-v4.1-flash",
			wantField: true,
		},
		{
			name:      "generic provider pointed at the DeepSeek API",
			provider:  "generic",
			baseURL:   "https://api.deepseek.com/v1",
			model:     "deepseek-v4-pro",
			wantField: true,
		},
		{
			name:     "always-thinking reasoner keeps the adapter default",
			provider: "deepseek",
			baseURL:  "https://api.deepseek.com/v1",
			model:    "deepseek-reasoner",
		},
		{
			name:     "other endpoint",
			provider: "generic",
			baseURL:  "https://api.moonshot.cn/v1",
			model:    "deepseek-v4.1-flash",
		},
		{
			name:        "explicit override wins",
			provider:    "generic",
			baseURL:     "https://api.deepseek.com/v1",
			model:       "deepseek-v4.1-flash",
			extraConfig: map[string]string{ExtraConfigThinkingControl: "none"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewRemoteAPIChat(&ChatConfig{
				Source:      types.ModelSourceRemote,
				BaseURL:     tc.baseURL,
				ModelName:   tc.model,
				APIKey:      "k",
				ModelID:     tc.model,
				Provider:    tc.provider,
				ExtraConfig: tc.extraConfig,
			})
			require.NoError(t, err)

			body, _, _, err := c.buildOutbound(context.Background(), msgs, &ChatOptions{Thinking: &thinking}, true)
			require.NoError(t, err)
			js := mustJSON(t, body)
			if tc.wantField {
				assert.Contains(t, js, `"thinking":`)
				assert.Contains(t, js, `"disabled"`)
			} else {
				assert.NotContains(t, js, `"thinking":`)
			}
		})
	}
}

// TestChatRetriesWithoutForcedToolChoice reproduces the 400 from a thinking
// model that refuses a pinned tool and asserts the retry drops tool_choice but
// keeps everything else.
func TestChatRetriesWithoutForcedToolChoice(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(func() { secutils.SetSSRFWhitelistFromRaw("") })

	var (
		mu          sync.Mutex
		toolChoices []any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		toolChoices = append(toolChoices, body["tool_choice"])
		first := len(toolChoices) == 1
		mu.Unlock()
		if first {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Thinking mode does not support this tool_choice","type":"invalid_request_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"1","object":"chat.completion","created":1,"model":"deepseek-v4.1-flash",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	c := newChatAgainst(t, srv.URL, "deepseek-v4.1-flash")
	resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, &ChatOptions{
		ToolChoice: "search_knowledge",
		Tools:      []Tool{{Type: "function", Function: FunctionDef{Name: "search_knowledge"}}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "ok", resp.Content)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, toolChoices, 2)
	assert.NotNil(t, toolChoices[0], "first attempt keeps the forced tool")
	assert.Nil(t, toolChoices[1], "retry leaves the choice to the model")
}

// TestChatDoesNotRetryOtherErrors guards against swallowing real failures.
func TestChatDoesNotRetryOtherErrors(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(func() { secutils.SetSSRFWhitelistFromRaw("") })

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	c := newChatAgainst(t, srv.URL, "deepseek-v4.1-flash")
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, &ChatOptions{
		ToolChoice: "search_knowledge",
	})
	require.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func newChatAgainst(t *testing.T, baseURL, model string) *RemoteAPIChat {
	t.Helper()
	c, err := NewRemoteAPIChat(&ChatConfig{
		Source:    types.ModelSourceRemote,
		BaseURL:   baseURL,
		ModelName: model,
		APIKey:    "k",
		ModelID:   model,
		Provider:  "generic",
	})
	require.NoError(t, err)
	return c
}
