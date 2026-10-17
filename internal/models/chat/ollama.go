package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	ollamaapi "github.com/ollama/ollama/api"
)

// OllamaChat cài đặt trò chuyện dựa trên Ollama
type OllamaChat struct {
	modelName     string
	modelID       string
	ollamaService *ollama.OllamaService
}

// NewOllamaChat tạo instance trò chuyện Ollama
func NewOllamaChat(config *ChatConfig, ollamaService *ollama.OllamaService) (*OllamaChat, error) {
	return &OllamaChat{
		modelName:     config.ModelName,
		modelID:       config.ModelID,
		ollamaService: ollamaService,
	}, nil
}

// convertMessages chuyển định dạng tin nhắn sang định dạng Ollama API
func (c *OllamaChat) convertMessages(messages []Message) []ollamaapi.Message {
	ollamaMessages := make([]ollamaapi.Message, 0, len(messages))
	for _, msg := range messages {
		msg = neutralizeMessageSpecialTokens(msg)
		msgOllama := ollamaapi.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			ToolCalls: c.toolCallFrom(msg.ToolCalls),
		}
		if msg.Role == "tool" {
			msgOllama.ToolName = msg.Name
		}
		if len(msg.Images) > 0 && msg.Role == "user" {
			for _, imgURL := range msg.Images {
				if imgData := resolveImageForOllama(imgURL); imgData != nil {
					msgOllama.Images = append(msgOllama.Images, imgData)
				}
			}
		}
		ollamaMessages = append(ollamaMessages, msgOllama)
	}
	return ollamaMessages
}

// resolveImageForOllama resolves an image URL into raw bytes for Ollama.
// Handles local serving paths (/files/...), data URIs, and remote HTTP URLs.
func resolveImageForOllama(imageURL string) ollamaapi.ImageData {
	if data := resolveImageURLForOllama(imageURL); data != nil {
		return data
	}
	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		if err := secutils.ValidateURLForSSRF(imageURL); err != nil {
			return nil
		}
		client := secutils.NewSSRFSafeHTTPClient(secutils.SSRFSafeHTTPClientConfig{
			Timeout:      30 * time.Second,
			MaxRedirects: 5,
		})
		resp, err := client.Get(imageURL)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
		if err != nil {
			return nil
		}
		return data
	}
	return nil
}

// buildChatRequest dựng tham số yêu cầu trò chuyện
func (c *OllamaChat) buildChatRequest(messages []Message, opts *ChatOptions, isStream bool) *ollamaapi.ChatRequest {
	// Đặt cờ stream
	streamFlag := isStream

	// Dựng tham số yêu cầu
	chatReq := &ollamaapi.ChatRequest{
		Model:    c.modelName,
		Messages: c.convertMessages(messages),
		Stream:   &streamFlag,
		Options:  make(map[string]interface{}),
	}

	// Thêm tham số tuỳ chọn
	if opts != nil {
		chatReq.Options["temperature"] = opts.Temperature
		if opts.TopP > 0 {
			chatReq.Options["top_p"] = opts.TopP
		}
		if budget := opts.CompletionBudget(); budget > 0 {
			chatReq.Options["num_predict"] = budget
		}
		if opts.Thinking != nil {
			chatReq.Think = &ollamaapi.ThinkValue{
				Value: *opts.Thinking,
			}
		}
		if len(opts.Format) > 0 {
			chatReq.Format = opts.Format
		}
		if len(opts.Tools) > 0 {
			chatReq.Tools = c.toolFrom(opts.Tools)
		}
	}

	return chatReq
}

