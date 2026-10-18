package docparser

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ocrTestImage = "\x89PNG\x0d\x0a\x1a\x0a\x00payload"

func ocrSuccessHandler(captured *map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		*captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello world"}}]}`))
	}
}

func TestReadOpenAINilRemoteResult(t *testing.T) {
	r := NewPaddleOCRVLReader(map[string]string{
		"paddleocr_vl_endpoint": "http://vllm-ocr:8001/v1",
		"paddleocr_vl_model":    "senocr-vi",
	}, &stubDocReader{result: nil, err: nil})
	res, err := r.Read(context.Background(), &types.ReadRequest{
		FileType:    "pdf",
		FileName:    "doc.pdf",
		FileContent: []byte("%PDF-1.4"),
	})
	require.NoError(t, err)
	require.NotNil(t, res, "nil remote result must not panic or return nil")
	assert.NotEmpty(t, res.Error)
}

func TestOpenAIOCRImageBlocksCrossOriginRedirect(t *testing.T) {
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)

	sinkHit := false
	var sinkAuth string
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sinkHit = true
		sinkAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"leaked"}}]}`))
	}))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL+"/chat/completions", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	r := NewPaddleOCRVLReader(map[string]string{
		"paddleocr_vl_endpoint": source.URL,
		"paddleocr_vl_model":    "senocr-vi",
		"paddleocr_vl_api_key":  "synthetic-secret",
	}, nil)
	_, err := r.openAIOCRImage(context.Background(), []byte(ocrTestImage))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redirect blocked")
	assert.False(t, sinkHit, "redirect target must not be reached")
	assert.Empty(t, sinkAuth, "Authorization must not be forwarded cross-origin")
}

func TestOpenAIOCRImageRepetitionPenaltyGating(t *testing.T) {
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)

	cases := []struct {
		name      string
		overrides map[string]string
		wantKey   bool
		wantValue float64
	}{
		{"default sends nothing", map[string]string{}, false, 0},
		{"explicit 1.2 sent", map[string]string{"paddleocr_vl_repetition_penalty": "1.2"}, true, 1.2},
		{"explicit 1 omitted", map[string]string{"paddleocr_vl_repetition_penalty": "1"}, false, 0},
		{"explicit 0 omitted", map[string]string{"paddleocr_vl_repetition_penalty": "0"}, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]any
			srv := httptest.NewServer(ocrSuccessHandler(&captured))
			defer srv.Close()
			overrides := map[string]string{
				"paddleocr_vl_endpoint": srv.URL,
				"paddleocr_vl_model":    "senocr-vi",
			}
			for k, v := range tc.overrides {
				overrides[k] = v
			}
			r := NewPaddleOCRVLReader(overrides, nil)
			md, err := r.openAIOCRImage(context.Background(), []byte(ocrTestImage))
			require.NoError(t, err)
			assert.Equal(t, "hello world", strings.TrimSpace(md))
			val, present := captured["repetition_penalty"]
			assert.Equal(t, tc.wantKey, present)
			if tc.wantKey {
				got, ok := val.(float64)
				require.True(t, ok, "repetition_penalty should be numeric, got %T", val)
				assert.InDelta(t, tc.wantValue, got, 1e-9)
			}
		})
	}
}

// The locked parse defaults route images to paddleocr_vl; without a local
// registration the name falls through to the docreader's builtin image
// parser and the reader below never runs.
func TestPaddleOCRVLEngineIsRegistered(t *testing.T) {
	reader, err := NewReader(context.Background(), PaddleOCRVLEngineName, "png", false, ReaderDeps{
		Overrides: map[string]string{"paddleocr_vl_endpoint": "http://vllm-ocr:8001/v1"},
	})
	require.NoError(t, err)
	assert.IsType(t, &PaddleOCRVLReader{}, reader)
}

func TestOpenAIOCRImageDropsLoopAndReportsUsage(t *testing.T) {
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)

	loop := strings.Repeat("1. The text content.\n", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": loop}}},
			"usage":   map[string]any{"prompt_tokens": 900, "completion_tokens": 300, "total_tokens": 1200},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	sink := &types.VLMUsageSink{}
	ctx := types.WithVLMUsageSink(context.Background(), sink)
	r := NewPaddleOCRVLReader(map[string]string{
		"paddleocr_vl_endpoint": srv.URL,
		"paddleocr_vl_model":    "senocr-vi",
	}, nil)
	md, err := r.openAIOCRImage(ctx, []byte(ocrTestImage))
	require.NoError(t, err)
	assert.Empty(t, md, "a looping answer is no text, not a document")
	calls, usage := sink.Snapshot()
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1200, usage.Total())
}

// stubDocReader stands in for the docreader client as the fallback reader.
type stubDocReader struct {
	got    *types.ReadRequest
	result *types.ReadResult
	err    error
}

func (s *stubDocReader) Read(_ context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	s.got = req
	return s.result, s.err
}
