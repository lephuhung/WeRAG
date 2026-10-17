package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/sashabaranov/go-openai"
)

// RemoteAPIChat cài đặt trò chuyện dựa trên API tương thích OpenAI.
// Bản thân nó chỉ lo phần yêu cầu/phản hồi/stream chung; mọi hành vi riêng của provider được uỷ quyền cho
// providerAdapter (xem provider.go), việc mã hoá thinking được uỷ quyền cho ThinkingStrategy
// (xem thinking.go).
type RemoteAPIChat struct {
	modelName string
	client    *openai.Client
	modelID   string
	baseURL   string
	apiKey    string
	provider  provider.ProviderName
	appID     string
	appSecret string
	// customHeaders là header HTTP tuỳ chỉnh người dùng đặt trong cấu hình mô hình (giống extra_headers của OpenAI Python SDK).
	customHeaders map[string]string

	// adapter chứa mọi hành vi riêng của provider (thinking / xử lý tham số riêng / endpoint / xác thực / biến đổi tin nhắn).
	adapter providerAdapter
	// thinkingOverride lấy từ extra_config.thinking_control, khác nil thì ghi đè adapter.Thinking().
	thinkingOverride ThinkingStrategy
}

// NewRemoteAPIChat tạo instance trò chuyện qua API từ xa
func NewRemoteAPIChat(chatConfig *ChatConfig) (*RemoteAPIChat, error) {
	if chatConfig.BaseURL != "" {
		if err := secutils.ValidateURLForSSRF(chatConfig.BaseURL); err != nil {
			return nil, fmt.Errorf("baseURL SSRF check failed: %w", err)
		}
	}

	apiKey := chatConfig.APIKey
	providerName := provider.ProviderName(chatConfig.Provider)
	if providerName == "" {
		providerName = provider.DetectProvider(chatConfig.BaseURL)
	}

	var config openai.ClientConfig
	if providerName == provider.ProviderAzureOpenAI {
		config = openai.DefaultAzureConfig(apiKey, chatConfig.BaseURL)
		config.AzureModelMapperFunc = func(model string) string {
			return model
		}
		if chatConfig.ExtraConfig != nil {
			if v, ok := chatConfig.ExtraConfig["api_version"]; ok {
				config.APIVersion = v
			}
		}
	} else {
		config = openai.DefaultConfig(apiKey)
		if baseURL := chatConfig.BaseURL; baseURL != "" {
			config.BaseURL = baseURL
		} else if providerName == provider.ProviderDeepSeek {
			config.BaseURL = provider.DeepSeekBaseURL
		}
	}

	// The SDK must use the same SSRF-safe transport as the raw HTTP paths.
	// Constructor-time URL validation alone cannot prevent DNS rebinding or a
	// later redirect to an internal address.
	sdkHTTPClient := rawHTTPClient
	// Nếu có CustomHeaders thì gắn thêm một lớp RoundTripper vào HTTPClient của SDK,
	// tự chèn các header này vào mỗi yêu cầu (đường HTTP thô xử lý riêng trước khi gửi).
	if len(chatConfig.CustomHeaders) > 0 {
		sdkHTTPClient = secutils.WrapHTTPClientWithHeaders(sdkHTTPClient, chatConfig.CustomHeaders)
	}
	config.HTTPClient = sdkHTTPClient

	modelName := chatConfig.ModelName
	if chatConfig.ExtraConfig != nil {
		if override := strings.TrimSpace(chatConfig.ExtraConfig["remote_model_name"]); override != "" {
			modelName = override
		}
	}
	if providerName == provider.ProviderWeKnoraCloud {
		if chatConfig.AppID == "" {
			return nil, fmt.Errorf("WeKnoraCloud provider: AppID is required")
		}
		if chatConfig.AppSecret == "" {
			return nil, fmt.Errorf("WeKnoraCloud provider: AppSecret is required")
		}
	}

	return &RemoteAPIChat{
		modelName:        modelName,
		client:           openai.NewClientWithConfig(config),
		modelID:          chatConfig.ModelID,
		baseURL:          strings.TrimRight(config.BaseURL, "/"),
		apiKey:           apiKey,
		provider:         providerName,
		appID:            chatConfig.AppID,
		appSecret:        chatConfig.AppSecret,
		customHeaders:    chatConfig.CustomHeaders,
		adapter:          resolveProvider(providerName, modelName),
		thinkingOverride: parseThinkingOverride(chatConfig.ExtraConfig),
	}, nil
}

