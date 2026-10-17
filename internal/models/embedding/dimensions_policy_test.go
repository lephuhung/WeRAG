package embedding

import "testing"

func TestEmbeddingDimensionsParamRequiresExplicitOverride(t *testing.T) {
	tests := []struct {
		name     string
		supports func(bool) bool
	}{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.supports(false); got {
				t.Fatalf("supportsDimensionsParam() = true with override disabled")
			}
			if got := tt.supports(true); !got {
				t.Fatalf("supportsDimensionsParam() = false with override enabled")
			}
		})
	}
}

func TestEmbeddingDimensionsParamRequiresPositiveDimension(t *testing.T) {
	e := &OpenAIEmbedder{dimensions: 0}
	e.SetSupportsDimensionOverride(true)
	if e.supportsDimensionsParam() {
		t.Fatal("expected dimensions param to be omitted when configured dimension is zero")
	}
}
