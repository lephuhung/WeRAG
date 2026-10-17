package docparser

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Engine names, as stored in knowledge base parser rules and shown in the UI.
const (
	// BuiltinEngineName is the DocReader (Python) parser suite.
	BuiltinEngineName = "builtin"
	// SimpleEngineName is Go-native handling of text formats and images.
	SimpleEngineName = "simple"
	// PaddleOCRVLEngineName is a self-hosted PaddleOCR-VL pipeline.
	PaddleOCRVLEngineName = "paddleocr_vl"
	// MarkitdownEngineName is the Microsoft MarkItDown converter in docreader.
	MarkitdownEngineName = "markitdown"
)

func init() {
	RegisterEngine(&builtinEngine{})
	RegisterEngine(&simpleEngine{})
	RegisterEngine(&paddleOCRVLEngine{})
	RegisterEngine(&markitdownEngine{})
}

// ---------------------------------------------------------------------------
// builtin — DocReader-backed parser for complex document formats.
// ---------------------------------------------------------------------------

type builtinEngine struct{}

func (e *builtinEngine) Name() string { return BuiltinEngineName }

func (e *builtinEngine) Description() string { return "DocReader built-in parser engine" }

func (e *builtinEngine) FileTypes(_ bool) []string {
	return []string{
		"docx", "doc", "pdf", "md", "markdown", "xlsx", "xls",
		"pptx", "ppt",
		"html", "htm", "mhtml",
		"jpg", "jpeg", "png", "gif", "bmp", "tiff", "webp",
		"mp3", "wav", "m4a", "flac", "ogg",
	}
}

func (e *builtinEngine) CheckAvailable(docreaderConnected bool, _ map[string]string) (bool, string) {
	if docreaderConnected {
		return true, ""
	}
	return false, "DocReader service not connected"
}

// NewReader returns the docreader client. Selecting "builtin" explicitly means
// the docreader is wanted even for formats the simple reader could handle.
func (e *builtinEngine) NewReader(_ context.Context, deps ReaderDeps) (interfaces.DocReader, error) {
	return remoteReader(deps)
}

// ---------------------------------------------------------------------------
// simple — Go handles md/txt/csv/json natively, no external service needed.
// Distinct from docreader's "builtin", which uses Python libraries for complex
// formats (docx, pdf, ...).
// ---------------------------------------------------------------------------

type simpleEngine struct{}

func (e *simpleEngine) Name() string { return SimpleEngineName }

func (e *simpleEngine) Description() string {
	return "Simple format & image parsing (no external service required)"
}

func (e *simpleEngine) FileTypes(_ bool) []string {
	return []string{
		"md", "markdown", "txt", "csv", "json",
		"jpg", "jpeg", "png", "gif", "bmp", "tiff", "webp",
		"mp3", "wav", "m4a", "flac", "ogg",
	}
}

func (e *simpleEngine) CheckAvailable(_ bool, _ map[string]string) (bool, string) {
	return true, ""
}

func (e *simpleEngine) NewReader(_ context.Context, _ ReaderDeps) (interfaces.DocReader, error) {
	return &SimpleFormatReader{}, nil
}

// ---------------------------------------------------------------------------
// paddleocr_vl — Go-native, calls a self-hosted PaddleOCR-VL pipeline service
// ---------------------------------------------------------------------------

type paddleOCRVLEngine struct{}

func (e *paddleOCRVLEngine) Name() string { return PaddleOCRVLEngineName }

func (e *paddleOCRVLEngine) Description() string {
	return "Self-hosted OCR service (PaddleOCR-VL pipeline, or OpenAI-compatible VLM OCR when the endpoint ends in /v1)"
}

func (e *paddleOCRVLEngine) FileTypes(_ bool) []string {
	return []string{"pdf", "jpg", "jpeg", "png", "bmp", "tiff"}
}

func (e *paddleOCRVLEngine) CheckAvailable(_ bool, overrides map[string]string) (bool, string) {
	endpoint := strings.TrimSpace(overrides["paddleocr_vl_endpoint"])
	if endpoint == "" {
		return false, "PaddleOCR-VL service not configured"
	}
	return PingPaddleOCRVL(endpoint)
}

func (e *paddleOCRVLEngine) NewReader(_ context.Context, deps ReaderDeps) (interfaces.DocReader, error) {
	return NewPaddleOCRVLReader(deps.Overrides, deps.Remote), nil
}

// ---------------------------------------------------------------------------
// markitdown — docreader-only engine reached via the
// remote ListEngines RPC. Local entries exist so the UI shows the
// authoritative English description even when the remote docreader is stale
// or unreachable; parsing itself still routes to the docreader client.
// ---------------------------------------------------------------------------

type markitdownEngine struct{}

func (e *markitdownEngine) Name() string { return MarkitdownEngineName }

func (e *markitdownEngine) Description() string {
	return "MarkItDown converter (Microsoft MarkItDown library)"
}

func (e *markitdownEngine) FileTypes(_ bool) []string {
	return []string{"md", "markdown", "pdf", "docx", "doc", "pptx", "ppt", "xlsx", "xls", "csv"}
}

func (e *markitdownEngine) CheckAvailable(docreaderConnected bool, _ map[string]string) (bool, string) {
	if docreaderConnected {
		return true, ""
	}
	return false, "DocReader service not connected"
}

func (e *markitdownEngine) NewReader(_ context.Context, deps ReaderDeps) (interfaces.DocReader, error) {
	return remoteReader(deps)
}
