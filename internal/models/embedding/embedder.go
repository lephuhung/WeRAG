package embedding

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
)

// Embedder defines the interface for text vectorization
type Embedder interface {
	// Embed converts text to vector
	Embed(ctx context.Context, text string) ([]float32, error)

	// BatchEmbed converts multiple texts to vectors in batch
	BatchEmbed(ctx context.Context, texts []string) ([][]float32, error)

	// GetModelName returns the model name
	GetModelName() string

	// GetDimensions returns the vector dimensions
	GetDimensions() int

	// GetModelID returns the model ID
	GetModelID() string

	EmbedderPooler
}

type EmbedderPooler interface {
	BatchEmbedWithPool(ctx context.Context, model Embedder, texts []string) ([][]float32, error)
}

// EmbedderType represents the embedder type
type EmbedderType string

// Config represents the embedder configuration
type Config struct {
	Source                    types.ModelSource `json:"source"`
	BaseURL                   string            `json:"base_url"`
	ModelName                 string            `json:"model_name"`
	APIKey                    string            `json:"api_key"`
	TruncatePromptTokens      int               `json:"truncate_prompt_tokens"`
	Dimensions                int               `json:"dimensions"`
	SupportsDimensionOverride bool              `json:"supports_dimension_override"`
	ModelID                   string            `json:"model_id"`
	Provider                  string            `json:"provider"`
	// MaxConcurrency caps concurrent background calls to this model; 0 falls
	// back to the process-wide default (see limiter.GateN).
	MaxConcurrency int               `json:"max_concurrency"`
	ExtraConfig    map[string]string `json:"extra_config"`
	// CustomHeaders cho phép đính kèm HTTP header tùy chỉnh khi gọi API từ xa (tương tự extra_headers của OpenAI Python SDK).
	CustomHeaders map[string]string `json:"custom_headers"`
	AppID         string
	AppSecret     string // giá trị mã hóa, bên gọi hàm factory truyền vào, đã giải mã trước khi dùng
}

// ConfigFromModel dựng embedding.Config từ types.Model.
// Đường chạy thật (nạp từ DB) và đường kiểm tra kết nối (biểu mẫu tạm) dùng chung ánh xạ này.
// appID / appSecret là thông tin WeKnoraCloud đã giải mã, bên gọi chịu trách nhiệm truyền vào.
func ConfigFromModel(m *types.Model, appID, appSecret string) Config {
	if m == nil {
		return Config{}
	}
	return Config{
		Source:                    m.Source,
		BaseURL:                   m.Parameters.BaseURL,
		APIKey:                    m.Parameters.APIKey,
		ModelID:                   m.ID,
		ModelName:                 m.Name,
		Dimensions:                m.Parameters.EmbeddingParameters.Dimension,
		SupportsDimensionOverride: m.Parameters.EmbeddingParameters.SupportsDimensionOverride,
		TruncatePromptTokens:      m.Parameters.EmbeddingParameters.TruncatePromptTokens,
		Provider:                  m.Parameters.Provider,
		MaxConcurrency:            m.Parameters.MaxConcurrency,
		ExtraConfig:               m.Parameters.ExtraConfig,
		CustomHeaders:             m.Parameters.CustomHeaders,
		AppID:                     appID,
		AppSecret:                 appSecret,
	}
}

// NewEmbedder creates an embedder based on the configuration
func NewEmbedder(config Config, pooler EmbedderPooler, ollamaService *ollama.OllamaService) (Embedder, error) {
	e, err := newEmbedder(config, pooler, ollamaService)
	if err != nil {
		return e, err
	}
	if setter, ok := e.(interface{ SetSupportsDimensionOverride(bool) }); ok {
		setter.SetSupportsDimensionOverride(config.SupportsDimensionOverride)
	}
	// Innermost: gate the real provider round-trips (including the per-sub-batch
	// pool callbacks) before debug/langfuse wrap for logging/tracing. See
	// concurrencyEmbedder for why this sits below the observability decorators.
	e = wrapEmbeddingConcurrency(e, config.MaxConcurrency)
	if logger.LLMDebugEnabled() {
		e = &debugEmbedder{inner: e}
	}
	if langfuse.GetManager().Enabled() {
		e = &langfuseEmbedder{inner: e}
	}
	return e, nil
}

