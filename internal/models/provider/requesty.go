package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	RequestyBaseURL = "https://router.requesty.ai/v1"
)

// RequestyProvider triển khai giao diện Provider cho Requesty
type RequestyProvider struct{}

func init() {
	Register(&RequestyProvider{})
}

// Info trả về metadata của provider Requesty
func (p *RequestyProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderRequesty,
		DisplayName: "Requesty",
		Description: "openai/gpt-4o-mini, anthropic/claude-sonnet-4-5, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: RequestyBaseURL,
			types.ModelTypeEmbedding:   RequestyBaseURL,
			types.ModelTypeVLLM:        RequestyBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeEmbedding,
			types.ModelTypeVLLM,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider Requesty
func (p *RequestyProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for Requesty provider")
	}
	return nil
}
