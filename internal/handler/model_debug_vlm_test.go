package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/assets"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDebugModelService serves a single model and records the images the VLM
// stub was asked to process. The embedded interface provides the methods the
// debug path never calls.
type stubDebugModelService struct {
	interfaces.ModelService
	model *types.Model
	vlm   vlm.VLM
}

func (s *stubDebugModelService) GetModelByID(_ context.Context, _ string) (*types.Model, error) {
	return s.model, nil
}

func (s *stubDebugModelService) GetVLMModel(_ context.Context, _ string) (vlm.VLM, error) {
	return s.vlm, nil
}

// stubDebugVLM records the images it received and answers with fixed text.
type stubDebugVLM struct {
	images [][]byte
	prompt string
}

func (s *stubDebugVLM) Predict(_ context.Context, imgBytes [][]byte, prompt string) (string, error) {
	s.images = imgBytes
	s.prompt = prompt
	return "WEKNORA VLM TEST 2026", nil
}

func (s *stubDebugVLM) GetModelName() string { return "chrome" }
func (s *stubDebugVLM) GetModelID() string   { return "chrome-id" }

func newDebugModelTestRouter(svc interfaces.ModelService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := NewModelHandler(svc)
	r.POST("/models/:id/debug", h.DebugModel)
	return r
}

// The model catalog's "Test" button sends no file, so the debug endpoint used
// to reject every VLLM test with "image file is required" before the upstream
// endpoint was reached. It must fall back to the built-in sample image.
func TestDebugModelVLLMWithoutUploadUsesBuiltInTestImage(t *testing.T) {
	model := &types.Model{
		ID:     "chrome-id",
		Name:   "chrome",
		Type:   types.ModelTypeVLLM,
		Source: types.ModelSourceRemote,
	}
	stub := &stubDebugVLM{}
	svc := &stubDebugModelService{model: model, vlm: stub}

	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	require.NoError(t, form.WriteField("input", "OCR:"))
	require.NoError(t, form.Close())

	req := httptest.NewRequest(http.MethodPost, "/models/chrome-id/debug", body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	newDebugModelTestRouter(svc).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			OK       bool   `json:"ok"`
			Error    string `json:"error"`
			Response string `json:"raw_response"`
			Request  struct {
				File struct {
					Name      string `json:"name"`
					Generated bool   `json:"generated"`
				} `json:"file"`
			} `json:"request"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.Success)
	assert.True(t, envelope.Data.OK, "unexpected error: %s", envelope.Data.Error)
	assert.Equal(t, "WEKNORA VLM TEST 2026", envelope.Data.Response)
	assert.Equal(t, modelDebugTestImageName, envelope.Data.Request.File.Name)
	assert.True(t, envelope.Data.Request.File.Generated)

	require.Len(t, stub.images, 1)
	assert.Equal(t, assets.VLMTestPNG, stub.images[0])
	_, err := png.Decode(bytes.NewReader(stub.images[0]))
	assert.NoError(t, err, "the built-in test image must be a decodable PNG")
}

// A real upload reaches the model unchanged and is not marked generated.
func TestDebugModelVLLMWithUploadUsesTheUpload(t *testing.T) {
	model := &types.Model{ID: "chrome-id", Name: "chrome", Type: types.ModelTypeVLLM, Source: types.ModelSourceRemote}
	stub := &stubDebugVLM{}
	svc := &stubDebugModelService{model: model, vlm: stub}

	img := &bytes.Buffer{}
	require.NoError(t, png.Encode(img, image.NewGray(image.Rect(0, 0, 1, 1))))
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	require.NoError(t, form.WriteField("input", "OCR:"))
	part, err := form.CreateFormFile("file", "mine.png")
	require.NoError(t, err)
	_, err = part.Write(img.Bytes())
	require.NoError(t, err)
	require.NoError(t, form.Close())

	req := httptest.NewRequest(http.MethodPost, "/models/chrome-id/debug", body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	newDebugModelTestRouter(svc).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var envelope struct {
		Data struct {
			Request struct {
				File struct {
					Name      string `json:"name"`
					Generated bool   `json:"generated"`
				} `json:"file"`
			} `json:"request"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	assert.Equal(t, "mine.png", envelope.Data.Request.File.Name)
	assert.False(t, envelope.Data.Request.File.Generated)
	require.Len(t, stub.images, 1)
	assert.Equal(t, img.Bytes(), stub.images[0])
}
