package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// A round that must call a given tool (types.WithFirstToolChoice) is not
// streamed: its only useful output is the call. Some servers ignore a named
// tool_choice — vLLM with the gemma4 tool parser answers in plain text, so
// "viết lại đoạn này" came back as chat text with no proposal card. Such a
// reply never reaches the user: the arguments are asked for once more as a
// JSON object (response_format, which the server does enforce) and the call
// is built here. A model seen ignoring tool_choice goes straight to JSON.

// ignoresToolChoice remembers chat model ids that answered a named
// tool_choice without calling the tool (for the life of the process).
var ignoresToolChoice sync.Map

// forcedToolRound runs the round as a call to `forced`. ok=false when no
// call could be had: the caller then streams the round as usual.
func (e *AgentEngine) forcedToolRound(
	ctx context.Context, messages []chat.Message, tools []chat.Tool, opts *chat.ChatOptions,
	forced string, iteration int, sessionID string,
) (*types.ChatResponse, bool) {
	params := toolParameters(tools, forced)
	if params == nil {
		return nil, false
	}
	messages = e.modelContext.EncodeMessages(messages)
	reportModelContextLeaks(ctx, "Agent", e.modelContext, messages)
	modelID := e.chatModel.GetModelID()

	if _, known := ignoresToolChoice.Load(modelID); !known {
		callCtx := types.WithLLMCallMetadata(ctx, "agent_round", chat.PromptPrefixFingerprint(messages, opts))
		resp, err := e.chatModel.Chat(callCtx, messages, opts)
		if err != nil {
			logger.Warnf(ctx, "[Agent][Forced] %s call failed: %v", forced, err)
			return nil, false
		}
		if calls := callsTo(resp.ToolCalls, forced); len(calls) > 0 {
			resp.ToolCalls = calls
			resp.Content = ""
			e.emitForcedToolCalls(ctx, calls, iteration, sessionID)
			return resp, true
		}
		ignoresToolChoice.Store(modelID, true)
		logger.Warnf(ctx, "[Agent][Forced] model %s ignored tool_choice=%s (finish=%s, %d chars of text); asking for the arguments as JSON",
			modelID, forced, resp.FinishReason, len(resp.Content))
	}

	resp, err := e.forcedToolArgsAsJSON(ctx, messages, opts, forced, params)
	if err != nil {
		logger.Warnf(ctx, "[Agent][Forced] %s arguments as JSON: %v", forced, err)
		return nil, false
	}
	e.emitForcedToolCalls(ctx, resp.ToolCalls, iteration, sessionID)
	return resp, true
}

// forcedToolArgsAsJSON asks for the arguments of `forced` as a JSON object
// and returns them as that tool's call.
func (e *AgentEngine) forcedToolArgsAsJSON(
	ctx context.Context, messages []chat.Message, opts *chat.ChatOptions, forced string, params json.RawMessage,
) (*types.ChatResponse, error) {
	instruction := fmt.Sprintf("\n\n<tool_call_required>Call the tool %s now. Reply with ONLY the JSON object of its arguments (no prose, no Markdown); the user sees the result through the tool.</tool_call_required>", forced)
	msgs := append([]chat.Message(nil), messages...)
	// the instruction rides on the last user message: chat templates such
	// as Gemma's reject two user messages in a row
	if last := len(msgs) - 1; last >= 0 && msgs[last].Role == "user" && len(msgs[last].MultiContent) == 0 {
		msgs[last].Content += instruction
	} else {
		msgs = append(msgs, chat.Message{Role: "user", Content: strings.TrimSpace(instruction)})
	}
	noThinking := false
	jsonOpts := &chat.ChatOptions{
		Temperature:         opts.Temperature,
		MaxCompletionTokens: opts.MaxCompletionTokens,
		Thinking:            &noThinking,
		Format:              params,
		PromptCacheKey:      opts.PromptCacheKey,
	}
	callCtx := types.WithLLMCallMetadata(ctx, "agent_round_forced_json", chat.PromptPrefixFingerprint(msgs, jsonOpts))
	resp, err := e.chatModel.Chat(callCtx, msgs, jsonOpts)
	if err != nil {
		return nil, err
	}
	args, err := jsonObjectOf(resp.Content)
	if err != nil {
		return nil, err
	}
	resp.Content = ""
	resp.FinishReason = "tool_calls"
	resp.ToolCalls = []types.LLMToolCall{{
		ID:       "call_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:24],
		Type:     "function",
		Function: types.FunctionCall{Name: forced, Arguments: args},
	}}
	return resp, nil
}

// emitForcedToolCalls shows the calls as pending, as the streamed round does.
func (e *AgentEngine) emitForcedToolCalls(ctx context.Context, calls []types.LLMToolCall, iteration int, sessionID string) {
	for _, tc := range calls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		e.eventBus.Emit(ctx, event.Event{
			ID:        fmt.Sprintf("%s-tool-call-pending", tc.ID),
			Type:      event.EventAgentToolCall,
			SessionID: sessionID,
			Data: event.AgentToolCallData{
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Arguments:  args,
				Iteration:  iteration,
			},
		})
	}
}

func toolParameters(tools []chat.Tool, name string) json.RawMessage {
	for _, t := range tools {
		if t.Function.Name == name && len(t.Function.Parameters) > 0 {
			return t.Function.Parameters
		}
	}
	return nil
}

func callsTo(calls []types.LLMToolCall, name string) []types.LLMToolCall {
	var out []types.LLMToolCall
	for _, tc := range calls {
		if tc.Function.Name == name {
			out = append(out, tc)
		}
	}
	return out
}

// jsonObjectOf is the JSON object in a reply (a ```json fence or prose
// around it is dropped).
func jsonObjectOf(reply string) (string, error) {
	reply = strings.TrimSpace(reply)
	i, j := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if i < 0 || j <= i {
		return "", fmt.Errorf("no JSON object in %d chars", len(reply))
	}
	obj := reply[i : j+1]
	var probe map[string]any
	if err := json.Unmarshal([]byte(obj), &probe); err != nil {
		return "", fmt.Errorf("bad JSON object: %w", err)
	}
	return obj, nil
}
