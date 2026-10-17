package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// ZhipuChatBaseURL BaseURL mặc định của Zhipu AI Chat
	ZhipuChatBaseURL = "https://open.bigmodel.cn/api/paas/v4"
	// ZhipuEmbeddingBaseURL BaseURL mặc định của Zhipu AI Embedding
	ZhipuEmbeddingBaseURL = "https://open.bigmodel.cn/api/paas/v4"
	// ZhipuRerankBaseURL BaseURL mặc định của Zhipu AI Rerank
	ZhipuRerankBaseURL = "https://open.bigmodel.cn/api/paas/v4/rerank"
)

// ZhipuProvider triển khai giao diện Provider cho Zhipu AI
type ZhipuProvider struct{}

func init() {
	Register(&ZhipuProvider{})
}

// Info trả về metadata của provider Zhipu AI
func (p *ZhipuProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderZhipu,
		DisplayName: "Zhipu BigModel",
		Description: "glm-4.7, embedding-3, rerank, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: ZhipuChatBaseURL,
			types.ModelTypeEmbedding:   ZhipuEmbeddingBaseURL,
			types.ModelTypeRerank:      ZhipuRerankBaseURL,
			types.ModelTypeVLLM:        ZhipuChatBaseURL,
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

// ValidateConfig kiểm tra cấu hình provider Zhipu AI
func (p *ZhipuProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for Zhipu AI")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
