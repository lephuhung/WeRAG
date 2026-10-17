package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// DeepSeekBaseURL BaseURL API chính thức của DeepSeek
	DeepSeekBaseURL = "https://api.deepseek.com/v1"
)

// DeepSeekProvider triển khai giao diện Provider cho DeepSeek
type DeepSeekProvider struct{}

func init() {
	Register(&DeepSeekProvider{})
}

// Info trả về metadata của provider DeepSeek
func (p *DeepSeekProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderDeepSeek,
		DisplayName: "DeepSeek",
		Description: "deepseek-chat, deepseek-reasoner, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: DeepSeekBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider DeepSeek
func (p *DeepSeekProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for DeepSeek provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
