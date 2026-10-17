package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// NovitaOpenAIBaseURL Novita OpenAI-compatible API BaseURL
	NovitaOpenAIBaseURL = "https://api.novita.ai/openai/v1"
)

// NovitaProvider triển khai giao diện Provider cho Novita AI
type NovitaProvider struct{}

func init() {
	Register(&NovitaProvider{})
}

// Info trả về metadata của provider Novita
func (p *NovitaProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderNovita,
		DisplayName: "Novita AI",
		Description: "moonshotai/kimi-k2.5, zai-org/glm-5, minimax/minimax-m2.7, qwen/qwen3-embedding-0.6b, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: NovitaOpenAIBaseURL,
			types.ModelTypeEmbedding:   NovitaOpenAIBaseURL,
			types.ModelTypeVLLM:        NovitaOpenAIBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeEmbedding,
			types.ModelTypeVLLM,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider Novita
func (p *NovitaProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for Novita provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
