package handler

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateParserEngineOutboundURLs_RejectsMetadataOCREndpoint(t *testing.T) {
	err := validateParserEngineOutboundURLs(&types.ParserEngineConfig{
		PaddleOCRVLEndpoint: "http://169.254.169.254/v1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "paddleocr_vl_endpoint")
}
