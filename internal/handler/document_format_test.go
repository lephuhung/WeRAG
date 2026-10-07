package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func formatCheckRequest(t *testing.T, h *DocumentFormatHandler, fileName string, content []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", fileName)
	require.NoError(t, err)
	_, _ = fw.Write(content)
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	require.NoError(t, mw.Close())

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Next()
	})
	r.POST("/document-format/check", h.Check)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/document-format/check", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	r.ServeHTTP(w, req)
	return w
}

func formatFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../docformat/testdata/parity/fx_good_cong_van_arial.docx")
	require.NoError(t, err)
	return b
}

type formatResponse struct {
	Data struct {
		Source       string `json:"source"`
		OK           bool   `json:"ok"`
		DocumentType struct {
			Detected string `json:"detected"`
		} `json:"document_type"`
		Segmentation struct {
			Method string `json:"method"`
			Model  string `json:"model"`
			Error  string `json:"error"`
		} `json:"segmentation"`
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"checks"`
	} `json:"data"`
}

func TestDocumentFormatCheckUsesWorkspaceDefaultModel(t *testing.T) {
	_, models := llmChatFixture()
	models.chat.result = &types.ChatResponse{Content: "not json at all"}
	h := NewDocumentFormatHandler(models)
	w := formatCheckRequest(t, h, "cv.docx", formatFixture(t), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "qwen", models.selected, "the workspace default chat model labels")
	require.False(t, *models.chat.options.Thinking)

	var resp formatResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Data.OK)
	require.Equal(t, "cv.docx", resp.Data.Source)
	require.Equal(t, "cong_van", resp.Data.DocumentType.Detected)
	// an unusable model answer degrades to the heuristic, reported
	require.Equal(t, "heuristic", resp.Data.Segmentation.Method)
	require.Contains(t, resp.Data.Segmentation.Error, "LLM")
	found := false
	for _, c := range resp.Data.Checks {
		if c.ID == "noi_dung.font" {
			found = true
			require.Equal(t, "fail", c.Status, "Arial body must fail the font rule")
		}
	}
	require.True(t, found)
	require.NotContains(t, w.Body.String(), "sk-secret")
}

func TestDocumentFormatCheckHeuristicSkipsModel(t *testing.T) {
	_, models := llmChatFixture()
	w := formatCheckRequest(t, NewDocumentFormatHandler(models), "cv.docx", formatFixture(t),
		map[string]string{"segmenter": "heuristic", "document_type": "quyet_dinh"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Empty(t, models.selected)
	require.Contains(t, w.Body.String(), `"used":"quyet_dinh"`)
}

func TestDocumentFormatCheckRejects(t *testing.T) {
	_, models := llmChatFixture()
	h := NewDocumentFormatHandler(models)
	for _, tc := range []struct {
		name, file string
		content    []byte
		fields     map[string]string
	}{
		{"not docx extension", "scan.pdf", []byte("%PDF"), nil},
		{"docx extension but not a docx", "fake.docx", []byte("hello"), nil},
		{"bad segmenter", "cv.docx", formatFixture(t), map[string]string{"segmenter": "labels"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := formatCheckRequest(t, h, tc.file, tc.content, tc.fields)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

func TestDocumentFormatCheckEvaluate(t *testing.T) {
	_, models := llmChatFixture()
	models.chat.result = &types.ChatResponse{Content: "## Kết luận\nChưa đạt thể thức."}
	w := formatCheckRequest(t, NewDocumentFormatHandler(models), "cv.docx", formatFixture(t),
		map[string]string{"evaluate": "true"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Data struct {
			Evaluation string   `json:"evaluation"`
			Skills     []string `json:"skills"`
			Format     []any    `json:"format"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "## Kết luận\nChưa đạt thể thức.", resp.Data.Evaluation)
	require.Equal(t, []string{"the-thuc-chung", "the-thuc-cong-van"}, resp.Data.Skills)
	require.NotEmpty(t, resp.Data.Format)
	require.True(t, *models.chat.options.Thinking, "the evaluation runs with thinking on")

	// without evaluate=true no reasoning call is made
	_, models = llmChatFixture()
	w = formatCheckRequest(t, NewDocumentFormatHandler(models), "cv.docx", formatFixture(t), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), `"evaluation"`)
	require.False(t, *models.chat.options.Thinking)
}
