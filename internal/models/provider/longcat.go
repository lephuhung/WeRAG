package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	LongCatBaseURL = "https://api.longcat.chat/openai/v1"
)

// LongCatProvider triển khai giao diện Provider cho LongCat AI
type LongCatProvider struct{}

func init() {
	Register(&LongCatProvider{})
}

// Info trả về metadata của provider LongCat
func (p *LongCatProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderLongCat,
		DisplayName: "LongCat AI",
		Description: "LongCat-Flash-Chat, LongCat-Flash-Thinking, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: LongCatBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider LongCat
func (p *LongCatProvider) ValidateConfig(config *Config) error {
	if config.BaseURL == "" {
		return fmt.Errorf("base URL is required for LongCat provider")
	}
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for LongCat provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