// authCreds bundles the credentials passed to the adapter's Auth method.
func (c *RemoteAPIChat) authCreds() authCreds {
	return authCreds{APIKey: c.apiKey, AppID: c.appID, AppSecret: c.appSecret}
}

// shapedRequest builds the standard request and applies the adapter's message
// transform and parameter shaping (but not thinking, which may wrap the body).
func (c *RemoteAPIChat) shapedRequest(messages []Message, opts *ChatOptions, isStream bool) openai.ChatCompletionRequest {
	req := c.BuildChatCompletionRequest(messages, opts, isStream)
	req.Messages = c.adapter.TransformMessages(req.Messages)
	c.adapter.ShapeRequest(&req, opts, isStream)
	return req
}

// buildOutbound assembles the final outbound request: the body to send, the
// endpoint override (empty for the standard endpoint), and whether the raw HTTP
// path is required. This is the single place that composes adapter + thinking,
// replacing the former buildRequestCustomizer plumbing.
func (c *RemoteAPIChat) buildOutbound(
	ctx context.Context, messages []Message, opts *ChatOptions, isStream bool,
) (body any, endpoint string, useRawHTTP bool, err error) {
	req := c.shapedRequest(messages, opts, isStream)

	thinking := c.thinkingOverride
	if thinking == nil {
		thinking = c.adapter.Thinking()
	}
	customBody, useRaw := thinking.Apply(&req, opts, isStream)

	body = &req
	if customBody != nil {
		body = customBody
	}
	body, err = c.shapeProviderRequest(body, req, messages)
	if err != nil {
		return nil, "", false, err
	}

	retention := resolveCacheRetention(opts)
	policy := promptCachePolicyFor(c.provider, c.baseURL)
	sessionID := promptCacheSessionID(ctx, opts)
	cachedBody, forceRaw, err := applyPromptCacheToJSONBody(body, policy, sessionID, retention)
	if err != nil {
		return nil, "", false, err
	}
	body = cachedBody

	endpoint = c.adapter.Endpoint(c.baseURL, c.modelID, isStream)
	useRawHTTP = useRaw || c.adapter.ForceRawHTTP() || endpoint != "" || forceRaw
	return body, endpoint, useRawHTTP, nil
}

// logRequest ghi log yêu cầu
func (c *RemoteAPIChat) logRequest(ctx context.Context, req any, isStream bool) {
	if jsonData, err := json.MarshalIndent(req, "", "  "); err == nil {
		logger.Infof(ctx, "[LLM Request] model=%s, stream=%v, request:\n%s",
			c.modelName, isStream, secutils.CompactImageDataURLForLog(string(jsonData)))
	}
}

