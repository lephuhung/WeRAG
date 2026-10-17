package provider

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// LKEAPBaseURL BaseURL tương thích giao thức OpenAI của Tencent Cloud LKEAP
	LKEAPBaseURL = "https://api.lkeap.cloud.tencent.com/v1"
	// LKEAPRerankBaseURL tên miền Rerank API của Tencent Cloud LKEAP (ký TC3)
	LKEAPRerankBaseURL = "https://lkeap.tencentcloudapi.com"
)

// LKEAPProvider triển khai giao diện Provider cho Tencent Cloud LKEAP
// Hỗ trợ các dòng mô hình DeepSeek-R1, DeepSeek-V3 có khả năng suy luận chuỗi (thinking)
type LKEAPProvider struct{}

func init() {
	Register(&LKEAPProvider{})
}

// Info trả về metadata của provider LKEAP
func (p *LKEAPProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderLKEAP,
		DisplayName: "Tencent Cloud LKEAP",
		Description: "DeepSeek-R1, DeepSeek-V3, lke-reranker-base, ...",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: LKEAPBaseURL,
			types.ModelTypeRerank:      LKEAPRerankBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeRerank,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider LKEAP
func (p *LKEAPProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for LKEAP provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}

// IsLKEAPDeepSeekV3Model kiểm tra có phải mô hình dòng DeepSeek V3.x không
// Dòng V3.x cho phép bật/tắt suy luận qua tham số Thinking
func IsLKEAPDeepSeekV3Model(modelName string) bool {
	return strings.Contains(strings.ToLower(modelName), "deepseek-v3")
}

// IsLKEAPDeepSeekR1Model kiểm tra có phải mô hình dòng DeepSeek R1 không
// Dòng R1 mặc định bật suy luận
func IsLKEAPDeepSeekR1Model(modelName string) bool {
	return strings.Contains(strings.ToLower(modelName), "deepseek-r1")
}

// IsLKEAPThinkingModel kiểm tra có phải mô hình LKEAP hỗ trợ suy luận không
func IsLKEAPThinkingModel(modelName string) bool {
	return IsLKEAPDeepSeekR1Model(modelName) || IsLKEAPDeepSeekV3Model(modelName)
}
