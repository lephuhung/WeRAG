package docformat

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// The documents of the judgment harness (live_eval_test.go) and the
// offline judgment tests (judgment_test.go): docformat/evaluation/
// judgment_docs.py, generated into a temp dir — correct where the rules
// measure, so only judgment differs — plus the ký-thay parity fixtures.
// Errors are the Bảng kiểm items of the-thuc-chung that must read "Lỗi";
// a document with none is correct and any "Lỗi" on it is a false alarm.

type liveDoc struct {
	Name   string
	Report *Report
	Errors []int // expected Lỗi items
	Any    []int // unscored items
	Note   string
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func loadLiveDocs(t *testing.T) []liveDoc {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join("..", "..", "docformat", "evaluation", "judgment_docs.py")
	if out, err := exec.Command("python3", script, dir).CombinedOutput(); err != nil {
		t.Fatalf("judgment_docs.py: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]struct {
		Errors []int  `json:"errors"`
		Any    []int  `json:"any"`
		Note   string `json:"note"`
	}
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	var docs []liveDoc
	check := func(name string, content []byte) *Report {
		r := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic, SourceName: name + ".docx"})
		if !r.OK {
			t.Fatalf("%s: %s", name, r.Error)
		}
		return r
	}
	for name, e := range expected {
		content, err := os.ReadFile(filepath.Join(dir, name+".docx"))
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, liveDoc{Name: name, Report: check(name, content), Errors: e.Errors, Any: e.Any, Note: e.Note})
	}
	// ký-thay parity fixtures (docformat/tests/ky_thay_docs.py)
	for _, fx := range []struct {
		name        string
		errors, any []int
		note        string
	}{
		{"fx_ky_thay_ok", nil, nil, "KT. + Đ/c Trưởng phòng (để b/c)"},
		{"fx_ky_thay_missing_kt", []int{2, 3, 4}, []int{6}, "PHÓ không KT., không báo cáo, Lưu thiếu VT"},
		{"fx_ky_thay_mismatch", []int{2, 4}, []int{3, 6}, "KT. GIÁM ĐỐC / PHÓ TRƯỞNG PHÒNG, không có dòng Lưu"},
	} {
		content, err := os.ReadFile("testdata/parity/" + fx.name + ".docx")
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, liveDoc{Name: fx.name, Report: check(fx.name, content), Errors: fx.errors, Any: fx.any, Note: fx.note})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })
	return docs
}
