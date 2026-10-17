package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// VolcengineChatBaseURL BaseURL API của Volcengine Ark Chat (chế độ tương thích OpenAI)
	VolcengineChatBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	// VolcengineEmbeddingBaseURL BaseURL API Multimodal Embedding của Volcengine Ark
	VolcengineEmbeddingBaseURL = "https://ark.cn-beijing.volces.com/api/v3/embeddings/multimodal"
	// VolcengineRerankBaseURL BaseURL Rerank API do Volcengine Knowledge Base cung cấp
	VolcengineRerankBaseURL = "https://api-knowledgebase.mlp.cn-beijing.volces.com"
)

// VolcengineProvider triển khai giao diện Provider cho Volcengine Ark
type VolcengineProvider struct{}

func init() {
	Register(&VolcengineProvider{})
}

// Info trả về metadata của provider Volcengine
func (p *VolcengineProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderVolcengine,
		DisplayName: "Volcengine",
		Description: "doubao-1-5-pro-32k-250115, doubao-embedding-vision-250615, doubao-seed-rerank, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: VolcengineChatBaseURL,
			types.ModelTypeEmbedding:   VolcengineEmbeddingBaseURL,
			types.ModelTypeRerank:      VolcengineRerankBaseURL,
			types.ModelTypeVLLM:        VolcengineChatBaseURL,
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

// ValidateConfig kiểm tra cấu hình provider Volcengine
func (p *VolcengineProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for Volcengine Ark provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
