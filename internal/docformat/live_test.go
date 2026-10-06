//go:build live

package docformat

import (
	"context"
	"encoding/json"
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
	type result struct {
		name              string
		total, ok, sTotal int
		sOK               int
		typeOK, failed    bool
		errs              []string
		elapsed           time.Duration
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
			task := PrepareTask(l, Segment(l))
			gold, _ := ApplyLabels(l, fx.Labels, task)
			start := time.Now()
			reply, err := LabelWithLLM(context.Background(), llm, task)
			r := result{name: filepath.Base(jf), elapsed: time.Since(start)}
			if err != nil {
				r.failed = true
				r.errs = append(r.errs, err.Error())
				results[i] = r
				return
			}
			got, _ := ApplyLabels(l, reply, task)
			r.typeOK = got.DetectedType == gold.DetectedType
			for _, u := range task.All {
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
	var elapsed time.Duration
	for _, r := range results {
		total, ok, sTotal, sOK = total+r.total, ok+r.ok, sTotal+r.sTotal, sOK+r.sOK
		elapsed += r.elapsed
		if r.typeOK {
			typeOK++
		}
		if r.failed {
			failed++
		} else if len(r.errs) == 0 {
			perfect++
		}
		if len(r.errs) > 0 {
			t.Logf("%s: %s", r.name, strings.Join(r.errs, "; "))
		}
	}
	n := len(results)
	t.Logf("model %s: %d docs, unit acc %.1f%%, structural acc %.1f%% (%d units), type %d/%d, perfect docs %d/%d, failed calls %d, avg %.1fs/doc",
		name, n, 100*float64(ok)/float64(total), 100*float64(sOK)/float64(sTotal), sTotal,
		typeOK, n, perfect, n, failed, elapsed.Seconds()/float64(n))
	if failed > n/10 {
		t.Fatalf("%d of %d model calls failed", failed, n)
	}
}
