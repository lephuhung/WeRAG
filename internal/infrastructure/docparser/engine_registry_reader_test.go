package docparser

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// stubRemote stands in for the docreader client. NewReader only ever hands it
// back, so the embedded interface is never called — and would panic if routing
// ever sent a parse here by mistake.
type stubRemote struct{ interfaces.DocReader }

func TestNewReaderRoutesByEngine(t *testing.T) {
	ctx := context.Background()
	remote := &stubRemote{}
	deps := ReaderDeps{Remote: remote}

	cases := []struct {
		name     string
		engine   string
		fileType string
		isURL    bool
		want     any
	}{
		{name: "simple engine", engine: SimpleEngineName, fileType: "md", want: &SimpleFormatReader{}},
		{name: "builtin engine goes remote", engine: BuiltinEngineName, fileType: "md", want: remote},
		{name: "unset engine handles simple formats in Go", fileType: "csv", want: &SimpleFormatReader{}},
		{name: "unset engine sends complex formats to docreader", fileType: "docx", want: remote},
		{name: "default pptx engine is remote markitdown", engine: "markitdown", fileType: "pptx", want: remote},
		{name: "URLs always go to docreader", fileType: "md", isURL: true, want: remote},
		{name: "docreader-only engines fall through", engine: "markitdown", fileType: "docx", want: remote},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader, err := NewReader(ctx, tc.engine, tc.fileType, tc.isURL, deps)
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}
			if _, isSimple := tc.want.(*SimpleFormatReader); isSimple {
				if _, ok := reader.(*SimpleFormatReader); !ok {
					t.Fatalf("reader = %T, want *SimpleFormatReader", reader)
				}
				return
			}
			if reader != tc.want {
				t.Fatalf("reader = %T, want the docreader client", reader)
			}
		})
	}
}

// A disconnected docreader has to surface as an error, not as a nil reader the
// caller would dereference.
func TestNewReaderReportsDisconnectedDocReader(t *testing.T) {
	if _, err := NewReader(context.Background(), BuiltinEngineName, "docx", false, ReaderDeps{}); err == nil {
		t.Fatal("NewReader succeeded without a docreader connection, want an error")
	}
}

// Rules saved before these engines were removed may still name them; they
// are unknown here now and go to the docreader like any other unknown name.
func TestRemovedEnginesFallThroughToDocReader(t *testing.T) {
	remote := &stubRemote{}
	for _, engine := range []string{"mineru", "mineru_cloud", "paddleocr_vl_cloud", "weknoracloud", "anydoc"} {
		reader, err := NewReader(context.Background(), engine, "pdf", false, ReaderDeps{Remote: remote})
		if err != nil {
			t.Fatalf("NewReader(%s): %v", engine, err)
		}
		if reader != remote {
			t.Fatalf("NewReader(%s) = %T, want the docreader client", engine, reader)
		}
	}
}
