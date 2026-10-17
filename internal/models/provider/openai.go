package provider

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	OpenAIBaseURL = "https://api.openai.com/v1"
)

// OpenAIProvider triển khai giao diện Provider cho OpenAI
type OpenAIProvider struct{}

func init() {
	Register(&OpenAIProvider{})
}

// Info trả về metadata của provider OpenAI
func (p *OpenAIProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderOpenAI,
		DisplayName: "OpenAI",
		Description: "gpt-5.2, gpt-5-mini, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: OpenAIBaseURL,
			types.ModelTypeEmbedding:   OpenAIBaseURL,
			types.ModelTypeRerank:      OpenAIBaseURL,
			types.ModelTypeVLLM:        OpenAIBaseURL,
			types.ModelTypeASR:         OpenAIBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeEmbedding,
			types.ModelTypeRerank,
			types.ModelTypeVLLM,
			types.ModelTypeASR,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider OpenAI
func (p *OpenAIProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for OpenAI provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}

// IsOpenAIReasoningOrGPT5Model xác định mô hình có phải dòng suy luận (o-series)
// hoặc GPT-5 của OpenAI / Azure OpenAI không.
//
// Với OpenAI Chat Completions API, các mô hình này:
//   - không còn hỗ trợ `max_tokens`, phải dùng `max_completion_tokens`;
//   - chỉ chấp nhận `temperature=1`, `top_p=1` mặc định và không hỗ trợ `frequency_penalty` /
//     `presence_penalty` (truyền giá trị khác mặc định sẽ bị từ chối).
//
// Tham khảo:
//   - https://platform.openai.com/docs/api-reference/chat
//   - https://learn.microsoft.com/azure/ai-services/openai/how-to/reasoning
//
// Chỉ so khớp heuristic theo tên mô hình; với Azure OpenAI tên mô hình thực ra là tên deployment,
// nếu người dùng đặt tên deployment tùy ý thì không nhận ra được và vẫn xử lý như mô hình thường (giữ hành vi cũ).
func IsOpenAIReasoningOrGPT5Model(modelName string) bool {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "gpt-5") {
		return true
	}
	// o1 / o1-mini / o1-preview / o3 / o3-mini / o4-mini ...
	// Phải khớp chính xác để tránh bắt nhầm kiểu "openai-...".
	for _, prefix := range []string{"o1", "o3", "o4"} {
		if name == prefix || strings.HasPrefix(name, prefix+"-") {
			return true
		}
	}
	return false
}
