package docformat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

type timedCompleter struct {
	llm  Completer
	rows *[]map[string]any
}

func (c timedCompleter) Complete(ctx context.Context, msgs []Message) (string, error) {
	start := time.Now()
	text, err := c.llm.Complete(ctx, msgs)
	in := 0
	for _, m := range msgs {
		in += len([]rune(m.Content))
	}
	*c.rows = append(*c.rows, map[string]any{"sec": time.Since(start).Seconds(), "in_chars": in, "out_chars": len([]rune(text))})
	return text, err
}

// TestLabelTiming times the model labelling of the parity documents the
// heuristics are not sure of, partial against full-document sends: per
// call seconds and characters in/out, written as JSON to
// DOCFORMAT_LIVE_OUT. Live only (DOCFORMAT_LIVE_BASE_URL, _MODEL,
// _PROVIDER, _API_KEY as in live_test.go).
func TestLabelTiming(t *testing.T) {
	base, name := os.Getenv("DOCFORMAT_LIVE_BASE_URL"), os.Getenv("DOCFORMAT_LIVE_MODEL")
	if base == "" || os.Getenv("DOCFORMAT_LIVE_OUT") == "" {
		t.Skip("set DOCFORMAT_LIVE_BASE_URL, DOCFORMAT_LIVE_MODEL and DOCFORMAT_LIVE_OUT")
	}
	model, err := chat.NewChat(&chat.ChatConfig{Source: types.ModelSourceRemote, BaseURL: base, ModelName: name, ModelID: "live", Provider: os.Getenv("DOCFORMAT_LIVE_PROVIDER"), APIKey: os.Getenv("DOCFORMAT_LIVE_API_KEY")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("testdata/parity/synth_*.json")
	limit := 12
	var out []map[string]any
	for _, jf := range files {
		content, _ := os.ReadFile(strings.TrimSuffix(jf, ".json") + ".docx")
		l := InspectDocx(content)
		heur := Segment(l)
		if ok, _ := Confident(heur); ok {
			continue
		}
		if limit == 0 {
			break
		}
		limit--
		for _, full := range []bool{false, true} {
			var rows []map[string]any
			opts := Options{LLM: timedCompleter{ChatCompleter(model), &rows}, ModelName: name, ForceLLM: true, FullDocument: full}
			start := time.Now()
			_, info, _ := segmentDoc(context.Background(), l, opts)
			out = append(out, map[string]any{"doc": filepath.Base(jf), "full": full, "total_sec": time.Since(start).Seconds(),
				"units_sent": info.Stats.UnitsSent, "units_total": info.Stats.UnitsTotal, "calls": rows})
			fmt.Printf("%s full=%v %.1fs sent %d/%d calls %v\n", filepath.Base(jf), full, time.Since(start).Seconds(), info.Stats.UnitsSent, info.Stats.UnitsTotal, rows)
		}
	}
	raw, _ := json.MarshalIndent(out, "", " ")
	_ = os.WriteFile(os.Getenv("DOCFORMAT_LIVE_OUT"), raw, 0o644)
}
