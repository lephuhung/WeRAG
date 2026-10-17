package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// NvidiaChatBaseURL BaseURL mặc định của NVIDIA Chat
	NvidiaChatBaseURL = "https://integrate.api.nvidia.com/v1"
	// NvidiaRerankBaseURL BaseURL mặc định của NVIDIA Rerank
	NvidiaRerankBaseURL = "https://ai.api.nvidia.com/v1/retrieval/nvidia/reranking"
)

// NvidiaProvider triển khai giao diện Provider cho NVIDIA AI
type NvidiaProvider struct{}

func init() {
	Register(&NvidiaProvider{})
}

// Info trả về metadata của provider NVIDIA
func (p *NvidiaProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderNvidia,
		DisplayName: "NVIDIA",
		Description: "deepseek-ai-deepseek-v3_1, nv-embed-v1, rerank-qa-mistral-4b, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: NvidiaChatBaseURL,
			types.ModelTypeEmbedding:   NvidiaChatBaseURL,
			types.ModelTypeRerank:      NvidiaRerankBaseURL,
			types.ModelTypeVLLM:        NvidiaChatBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeEmbedding,
			types.ModelTypeRerank,
			types.ModelTypeVLLM,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider NVIDIA
func (p *NvidiaProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for NVIDIA")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
