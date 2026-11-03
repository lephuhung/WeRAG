package memory

// Offline comparison of the write gate's sentence rules with a small model
// classifying the same user message. Not part of the normal test run:
//
//	SSRF_WHITELIST=10.10.0.0/24 WEKNORA_CLASSIFY_EVAL=<labels.jsonl> [WEKNORA_CLASSIFY_MODELS=gemma,qwen] \
//	  go test -run TestClassifyGateEval -count=1 -v .
//
// labels.jsonl lines: {"id","text","label"} with label one of classifyLabels.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
)

var classifyLabels = func() []string {
	out := make([]string, 0, len(sentenceClasses))
	for _, c := range sentenceClasses {
		out = append(out, string(c))
	}
	return out
}()

type classifyModel struct{ name, baseURL, model string }

func classifyModels() []classifyModel {
	all := map[string]classifyModel{
		"gemma": {name: "gemma", baseURL: "http://10.10.0.240:8001/v1", model: "gemma-4-e4b-it"},
		"qwen":  {name: "qwen", baseURL: "http://10.10.0.240:8000/v1", model: "Qwen/Qwen3.6-35B-A3B-FP8"},
	}
	want := strings.Split(firstNonEmpty(os.Getenv("WEKNORA_CLASSIFY_MODELS"), "gemma,qwen"), ",")
	var out []classifyModel
	for _, w := range want {
		if m, ok := all[strings.TrimSpace(w)]; ok {
			out = append(out, m)
		}
	}
	return out
}

// callClassifier goes through the production prompt and parser.
func callClassifier(ctx context.Context, m classifyModel, text string) (string, time.Duration, error) {
	model, err := chat.NewChat(&chat.ChatConfig{
		Source: types.ModelSourceRemote, ModelName: m.model, BaseURL: m.baseURL, APIKey: "none",
		Provider: string(provider.ProviderGeneric),
	}, nil)
	if err != nil {
		return "", 0, err
	}
	start := time.Now()
	c, err := classifySentence(ctx, model, text)
	return string(c), time.Since(start), err
}

type classifyItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Label string `json:"label"`
}

type classifyScore struct {
	name      string
	pred      map[string]string
	latencies []time.Duration
}

func TestClassifyGateEval(t *testing.T) {
	path := os.Getenv("WEKNORA_CLASSIFY_EVAL")
	if path == "" {
		t.Skip("set WEKNORA_CLASSIFY_EVAL")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var items []classifyItem
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var it classifyItem
		if err := json.Unmarshal(sc.Bytes(), &it); err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}
	f.Close()

	scores := []*classifyScore{{name: "regex", pred: map[string]string{}}}
	for _, it := range items {
		scores[0].pred[it.ID] = string(ruleClass(it.Text))
	}
	ctx := context.Background()
	for _, m := range classifyModels() {
		s := &classifyScore{name: m.name, pred: map[string]string{}}
		var mu sync.Mutex
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for _, it := range items {
			wg.Add(1)
			sem <- struct{}{}
			go func(it classifyItem) {
				defer wg.Done()
				defer func() { <-sem }()
				label, took, err := callClassifier(ctx, m, it.Text)
				if err != nil {
					t.Logf("%s %s: %v", m.name, it.ID, err)
					label = "error"
				}
				mu.Lock()
				s.pred[it.ID] = label
				s.latencies = append(s.latencies, took)
				mu.Unlock()
			}(it)
		}
		wg.Wait()
		scores = append(scores, s)
	}

	for _, s := range scores {
		reportClassify(t, s, items)
	}
	if out := os.Getenv("WEKNORA_CLASSIFY_OUT"); out != "" {
		w, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			row := map[string]string{"id": it.ID, "text": it.Text, "gold": it.Label}
			for _, s := range scores {
				row[s.name] = s.pred[it.ID]
			}
			b, _ := json.Marshal(row)
			w.Write(append(b, '\n'))
		}
		w.Close()
	}
}

func reportClassify(t *testing.T, s *classifyScore, items []classifyItem) {
	correct := 0
	// The decision that matters: keep (self) or not.
	var tp, fp, fn int
	tpC, fpC, fnC := map[string]int{}, map[string]int{}, map[string]int{}
	for _, it := range items {
		p := s.pred[it.ID]
		if p == it.Label {
			correct++
			tpC[p]++
		} else {
			fpC[p]++
			fnC[it.Label]++
		}
		switch {
		case p == "self" && it.Label == "self":
			tp++
		case p == "self":
			fp++
		case it.Label == "self":
			fn++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "== %s: exact %d/%d (%.1f%%)\n", s.name, correct, len(items), 100*float64(correct)/float64(len(items)))
	fmt.Fprintf(&b, "   keep-decision (self): precision %.2f recall %.2f  (wrongly kept %d, wrongly dropped %d)\n",
		ratio(tp, tp+fp), ratio(tp, tp+fn), fp, fn)
	for _, l := range classifyLabels {
		fmt.Fprintf(&b, "   %-9s P %.2f R %.2f (gold %d)\n", l, ratio(tpC[l], tpC[l]+fpC[l]), ratio(tpC[l], tpC[l]+fnC[l]), tpC[l]+fnC[l])
	}
	if len(s.latencies) > 0 {
		sort.Slice(s.latencies, func(i, j int) bool { return s.latencies[i] < s.latencies[j] })
		fmt.Fprintf(&b, "   latency p50 %v p95 %v (4 parallel)\n",
			s.latencies[len(s.latencies)/2].Round(time.Millisecond), s.latencies[len(s.latencies)*95/100].Round(time.Millisecond))
	}
	t.Log("\n" + b.String())
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
