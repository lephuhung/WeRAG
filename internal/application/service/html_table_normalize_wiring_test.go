package service

import (
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
)

// Regression guard for the central "normalize inline HTML tables before
// chunking" wiring. The bug this guards: normalization was only wired into the
// paddleocr-vl converters, so tables emitted by other engines (e.g. MinerU)
// reached the chunker as raw single-line <table> blocks and were force-cut at
// the absolute size limit.
func TestHTMLEmbeddedTableWiring_ProducesChunkableOutput(t *testing.T) {
	input := strings.Join([]string{
		"# Báo cáo kiểm định",
		"",
		"Dưới đây là đoạn văn bản, dùng để xác nhận việc chuẩn hóa không phá vỡ markdown thường.",
		"",
		// colspan=1: redundant spans must still be convertible to GFM.
		`<table><tr><td colspan="1" style="text-align:left">Hạng mục</td>` +
			`<td colspan="1">Kết quả</td></tr>` +
			`<tr><td>Độ bền kéo</td><td>Đạt</td></tr></table>`,
		"",
		// colspan=6: a real merge -> cannot be GFM, but must stay splittable.
		`<table><tr><td colspan="6" style="text-align:center">Tổng hợp</td></tr>` +
			`<tr><td colspan="6">Ghi chú</td></tr></table>`,
		"",
		"Đoạn kết.",
	}, "\n")

	got := docparser.NormalizeHTMLTables(input)

	if strings.Contains(got, `colspan="1"`) {
		t.Fatalf("redundant colspan=1 table was not normalized:\n%s", got)
	}
	if !strings.Contains(got, "|") || !strings.Contains(got, "Độ bền kéo") {
		t.Fatalf("expected colspan=1 table to become GFM, got:\n%s", got)
	}
	if !strings.Contains(got, "<table") {
		t.Fatalf("expected colspan=6 table to remain HTML, got:\n%s", got)
	}
	for i := 0; ; {
		idx := strings.Index(got[i:], "<tr")
		if idx < 0 {
			break
		}
		abs := i + idx
		if abs == 0 || got[abs-1] != '\n' {
			t.Fatalf("remaining <tr> at offset %d is not preceded by a newline:\n%s", abs, got)
		}
		i = abs + 1
	}
}

func lastCallBefore(src, call, anchor string) int {
	idx := strings.Index(src, anchor)
	if idx < 0 {
		return -1
	}
	return strings.LastIndex(src[:idx], call)
}

// TestHTMLEmbeddedTableWiring_CallSitesPresent is a lightweight static guard:
// it fails if the central normalization call is removed from a pre-chunk
// (or pre-image-resolution) location.
func TestHTMLEmbeddedTableWiring_CallSitesPresent(t *testing.T) {
	call := "docparser.NormalizeHTMLTables("

	processSrc, err := os.ReadFile("knowledge_process.go")
	if err != nil {
		t.Fatalf("read knowledge_process.go: %v", err)
	}
	process := string(processSrc)
	if lastCallBefore(process, call, "s.imageResolver.ResolveAndStore") < 0 {
		t.Fatalf("knowledge_process.go: %s must run before image resolution", call)
	}
	if lastCallBefore(process, call, "chunkCfg := buildSplitterConfigFromChunking") < 0 {
		t.Fatalf("knowledge_process.go: %s must run before the chunking step", call)
	}

	createSrc, err := os.ReadFile("knowledge_create.go")
	if err != nil {
		t.Fatalf("read knowledge_create.go: %v", err)
	}
	if lastCallBefore(string(createSrc), call, "chunkCfg := buildSplitterConfigFromChunking") < 0 {
		t.Fatalf("knowledge_create.go: %s must run before buildSplitterConfigFromChunking", call)
	}

	tempSrc, err := os.ReadFile("temporary_document.go")
	if err != nil {
		t.Fatalf("read temporary_document.go: %v", err)
	}
	if lastCallBefore(string(tempSrc), call, "chunker.Split(") < 0 {
		t.Fatalf("temporary_document.go: %s must run before chunker.Split", call)
	}

	previewSrc, err := os.ReadFile("../../handler/chunker_debug.go")
	if err != nil {
		t.Fatalf("read chunker_debug.go: %v", err)
	}
	if !strings.Contains(string(previewSrc), call) {
		t.Fatalf("chunker_debug.go is missing the preview %s call", call)
	}
}
