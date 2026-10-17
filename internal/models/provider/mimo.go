package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// MimoBaseURL BaseURL API của Xiaomi Mimo
	MimoBaseURL = "https://api.xiaomimimo.com/v1"
)

// MimoProvider triển khai giao diện Provider cho Xiaomi Mimo
type MimoProvider struct{}

func init() {
	Register(&MimoProvider{})
}

// Info trả về metadata của provider Xiaomi Mimo
func (p *MimoProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderMimo,
		DisplayName: "Xiaomi MiMo",
		Description: "mimo-v2-flash",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: MimoBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider Xiaomi Mimo
func (p *MimoProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for Mimo provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
