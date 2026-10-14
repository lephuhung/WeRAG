package docformat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type goldFixture struct {
	name    string
	layout  *Layout
	heur    *Segmentation
	gold    *Segmentation
	content []byte
}

// goldFixtures are the parity fixtures with gold labels (the synthetic
// corpus).
func goldFixtures(t *testing.T) []goldFixture {
	t.Helper()
	files, _ := filepath.Glob("testdata/parity/*.json")
	var out []goldFixture
	for _, jf := range files {
		raw, err := os.ReadFile(jf)
		if err != nil {
			t.Fatal(err)
		}
		var fx parityFixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatal(err)
		}
		if fx.Labels == nil {
			continue
		}
		content, err := os.ReadFile(strings.TrimSuffix(jf, ".json") + ".docx")
		if err != nil {
			t.Fatal(err)
		}
		l := InspectDocx(content)
		heur := Segment(l)
		gold, _ := ApplyLabels(l, fx.Labels, PrepareFullTask(l, heur, nil))
		out = append(out, goldFixture{strings.TrimSuffix(filepath.Base(jf), ".json"), l, heur, gold, content})
	}
	if len(out) == 0 {
		t.Skip("no parity fixtures with gold labels")
	}
	return out
}

// structuralErrors: units whose label differs from gold where either side
// is not nội dung (run_eval.py's "văn bản đúng hoàn toàn").
func structuralErrors(units []Unit, gold, got *Segmentation) []string {
	var errs []string
	for _, u := range units {
		g, p := LabelOf(gold, u), LabelOf(got, u)
		if g != p && (g != "noi_dung" || p != "noi_dung") {
			errs = append(errs, u.ID+" "+g+"→"+p)
		}
	}
	return errs
}

// The confidence gate must never pass a document the heuristic got wrong:
// such a document would skip the model and keep its mislabels.
func TestConfidenceGateNeverPassesAMislabel(t *testing.T) {
	fxs := goldFixtures(t)
	confident, wrong := 0, 0
	reasons := map[string]int{}
	var cautious []string
	for _, fx := range fxs {
		ok, why := Confident(fx.heur)
		errs := structuralErrors(BuildUnits(fx.layout, nil), fx.gold, fx.heur)
		if fx.heur.DetectedType != fx.gold.DetectedType {
			errs = append(errs, "type "+fx.gold.DetectedType+"→"+fx.heur.DetectedType)
		}
		if len(errs) > 0 {
			wrong++
		}
		if !ok {
			reasons[strings.SplitN(why, ":", 2)[0]]++
			if len(errs) == 0 {
				cautious = append(cautious, fx.name+" ("+why+")")
			}
			continue
		}
		confident++
		if len(errs) > 0 {
			t.Errorf("%s passes the gate with heuristic errors: %s", fx.name, strings.Join(errs, "; "))
		}
	}
	t.Logf("gate: %d of %d documents confident (no model call), %d sent to the model; heuristic wrong on %d; reasons %v",
		confident, len(fxs), len(fxs)-confident, wrong, reasons)
	t.Logf("sent to the model although the heuristic was right: %d\n  %s", len(cautious), strings.Join(cautious, "\n  "))
}

// The chunked task never elides a unit whose gold label is a component
// other than nội dung (phụ lục past its first line keeps its label), so the
// model sees every component of the corpus.
func TestChunkedTaskKeepsEveryComponent(t *testing.T) {
	sentTotal, allTotal := 0, 0
	for _, fx := range goldFixtures(t) {
		task := PrepareTask(fx.layout, fx.heur)
		if !task.Chunked {
			t.Errorf("%s: no head/tail bounds (%+v)", fx.name, fx.heur.Bounds)
			continue
		}
		sentTotal += len(task.Units)
		allTotal += len(task.All)
		for _, u := range task.All {
			if !task.Elided[u.ID] {
				continue
			}
			g := LabelOf(fx.gold, u)
			want := "noi_dung"
			if u.Hint == "phu_luc" {
				want = "phu_luc"
			}
			if g != want {
				t.Errorf("%s: unit %s (%s, gold %s) elided as %s — bounds %+v", fx.name, u.ID, truncateRunes(u.Text, 40), g, want, fx.heur.Bounds)
			}
		}
	}
	t.Logf("chunked tasks send %d of %d units (%.0f%%)", sentTotal, allTotal, 100*float64(sentTotal)/float64(allTotal))
}

