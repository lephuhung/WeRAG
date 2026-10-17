package chat

import (
	"encoding/json"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// ConvertMessages chuyển định dạng tin nhắn sang định dạng OpenAI (export để lớp con dùng)
func (c *RemoteAPIChat) ConvertMessages(messages []Message) []openai.ChatCompletionMessage {
	openaiMessages := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, msg := range messages {
		msg = neutralizeMessageSpecialTokens(msg)
		openaiMsg := openai.ChatCompletionMessage{
			Role: msg.Role,
		}

		// Ưu tiên xử lý tin nhắn nhiều phần (có ảnh, ...)
		if len(msg.MultiContent) > 0 {
			openaiMsg.MultiContent = make([]openai.ChatMessagePart, 0, len(msg.MultiContent))
			for _, part := range msg.MultiContent {
				switch part.Type {
				case "text":
					openaiMsg.MultiContent = append(openaiMsg.MultiContent, openai.ChatMessagePart{
						Type: openai.ChatMessagePartTypeText,
						Text: part.Text,
					})
				case "image_url":
					if part.ImageURL != nil {
						openaiMsg.MultiContent = append(openaiMsg.MultiContent, openai.ChatMessagePart{
							Type: openai.ChatMessagePartTypeImageURL,
							ImageURL: &openai.ChatMessageImageURL{
								URL:    part.ImageURL.URL,
								Detail: openai.ImageURLDetail(part.ImageURL.Detail),
							},
						})
					}
				}
			}
		} else if len(msg.Images) > 0 && msg.Role == "user" {
			parts := make([]openai.ChatMessagePart, 0, len(msg.Images)+1)
			for _, imgURL := range msg.Images {
				resolved := resolveImageURLForLLM(imgURL)
				parts = append(parts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeImageURL,
					ImageURL: &openai.ChatMessageImageURL{
						URL:    resolved,
						Detail: openai.ImageURLDetailAuto,
					},
				})
			}
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeText,
				Text: msg.Content,
			})
			openaiMsg.MultiContent = parts
		} else if msg.Content != "" {
			openaiMsg.Content = msg.Content
		}

		if len(msg.ToolCalls) > 0 {
			openaiMsg.ToolCalls = make([]openai.ToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				toolType := openai.ToolType(tc.Type)
				openaiMsg.ToolCalls = append(openaiMsg.ToolCalls, openai.ToolCall{
					ID:   tc.ID,
					Type: toolType,
					Function: openai.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}

		if msg.Role == "tool" {
			openaiMsg.ToolCallID = msg.ToolCallID
			openaiMsg.Name = msg.Name
		}

		// Round-trip reasoning_content on assistant turns. MiMo and DeepSeek V3.2+
		// thinking mode reject multi-turn requests where the prior assistant
		// message lacks its reasoning_content with HTTP 400 ("The reasoning_content
		// in the thinking mode must be passed back to the API."). Providers that
		// don't recognize the field ignore it harmlessly.
		if msg.Role == "assistant" && msg.ReasoningContent != "" {
			openaiMsg.ReasoningContent = msg.ReasoningContent
		}

		openaiMessages = append(openaiMessages, openaiMsg)
	}
	return openaiMessages
}

// BuildChatCompletionRequest dựng tham số yêu cầu chat chuẩn (export để lớp con dùng).
//
// Tham số lấy mẫu (temperature / top_p / penalties) ánh xạ trực tiếp từ opts. Ngân sách hoàn thành được
// CompletionBudget gom thành một giá trị, rồi tuỳ nhà cung cấp chỉ ghi vào max_tokens hoặc
// max_completion_tokens (hai trường loại trừ nhau, xem #3014). Các xử lý riêng khác của nhà cung cấp
// (tham số lấy mẫu o-series / GPT-5, temperature cố định của Moonshot, ...) vẫn do
// providerAdapter.ShapeRequest áp dụng sau đó, xem provider.go.
func (c *RemoteAPIChat) BuildChatCompletionRequest(
	messages []Message, opts *ChatOptions, isStream bool,
) openai.ChatCompletionRequest {
	req := openai.ChatCompletionRequest{
		Model:    c.modelName,
		Messages: c.ConvertMessages(messages),
		Stream:   isStream,
	}

	if isStream {
		req.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	}

	if opts == nil {
		return req
	}

	req.Temperature = float32(opts.Temperature)
	if opts.TopP > 0 {
		req.TopP = float32(opts.TopP)
	}
	if opts.FrequencyPenalty > 0 {
		req.FrequencyPenalty = float32(opts.FrequencyPenalty)
	}
	if opts.PresencePenalty > 0 {
		req.PresencePenalty = float32(opts.PresencePenalty)
	}

	applyCompletionBudget(&req, opts.CompletionBudget(), wireCompletionTokenField(c.provider, c.modelName))

	// Xử lý Tools
	if len(opts.Tools) > 0 {
		req.Tools = make([]openai.Tool, 0, len(opts.Tools))
		for _, tool := range opts.Tools {
			toolType := openai.ToolType(tool.Type)
			openaiTool := openai.Tool{
				Type: toolType,
				Function: &openai.FunctionDefinition{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
				},
			}
			if tool.Function.Parameters != nil {
				openaiTool.Function.Parameters = tool.Function.Parameters
			}
			req.Tools = append(req.Tools, openaiTool)
		}
	}

	// Xử lý ParallelToolCalls
	if opts.ParallelToolCalls != nil {
		val := *opts.ParallelToolCalls
		req.ParallelToolCalls = val
	}

	// Xử lý ToolChoice (cài đặt chuẩn)
	if opts.ToolChoice != "" {
		switch opts.ToolChoice {
		case "none", "required", "auto":
			req.ToolChoice = opts.ToolChoice
		default:
			req.ToolChoice = openai.ToolChoice{
				Type: "function",
				Function: openai.ToolFunction{
					Name: opts.ToolChoice,
				},
			}
		}
	}

	if len(opts.Format) > 0 {
		req.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
		req.Messages[len(req.Messages)-1].Content += fmt.Sprintf("\nUse this JSON schema: %s", opts.Format)
	}

	return req
}

func (c *RemoteAPIChat) buildProviderOpenAIRequest(
	body any,
	openAIMessages []openai.ChatCompletionMessage,
	messages []Message,
) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal provider request: %w", err)
	}

	providerMessages := make([]map[string]any, 0, len(openAIMessages))
	for i, msg := range openAIMessages {
		msgData, err := json.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("marshal provider message: %w", err)
		}
		var msgMap map[string]any
		if err := json.Unmarshal(msgData, &msgMap); err != nil {
			return nil, fmt.Errorf("unmarshal provider message: %w", err)
		}

		if i < len(messages) && len(messages[i].ToolCalls) > 0 && len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, 0, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				tcData, err := json.Marshal(tc)
				if err != nil {
					return nil, fmt.Errorf("marshal provider tool call: %w", err)
				}
				var tcMap map[string]any
				if err := json.Unmarshal(tcData, &tcMap); err != nil {
					return nil, fmt.Errorf("unmarshal provider tool call: %w", err)
				}
				if j < len(messages[i].ToolCalls) {
					c.adapter.InjectToolCallMetadata(tcMap, messages[i].ToolCalls[j].ProviderMetadata)
				}
				toolCalls = append(toolCalls, tcMap)
			}
			msgMap["tool_calls"] = toolCalls
		}

		providerMessages = append(providerMessages, msgMap)
	}
	out["messages"] = providerMessages
	return out, nil
}

func (c *RemoteAPIChat) shapeProviderRequest(body any, req openai.ChatCompletionRequest, messages []Message) (any, error) {
	if !c.adapter.ForceRawHTTP() {
		return body, nil
	}
	return c.buildProviderOpenAIRequest(body, req.Messages, messages)
}
