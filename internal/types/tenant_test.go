package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRetrieverEngineMappingOnlyPostgresAndSQLite(t *testing.T) {
	mapping := GetRetrieverEngineMapping()

	assert.Len(t, mapping, 2)
	for driver, engine := range map[string]RetrieverEngineType{
		"postgres": PostgresRetrieverEngineType,
		"sqlite":   SQLiteRetrieverEngineType,
	} {
		assert.ElementsMatch(t, []RetrieverEngineParams{
			{RetrieverType: KeywordsRetrieverType, RetrieverEngineType: engine},
			{RetrieverType: VectorRetrieverType, RetrieverEngineType: engine},
		}, mapping[driver], "driver %s", driver)
	}
	assert.NotContains(t, mapping, "tencent_vectordb")
	assert.NotContains(t, mapping, "elasticsearch_v8")
}
