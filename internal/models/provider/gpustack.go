package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// GPUStackBaseURL BaseURL API của GPUStack (chế độ tương thích OpenAI)
	GPUStackBaseURL = "http://your_gpustack_server_url/v1-openai"
	// GPUStackRerankBaseURL Rerank API của GPUStack tương thích OpenAI nhưng path khác (/v1/rerank thay vì /v1-openai/rerank)
	GPUStackRerankBaseURL = "http://your_gpustack_server_url/v1"
)

// GPUStackProvider triển khai giao diện Provider cho GPUStack
type GPUStackProvider struct{}

func init() {
	Register(&GPUStackProvider{})
}

// Info trả về metadata của provider GPUStack
func (p *GPUStackProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderGPUStack,
		DisplayName: "GPUStack",
		Description: "Choose your deployed model on GPUStack",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: GPUStackBaseURL,
			types.ModelTypeEmbedding:   GPUStackBaseURL,
			types.ModelTypeRerank:      GPUStackRerankBaseURL,
			types.ModelTypeVLLM:        GPUStackBaseURL,
			types.ModelTypeASR:         GPUStackBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeEmbedding,
			types.ModelTypeRerank,
			types.ModelTypeVLLM,
			types.ModelTypeASR,
		},
		RequiresAuth: true, // GPUStack cần API Key
	}
}

// ValidateConfig kiểm tra cấu hình provider GPUStack
func (p *GPUStackProvider) ValidateConfig(config *Config) error {
	if config.BaseURL == "" {
		return fmt.Errorf("base URL is required for GPUStack provider")
	}
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for GPUStack provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
