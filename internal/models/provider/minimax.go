package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// MiniMaxBaseURL BaseURL API bản quốc tế của MiniMax
	MiniMaxBaseURL = "https://api.minimax.io/v1"
	// MiniMaxCNBaseURL BaseURL API bản Trung Quốc của MiniMax
	MiniMaxCNBaseURL = "https://api.minimaxi.com/v1"
)

// MiniMaxProvider triển khai giao diện Provider cho MiniMax
type MiniMaxProvider struct{}

func init() {
	Register(&MiniMaxProvider{})
}

// Info trả về metadata của provider MiniMax
func (p *MiniMaxProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderMiniMax,
		DisplayName: "MiniMax",
		Description: "MiniMax-M3, MiniMax-M2.7, MiniMax-M2.7-highspeed, etc.",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: MiniMaxCNBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
		},
		RequiresAuth: true,
	}
}

// ValidateConfig kiểm tra cấu hình provider MiniMax
func (p *MiniMaxProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for MiniMax provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
