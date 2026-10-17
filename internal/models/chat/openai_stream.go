package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/sashabaranov/go-openai"
)

// parseCompletionResponse phân tích phản hồi không stream
func (c *RemoteAPIChat) parseCompletionResponse(resp *openai.ChatCompletionResponse) (*types.ChatResponse, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from API")
	}

	choice := resp.Choices[0]

	// Xử lý đầu ra của mô hình reasoning: bỏ phần suy luận bọc trong thẻ <think></think>
	// Dự phòng cho trường hợp đã đặt Thinking=false nhưng mô hình vẫn trả nội dung suy luận, và một số mô hình reasoning không hỗ trợ Thinking=false (ví dụ Minimax-M2.1)
	content := removeThinkingContent(choice.Message.Content)

	usage := tokenUsageFromOpenAI(resp.Usage, c.provider)
	response := &types.ChatResponse{
		Content:      content,
		FinishReason: string(choice.FinishReason),
		Usage:        usage,
	}

	if len(choice.Message.ToolCalls) > 0 {
		response.ToolCalls = make([]types.LLMToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			response.ToolCalls = append(response.ToolCalls, types.LLMToolCall{
				ID:   tc.ID,
				Type: string(tc.Type),
				Function: types.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}

	return response, nil
}

func (c *RemoteAPIChat) applyCompletionToolCallMetadata(body []byte, result *types.ChatResponse) {
	if result == nil || len(result.ToolCalls) == 0 {
		return
	}

	var raw struct {
		Choices []struct {
			Message struct {
				ToolCalls []json.RawMessage `json:"tool_calls,omitempty"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || len(raw.Choices) == 0 {
		return
	}

	for i, rawToolCall := range raw.Choices[0].Message.ToolCalls {
		var indexed struct {
			Index *int `json:"index,omitempty"`
		}
		_ = json.Unmarshal(rawToolCall, &indexed)
		idx := i
		if indexed.Index != nil {
			idx = *indexed.Index
		}
		if idx >= 0 && idx < len(result.ToolCalls) {
			result.ToolCalls[idx].ProviderMetadata = c.adapter.ExtractToolCallMetadata(rawToolCall)
		}
	}
}

// removeThinkingContent bỏ phần suy luận <think></think> trong đầu ra của mô hình reasoning
// Chỉ xử lý khi nội dung bắt đầu bằng <think>
func removeThinkingContent(content string) string {
	const thinkStartTag = "<think>"
	const thinkEndTag = "</think>"

	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, thinkStartTag) {
		return content
	}

	// Tìm thẻ </think> cuối cùng (xử lý trường hợp lồng nhau)
	if lastEndIdx := strings.LastIndex(trimmed, thinkEndTag); lastEndIdx != -1 {
		if result := strings.TrimSpace(trimmed[lastEndIdx+len(thinkEndTag):]); result != "" {
			return result
		}
		return ""
	}

	return "" // không thấy </think>, có thể nội dung suy luận quá dài bị cắt, trả về chuỗi rỗng
}

// processStream xử lý phản hồi stream của OpenAI SDK
func (c *RemoteAPIChat) processStream(
	ctx context.Context,
	stream *openai.ChatCompletionStream,
	streamChan chan types.StreamResponse,
	dumper *streamPacketDumper,
) {
	defer close(streamChan)
	defer stream.Close()

	state := newStreamState()

	for {
		response, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				logUsage(ctx, c.modelName, state.usage)
				toolCalls := state.buildOrderedToolCalls()
				streamChan <- types.StreamResponse{
					ResponseType: types.ResponseTypeAnswer,
					Content:      "",
					Done:         true,
					ToolCalls:    toolCalls,
					Usage:        state.usage,
					FinishReason: state.lastFinishReason,
				}
			} else {
				logger.Errorf(ctx, "Stream read error: %v (tool_calls_assembled=%d)",
					err, len(state.toolCallMap))
				streamChan <- types.StreamResponse{
					ResponseType: types.ResponseTypeError,
					Content:      err.Error(),
					Done:         true,
					ToolCalls:    state.buildOrderedToolCalls(),
					Usage:        state.usage,
					FinishReason: types.FinishReasonIncomplete,
				}
			}
			return
		}

		if dumper != nil {
			dumper.WritePacket(response)
		}

		if response.Usage != nil {
			usage := tokenUsageFromOpenAI(*response.Usage, c.provider)
			state.usage = &usage
		}

		if len(response.Choices) > 0 {
			c.processStreamDelta(ctx, &response.Choices[0], state, streamChan, response.Choices[0].Delta.ReasoningContent)
		}
	}
}

// processRawHTTPStream xử lý phản hồi stream HTTP thô
func (c *RemoteAPIChat) processRawHTTPStream(
	ctx context.Context,
	resp *http.Response,
	streamChan chan types.StreamResponse,
	dumper *streamPacketDumper,
) {
	defer close(streamChan)
	defer resp.Body.Close()

	state := newStreamState()
	reader := NewSSEReader(resp.Body)

	for {
		event, err := reader.ReadEvent()
		if err != nil {
			if err == io.EOF {
				logUsage(ctx, c.modelName, state.usage)
				toolCalls := state.buildOrderedToolCalls()
				streamChan <- types.StreamResponse{
					ResponseType: types.ResponseTypeAnswer,
					Content:      "",
					Done:         true,
					ToolCalls:    toolCalls,
					Usage:        state.usage,
				}
			} else {
				logger.Errorf(ctx, "Stream read error: %v (tool_calls_assembled=%d)",
					err, len(state.toolCallMap))
				streamChan <- types.StreamResponse{
					ResponseType: types.ResponseTypeError,
					Content:      err.Error(),
					Done:         true,
					ToolCalls:    state.buildOrderedToolCalls(),
					Usage:        state.usage,
					FinishReason: types.FinishReasonIncomplete,
				}
			}
			return
		}

		if event == nil {
			continue
		}

		if event.Done {
			logUsage(ctx, c.modelName, state.usage)
			toolCalls := state.buildOrderedToolCalls()
			streamChan <- types.StreamResponse{
				ResponseType: types.ResponseTypeAnswer,
				Content:      "",
				Done:         true,
				ToolCalls:    toolCalls,
				Usage:        state.usage,
			}
			return
		}

		if event.Data == nil {
			continue
		}

		if dumper != nil {
			// Giữ nguyên JSON gốc của dòng SSE data từ upstream, không cắt qua struct trung gian.
			raw := make([]byte, len(event.Data))
			copy(raw, event.Data)
			dumper.WritePacketRaw(raw)
		}

		// Dùng struct cục bộ để phân tích một lần, bắt cả trường chuẩn lẫn trường reasoning của vLLM, tránh tốn hiệu năng
		var streamResp struct {
			openai.ChatCompletionStreamResponse
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					openai.ChatCompletionStreamChoiceDelta
					Reasoning string `json:"reasoning,omitempty"`
				} `json:"delta"`
				FinishReason openai.FinishReason `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal(event.Data, &streamResp); err != nil {
			logger.Errorf(ctx, "Failed to parse stream response: %v", err)
			continue
		}

		if streamResp.Usage != nil {
			usage := tokenUsageFromOpenAI(*streamResp.Usage, c.provider)
			applyRawPromptCacheUsage(event.Data, &usage)
			state.usage = &usage
		}

		if len(streamResp.Choices) > 0 {
			choice := streamResp.Choices[0]
			// Logic lấy thống nhất (hỗ trợ cả đường chuẩn và vLLM)
			reasoning := choice.Delta.Reasoning
			if reasoning == "" {
				reasoning = choice.Delta.ReasoningContent
			}

			// Dựng một đối tượng choice tương thích SDK chuẩn để truyền xuống, giữ nguyên logic hiện có
			sdkChoice := openai.ChatCompletionStreamChoice{
				Index:        choice.Index,
				Delta:        choice.Delta.ChatCompletionStreamChoiceDelta,
				FinishReason: choice.FinishReason,
			}
			c.applyStreamToolCallMetadata(event.Data, state)
			c.processStreamDelta(ctx, &sdkChoice, state, streamChan, reasoning)
		}
	}
}

func (c *RemoteAPIChat) applyStreamToolCallMetadata(data []byte, state *streamState) {
	if state == nil {
		return
	}

	var raw struct {
		Choices []struct {
			Delta struct {
				ToolCalls []json.RawMessage `json:"tool_calls,omitempty"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.Choices) == 0 {
		return
	}

	for _, rawToolCall := range raw.Choices[0].Delta.ToolCalls {
		metadata := c.adapter.ExtractToolCallMetadata(rawToolCall)
		if len(metadata) == 0 {
			continue
		}
		var indexed struct {
			Index *int `json:"index,omitempty"`
		}
		_ = json.Unmarshal(rawToolCall, &indexed)
		idx := 0
		if indexed.Index != nil {
			idx = *indexed.Index
		}
		state.setToolCallProviderMetadata(idx, metadata)
	}
}

// streamState là trạng thái xử lý stream
type streamState struct {
	thinkingEmitter
	toolCallMap      map[int]*types.LLMToolCall
	lastFunctionName map[int]string
	nameNotified     map[int]bool
	fieldExtractors  map[int]*jsonFieldExtractor // per tool-call-index extractors for streaming field extraction
	fileProgress     map[int]*sandboxFileProgress
	usage            *types.TokenUsage // captured from the final stream chunk when include_usage is enabled
	lastFinishReason string            // last observed finish_reason for EOF handler fallback

	// Diagnostic flags (fire-once) used to log earliest signals of tool_call
	// presence/absence at the OpenAI-protocol level. These are independent of
	// the higher-level ResponseTypeToolCall marker (which only fires once
	// function name has stabilized) and let us distinguish between
	//   (A) no tool_calls field ever observed (true natural-stop), and
	//   (B) tool_calls field observed but marker not yet emitted.
	firstToolCallSeen    bool // true once any delta carried tool_calls
	noToolCallStopLogged bool // true once we logged "stop without tool_calls"
	firstContentSeen     bool // true once delta.Content first appeared
	firstReasoningSeen   bool // true once reasoning_content first appeared
	streamStartedAt      time.Time
}

func newStreamState() *streamState {
	return &streamState{
		toolCallMap:      make(map[int]*types.LLMToolCall),
		lastFunctionName: make(map[int]string),
		nameNotified:     make(map[int]bool),
		fieldExtractors:  make(map[int]*jsonFieldExtractor),
		fileProgress:     make(map[int]*sandboxFileProgress),
		streamStartedAt:  time.Now(),
	}
}

// elapsedMs returns the milliseconds elapsed since the stream state was
// initialized. Used to attach time-since-stream-start to fire-once diagnostic
// logs so a single grep can reveal the temporal layout of a single stream
// (TTFC / TTFT / first-tool-call / natural-stop confirmation, etc).
func (s *streamState) elapsedMs() int64 {
	if s.streamStartedAt.IsZero() {
		return 0
	}
	return time.Since(s.streamStartedAt).Milliseconds()
}

func (s *streamState) buildOrderedToolCalls() []types.LLMToolCall {
	if len(s.toolCallMap) == 0 {
		return nil
	}
	result := make([]types.LLMToolCall, 0, len(s.toolCallMap))
	for i := 0; i < len(s.toolCallMap); i++ {
		if tc, ok := s.toolCallMap[i]; ok && tc != nil {
			result = append(result, *tc)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (s *streamState) setToolCallProviderMetadata(index int, metadata types.ToolCallMetadata) {
	if len(metadata) == 0 {
		return
	}
	toolCallEntry, exists := s.toolCallMap[index]
	if !exists || toolCallEntry == nil {
		toolCallEntry = &types.LLMToolCall{
			Type: "function",
			Function: types.FunctionCall{
				Name:      "",
				Arguments: "",
			},
		}
		s.toolCallMap[index] = toolCallEntry
	}
	toolCallEntry.ProviderMetadata = metadata
}

// processStreamDelta xử lý một delta của phản hồi stream
func (c *RemoteAPIChat) processStreamDelta(
	ctx context.Context,
	choice *openai.ChatCompletionStreamChoice,
	state *streamState,
	streamChan chan types.StreamResponse,
	reasoningContent string,
) {
	delta := choice.Delta
	isDone := string(choice.FinishReason) != ""

	// Track finish_reason for EOF handler fallback
	if isDone {
		state.lastFinishReason = string(choice.FinishReason)
	}

	// Xử lý tool calls
	if len(delta.ToolCalls) > 0 {
		c.processToolCallsDelta(ctx, delta.ToolCalls, state, streamChan)
	}

	// Earliest reliable "no tool_calls" signal at the OpenAI-protocol level:
	// finish_reason=stop arrived AND we never observed a tool_calls field on
	// any prior delta. Logged once per stream so callers can grep for the
	// natural-stop entry point without waiting for the higher-level summary.
	if isDone &&
		string(choice.FinishReason) == "stop" &&
		!state.firstToolCallSeen &&
		!state.noToolCallStopLogged {
		logger.Infof(ctx, "[LLM Stream] Natural-stop at OpenAI layer "+
			"(finish=stop, tool_calls field never observed, thinking_seen=%t, "+
			"first_content_seen=%t, elapsed_ms=%d)",
			state.active, state.firstContentSeen, state.elapsedMs())
		state.noToolCallStopLogged = true
	}

	// Gửi nội dung suy luận (ReasoningContent, hỗ trợ DeepSeek, ...)
	if reasoningContent != "" {
		// Earliest reasoning_content signal at the OpenAI-protocol level. Fired
		// once per stream so we can distinguish "model emitted thinking before
		// answer" vs "model never produced thinking" when triaging logs.
		if !state.firstReasoningSeen {
			state.firstReasoningSeen = true
			logger.Infof(ctx, "[LLM Stream] First reasoning_content at OpenAI layer "+
				"(len=%d, preview=%q, elapsed_ms=%d)",
				len(reasoningContent), truncateForDebug(reasoningContent, 80), state.elapsedMs())
		}
		state.emit(streamChan, reasoningContent)
	}

	// Gửi nội dung trả lời
	if delta.Content != "" {
		// Earliest delta.Content signal at the OpenAI-protocol level. Fired once
		// per stream so we can measure TTFC (time-to-first-content) and tell
		// "answer started before any tool_call" from "tool_call came first".
		if !state.firstContentSeen {
			state.firstContentSeen = true
			logger.Infof(ctx, "[LLM Stream] First delta.Content at OpenAI layer "+
				"(len=%d, preview=%q, tool_call_seen=%t, thinking_seen=%t, elapsed_ms=%d)",
				len(delta.Content), truncateForDebug(delta.Content, 80),
				state.firstToolCallSeen, state.firstReasoningSeen, state.elapsedMs())
		}
		// If we had thinking content and this is the first answer chunk,
		// send a thinking done event first.
		state.finish(streamChan)
		streamChan <- types.StreamResponse{
			ResponseType: types.ResponseTypeAnswer,
			Content:      delta.Content,
			Done:         isDone,
			ToolCalls:    state.buildOrderedToolCalls(),
			FinishReason: string(choice.FinishReason),
		}
	}

	if isDone && len(state.toolCallMap) > 0 {
		streamChan <- types.StreamResponse{
			ResponseType: types.ResponseTypeAnswer,
			Content:      "",
			Done:         true,
			ToolCalls:    state.buildOrderedToolCalls(),
			FinishReason: string(choice.FinishReason),
		}
	}

	// Ensure thinking done is sent when stream finishes without any answer content
	// (e.g., model only produced reasoning then hit finish_reason with empty content).
	if isDone {
		state.finish(streamChan)
	}

	// Catch-all: isDone but none of the above branches sent a response with
	// FinishReason (empty content, no tool calls, no thinking). This prevents
	// the finish_reason from being lost in the streaming pipeline.
	if isDone && delta.Content == "" && len(state.toolCallMap) == 0 {
		streamChan <- types.StreamResponse{
			ResponseType: types.ResponseTypeAnswer,
			Done:         true,
			FinishReason: string(choice.FinishReason),
		}
	}
}

// processToolCallsDelta xử lý cập nhật tăng dần của tool calls
func (c *RemoteAPIChat) processToolCallsDelta(
	ctx context.Context,
	toolCalls []openai.ToolCall,
	state *streamState,
	streamChan chan types.StreamResponse,
) {
	// Earliest signal at the OpenAI-protocol level that this stream will
	// produce at least one tool call. Fires *before* the function name has
	// stabilized, i.e. earlier than the higher-level ResponseTypeToolCall
	// marker downstream consumers see. Useful for distinguishing
	// "tool_calls field arrived but marker not yet emitted" from
	// "tool_calls field truly absent" when triaging stream behavior.
	if !state.firstToolCallSeen && len(toolCalls) > 0 {
		state.firstToolCallSeen = true
		var firstID, firstName string
		for _, tc := range toolCalls {
			if tc.ID != "" {
				firstID = tc.ID
			}
			if tc.Function.Name != "" {
				firstName = tc.Function.Name
			}
			if firstID != "" || firstName != "" {
				break
			}
		}
		logger.Infof(ctx, "[LLM Stream] First tool_calls delta at OpenAI layer "+
			"(count=%d, first_id=%q, first_name=%q, "+
			"first_content_seen=%t, thinking_seen=%t, elapsed_ms=%d)",
			len(toolCalls), firstID, firstName,
			state.firstContentSeen, state.firstReasoningSeen, state.elapsedMs())
	}

	for _, tc := range toolCalls {
		var toolCallIndex int
		if tc.Index != nil {
			toolCallIndex = *tc.Index
		}
		toolCallEntry, exists := state.toolCallMap[toolCallIndex]
		if !exists || toolCallEntry == nil {
			toolCallEntry = &types.LLMToolCall{
				Type: string(tc.Type),
				Function: types.FunctionCall{
					Name:      "",
					Arguments: "",
				},
			}
			state.toolCallMap[toolCallIndex] = toolCallEntry
		}

		if tc.ID != "" {
			toolCallEntry.ID = tc.ID
		}
		if tc.Type != "" {
			toolCallEntry.Type = string(tc.Type)
		}
		if tc.Function.Name != "" {
			// Kiểm tra phòng vệ: một số nhà cung cấp (như vLLM Ascend) gửi lặp tên công cụ đầy đủ trong mỗi chunk của stream.
			// Nếu tên đã lưu trùng với tên mới nhận thì coi là lặp thừa, không nối thêm.
			if toolCallEntry.Function.Name != tc.Function.Name {
				toolCallEntry.Function.Name += tc.Function.Name
			}
		}

		argsUpdated := false
		if tc.Function.Arguments != "" {
			toolCallEntry.Function.Arguments += tc.Function.Arguments
			argsUpdated = true
		}

		currName := toolCallEntry.Function.Name
		var progressArgs map[string]any
		if isSandboxMutationTool(currName) && argsUpdated && tc.Function.Arguments != "" {
			prog := state.fileProgress[toolCallIndex]
			if prog == nil {
				prog = newSandboxFileProgress(currName)
				state.fileProgress[toolCallIndex] = prog
			}
			if payload, ok := prog.Feed(tc.Function.Arguments); ok {
				progressArgs = payload
			}
		}

		if currName != "" &&
			currName == state.lastFunctionName[toolCallIndex] &&
			argsUpdated &&
			!state.nameNotified[toolCallIndex] &&
			toolCallEntry.ID != "" {
			data := map[string]interface{}{
				"tool_name":    currName,
				"tool_call_id": toolCallEntry.ID,
			}
			if progressArgs != nil {
				data["arguments"] = progressArgs
			}
			streamChan <- types.StreamResponse{
				ResponseType: types.ResponseTypeToolCall,
				Content:      "",
				Done:         false,
				Data:         data,
			}
			state.nameNotified[toolCallIndex] = true
			progressArgs = nil
		} else if progressArgs != nil && toolCallEntry.ID != "" && currName != "" {
			streamChan <- types.StreamResponse{
				ResponseType: types.ResponseTypeToolCall,
				Content:      "",
				Done:         false,
				Data: map[string]interface{}{
					"tool_name":    currName,
					"tool_call_id": toolCallEntry.ID,
					"arguments":    progressArgs,
				},
			}
		}

		state.lastFunctionName[toolCallIndex] = currName

		// Stream thinking tool's thought field as thinking-type chunks
		if toolCallEntry.Function.Name == "thinking" && argsUpdated {
			extractor, exists := state.fieldExtractors[toolCallIndex]
			if !exists {
				extractor = newJSONFieldExtractor("thought")
				state.fieldExtractors[toolCallIndex] = extractor
			}
			thoughtChunk := extractor.Feed(tc.Function.Arguments)
			if thoughtChunk != "" {
				streamChan <- types.StreamResponse{
					ResponseType: types.ResponseTypeThinking,
					Content:      thoughtChunk,
					Done:         false,
					Data: map[string]interface{}{
						"source":       "thinking_tool",
						"tool_call_id": toolCallEntry.ID,
					},
				}
			}
		}
	}
}
