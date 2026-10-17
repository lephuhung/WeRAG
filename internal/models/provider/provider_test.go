package provider

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRegistry(t *testing.T) {
	// Test that all default providers are registered

	t.Run("GetOrDefault fallback", func(t *testing.T) {
		// Non-existent provider should fall back to generic
		p := GetOrDefault("nonexistent")
		require.NotNil(t, p)
		assert.Equal(t, ProviderGeneric, p.Info().Name)
	})
}

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		url      string
		expected ProviderName
	}{
		{"https://api.openai.com/v1", ProviderOpenAI},
		{"http://localhost:4000/v1", ProviderGeneric},
		{"https://custom-endpoint.example.com/v1", ProviderGeneric},
		{"http://localhost:11434/v1", ProviderGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := DetectProvider(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOpenAIProviderValidation(t *testing.T) {
	p := &OpenAIProvider{}

	t.Run("valid config", func(t *testing.T) {
		config := &Config{
			APIKey:    "sk-test",
			ModelName: "gpt-4",
		}
		err := p.ValidateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("missing API key", func(t *testing.T) {
		config := &Config{
			ModelName: "gpt-4",
		}
		err := p.ValidateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API key")
	})

	t.Run("missing model name", func(t *testing.T) {
		config := &Config{
			APIKey: "sk-test",
		}
		err := p.ValidateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "model name")
	})
}

func TestListByModelType(t *testing.T) {
	t.Run("chat models", func(t *testing.T) {
		providers := ListByModelType(types.ModelTypeKnowledgeQA)
		assert.NotEmpty(t, providers)
		// generic, openai and WeKnora Cloud serve chat
		assert.GreaterOrEqual(t, len(providers), 3)
	})

}
