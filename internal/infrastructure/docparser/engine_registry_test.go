package docparser

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestListAllEnginesBuiltinIncludesDocumentFormats(t *testing.T) {
	engines := ListAllEngines(true, nil, nil)
	for _, engine := range engines {
		if engine.Name != "builtin" {
			continue
		}
		if !engine.Available {
			t.Fatalf("builtin engine is unavailable: %s", engine.UnavailableReason)
		}

		fileTypes := make(map[string]bool, len(engine.FileTypes))
		for _, fileType := range engine.FileTypes {
			fileTypes[fileType] = true
		}
		for _, want := range []string{"html", "htm", "ppt", "pptx"} {
			if !fileTypes[want] {
				t.Errorf("builtin engine file types do not include %q: %v", want, engine.FileTypes)
			}
		}
		return
	}

	t.Fatal("builtin engine not found")
}

func TestDefaultParserEngineByType(t *testing.T) {
	if got := types.DefaultParserEngine("pptx"); got != "markitdown" {
		t.Fatalf("DefaultParserEngine(pptx) = %q, want markitdown", got)
	}
	if got := types.DefaultParserEngine("pdf"); got != "vietnamese_legal" {
		t.Fatalf("DefaultParserEngine(pdf) = %q, want vietnamese_legal", got)
	}
	if got := types.DefaultParserEngine("docx"); got != "" {
		t.Fatalf("DefaultParserEngine(docx) = %q, want empty (docreader builtin)", got)
	}
	if got := types.DefaultParserEngine("csv"); got != "" {
		t.Fatalf("DefaultParserEngine(csv) = %q, want empty so the Go simple reader stays default", got)
	}
}
