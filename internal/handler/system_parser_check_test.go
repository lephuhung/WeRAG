package handler

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParserEngineCheckOverrides_PreservesStoredSecrets(t *testing.T) {
	body := types.ParserEngineConfig{
		PaddleOCRVLAPIKey:   types.RedactedSecretPlaceholder,
		PaddleOCRVLEndpoint: "http://vllm-ocr-new:8001/v1",
	}
	existing := &types.ParserEngineConfig{
		PaddleOCRVLAPIKey:   "ocr-secret",
		PaddleOCRVLEndpoint: "http://vllm-ocr-old:8001/v1",
	}

	merged := types.MergeParserEngineConfigForUpdate(&body, existing)
	require.NotNil(t, merged)
	overrides := merged.ToOverridesMap()
	assert.Equal(t, "ocr-secret", overrides["paddleocr_vl_api_key"])
	assert.Equal(t, "http://vllm-ocr-new:8001/v1", overrides["paddleocr_vl_endpoint"])
}
