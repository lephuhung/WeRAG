package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// ModelScopeBaseURL BaseURL API của ModelScope (chế độ tương thích OpenAI)
	ModelScopeBaseURL = "https://api-inference.modelscope.cn/v1"
)

// ModelScopeProvider triển khai giao diện Provider cho ModelScope (ModelScope)
type ModelScopeProvider struct{}

func init() {
	Register(&ModelScopeProvider{})
}

// Info trả về metadata của provider ModelScope
func (p *ModelScopeProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderModelScope,
		DisplayName: "ModelScope",
		Description: "Qwen/Qwen3-8B, Qwen/Qwen3-Embedding-8B, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: ModelScopeBaseURL,
			types.ModelTypeEmbedding:   ModelScopeBaseURL,
			types.ModelTypeVLLM:        ModelScopeBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeEmbedding,
			types.ModelTypeVLLM,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider ModelScope
func (p *ModelScopeProvider) ValidateConfig(config *Config) error {
	if config.BaseURL == "" {
		return fmt.Errorf("base URL is required for ModelScope provider")
	}
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for ModelScope provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
