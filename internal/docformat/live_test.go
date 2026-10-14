//go:build live

package docformat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// Live labeling accuracy against a real model through WeKnora's own chat
// stack (provider thinking switch, JSON format). Opt-in:
//
//	DOCFORMAT_LIVE_BASE_URL=http://10.10.0.240:8000/v1 \
//	DOCFORMAT_LIVE_MODEL=Qwen/Qwen3.6-35B-A3B-FP8 \
//	go test -tags live -run Live -v ./internal/docformat/
//
// It runs three modes — the production path (confidence gate, head/tail
// chunks, widening), ForceLLM with chunks, ForceLLM on the whole document
// (the old behaviour); DOCFORMAT_LIVE_MODES=0,2 picks some.
func TestLiveLabelingAccuracy(t *testing.T) {
	base, name := os.Getenv("DOCFORMAT_LIVE_BASE_URL"), os.Getenv("DOCFORMAT_LIVE_MODEL")
	if base == "" || name == "" {
		t.Skip("DOCFORMAT_LIVE_BASE_URL / DOCFORMAT_LIVE_MODEL not set")
	}
	model, err := chat.NewChat(&chat.ChatConfig{
		Source: types.ModelSourceRemote, BaseURL: base, ModelName: name,
		ModelID: "live", Provider: os.Getenv("DOCFORMAT_LIVE_PROVIDER"),
		APIKey: os.Getenv("DOCFORMAT_LIVE_API_KEY"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	llm := ChatCompleter(model)

	files, _ := filepath.Glob("testdata/parity/synth_*.json")
	modes := []struct {
		name string
		opts Options
	}{
		{"production (gate + head/tail chunks + widening)", Options{}},
		{"ForceLLM, head/tail chunks + widening", Options{ForceLLM: true}},
		{"ForceLLM, full document (old behaviour)", Options{ForceLLM: true, FullDocument: true}},
	}
	if m := os.Getenv("DOCFORMAT_LIVE_MODES"); m != "" { // e.g. "0,2"
		var keep []int
		for _, f := range strings.Split(m, ",") {
			var i int
			if _, err := fmt.Sscan(f, &i); err == nil && i >= 0 && i < len(modes) {
				keep = append(keep, i)
			}
		}
		sel := modes[:0:0]
		for _, i := range keep {
			sel = append(sel, modes[i])
		}
		modes = sel
	}
	for _, mode := range modes {
		liveMode(t, llm, name, files, mode.name, mode.opts)
	}
}

func liveMode(t *testing.T, llm Completer, model string, files []string, modeName string, base Options) {
	type result struct {
		name              string
		total, ok, sTotal int
		sOK               int
		typeOK, failed    bool
		errs              []string
		elapsed           time.Duration
		stats             LabelStats
		method            string
	}
	results := make([]result, len(files))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, jf := range files {
		wg.Add(1)
		go func(i int, jf string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, _ := os.ReadFile(jf)
			var fx parityFixture
			_ = json.Unmarshal(raw, &fx)
			content, _ := os.ReadFile(strings.TrimSuffix(jf, ".json") + ".docx")
			l := InspectDocx(content)
			units := BuildUnits(l, nil)
			gold, _ := ApplyLabels(l, fx.Labels, PrepareFullTask(l, Segment(l), nil))
			opts := base
			opts.LLM, opts.ModelName = llm, model
			start := time.Now()
			got, info, err := segmentDoc(context.Background(), l, opts)
			r := result{name: filepath.Base(jf), elapsed: time.Since(start)}
			if err != nil || info.Error != "" {
				r.failed = true
				if err != nil {
					r.errs = append(r.errs, err.Error())
				} else {
					r.errs = append(r.errs, info.Error)
				}
			}
			if info != nil {
				r.method = info.Method
				if info.Stats != nil {
					r.stats = *info.Stats
				}
			}
			if got == nil {
				results[i] = r
				return
			}
			r.typeOK = got.DetectedType == gold.DetectedType
			for _, u := range units {
				g, p := LabelOf(gold, u), LabelOf(got, u)
				r.total++
				if g == p {
					r.ok++
				}
				if g != "noi_dung" {
					r.sTotal++
					if g == p {
						r.sOK++
					}
				}
				if g != p {
					r.errs = append(r.errs, u.ID+" "+g+"→"+p+" "+truncateRunes(u.Text, 40))
				}
			}
			results[i] = r
		}(i, jf)
	}
	wg.Wait()

	var total, ok, sTotal, sOK, typeOK, perfect, failed int
	var calls, docsCalled, widenings, promptChars, sent, all int
	var elapsed time.Duration
	for _, r := range results {
		total, ok, sTotal, sOK = total+r.total, ok+r.ok, sTotal+r.sTotal, sOK+r.sOK
		elapsed += r.elapsed
		calls += r.stats.LLMCalls
		widenings += r.stats.Widenings
		promptChars += r.stats.PromptChars
		if r.stats.LLMCalls > 0 {
			docsCalled++
			sent += r.stats.UnitsSent
			all += r.stats.UnitsTotal
		}
		if r.typeOK {
			typeOK++
		}
		if r.failed {
			failed++
		} else if len(r.errs) == 0 {
			perfect++
		}
		if len(r.errs) > 0 || r.stats.Widenings > 0 {
			t.Logf("[%s] %s (%s, gate %q, calls %d, widenings %d): %s", modeName, r.name, r.method, r.stats.Gate,
				r.stats.LLMCalls, r.stats.Widenings, strings.Join(r.errs, "; "))
		}
	}
	n := len(results)
	perCalled := func(x int) float64 {
		if docsCalled == 0 {
			return 0
		}
		return float64(x) / float64(docsCalled)
	}
	t.Logf("== %s — model %s: %d docs, unit acc %.1f%%, structural acc %.2f%% (%d/%d units), type %d/%d, perfect docs %d/%d, "+
		"failed %d, mean %.2fs/doc; docs calling the model %d, model calls %d, widenings %d, "+
		"prompt chars %.0f/doc overall (%.0f per calling doc), units sent %d of %d on calling docs",
		modeName, model, n, 100*float64(ok)/float64(total), 100*float64(sOK)/float64(sTotal), sOK, sTotal,
		typeOK, n, perfect, n, failed, elapsed.Seconds()/float64(n), docsCalled, calls, widenings,
		float64(promptChars)/float64(n), perCalled(promptChars), sent, all)
	if failed > n/10 {
		t.Fatalf("%d of %d model calls failed", failed, n)
	}
}
