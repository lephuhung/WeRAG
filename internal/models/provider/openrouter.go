package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	OpenRouterBaseURL = "https://openrouter.ai/api/v1"
)

// OpenRouterProvider triển khai giao diện Provider cho OpenRouter
type OpenRouterProvider struct{}

func init() {
	Register(&OpenRouterProvider{})
}

// Info trả về metadata của provider OpenRouter
func (p *OpenRouterProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderOpenRouter,
		DisplayName: "OpenRouter",
		Description: "openai/gpt-5.2-chat, google/gemini-3-flash-preview, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: OpenRouterBaseURL,
			types.ModelTypeEmbedding:   OpenRouterBaseURL,
			types.ModelTypeVLLM:        OpenRouterBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeEmbedding,
			types.ModelTypeVLLM,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider OpenRouter
func (p *OpenRouterProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for OpenRouter provider")
	}
	return nil
}
