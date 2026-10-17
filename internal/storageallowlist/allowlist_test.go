package storageallowlist

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowedMap_DefaultAllowsAll(t *testing.T) {
	t.Setenv(AllowListEnv, "")
	allowed := AllowedMap()
	for _, provider := range Supported() {
		assert.True(t, allowed[provider], provider)
	}
}

func TestAllowedMap_RespectsEnv(t *testing.T) {
	t.Setenv(AllowListEnv, "minio,cos")
	allowed := AllowedMap()
	assert.True(t, allowed["minio"])
	assert.False(t, allowed["cos"], "unsupported providers are never allowed")
	assert.False(t, allowed["local"])
}

func TestFirstAllowed(t *testing.T) {
	t.Setenv(AllowListEnv, "minio")
	assert.Equal(t, "minio", FirstAllowed())
}

func TestAllowedList(t *testing.T) {
	t.Setenv(AllowListEnv, "minio,local")
	assert.Equal(t, []string{"local", "minio"}, AllowedList())
}

func TestIsAllowed_EmptyProvider(t *testing.T) {
	t.Setenv(AllowListEnv, "minio")
	require.True(t, IsAllowed(""))
}
