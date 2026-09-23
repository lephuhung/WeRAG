package vlm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/provider"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	openai "github.com/sashabaranov/go-openai"
)

const (
	// defaultTimeout is the fallback HTTP timeout for a single VLM request.
	// Dense scanned-PDF OCR (full-page text + layout extraction) can take well
	// over a minute on slow endpoints, so this is intentionally generous and
	// can be raised further via VLM_HTTP_TIMEOUT_SECONDS.
	defaultTimeout = 180 * time.Second
	defaultMaxToks = 5000
	defaultTemp    = float32(0.1)
)

// vlmHTTPTimeout returns the HTTP client timeout for VLM requests, read from
// the VLM_HTTP_TIMEOUT_SECONDS env var when set (and positive), falling back to
// defaultTimeout otherwise. Shared by all OpenAI-compatible VLM backends.
func vlmHTTPTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("VLM_HTTP_TIMEOUT_SECONDS")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return defaultTimeout
}

// RemoteAPIVLM implements VLM via an OpenAI-compatible chat completions API.
type RemoteAPIVLM struct {
	modelName   string
	modelID     string
	client      *openai.Client
	baseURL     string
	temperature float32
	// repPenalty is an optional repetition_penalty sent on the request wire.
	// go-openai has no field for it, so when set (and non-1) Predict falls
	// back to a raw HTTP call. Configure via the model's extra_config
	// "repetition_penalty" — e.g. 1.05 breaks SenOCR-Vi's degenerate loops.
	repPenalty float32
	apiKey     string
	httpClient *http.Client
}

// NewRemoteAPIVLM creates a remote-API backed VLM instance.
func NewRemoteAPIVLM(config *Config) (*RemoteAPIVLM, error) {
	if err := validateVLMBaseURL(config.BaseURL); err != nil {
		return nil, err
	}

	providerName := provider.ProviderName(config.Provider)
	if providerName == "" {
		providerName = provider.DetectProvider(config.BaseURL)
	}

	var apiCfg openai.ClientConfig
	if providerName == provider.ProviderAzureOpenAI {
		apiCfg = openai.DefaultAzureConfig(config.APIKey, config.BaseURL)
		apiCfg.AzureModelMapperFunc = func(model string) string {
			return model
		}
		if config.Extra != nil {
			if v, ok := config.Extra["api_version"]; ok {
				if vs, ok := v.(string); ok && vs != "" {
					apiCfg.APIVersion = vs
				}
			}
		}
	} else {
		apiCfg = openai.DefaultConfig(config.APIKey)
		if config.BaseURL != "" {
			apiCfg.BaseURL = config.BaseURL
		}
	}
	httpClient := newVLMHTTPClient(vlmHTTPTimeout())

	// 注入用户自定义 HTTP header（类似 OpenAI Python SDK 的 extra_headers）
	if len(config.CustomHeaders) > 0 {
		httpClient = secutils.WrapHTTPClientWithHeaders(httpClient, config.CustomHeaders)
	}
	apiCfg.HTTPClient = httpClient

	temp := defaultTemp
	var repPenalty float32
	if config.Extra != nil {
		if v, ok := config.Extra["temperature"]; ok {
			if vs, ok := v.(string); ok {
				if f, err := strconv.ParseFloat(vs, 32); err == nil {
					temp = float32(f)
				}
			}
		}
		if v, ok := config.Extra["repetition_penalty"]; ok {
			if vs, ok := v.(string); ok {
				if f, err := strconv.ParseFloat(vs, 32); err == nil {
					repPenalty = float32(f)
				}
			}
		}
	}

	return &RemoteAPIVLM{
		modelName:   config.ModelName,
		modelID:     config.ModelID,
		client:      openai.NewClientWithConfig(apiCfg),
		baseURL:     config.BaseURL,
		temperature: temp,
		repPenalty:  repPenalty,
		apiKey:      config.APIKey,
		httpClient:  httpClient,
	}, nil
}

