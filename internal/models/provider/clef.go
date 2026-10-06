package provider

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

// ClefBaseURL is the Cloudflare API root; requests go to
// {base}/accounts/{account_id}/ai/run/@cf/cloudflare/{model}.
const ClefBaseURL = "https://api.cloudflare.com/client/v4"

// ClefAccountIDKey is the ExtraConfig key holding the Cloudflare account ID.
const ClefAccountIDKey = "account_id"

// ClefProvider is Cloudflare's Clef decision model on Workers AI
// (Jev-API compatible).
type ClefProvider struct{}

func init() {
	Register(&ClefProvider{})
}

// Info returns the Clef provider metadata.
func (p *ClefProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderClef,
		DisplayName: "Cloudflare Clef",
		Description: "clef-flash, clef",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeDecision: ClefBaseURL,
		},
		ModelTypes:   []types.ModelType{types.ModelTypeDecision},
		RequiresAuth: true,
		ExtraFields: []ExtraFieldConfig{{
			Key:         ClefAccountIDKey,
			Label:       "Account ID",
			Type:        "string",
			Required:    true,
			Placeholder: "Cloudflare account ID",
		}},
	}
}

// ValidateConfig validates the Clef provider configuration.
func (p *ClefProvider) ValidateConfig(config *Config) error {
	if config.APIKey == "" {
		return fmt.Errorf("API token is required for Cloudflare Clef")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	if v, _ := config.Extra[ClefAccountIDKey].(string); v == "" {
		return fmt.Errorf("account_id is required for Cloudflare Clef")
	}
	return nil
}
