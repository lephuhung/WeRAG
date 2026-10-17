package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestLLMToolOutputsUseMarkdownImages(t *testing.T) {
	imageInfo, err := json.Marshal([]types.ImageInfo{
		{
			URL:     "resource://AbCdEfGhIjKlMnOpQrStUv",
			Caption: "Sơ đồ quy trình trích xuất người nói mục tiêu",
			OCRText: "Đầu vào\nĐầu ra",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := &types.Chunk{
		ID:         "chunk-1",
		ChunkIndex: 0,
		ChunkType:  types.ChunkTypeText,
		Content:    "Quy trình chính của giai đoạn thử nghiệm",
		ImageInfo:  string(imageInfo),
	}

	readOutput := (&ReadDocumentTool{}).buildOutput(
		&types.Knowledge{ID: "knowledge-1", Title: "Tài liệu thử nghiệm"}, 1, []readChunkRow{{chunk: chunk}}, "",
	)
	enrichedOutput := enrichChunkContent(chunk)
	for name, output := range map[string]string{
		"read_document":        readOutput,
		"enrich_chunk_content": enrichedOutput,
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(output, "![Sơ đồ quy trình trích xuất người nói mục tiêu](resource://AbCdEfGhIjKlMnOpQrStUv)") {
				t.Fatalf("expected Markdown image in tool output:\n%s", output)
			}
			if strings.Contains(output, "<image") || strings.Contains(output, "<caption>") || strings.Contains(output, "<ocr_text>") {
				t.Fatalf("tool output leaked legacy image XML:\n%s", output)
			}
		})
	}
}
