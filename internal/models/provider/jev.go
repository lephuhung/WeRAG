package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

// JevBaseURL is the TypeSafe API root; requests go to {base}/systemone.
const JevBaseURL = "https://api.typesafe.ai/v1"

// JevProvider is TypeSafe's Jev System One decision model.
type JevProvider struct{}

func init() {
	Register(&JevProvider{})
}

// Info returns the Jev provider metadata.
func (p *JevProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderJev,
		DisplayName: "TypeSafe Jev",
		Description: "jev-latest, jev-preview, jev-1.13.0",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeDecision: JevBaseURL,
		},
		ModelTypes:   []types.ModelType{types.ModelTypeDecision},
		RequiresAuth: true,
	}
}

// ValidateConfig validates the Jev provider configuration.
func (p *JevProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API key is required for TypeSafe Jev")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}
