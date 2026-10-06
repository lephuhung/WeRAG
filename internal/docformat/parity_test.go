package docformat

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Parity with the Python reference implementation (docformat/): for every
// document in testdata/parity — a synthetic corpus covering 9 document types
// × 5 header layouts × 5 signature layouts, plus the Python unit-test
// fixtures — the Go port must produce the same labeling units, components
// and check verdicts. Regenerate with:
//
//	python3 docformat/evaluation/export_parity.py internal/docformat/testdata/parity

type parityFixture struct {
	Units     []map[string]any `json:"units"`
	Heuristic parityReport     `json:"heuristic"`
	Labels    *Reply           `json:"labels"`
	Labeled   *parityReport    `json:"labeled"`
}

type parityReport struct {
	Detected   string         `json:"detected"`
	Used       string         `json:"used"`
	RuleSet    string         `json:"rule_set"`
	Components map[string]any `json:"components"`
	Checks     []any          `json:"checks"`
	Summary    map[string]any `json:"summary"`
}

func toGeneric(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// diff collects differences between two decoded JSON values.
func diff(path string, want, got any, out *[]string) {
	if len(*out) >= 12 {
		return
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: want object, got %v", path, got))
			return
		}
		keys := map[string]bool{}
		for k := range w {
			keys[k] = true
		}
		for k := range g {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			diff(path+"."+k, w[k], g[k], out)
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			*out = append(*out, fmt.Sprintf("%s: want %v\n      got %v", path, short(want), short(got)))
			return
		}
		for i := range w {
			diff(fmt.Sprintf("%s[%d]", path, i), w[i], g[i], out)
		}
	case float64:
		g, ok := got.(float64)
		if !ok || math.Abs(w-g) > 1e-9 {
			*out = append(*out, fmt.Sprintf("%s: want %v, got %v", path, want, got))
		}
	default:
		if want != got {
			*out = append(*out, fmt.Sprintf("%s: want %v, got %v", path, short(want), short(got)))
		}
	}
}

func short(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func viewOf(t *testing.T, r *Report) map[string]any {
	t.Helper()
	if !r.OK {
		t.Fatalf("check failed: %s", r.Error)
	}
	comps := map[string]any{}
	for k, c := range r.Components {
		comps[k] = map[string]any{"found": c.Found, "paras": c.Paras, "text": c.Text, "zone": c.Zone}
	}
	return toGeneric(t, map[string]any{
		"detected": r.DocumentType.Detected, "used": r.DocumentType.Used,
		"rule_set": r.DocumentType.RuleSet, "components": comps,
		"checks": r.Checks, "summary": r.Summary,
	}).(map[string]any)
}

func TestParityWithPythonReference(t *testing.T) {
	files, _ := filepath.Glob("testdata/parity/*.json")
	if len(files) == 0 {
		t.Skip("no parity fixtures")
	}
	ctx := context.Background()
	for _, jf := range files {
		name := strings.TrimSuffix(filepath.Base(jf), ".json")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(jf)
			if err != nil {
				t.Fatal(err)
			}
			var fx parityFixture
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(strings.TrimSuffix(jf, ".json") + ".docx")
			if err != nil {
				t.Fatal(err)
			}
			if len(fx.Units) == 0 || len(fx.Heuristic.Checks) == 0 {
				t.Fatal("fixture has no units/checks — regenerate it")
			}
			var diffs []string

			l := InspectDocx(content)
			units := BuildUnits(l, Segment(l))
			diff("units", toGeneric(t, fx.Units), toGeneric(t, units), &diffs)

			heur := Check(ctx, content, Options{Segmenter: SegmenterHeuristic})
			diff("heuristic", toGeneric(t, fx.Heuristic), viewOf(t, heur), &diffs)

			if fx.Labels != nil && fx.Labeled != nil {
				lab := Check(ctx, content, Options{Labels: fx.Labels})
				diff("labeled", toGeneric(t, fx.Labeled), viewOf(t, lab), &diffs)
			}
			if len(diffs) > 0 {
				t.Errorf("%d+ differences from the Python reference:\n  %s", len(diffs), strings.Join(diffs, "\n  "))
			}
		})
	}
}