// goldLLM answers with the gold labels of the units it is sent (or with
// every unit as nội dung), counting the units per call.
type goldLLM struct {
	gold  map[string]string
	blank bool
	sent  []int
}

func (f *goldLLM) Complete(_ context.Context, msgs []Message) (string, error) {
	user := msgs[len(msgs)-1].Content
	labels := map[string]string{}
	n := 0
	for _, line := range strings.Split(user, "\n") {
		if !strings.HasPrefix(line, `{"id":`) {
			continue
		}
		var u Unit
		if json.Unmarshal([]byte(line), &u) != nil {
			continue
		}
		n++
		labels[u.ID] = "noi_dung"
		if !f.blank {
			labels[u.ID] = f.gold[u.ID]
		}
	}
	f.sent = append(f.sent, n)
	b, _ := json.Marshal(map[string]any{"document_type": "unknown", "labels": labels})
	return string(b), nil
}

// With gold answers for the units sent, the chunked production path gets
// every document right; a model that finds nothing makes the loop widen at
// most maxWidenings times.
func TestChunkedLabelingWithGoldAnswers(t *testing.T) {
	ctx := context.Background()
	calls := 0
	for _, fx := range goldFixtures(t) {
		raw, _ := os.ReadFile("testdata/parity/" + fx.name + ".json")
		var pf parityFixture
		_ = json.Unmarshal(raw, &pf)
		llm := &goldLLM{gold: pf.Labels.Labels}
		r := Check(ctx, fx.content, Options{LLM: llm})
		if r.Segmentation.Method == MethodHeuristicConfident {
			if len(llm.sent) != 0 {
				t.Errorf("%s: confident but the model was called", fx.name)
			}
			continue
		}
		calls += len(llm.sent)
		task := PrepareTask(fx.layout, fx.heur)
		seg, _, _, err := LabelChunked(ctx, llm, fx.layout, task, &LabelStats{})
		if err != nil {
			t.Fatal(err)
		}
		if errs := structuralErrors(task.All, fx.gold, seg); len(errs) > 0 {
			t.Errorf("%s: chunked labeling with gold answers is wrong: %s", fx.name, strings.Join(errs, "; "))
		}
		if r.Segmentation.Stats == nil || r.Segmentation.Stats.Widenings != 0 {
			t.Errorf("%s: gold answers need no widening: %+v", fx.name, r.Segmentation.Stats)
		}

		blank := &goldLLM{blank: true}
		stats := &LabelStats{}
		if _, _, _, err := LabelChunked(ctx, blank, fx.layout, PrepareTask(fx.layout, fx.heur), stats); err != nil {
			t.Fatal(err)
		}
		if stats.Widenings > maxWidenings || stats.LLMCalls != stats.Widenings+1 {
			t.Errorf("%s: %+v", fx.name, stats)
		}
		for i := 1; i < len(blank.sent); i++ {
			if blank.sent[i] <= blank.sent[i-1] {
				t.Errorf("%s: widening did not grow the task: %v", fx.name, blank.sent)
			}
		}
	}
	t.Logf("model calls on the documents the gate sent: %d", calls)
}

func TestForceLLMBypassesTheGate(t *testing.T) {
	for _, fx := range goldFixtures(t) {
		if ok, _ := Confident(fx.heur); !ok {
			continue
		}
		llm := &goldLLM{blank: true}
		r := Check(context.Background(), fx.content, Options{LLM: llm, ForceLLM: true, FullDocument: true})
		if r.Segmentation.Method != SegmenterLLM || len(llm.sent) != 1 || llm.sent[0] != len(PrepareFullTask(fx.layout, fx.heur, nil).Units) {
			t.Errorf("%s: ForceLLM+FullDocument: method %s, sent %v", fx.name, r.Segmentation.Method, llm.sent)
		}
		return
	}
	t.Skip("no confident fixture")
}

// Without a tail (no Nơi nhận, no signature) the task falls back to the
// whole document (first and last 45 units).
func TestPrepareTaskFallsBackWithoutBounds(t *testing.T) {
	content, err := os.ReadFile("testdata/parity/fx_stray_chars.docx")
	if err != nil {
		t.Fatal(err)
	}
	l := InspectDocx(content)
	heur := Segment(l)
	task := PrepareTask(l, heur)
	if heur.Bounds.TailOK || task.Chunked || len(task.Units) != len(task.All) || task.Widen([]string{"signature"}) != nil {
		t.Fatalf("bounds %+v, chunked %v, %d/%d units", heur.Bounds, task.Chunked, len(task.Units), len(task.All))
	}
}