func newEmbedder(config Config, pooler EmbedderPooler, ollamaService *ollama.OllamaService) (Embedder, error) {
	var embedder Embedder
	var err error
	switch strings.ToLower(string(config.Source)) {
	case string(types.ModelSourceLocal):
		embedder, err = NewOllamaEmbedder(config.BaseURL,
			config.ModelName, config.TruncatePromptTokens, config.Dimensions, config.ModelID, pooler, ollamaService)
		return embedder, err
	case string(types.ModelSourceRemote):
		// Detect or use configured provider for routing
		providerName := provider.ProviderName(config.Provider)
		if providerName == "" {
			providerName = provider.DetectProvider(config.BaseURL)
		}

		// Route to provider-specific embedders
		switch providerName {
		case provider.ProviderAliyun:
			// Kiểm tra có phải mô hình embedding đa phương thức không
			// Mô hình đa phương thức: tongyi-embedding-vision-*, multimodal-embedding-*
			// Mô hình chỉ văn bản: text-embedding-v1/v2/v3/v4 phải dùng giao diện tương thích OpenAI, nếu không định dạng phản hồi lệch và embedding trả về mảng rỗng
			isMultimodalModel := strings.Contains(strings.ToLower(config.ModelName), "vision") ||
				strings.Contains(strings.ToLower(config.ModelName), "multimodal")

			if isMultimodalModel {
				// Mô hình đa phương thức cần endpoint API riêng của DashScope
				// Nếu người dùng điền URL chế độ tương thích OpenAI, tự sửa thành baseURL của API đa phương thức
				baseURL := config.BaseURL
				if baseURL == "" {
					baseURL = "https://dashscope.aliyuncs.com"
				} else if strings.Contains(baseURL, "/compatible-mode/") {
					// Bỏ đường dẫn compatible-mode, AliyunEmbedder sẽ tự thêm endpoint đa phương thức
					baseURL = strings.Replace(baseURL, "/compatible-mode/v1", "", 1)
					baseURL = strings.Replace(baseURL, "/compatible-mode", "", 1)
				}
				aliyunEmb, aErr := NewAliyunEmbedder(config.APIKey,
					baseURL,
					config.ModelName,
					config.TruncatePromptTokens,
					config.Dimensions,
					config.ModelID,
					pooler)
				if aliyunEmb != nil {
					aliyunEmb.SetCustomHeaders(config.CustomHeaders)
				}
				embedder, err = aliyunEmb, aErr
			} else {
				baseURL := config.BaseURL
				if baseURL == "" || !strings.Contains(baseURL, "/compatible-mode/") {
					baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
				}
				openaiEmb, oErr := NewOpenAIEmbedder(config.APIKey,
					baseURL,
					config.ModelName,
					config.TruncatePromptTokens,
					config.Dimensions,
					config.ModelID,
					pooler)
				if openaiEmb != nil {
					openaiEmb.SetCustomHeaders(config.CustomHeaders)
				}
				embedder, err = openaiEmb, oErr
			}
			return embedder, err
		case provider.ProviderVolcengine:
			// Volcengine Ark uses multimodal embedding API
			volcEmb, vErr := NewVolcengineEmbedder(config.APIKey,
				config.BaseURL,
				config.ModelName,
				config.TruncatePromptTokens,
				config.Dimensions,
				config.ModelID,
				pooler)
			if volcEmb != nil {
				volcEmb.SetCustomHeaders(config.CustomHeaders)
			}
			embedder, err = volcEmb, vErr
			return embedder, err
		case provider.ProviderJina:
			// Jina AI uses different API format (truncate instead of truncate_prompt_tokens)
			jinaEmb, jErr := NewJinaEmbedder(config.APIKey,
				config.BaseURL,
				config.ModelName,
				config.TruncatePromptTokens,
				config.Dimensions,
				config.ModelID,
				pooler)
			if jinaEmb != nil {
				jinaEmb.SetCustomHeaders(config.CustomHeaders)
			}
			embedder, err = jinaEmb, jErr
			return embedder, err
		case provider.ProviderAzureOpenAI:
			apiVersion := "2024-10-21"
			if config.ExtraConfig != nil {
				if v, ok := config.ExtraConfig["api_version"]; ok {
					apiVersion = v
				}
			}
			azureEmb, azErr := NewAzureOpenAIEmbedder(config.APIKey,
				config.BaseURL,
				config.ModelName,
				config.TruncatePromptTokens,
				config.Dimensions,
				config.ModelID,
				apiVersion,
				pooler)
			if azureEmb != nil {
				azureEmb.SetCustomHeaders(config.CustomHeaders)
			}
			embedder, err = azureEmb, azErr
			return embedder, err
		case provider.ProviderNvidia:
			nvEmb, nErr := NewNvidiaEmbedder(config.APIKey,
				config.BaseURL,
				config.ModelName,
				config.Dimensions,
				config.ModelID,
				pooler)
			if nvEmb != nil {
				nvEmb.SetCustomHeaders(config.CustomHeaders)
			}
			embedder, err = nvEmb, nErr
			return embedder, err
		case provider.ProviderGemini:
			geminiEmb, gErr := NewGeminiEmbedder(config.APIKey,
				config.BaseURL,
				config.ModelName,
				config.TruncatePromptTokens,
				config.Dimensions,
				config.ModelID,
				pooler)
			if geminiEmb != nil {
				geminiEmb.SetCustomHeaders(config.CustomHeaders)
			}
			embedder, err = geminiEmb, gErr
			return embedder, err
		case provider.ProviderZhipu:
			zhipuEmb, zErr := NewZhipuEmbedder(config.APIKey,
				config.BaseURL,
				config.ModelName,
				config.TruncatePromptTokens,
				config.Dimensions,
				config.ModelID,
				pooler)
			if zhipuEmb != nil {
				zhipuEmb.SetCustomHeaders(config.CustomHeaders)
			}
			embedder, err = zhipuEmb, zErr
			return embedder, err
		case provider.ProviderWeKnoraCloud:
			embedder, err = NewWeKnoraCloudEmbedder(config)
			return embedder, err
		default:
			// Use OpenAI-compatible embedder for other providers
			openaiEmb, oErr := NewOpenAIEmbedder(config.APIKey,
				config.BaseURL,
				config.ModelName,
				config.TruncatePromptTokens,
				config.Dimensions,
				config.ModelID,
				pooler)
			if openaiEmb != nil {
				openaiEmb.SetCustomHeaders(config.CustomHeaders)
			}
			embedder, err = openaiEmb, oErr
			return embedder, err
		}
	default:
		return nil, fmt.Errorf("unsupported embedder source: %s", config.Source)
	}
}
