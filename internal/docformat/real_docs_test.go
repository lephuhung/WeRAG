package docformat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Real documents downloaded from vbpl.vn (public): the checker run offline
// (heuristic segmenter, no model) over every .docx of DOCFORMAT_REAL_DIR,
// against the expectations of docformat/evaluation/real_vbpl_manifest.json.
// Fetch the files with
//
//	python3 docformat/evaluation/fetch_vbpl_samples.py
//
// then
//
//	DOCFORMAT_REAL_DIR=docformat/evaluation/samples/real_vbpl go test -run TestRealDocuments -v ./internal/docformat/

// realDocExpect is one manifest entry.
type realDocExpect struct {
	File     string `json:"file"`
	DocID    string `json:"doc_id"`
	DocNum   string `json:"doc_num"`
	FileName string `json:"file_name"`
	SHA256   string `json:"sha256"`
	Note     string `json:"note,omitempty"`

	ExpectedScope string `json:"expected_scope"`
	// ExpectedType is the detected type; "" leaves it unchecked (the type
	// read off a phụ lục's heading means little).
	ExpectedType string `json:"expected_type"`
	// MustNotFail are check ids that must not fail (status fail).
	MustNotFail []string `json:"must_not_fail,omitempty"`
	// ExpectedComponents: component key → its exact lines (Component.Texts);
	// an empty list only requires the component to be found.
	ExpectedComponents map[string][]string `json:"expected_components,omitempty"`
	// ExpectedItems: Bảng kiểm item → status (pass/warn/fail/unknown),
	// judged in ItemsScope ("" = the detected scope).
	ExpectedItems map[string]string `json:"expected_items,omitempty"`
	ItemsScope    string            `json:"items_scope,omitempty"`
}

type realManifest struct {
	Files []realDocExpect `json:"files"`
}

func TestRealDocuments(t *testing.T) {
	dir := os.Getenv("DOCFORMAT_REAL_DIR")
	if dir == "" {
		t.Skip("DOCFORMAT_REAL_DIR not set")
	}
	raw, err := os.ReadFile("../../docformat/evaluation/real_vbpl_manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var man realManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	byFile := map[string]realDocExpect{}
	for _, e := range man.Files {
		byFile[e.File] = e
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.docx"))
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatalf("no .docx in %s", dir)
	}
	ctx := context.Background()
	var total time.Duration
	for _, f := range files {
		name := filepath.Base(f)
		exp, ok := byFile[name]
		if !ok {
			t.Errorf("%s: not in the manifest", name)
			continue
		}
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		r := Check(ctx, content, Options{SourceName: name, Segmenter: SegmenterHeuristic})
		dt := time.Since(t0)
		total += dt
		if !r.OK {
			t.Errorf("%s: %s", name, r.Error)
			continue
		}
		if _, err := EvaluateWithSkills(ctx, nil, r); err != nil {
			t.Errorf("%s: evaluate: %v", name, err)
		}
		var fails []string
		for _, c := range r.Checks {
			if c.Status == StatusFail {
				fails = append(fails, c.ID)
			}
		}
		items := itemSummary(r)
		t.Logf("%-40s %4dms scope=%-9s type=%-11s fails=[%s]%s", truncateRunes(name, 40), dt.Milliseconds(),
			r.Scope.Used, r.DocumentType.Detected, strings.Join(fails, " "), items)

		if r.Scope.Used != exp.ExpectedScope {
			t.Errorf("%s: scope %s (%s), want %s", name, r.Scope.Used, r.Scope.Reason, exp.ExpectedScope)
		}
		if exp.ExpectedType != "" && r.DocumentType.Detected != exp.ExpectedType {
			t.Errorf("%s: type %s, want %s", name, r.DocumentType.Detected, exp.ExpectedType)
		}
		for _, id := range exp.MustNotFail {
			for _, c := range r.Checks {
				if c.ID == id && c.Status == StatusFail {
					t.Errorf("%s: %s fails (actual %v)", name, id, compactJSON(c.Actual))
				}
			}
		}
		for key, want := range exp.ExpectedComponents {
			c := r.Components[key]
			if c == nil || !c.Found {
				t.Errorf("%s: component %s not found", name, key)
				continue
			}
			if len(want) > 0 && strings.Join(c.Texts, "\n") != strings.Join(want, "\n") {
				t.Errorf("%s: component %s = %q, want %q", name, key, c.Texts, want)
			}
		}
		// no document is judged against a type requirement it cannot have
		if r.Judgment != nil {
			for _, it := range r.Judgment.Items {
				if it.Item == 6 && it.Status == JudgeFail {
					t.Errorf("%s: Bảng kiểm item 6 fails: %+v", name, it.Findings)
				}
			}
		}
		if len(exp.ExpectedItems) > 0 {
			ri := r
			if exp.ItemsScope != "" && exp.ItemsScope != r.Scope.Used {
				ri = Check(ctx, content, Options{SourceName: name, Segmenter: SegmenterHeuristic, Scope: exp.ItemsScope})
				if _, err := EvaluateWithSkills(ctx, nil, ri); err != nil {
					t.Errorf("%s: evaluate (%s): %v", name, exp.ItemsScope, err)
				}
			}
			got := map[string]JudgmentItem{}
			if ri.Judgment != nil {
				for _, it := range ri.Judgment.Items {
					got[strconv.Itoa(it.Item)] = it
				}
			}
			for item, want := range exp.ExpectedItems {
				if it := got[item]; it.Status != want {
					t.Errorf("%s: Bảng kiểm item %s = %q %+v, want %q", name, item, it.Status, it.Findings, want)
				}
			}
		}
	}
	t.Logf("Check (heuristic, no model): %d files in %s, %s per file", len(files),
		total.Round(time.Millisecond), (total / time.Duration(len(files))).Round(time.Millisecond))
}

// itemSummary lists the Bảng kiểm items that are not passed.
func itemSummary(r *Report) string {
	if r.Judgment == nil || len(r.Judgment.Items) == 0 {
		return ""
	}
	var parts []string
	for _, it := range r.Judgment.Items {
		if it.Status != JudgePass {
			parts = append(parts, fmt.Sprintf("%d:%s", it.Item, it.Status))
		}
	}
	return " items=[" + strings.Join(parts, " ") + "]"
}