// Predict sends an image with a text prompt to the OpenAI-compatible API.
func (v *RemoteAPIVLM) Predict(ctx context.Context, imgBytesList [][]byte, prompt string) (string, error) {
	var parts []openai.ChatMessagePart

	// Add text prompt first
	parts = append(parts, openai.ChatMessagePart{
		Type: openai.ChatMessagePartTypeText,
		Text: prompt,
	})

	// Add images
	for _, imgBytes := range imgBytesList {
		if len(imgBytes) > 0 {
			mimeType := detectImageMIME(imgBytes)
			b64 := base64.StdEncoding.EncodeToString(imgBytes)
			dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL:    dataURI,
					Detail: openai.ImageURLDetailAuto,
				},
			})
		}
	}

	req := openai.ChatCompletionRequest{
		Model: v.modelName,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:         openai.ChatMessageRoleUser,
				MultiContent: parts,
			},
		},
		MaxTokens:   defaultMaxToks,
		Temperature: v.temperature,
	}
	shapeReasoningVLMRequest(&req)

	if v.repPenalty > 0 && v.repPenalty != 1.0 {
		// repetition_penalty is not in the go-openai request struct; send the
		// equivalent payload over raw HTTP instead of silently dropping it.
		return v.predictRaw(ctx, parts)
	}

	totalImageSize := 0
	for _, img := range imgBytesList {
		totalImageSize += len(img)
	}
	logger.Infof(ctx, "[VLM] Calling OpenAI-compatible API, model=%s, baseURL=%s, numImages=%d, totalImageSize=%d",
		v.modelName, v.baseURL, len(imgBytesList), totalImageSize)

	resp, err := v.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("OpenAI VLM request: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("OpenAI VLM returned no choices")
	}

	choice := resp.Choices[0]
	content := choice.Message.Content
	if strings.TrimSpace(content) == "" && choice.FinishReason == openai.FinishReasonLength {
		// Reasoning models spend max_completion_tokens on reasoning before any
		// visible output, so an exhausted budget yields an empty message rather
		// than an API error. Returning "" here would be recorded as
		// "no_extracted_content" and look identical to an image with no text.
		return "", fmt.Errorf(
			"OpenAI VLM returned no content: completion truncated at %d tokens (finish_reason=length)",
			defaultMaxToks,
		)
	}
	logger.Infof(ctx, "[VLM] OpenAI response received, len=%d", len(content))
	return content, nil
}

// predictRaw mirrors Predict but marshals the request body by hand so
// OpenAI-extension fields go-openai does not model (repetition_penalty) can
// be sent to compatible endpoints (vLLM).
func (v *RemoteAPIVLM) predictRaw(ctx context.Context, parts []openai.ChatMessagePart) (string, error) {
	content := make([]any, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case openai.ChatMessagePartTypeText:
			content = append(content, map[string]any{"type": "text", "text": p.Text})
		case openai.ChatMessagePartTypeImageURL:
			if p.ImageURL != nil {
				content = append(content, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": p.ImageURL.URL, "detail": string(p.ImageURL.Detail)},
				})
			}
		}
	}
	payload, err := json.Marshal(map[string]any{
		"model": v.modelName,
		"messages": []any{
			map[string]any{"role": "user", "content": content},
		},
		"max_tokens":         defaultMaxToks,
		"temperature":        v.temperature,
		"repetition_penalty": v.repPenalty,
	})
	if err != nil {
		return "", fmt.Errorf("marshal VLM request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		strings.TrimRight(v.baseURL, "/")+"/chat/completions",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", fmt.Errorf("create VLM request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if v.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+v.apiKey)
	}

	resp, err := v.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("OpenAI VLM request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read VLM response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI VLM request: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("OpenAI VLM response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("OpenAI VLM returned no choices")
	}

	choice := parsed.Choices[0]
	var text string
	if err := json.Unmarshal(choice.Message.Content, &text); err != nil {
		var parts2 []struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(choice.Message.Content, &parts2); err != nil {
			return "", fmt.Errorf("unrecognized VLM response content")
		}
		for _, p := range parts2 {
			text += p.Text
		}
	}
	if strings.TrimSpace(text) == "" && choice.FinishReason == "length" {
		return "", fmt.Errorf(
			"OpenAI VLM returned no content: completion truncated at %d tokens (finish_reason=length)",
			defaultMaxToks,
		)
	}
	logger.Infof(ctx, "[VLM] OpenAI response received, len=%d", len(text))
	return text, nil
}

// shapeReasoningVLMRequest adapts an OpenAI-compatible VLM request for
// reasoning (o-series) and GPT-5 models, which reject `max_tokens` and every
// non-default sampling parameter.
//
// This mirrors shapeOpenAIReasoning in internal/models/chat, which fixed the
// same incompatibility on the chat path for issue #1283. The VLM path was
// never wired to it, so image OCR and captioning failed for every one of these
// models (issue #2537).
//
// Both quirks have to be handled together: migrating max_tokens alone still
// fails, because the VLM default temperature (0.1) is itself rejected.
func shapeReasoningVLMRequest(req *openai.ChatCompletionRequest) {
	if !provider.IsOpenAIReasoningOrGPT5Model(req.Model) {
		return
	}
	if req.MaxCompletionTokens == 0 && req.MaxTokens > 0 {
		req.MaxCompletionTokens = req.MaxTokens
	}
	req.MaxTokens = 0
	req.Temperature = 0
	req.TopP = 0
	req.FrequencyPenalty = 0
	req.PresencePenalty = 0
}

func (v *RemoteAPIVLM) GetModelName() string { return v.modelName }
func (v *RemoteAPIVLM) GetModelID() string   { return v.modelID }

// detectImageMIME returns the MIME type for the given image bytes.
func detectImageMIME(data []byte) string {
	ct := http.DetectContentType(data)
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	return "image/png"
}
