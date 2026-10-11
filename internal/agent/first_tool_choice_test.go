package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFirstRoundToolChoice(t *testing.T) {
	offered := []chat.Tool{
		{Type: "function", Function: chat.FunctionDef{Name: "rewrite_paragraphs"}},
		{Type: "function", Function: chat.FunctionDef{Name: "mark_passages"}},
	}
	ctx := types.WithFirstToolChoice(context.Background(), "rewrite_paragraphs")
	if got := firstRoundToolChoice(ctx, 0, offered); got != "rewrite_paragraphs" {
		t.Fatalf("first round: %q", got)
	}
	if got := firstRoundToolChoice(ctx, 1, offered); got != "" {
		t.Fatalf("later rounds choose freely: %q", got)
	}
	if got := firstRoundToolChoice(types.WithFirstToolChoice(context.Background(), "check_spelling"), 0, offered); got != "" {
		t.Fatalf("a tool not offered is not forced: %q", got)
	}
	if got := firstRoundToolChoice(context.Background(), 0, offered); got != "" {
		t.Fatalf("nothing asked: %q", got)
	}
}

// scriptedChat answers Chat calls in order and records their options.
type scriptedChat struct {
	id      string
	replies []*types.ChatResponse
	opts    []*chat.ChatOptions
	msgs    [][]chat.Message
}

func (s *scriptedChat) Chat(_ context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	if len(s.opts) >= len(s.replies) {
		return nil, fmt.Errorf("unexpected Chat call #%d", len(s.opts))
	}
	s.opts = append(s.opts, opts)
	s.msgs = append(s.msgs, append([]chat.Message(nil), messages...))
	return s.replies[len(s.opts)-1], nil
}

func (s *scriptedChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, fmt.Errorf("a forced round is not streamed")
}
func (s *scriptedChat) GetModelName() string { return s.id }
func (s *scriptedChat) GetModelID() string   { return s.id }

var rewriteTool = []chat.Tool{{Type: "function", Function: chat.FunctionDef{
	Name: "rewrite_paragraphs", Parameters: json.RawMessage(`{"type":"object","properties":{"variants":{"type":"array"}}}`),
}}}

// Gemma on vLLM answers a named tool_choice in plain text: the arguments
// are asked for as JSON and the call is built; the next turn skips the
// ignored tool_choice.
func TestForcedToolRoundFallsBackToJSON(t *testing.T) {
	model := &scriptedChat{id: "ignores-tool-choice-test", replies: []*types.ChatResponse{
		{Content: "Tôi đã viết lại… nhấn nút Thay vào văn bản", FinishReason: "stop"},
		{Content: "```json\n{\"variants\":[{\"text\":\"Câu mới\"}]}\n```", FinishReason: "stop"},
		{Content: "{\"variants\":[]}", FinishReason: "stop"},
	}}
	engine := newTestEngine(t, model)
	msgs := []chat.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "viết lại"}}
	opts := &chat.ChatOptions{ToolChoice: "rewrite_paragraphs", Tools: rewriteTool}

	resp, ok := engine.forcedToolRound(context.Background(), msgs, rewriteTool, opts, "rewrite_paragraphs", 0, "s1")
	require.True(t, ok)
	require.Empty(t, resp.Content)
	require.Len(t, resp.ToolCalls, 1)
	require.Equal(t, "rewrite_paragraphs", resp.ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"variants":[{"text":"Câu mới"}]}`, resp.ToolCalls[0].Function.Arguments)
	require.NotEmpty(t, model.opts[1].Format)
	require.Empty(t, model.opts[1].Tools)
	require.Len(t, model.msgs[1], 2, "the instruction rides on the last user message")
	require.Contains(t, model.msgs[1][1].Content, "<tool_call_required>")
	require.Equal(t, "viết lại", msgs[1].Content, "the caller's messages are untouched")

	_, ok = engine.forcedToolRound(context.Background(), msgs, rewriteTool, opts, "rewrite_paragraphs", 0, "s1")
	require.True(t, ok)
	require.Len(t, model.opts, 3, "second turn: one JSON call only")
	require.NotEmpty(t, model.opts[2].Format)
}

func TestForcedToolRoundKeepsAHonouredCall(t *testing.T) {
	call := types.LLMToolCall{ID: "c1", Type: "function", Function: types.FunctionCall{Name: "rewrite_paragraphs", Arguments: "{}"}}
	model := &scriptedChat{id: "honours-tool-choice-test", replies: []*types.ChatResponse{
		{ToolCalls: []types.LLMToolCall{call}, FinishReason: "tool_calls"},
	}}
	engine := newTestEngine(t, model)
	resp, ok := engine.forcedToolRound(context.Background(), []chat.Message{{Role: "user", Content: "x"}}, rewriteTool,
		&chat.ChatOptions{ToolChoice: "rewrite_paragraphs"}, "rewrite_paragraphs", 0, "s1")
	require.True(t, ok)
	require.Equal(t, []types.LLMToolCall{call}, resp.ToolCalls)
	require.Len(t, model.opts, 1)
}

func TestJSONObjectOf(t *testing.T) {
	for _, bad := range []string{"", "không có", "{bad json}"} {
		if _, err := jsonObjectOf(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	got, err := jsonObjectOf("Đây: {\"a\":1} xong")
	require.NoError(t, err)
	require.Equal(t, `{"a":1}`, got)
}
