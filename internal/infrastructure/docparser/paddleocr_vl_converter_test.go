package docparser

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestResolveAPIModeAutoDetectsV1Endpoint(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]string
		want      string
	}{
		{"v1 endpoint selects openai", map[string]string{
			"paddleocr_vl_endpoint": "http://vllm-ocr:8001/v1",
		}, "openai"},
		{"trailing slash on v1", map[string]string{
			"paddleocr_vl_endpoint": "http://vllm-ocr:8001/v1/",
		}, "openai"},
		{"plain endpoint selects layout", map[string]string{
			"paddleocr_vl_endpoint": "http://paddleocr:8080",
		}, "layout"},
		{"explicit openai wins", map[string]string{
			"paddleocr_vl_endpoint": "http://paddleocr:8080",
			"paddleocr_vl_api":      "openai",
		}, "openai"},
		{"explicit layout wins", map[string]string{
			"paddleocr_vl_endpoint": "http://vllm-ocr:8001/v1",
			"paddleocr_vl_api":      "layout",
		}, "layout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePaddleOCRVLAPIMode(tc.overrides); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestIsImageFileType(t *testing.T) {
	if !isImageFileType(&types.ReadRequest{FileType: "jpg"}) {
		t.Fatal("jpg should be an image file type")
	}
	if !isImageFileType(&types.ReadRequest{FileName: "scan.PNG"}) {
		t.Fatal("PNG extension should be an image file type")
	}
	if isImageFileType(&types.ReadRequest{FileType: "pdf", FileName: "doc.pdf"}) {
		t.Fatal("pdf is not an image file type")
	}
}

func TestStripOCRMarkup(t *testing.T) {
	raw := "<|det|>text [[1,2,3,4]]<|/det|>Điều 1. Nội dung"
	if got := stripOCRMarkup(raw); got != "Điều 1. Nội dung" {
		t.Fatalf("got %q", got)
	}
	if got := stripOCRMarkup("plain text"); got != "plain text" {
		t.Fatalf("plain text mangled: %q", got)
	}
}

func TestReadOpenAIImagePath(t *testing.T) {
	r := NewPaddleOCRVLReader(map[string]string{
		"paddleocr_vl_endpoint": "http://127.0.0.1:1/v1",
		"paddleocr_vl_model":    "senocr-vi",
	}, nil)
	if r.apiMode != "openai" {
		t.Fatalf("expected openai mode, got %q", r.apiMode)
	}
	res, err := r.Read(t.Context(), &types.ReadRequest{
		FileType:    "jpg",
		FileName:    "scan.jpg",
		FileContent: []byte{0xFF, 0xD8, 0xFF, 0x00},
	})
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if res.Error == "" {
		t.Fatal("expected connection error for unreachable endpoint")
	}
}

func TestReadOpenAIDelegatesPDFToDocreader(t *testing.T) {
	r := NewPaddleOCRVLReader(map[string]string{
		"paddleocr_vl_endpoint": "http://vllm-ocr:8001/v1",
		"paddleocr_vl_model":    "senocr-vi",
	}, nil)
	res, err := r.Read(t.Context(), &types.ReadRequest{
		FileType:    "pdf",
		FileName:    "doc.pdf",
		FileContent: []byte("%PDF-1.4"),
	})
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if res.Error == "" {
		t.Fatal("expected docreader-unavailable error with nil remote")
	}
}