// Chat thực hiện trò chuyện không stream
func (c *OllamaChat) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*types.ChatResponse, error) {
	// Đảm bảo mô hình khả dụng
	if err := c.ensureModelAvailable(ctx); err != nil {
		return nil, err
	}

	// Dựng tham số yêu cầu
	chatReq := c.buildChatRequest(messages, opts, false)

	// Ghi log yêu cầu
	logger.GetLogger(ctx).Infof("sending chat request to model %s", c.modelName)

	var responseContent string
	var toolCalls []types.LLMToolCall
	var promptTokens, completionTokens int

	// Gửi yêu cầu bằng Ollama client
	err := c.ollamaService.Chat(ctx, chatReq, func(resp ollamaapi.ChatResponse) error {
		responseContent = resp.Message.Content
		// Khi Content rỗng nhưng Thinking có nội dung (ví dụ mô hình reasoning chưa cấu hình đúng tham số thinking), dùng Thinking làm dự phòng
		if responseContent == "" && resp.Message.Thinking != "" {
			responseContent = resp.Message.Thinking
		}
		toolCalls = c.toolCallTo(resp.Message.ToolCalls)

		// Lấy số token
		if resp.EvalCount > 0 {
			promptTokens = resp.PromptEvalCount
			completionTokens = resp.EvalCount - promptTokens
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("yêu cầu trò chuyện thất bại: %w", err)
	}

	usage := types.TokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
	usage.MarkPromptCacheUnsupported()
	logUsage(ctx, c.modelName, &usage)

	return &types.ChatResponse{
		Content:   responseContent,
		ToolCalls: toolCalls,
		Usage:     usage,
	}, nil
}

// ChatStream thực hiện trò chuyện dạng stream
func (c *OllamaChat) ChatStream(
	ctx context.Context,
	messages []Message,
	opts *ChatOptions,
) (<-chan types.StreamResponse, error) {
	// Đảm bảo mô hình khả dụng
	if err := c.ensureModelAvailable(ctx); err != nil {
		return nil, err
	}

	// Dựng tham số yêu cầu
	chatReq := c.buildChatRequest(messages, opts, true)

	// Ghi log yêu cầu
	logger.GetLogger(ctx).Infof("sending streaming chat request to model %s", c.modelName)

	// Tạo kênh phản hồi stream
	streamChan := make(chan types.StreamResponse)

	// Khởi chạy goroutine xử lý phản hồi stream
	go func() {
		defer close(streamChan)

		var thinking thinkingEmitter
		err := c.ollamaService.Chat(ctx, chatReq, func(resp ollamaapi.ChatResponse) error {
			// Gửi nội dung suy luận (hỗ trợ các mô hình reasoning như Qwen3, DeepSeek)
			if resp.Message.Thinking != "" {
				thinking.emit(streamChan, resp.Message.Thinking)
			}

			if resp.Message.Content != "" {
				// Sau khi giai đoạn suy luận kết thúc, gửi sự kiện hoàn tất suy luận
				thinking.finish(streamChan)
				streamChan <- types.StreamResponse{
					ResponseType: types.ResponseTypeAnswer,
					Content:      resp.Message.Content,
					Done:         false,
				}
			}

			if len(resp.Message.ToolCalls) > 0 {
				streamChan <- types.StreamResponse{
					ResponseType: types.ResponseTypeToolCall,
					ToolCalls:    c.toolCallTo(resp.Message.ToolCalls),
					Done:         false,
				}

				// Ollama returns tool calls as complete objects (not incremental deltas).
				// Log this so we can trace non-streaming thought delivery.
				for _, tc := range resp.Message.ToolCalls {
					if tc.Function.Name == "thinking" {
						argsBytes, _ := json.Marshal(tc.Function.Arguments)
						logger.Warnf(ctx, "[Ollama Stream] Tool %q arrived non-incrementally (%d bytes args), "+
							"thought will not be token-streamed to frontend",
							tc.Function.Name, len(argsBytes))
					}
				}

				for _, tc := range resp.Message.ToolCalls {
					argsMap := tc.Function.Arguments.ToMap()
					switch tc.Function.Name {
					case "thinking":
						if thought, ok := argsMap["thought"].(string); ok && thought != "" {
							streamChan <- types.StreamResponse{
								ResponseType: types.ResponseTypeThinking,
								Content:      thought,
								Done:         false,
								Data: map[string]interface{}{
									"source":       "thinking_tool",
									"tool_call_id": tooli2s(tc.Function.Index),
								},
							}
						}
					}
				}
			}

			if resp.Done {
				var usage *types.TokenUsage
				if resp.PromptEvalCount > 0 || resp.EvalCount > 0 {
					usage = &types.TokenUsage{
						PromptTokens:     resp.PromptEvalCount,
						CompletionTokens: resp.EvalCount,
						TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
					}
					usage.MarkPromptCacheUnsupported()
				}
				logUsage(ctx, c.modelName, usage)
				streamChan <- types.StreamResponse{
					ResponseType: types.ResponseTypeAnswer,
					Done:         true,
					Usage:        usage,
				}
			}

			return nil
		})
		if err != nil {
			logger.GetLogger(ctx).Errorf("streaming chat request failed: %v", err)
			// Gửi phản hồi lỗi
			streamChan <- types.StreamResponse{
				ResponseType: types.ResponseTypeError,
				Content:      err.Error(),
				Done:         true,
			}
		}
	}()

	return streamChan, nil
}

// Đảm bảo mô hình khả dụng
func (c *OllamaChat) ensureModelAvailable(ctx context.Context) error {
	logger.GetLogger(ctx).Infof("ensuring model %s is available", c.modelName)
	return c.ollamaService.EnsureModelAvailable(ctx, c.modelName)
}

// GetModelName trả về tên mô hình
func (c *OllamaChat) GetModelName() string {
	return c.modelName
}

// GetModelID trả về ID mô hình
func (c *OllamaChat) GetModelID() string {
	return c.modelID
}

// toolFrom chuyển Tool của module này sang Tool của Ollama
func (c *OllamaChat) toolFrom(tools []Tool) ollamaapi.Tools {
	if len(tools) == 0 {
		return nil
	}
	ollamaTools := make(ollamaapi.Tools, 0, len(tools))
	for _, tool := range tools {
		function := ollamaapi.ToolFunction{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
		}
		if len(tool.Function.Parameters) > 0 {
			_ = json.Unmarshal(tool.Function.Parameters, &function.Parameters)
		}

		ollamaTools = append(ollamaTools, ollamaapi.Tool{
			Type:     tool.Type,
			Function: function,
		})
	}
	return ollamaTools
}

// toolTo chuyển Tool của Ollama sang Tool của module này
func (c *OllamaChat) toolTo(ollamaTools ollamaapi.Tools) []Tool {
	if len(ollamaTools) == 0 {
		return nil
	}
	tools := make([]Tool, 0, len(ollamaTools))
	for _, tool := range ollamaTools {
		paramsBytes, _ := json.Marshal(tool.Function.Parameters)
		tools = append(tools, Tool{
			Type: tool.Type,
			Function: FunctionDef{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  paramsBytes,
			},
		})
	}
	return tools
}

// toolCallFrom chuyển ToolCall của module này sang ToolCall của Ollama
func (c *OllamaChat) toolCallFrom(toolCalls []ToolCall) []ollamaapi.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	ollamaToolCalls := make([]ollamaapi.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		args := ollamaapi.NewToolCallFunctionArguments()
		if tc.Function.Arguments != "" {
			_ = args.UnmarshalJSON([]byte(tc.Function.Arguments))
		}
		ollamaToolCalls = append(ollamaToolCalls, ollamaapi.ToolCall{
			Function: ollamaapi.ToolCallFunction{
				Index:     tools2i(tc.ID),
				Name:      tc.Function.Name,
				Arguments: args,
			},
		})
	}
	return ollamaToolCalls
}

// toolCallTo chuyển ToolCall của Ollama sang ToolCall của module này
func (c *OllamaChat) toolCallTo(ollamaToolCalls []ollamaapi.ToolCall) []types.LLMToolCall {
	if len(ollamaToolCalls) == 0 {
		return nil
	}
	toolCalls := make([]types.LLMToolCall, 0, len(ollamaToolCalls))
	for _, tc := range ollamaToolCalls {
		argsBytes, _ := json.Marshal(tc.Function.Arguments)
		toolCalls = append(toolCalls, types.LLMToolCall{
			ID:   tooli2s(tc.Function.Index),
			Type: "function",
			Function: types.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: string(argsBytes),
			},
		})
	}
	return toolCalls
}

func tooli2s(i int) string {
	return strconv.Itoa(i)
}

func tools2i(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}
