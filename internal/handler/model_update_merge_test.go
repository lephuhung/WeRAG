package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeModelService struct {
	interfaces.ModelService
	stored  *types.Model
	updated *types.Model
}

func (f *fakeModelService) GetModelByID(_ context.Context, _ string) (*types.Model, error) {
	cp := *f.stored
	cp.Parameters.ExtraConfig = cloneStringMap(f.stored.Parameters.ExtraConfig)
	cp.Parameters.CustomHeaders = cloneStringMap(f.stored.Parameters.CustomHeaders)
	return &cp, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (f *fakeModelService) UpdateModel(_ context.Context, model *types.Model) error {
	f.updated = model
	return nil
}

func storedModelFixture() *types.Model {
	return &types.Model{
		ID:          "m1",
		TenantID:    7,
		Name:        "orig",
		DisplayName: "Orig Display",
		Type:        types.ModelTypeKnowledgeQA,
		Source:      types.ModelSourceOpenAI,
		Description: "orig desc",
		Parameters: types.ModelParameters{
			BaseURL:         "https://api.example.com/v1",
			APIKey:          "stored-secret",
			InterfaceType:   "openai",
			Provider:        "openai",
			ExtraConfig:     map[string]string{"k": "v"},
			CustomHeaders:   map[string]string{"X-Trace": "abc"},
			SupportsVision:  true,
			ContextWindow:   128000,
			MaxOutputTokens: 4096,
			AppID:           "appid-1",
			AppSecret:       "app-secret-1",
			EmbeddingParameters: types.EmbeddingParameters{
				Dimension: 1536,
			},
		},
	}
}

func runUpdateModel(t *testing.T, stored *types.Model, body string) (*fakeModelService, *gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fake := &fakeModelService{stored: stored}
	h := NewModelHandler(fake)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/models/m1", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "m1"}}
	h.UpdateModel(c)
	return fake, c, w
}

func TestMergeModelParametersPreservesOmitted(t *testing.T) {
	stored := storedModelFixture().Parameters
	merged, attempt, err := mergeModelParameters(stored, json.RawMessage(`{"provider":"new-provider"}`))
	require.NoError(t, err)
	assert.False(t, attempt)
	assert.Equal(t, "new-provider", merged.Provider)
	assert.Equal(t, "https://api.example.com/v1", merged.BaseURL)
	assert.True(t, merged.SupportsVision)
	assert.Equal(t, 128000, merged.ContextWindow)
	assert.Equal(t, map[string]string{"X-Trace": "abc"}, merged.CustomHeaders)
	assert.Equal(t, map[string]string{"k": "v"}, merged.ExtraConfig)
	assert.Equal(t, 1536, merged.EmbeddingParameters.Dimension)
	assert.Equal(t, "stored-secret", merged.APIKey)
}

func TestMergeModelParametersExplicitZero(t *testing.T) {
	stored := storedModelFixture().Parameters
	merged, _, err := mergeModelParameters(stored,
		json.RawMessage(`{"supports_vision":false,"context_window":0,"max_output_tokens":0,"custom_headers":{}}`))
	require.NoError(t, err)
	assert.False(t, merged.SupportsVision)
	assert.Zero(t, merged.ContextWindow)
	assert.Zero(t, merged.MaxOutputTokens)
	require.NotNil(t, merged.CustomHeaders)
	assert.Empty(t, merged.CustomHeaders)
	assert.Equal(t, "openai", merged.Provider)
}

func TestMergeModelParametersCredentialAttemptPreserved(t *testing.T) {
	stored := storedModelFixture().Parameters
	merged, attempt, err := mergeModelParameters(stored,
		json.RawMessage(`{"api_key":"attacker","app_secret":"evil","provider":"p"}`))
	require.NoError(t, err)
	assert.True(t, attempt)
	assert.Equal(t, "stored-secret", merged.APIKey)
	assert.Equal(t, "app-secret-1", merged.AppSecret)
	assert.Equal(t, "p", merged.Provider)
}

func TestUpdateModelPartialPreservesStored(t *testing.T) {
	stored := storedModelFixture()
	fake, _, w := runUpdateModel(t, stored, `{"description":"new desc","parameters":{"provider":"new-provider"}}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, fake.updated)
	assert.Equal(t, "new desc", fake.updated.Description)
	assert.Equal(t, "new-provider", fake.updated.Parameters.Provider)
	assert.Equal(t, "https://api.example.com/v1", fake.updated.Parameters.BaseURL)
	assert.True(t, fake.updated.Parameters.SupportsVision)
	assert.Equal(t, 128000, fake.updated.Parameters.ContextWindow)
	assert.Equal(t, map[string]string{"X-Trace": "abc"}, fake.updated.Parameters.CustomHeaders)
	assert.Equal(t, "stored-secret", fake.updated.Parameters.APIKey)
	assert.Equal(t, types.ModelTypeKnowledgeQA, fake.updated.Type)
	assert.Equal(t, types.ModelSourceOpenAI, fake.updated.Source)
}

func TestUpdateModelMissingParametersKeepsAll(t *testing.T) {
	stored := storedModelFixture()
	fake, _, w := runUpdateModel(t, stored, `{"description":"only desc"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, fake.updated)
	assert.Equal(t, "only desc", fake.updated.Description)
	assert.Equal(t, stored.Parameters, fake.updated.Parameters)
}

func TestUpdateModelOmitsDescriptionPreservesIt(t *testing.T) {
	stored := storedModelFixture()
	fake, _, w := runUpdateModel(t, stored, `{"parameters":{"provider":"p2"}}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, fake.updated)
	assert.Equal(t, "orig desc", fake.updated.Description)
}

func TestUpdateModelCredentialAttemptIgnored(t *testing.T) {
	stored := storedModelFixture()
	fake, _, w := runUpdateModel(t, stored,
		`{"parameters":{"api_key":"attacker","provider":"p3"}}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, fake.updated)
	assert.Equal(t, "stored-secret", fake.updated.Parameters.APIKey)
	assert.Equal(t, "app-secret-1", fake.updated.Parameters.AppSecret)
	assert.Equal(t, "p3", fake.updated.Parameters.Provider)
}

func TestUpdateModelSSRFBlockedBaseURLNotStored(t *testing.T) {
	stored := storedModelFixture()
	fake, c, _ := runUpdateModel(t, stored,
		`{"parameters":{"base_url":"http://169.254.169.254/latest/meta-data/"}}`)
	assert.Nil(t, fake.updated)
	require.NotEmpty(t, c.Errors)
}

func TestMergeModelParametersPartialMapPreservesKeys(t *testing.T) {
	stored := storedModelFixture().Parameters
	stored.ExtraConfig = map[string]string{"keep": "1", "over": "old"}
	stored.CustomHeaders = map[string]string{"X-Trace": "abc", "X-Keep": "k"}
	merged, attempt, err := mergeModelParameters(stored, json.RawMessage(
		`{"extra_config":{"over":"new"},"custom_headers":{"X-New":"n"}}`))
	require.NoError(t, err)
	assert.False(t, attempt)
	assert.Equal(t, map[string]string{"keep": "1", "over": "new"}, merged.ExtraConfig)
	assert.Equal(t,
		map[string]string{"X-Trace": "abc", "X-Keep": "k", "X-New": "n"},
		merged.CustomHeaders)
}

func TestMergeModelParametersExplicitEmptyClearsMap(t *testing.T) {
	stored := storedModelFixture().Parameters
	merged, _, err := mergeModelParameters(stored, json.RawMessage(
		`{"extra_config":{},"custom_headers":{}}`))
	require.NoError(t, err)
	require.NotNil(t, merged.ExtraConfig)
	assert.Empty(t, merged.ExtraConfig)
	require.NotNil(t, merged.CustomHeaders)
	assert.Empty(t, merged.CustomHeaders)
}

func TestMergeModelParametersNullClearsMap(t *testing.T) {
	stored := storedModelFixture().Parameters
	merged, _, err := mergeModelParameters(stored, json.RawMessage(
		`{"extra_config":null,"custom_headers":null}`))
	require.NoError(t, err)
	assert.Nil(t, merged.ExtraConfig)
	assert.Nil(t, merged.CustomHeaders)
}

func TestMergeModelParametersPerKeyNullDeletesEntry(t *testing.T) {
	stored := storedModelFixture().Parameters
	stored.ExtraConfig = map[string]string{"k": "v", "keep": "1"}
	merged, _, err := mergeModelParameters(stored, json.RawMessage(
		`{"extra_config":{"k":null}}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"keep": "1"}, merged.ExtraConfig)
	// The untouched map keeps its stored entries.
	assert.Equal(t, map[string]string{"X-Trace": "abc"}, merged.CustomHeaders)
}

func TestMergeModelParametersValidationErrorPreservesStored(t *testing.T) {
	stored := storedModelFixture().Parameters
	stored.ExtraConfig = map[string]string{"keep": "1"}
	beforeExtra := cloneStringMap(stored.ExtraConfig)
	beforeHeaders := cloneStringMap(stored.CustomHeaders)
	_, _, err := mergeModelParameters(stored, json.RawMessage(
		`{"extra_config":{"keep":"changed","bad":123},"provider":"p"}`))
	require.Error(t, err)
	assert.Equal(t, beforeExtra, stored.ExtraConfig)
	assert.Equal(t, beforeHeaders, stored.CustomHeaders)
}

func TestUpdateModelPartialMapMergePreservesKeys(t *testing.T) {
	stored := storedModelFixture()
	stored.Parameters.ExtraConfig = map[string]string{"keep": "1", "over": "old"}
	fake, _, w := runUpdateModel(t, stored,
		`{"parameters":{"extra_config":{"over":"new"}}}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, fake.updated)
	assert.Equal(t,
		map[string]string{"keep": "1", "over": "new"},
		fake.updated.Parameters.ExtraConfig)
	assert.Equal(t, map[string]string{"X-Trace": "abc"}, fake.updated.Parameters.CustomHeaders)
}
