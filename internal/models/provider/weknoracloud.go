package provider

import "github.com/Tencent/WeKnora/internal/types"

const (
	ProviderWeKnoraCloud ProviderName = "weknoracloud"

	// WeKnoraCloudBaseURL Base URL cố định của dịch vụ WeKnoraCloud (một điểm vào, từng phần tự nối path)
	WeKnoraCloudBaseURL = "https://weknora.weixin.qq.com"
)

type WeKnoraCloudProvider struct{}

func init() {
	Register(&WeKnoraCloudProvider{})
}

func (p *WeKnoraCloudProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderWeKnoraCloud,
		DisplayName: "WeKnoraCloud",
		Description: "Dịch vụ đám mây WeKnora, mô hình: chat, embedding, rerank, vlm",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: WeKnoraCloudBaseURL,
			types.ModelTypeEmbedding:   WeKnoraCloudBaseURL,
			types.ModelTypeRerank:      WeKnoraCloudBaseURL,
			types.ModelTypeVLLM:        WeKnoraCloudBaseURL,
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

func (p *WeKnoraCloudProvider) ValidateConfig(config *Config) error {
	// AppID/AppSecret được ghi qua API khởi tạo riêng, ở đây chỉ kiểm tra cấu trúc.
	// Trường AppSecret hiện đang chứa API Key của upstream.
	return nil
}
