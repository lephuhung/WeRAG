package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRetrieverEngineMappingOnlyPostgres(t *testing.T) {
	mapping := GetRetrieverEngineMapping()

	assert.Len(t, mapping, 1)
	assert.ElementsMatch(t, []RetrieverEngineParams{
		{RetrieverType: KeywordsRetrieverType, RetrieverEngineType: PostgresRetrieverEngineType},
		{RetrieverType: VectorRetrieverType, RetrieverEngineType: PostgresRetrieverEngineType},
	}, mapping["postgres"])
	assert.NotContains(t, mapping, "sqlite")
	assert.NotContains(t, mapping, "tencent_vectordb")
	assert.NotContains(t, mapping, "elasticsearch_v8")
}