// Chat thực hiện trò chuyện không stream
func (c *RemoteAPIChat) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*types.ChatResponse, error) {
	// Chỉ thêm timeout dự phòng khi bên gọi chưa đặt deadline, tránh yêu cầu treo chặn worker mãi;
	// nếu bên gọi đã đặt deadline ngắn hơn hay dài hơn thì đều được tôn trọng nguyên vẹn.
	timeoutCtx, cancel := withLLMTimeout(ctx, defaultChatTimeout)
	defer cancel()

	body, endpoint, useRawHTTP, err := c.buildOutbound(timeoutCtx, messages, opts, false)
	if err != nil {
		return nil, err
	}
	if useRawHTTP {
		return c.chatWithRawHTTP(timeoutCtx, endpoint, body, opts)
	}

	req := *(body.(*openai.ChatCompletionRequest))
	c.logRequest(timeoutCtx, req, false)
	resp, err := c.client.CreateChatCompletion(timeoutCtx, req)
	if err != nil {
		if isMultimodalNotSupportedError(err) {
			logger.Warnf(timeoutCtx, "[LLM Request] Model %s does not support multimodal, retrying without images", c.modelName)
			cleaned := stripImagesFromMessages(messages)
			req = c.shapedRequest(cleaned, opts, false)
			resp, err = c.client.CreateChatCompletion(timeoutCtx, req)
		}
		if err != nil {
			return nil, fmt.Errorf("create chat completion: %w", err)
		}
	}

	result, err := c.parseCompletionResponse(&resp)
	if err != nil {
		return nil, err
	}
	logUsage(timeoutCtx, c.modelName, &result.Usage)
	return result, nil
}

// chatWithRawHTTP trò chuyện bằng yêu cầu HTTP thô (dùng cho yêu cầu tuỳ chỉnh)
func (c *RemoteAPIChat) chatWithRawHTTP(ctx context.Context, endpoint string, customReq any, opts *ChatOptions) (*types.ChatResponse, error) {
	jsonData, err := json.Marshal(customReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if endpoint == "" {
		endpoint = c.baseURL + "/chat/completions"
	}
	if err := secutils.ValidateURLForSSRF(endpoint); err != nil {
		return nil, fmt.Errorf("endpoint SSRF check failed: %w", err)
	}
	logger.Infof(ctx, "[LLM Request] Remote HTTP, endpoint=%s, model=%s, raw HTTP request:\n%s",
		endpoint, c.modelName, secutils.CompactImageDataURLForLog(string(jsonData)))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	c.adapter.Auth(httpReq, c.authCreds(), jsonData)

	// Chèn header tuỳ chỉnh của người dùng (các header dành riêng được bỏ qua tự động trong hàm tiện ích)
	secutils.ApplyCustomHeaders(httpReq, c.customHeaders)
	attachPromptCacheHeaders(httpReq, promptCachePolicyFor(c.provider, c.baseURL), promptCacheSessionID(ctx, opts))

	logger.Infof(ctx, "[LLM Request] Remote HTTP, endpoint=%s, model=%s",
		endpoint, c.modelName)

	resp, err := rawHTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var chatResp openai.ChatCompletionResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result, err := c.parseCompletionResponse(&chatResp)
	if err != nil {
		return nil, err
	}
	c.applyCompletionToolCallMetadata(body, result)
	applyRawPromptCacheUsage(body, &result.Usage)
	logUsage(ctx, c.modelName, &result.Usage)
	return result, nil
}

// ChatStream thực hiện trò chuyện dạng stream
func (c *RemoteAPIChat) ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan types.StreamResponse, error) {
	// Chỉ thêm timeout dự phòng khi bên gọi chưa đặt deadline; gọi stream có timeout mặc định dài hơn,
	// vì mô hình có suy luận có thể mất hàng chục giây đến vài phút mới ra token đầu tiên.
	timeoutCtx, cancel := withLLMTimeout(ctx, defaultStreamTimeout)

	body, endpoint, useRawHTTP, err := c.buildOutbound(timeoutCtx, messages, opts, true)
	if err != nil {
		cancel()
		return nil, err
	}
	if useRawHTTP {
		ch, err := c.chatStreamWithRawHTTP(timeoutCtx, endpoint, body, opts)
		return wrapStreamCancel(ch, err, cancel)
	}

	req := *(body.(*openai.ChatCompletionRequest))
	c.logRequest(timeoutCtx, req, true)

	streamDumper := newStreamPacketDumper(c.modelName, &req)
	if streamDumper != nil {
		logger.Infof(timeoutCtx, "[LLM Stream Raw Dump] writing packets to %s", streamDumper.Path())
	}

	streamChan := make(chan types.StreamResponse)

	stream, err := c.client.CreateChatCompletionStream(timeoutCtx, req)
	if err != nil {
		if isMultimodalNotSupportedError(err) {
			logger.Warnf(timeoutCtx, "[LLM Stream] Model %s does not support multimodal, retrying without images", c.modelName)
			cleaned := stripImagesFromMessages(messages)
			req = c.shapedRequest(cleaned, opts, true)
			stream, err = c.client.CreateChatCompletionStream(timeoutCtx, req)
		}
		if err != nil {
			cancel()
			close(streamChan)
			return nil, fmt.Errorf("create chat completion stream: %w", err)
		}
	}

	go func() {
		defer cancel()
		if streamDumper != nil {
			defer streamDumper.Close()
		}
		c.processStream(timeoutCtx, stream, streamChan, streamDumper)
	}()

	return streamChan, nil
}

