package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebSearchConfigForResponse_MasksSecrets(t *testing.T) {
	cfg := &WebSearchConfig{
		APIKey:   "search-secret",
		ProxyURL: "http://proxy.internal:8080",
	}
	resp := WebSearchConfigForResponse(cfg, true)
	require.NotNil(t, resp)
	assert.Empty(t, resp.APIKey)
	assert.Equal(t, RedactedSecretPlaceholder, resp.ProxyURL)
}

func TestWebSearchConfigForResponse_Unmasked(t *testing.T) {
	cfg := &WebSearchConfig{APIKey: "search-secret", ProxyURL: "http://proxy"}
	resp := WebSearchConfigForResponse(cfg, false)
	require.NotNil(t, resp)
	assert.Equal(t, "search-secret", resp.APIKey)
	assert.Equal(t, "http://proxy", resp.ProxyURL)
}

func TestMergeWebSearchConfigForUpdate_PreservesRedactedSecrets(t *testing.T) {
	existing := &WebSearchConfig{APIKey: "stored-key", ProxyURL: "http://stored"}
	incoming := &WebSearchConfig{
		APIKey:     "",
		ProxyURL:   RedactedSecretPlaceholder,
		MaxResults: 10,
	}
	merged := MergeWebSearchConfigForUpdate(incoming, existing)
	require.NotNil(t, merged)
	assert.Equal(t, "stored-key", merged.APIKey)
	assert.Equal(t, "http://stored", merged.ProxyURL)
	assert.Equal(t, 10, merged.MaxResults)
}

func TestMergeParserEngineConfigForUpdate_PreservesRedactedSecrets(t *testing.T) {
	existing := &ParserEngineConfig{
		PaddleOCRVLAPIKey:   "ocr-secret",
		PaddleOCRVLEndpoint: "http://vllm-ocr:8001/v1",
	}
	incoming := &ParserEngineConfig{
		PaddleOCRVLAPIKey:   RedactedSecretPlaceholder,
		PaddleOCRVLEndpoint: "http://vllm-ocr-new:8001/v1",
	}
	merged := MergeParserEngineConfigForUpdate(incoming, existing)
	require.NotNil(t, merged)
	assert.Equal(t, "ocr-secret", merged.PaddleOCRVLAPIKey)
	assert.Equal(t, "http://vllm-ocr-new:8001/v1", merged.PaddleOCRVLEndpoint)

	// no stored config yet: the placeholder must not be saved as the key
	merged = MergeParserEngineConfigForUpdate(incoming, nil)
	require.NotNil(t, merged)
	assert.Equal(t, "", merged.PaddleOCRVLAPIKey)
}

func TestParserEngineConfigForResponse_MasksOCRKey(t *testing.T) {
	cfg := &ParserEngineConfig{PaddleOCRVLAPIKey: "ocr-secret", PaddleOCRVLModel: "senocr-vi"}
	masked := ParserEngineConfigForResponse(cfg, true)
	assert.Equal(t, RedactedSecretPlaceholder, masked.PaddleOCRVLAPIKey)
	assert.Equal(t, "senocr-vi", masked.PaddleOCRVLModel)
	assert.Equal(t, "ocr-secret", ParserEngineConfigForResponse(cfg, false).PaddleOCRVLAPIKey)
	assert.Equal(t, "ocr-secret", cfg.PaddleOCRVLAPIKey, "the stored config is not modified")
}

func TestMergeParserEngineConfigForUpdate_PreservesLegacyChatParserRules(t *testing.T) {
	existing := &ParserEngineConfig{
		ChatParserEngineRules: []ParserEngineRule{
			{FileTypes: []string{"pdf"}, Engine: "vietnamese_legal"},
		},
	}
	incoming := &ParserEngineConfig{
		PaddleOCRVLEndpoint: "http://vllm-ocr:8001/v1",
	}
	merged := MergeParserEngineConfigForUpdate(incoming, existing)
	require.NotNil(t, merged)
	require.Len(t, merged.ChatParserEngineRules, 1)
	assert.Equal(t, "vietnamese_legal", merged.ChatParserEngineRules[0].Engine)
}

func TestMergeStorageEngineConfigForUpdate_PreservesRedactedSecrets(t *testing.T) {
	existing := &StorageEngineConfig{
		DefaultProvider: "minio",
		MinIO: &MinIOEngineConfig{
			AccessKeyID:     "access-id",
			SecretAccessKey: "secret-key",
			BucketName:      "bucket",
		},
	}
	incoming := &StorageEngineConfig{
		DefaultProvider: "minio",
		MinIO: &MinIOEngineConfig{
			AccessKeyID:     RedactedSecretPlaceholder,
			SecretAccessKey: RedactedSecretPlaceholder,
			BucketName:      "bucket-new",
		},
	}
	merged := MergeStorageEngineConfigForUpdate(incoming, existing)
	require.NotNil(t, merged)
	require.NotNil(t, merged.MinIO)
	assert.Equal(t, "access-id", merged.MinIO.AccessKeyID)
	assert.Equal(t, "secret-key", merged.MinIO.SecretAccessKey)
	assert.Equal(t, "bucket-new", merged.MinIO.BucketName)
}

func TestParserEngineConfigForResponse_NilSafe(t *testing.T) {
	assert.Nil(t, ParserEngineConfigForResponse(nil, true))
}

func TestStorageEngineConfigForResponse_NilSafe(t *testing.T) {
	assert.Nil(t, StorageEngineConfigForResponse(nil, true))
}