// wrapStreamCancel gọi cancel sau khi channel con đóng, tránh rò rỉ timeout context.
// Khi lời gọi bên dưới trả về error ngay thì gọi cancel lập tức và chuyển error ra ngoài.
func wrapStreamCancel(in <-chan types.StreamResponse, err error, cancel context.CancelFunc) (<-chan types.StreamResponse, error) {
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan types.StreamResponse)
	go func() {
		defer cancel()
		defer close(out)
		for v := range in {
			out <- v
		}
	}()
	return out, nil
}

// chatStreamWithRawHTTP trò chuyện dạng stream bằng yêu cầu HTTP thô
func (c *RemoteAPIChat) chatStreamWithRawHTTP(ctx context.Context, endpoint string, customReq any, opts *ChatOptions) (<-chan types.StreamResponse, error) {
	jsonData, err := json.Marshal(customReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if endpoint == "" {
		endpoint = c.baseURL + "/chat/completions"
	}
	if err := secutils.ValidateURLForSSRF(endpoint); err != nil {
		return nil, fmt.Errorf("endpoint SSRF check failed: %w", err)
	}

	if prettyJSON, pErr := json.MarshalIndent(customReq, "", "  "); pErr == nil {
		logger.Infof(ctx, "[LLM Stream Request] endpoint=%s, model=%s, stream=true, request:\n%s",
			endpoint, c.modelName, secutils.CompactImageDataURLForLog(string(prettyJSON)))
	} else {
		logger.Infof(ctx, "[LLM Stream] endpoint=%s, model=%s", endpoint, c.modelName)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	c.adapter.Auth(httpReq, c.authCreds(), jsonData)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Chèn header tuỳ chỉnh của người dùng (các header dành riêng được bỏ qua tự động trong hàm tiện ích)
	secutils.ApplyCustomHeaders(httpReq, c.customHeaders)
	attachPromptCacheHeaders(httpReq, promptCachePolicyFor(c.provider, c.baseURL), promptCacheSessionID(ctx, opts))

	resp, err := rawHTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	streamChan := make(chan types.StreamResponse)
	streamDumper := newStreamPacketDumper(c.modelName, customReq)
	if streamDumper != nil {
		logger.Infof(ctx, "[LLM Stream Raw Dump] writing packets to %s", streamDumper.Path())
	}

	go func() {
		if streamDumper != nil {
			defer streamDumper.Close()
		}
		c.processRawHTTPStream(ctx, resp, streamChan, streamDumper)
	}()

	return streamChan, nil
}

// GetModelName trả về tên mô hình
func (c *RemoteAPIChat) GetModelName() string {
	return c.modelName
}

// GetModelID trả về ID mô hình
func (c *RemoteAPIChat) GetModelID() string {
	return c.modelID
}

// GetProvider trả về tên provider
func (c *RemoteAPIChat) GetProvider() provider.ProviderName {
	return c.provider
}

// GetBaseURL trả về baseURL
func (c *RemoteAPIChat) GetBaseURL() string {
	return c.baseURL
}

// GetAPIKey trả về apiKey
func (c *RemoteAPIChat) GetAPIKey() string {
	return c.apiKey
}
